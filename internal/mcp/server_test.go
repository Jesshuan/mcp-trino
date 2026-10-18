package mcp

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/tuannvm/mcp-trino/internal/config"
)

// newTestStreamableServer builds a real StreamableHTTPServer around an empty
// MCP server (no Trino connection needed).
func newTestStreamableServer() *mcpserver.StreamableHTTPServer {
	return mcpserver.NewStreamableHTTPServer(mcpserver.NewMCPServer("test-server", "test-version"))
}

// validInitializeBody is a minimal MCP initialize request that the streamable
// server accepts once past the auth gate.
const validInitializeBody = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}`

func TestListenAddr(t *testing.T) {
	original, hadEnv := os.LookupEnv("MCP_HOST")
	defer func() {
		if hadEnv {
			os.Setenv("MCP_HOST", original)
		} else {
			os.Unsetenv("MCP_HOST")
		}
	}()

	tests := []struct {
		name     string
		host     string // env value; "unset" means remove the variable
		port     string
		expected string
	}{
		{"default binds loopback only", "unset", "8080", "127.0.0.1:8080"},
		{"explicit all interfaces", "0.0.0.0", "8080", "0.0.0.0:8080"},
		{"explicit custom host", "trino.internal", "9000", "trino.internal:9000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.host == "unset" {
				os.Unsetenv("MCP_HOST")
			} else {
				os.Setenv("MCP_HOST", tt.host)
			}
			if got := listenAddr(tt.port); got != tt.expected {
				t.Errorf("listenAddr(%q) = %q, want %q", tt.port, got, tt.expected)
			}
		})
	}
}

func TestValidateHTTPAuth(t *testing.T) {
	original, hadEnv := os.LookupEnv("MCP_ALLOW_UNAUTHENTICATED_HTTP")
	defer func() {
		if hadEnv {
			os.Setenv("MCP_ALLOW_UNAUTHENTICATED_HTTP", original)
		} else {
			os.Unsetenv("MCP_ALLOW_UNAUTHENTICATED_HTTP")
		}
	}()

	tests := []struct {
		name    string
		cfg     *config.TrinoConfig
		optIn   string // env value; "unset" means remove the variable
		wantErr bool
	}{
		{"no auth, no opt-in is rejected", &config.TrinoConfig{}, "unset", true},
		{"no auth, explicit opt-in allowed", &config.TrinoConfig{}, "true", false},
		{"no auth, invalid opt-in value rejected", &config.TrinoConfig{}, "yes", true},
		{"OAuth counts as authentication", &config.TrinoConfig{OAuthEnabled: true}, "unset", false},
		{"passthrough counts as authentication", &config.TrinoConfig{AuthMode: "passthrough"}, "unset", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.optIn == "unset" {
				os.Unsetenv("MCP_ALLOW_UNAUTHENTICATED_HTTP")
			} else {
				os.Setenv("MCP_ALLOW_UNAUTHENTICATED_HTTP", tt.optIn)
			}

			s := &Server{config: tt.cfg}
			err := s.validateHTTPAuth()
			if tt.wantErr && err == nil {
				t.Errorf("validateHTTPAuth() expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("validateHTTPAuth() unexpected error: %v", err)
			}
		})
	}
}

func TestMCPHandlerNoCORSHeaders(t *testing.T) {
	// The MCP server serves MCP clients (Claude Code, agents, IDEs), not
	// browsers. It must never emit Access-Control-Allow-Origin: wildcard
	// CORS would let any website read responses from a victim's browser.
	s := &Server{config: &config.TrinoConfig{}}
	handler := s.createMCPHandler(newTestStreamableServer())

	requests := []*http.Request{
		httptest.NewRequest("OPTIONS", "/mcp", nil),
		httptest.NewRequest("POST", "/mcp", strings.NewReader(validInitializeBody)),
	}

	for _, req := range requests {
		rec := httptest.NewRecorder()
		handler(rec, req)
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("%s /mcp: Access-Control-Allow-Origin = %q, want absent", req.Method, got)
		}
		if got := rec.Header().Get("Access-Control-Allow-Methods"); got != "" {
			t.Errorf("%s /mcp: Access-Control-Allow-Methods = %q, want absent", req.Method, got)
		}
	}
}

func TestMCPHandlerPassthroughRequiresCredentials(t *testing.T) {
	s := &Server{config: &config.TrinoConfig{AuthMode: "passthrough"}}
	handler := s.createMCPHandler(newTestStreamableServer())

	t.Run("missing credentials returns 401 with Basic and Bearer challenges", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler(rec, httptest.NewRequest("POST", "/mcp", strings.NewReader(validInitializeBody)))

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
		}
		challenges := rec.Header().Values("WWW-Authenticate")
		if len(challenges) != 2 {
			t.Errorf("WWW-Authenticate = %v, want both Basic and Bearer challenges", challenges)
		}
		if body := rec.Body.String(); !strings.Contains(body, "invalid_credentials") {
			t.Errorf("body = %q, want error code invalid_credentials", body)
		}
	})

	t.Run("empty bearer token returns 401", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/mcp", strings.NewReader(validInitializeBody))
		req.Header.Set("Authorization", "Bearer ")

		rec := httptest.NewRecorder()
		handler(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want %d (an empty bearer token authenticates nobody)", rec.Code, http.StatusUnauthorized)
		}
	})

	t.Run("bearer token passes the auth gate", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/mcp", strings.NewReader(validInitializeBody))
		req.Header.Set("Authorization", "Bearer some-token")

		rec := httptest.NewRecorder()
		handler(rec, req)

		if rec.Code == http.StatusUnauthorized {
			t.Error("status = 401, want the bearer token to reach the MCP server (Trino validates it)")
		}
		if got := rec.Header().Get("WWW-Authenticate"); got != "" {
			t.Errorf("WWW-Authenticate = %q, want absent after successful auth", got)
		}
	})

	t.Run("valid Basic credentials pass the auth gate", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/mcp", strings.NewReader(validInitializeBody))
		req.SetBasicAuth("alice", "password")

		rec := httptest.NewRecorder()
		handler(rec, req)

		if rec.Code == http.StatusUnauthorized {
			t.Error("status = 401, want the request to reach the MCP server")
		}
		if got := rec.Header().Get("WWW-Authenticate"); got != "" {
			t.Errorf("WWW-Authenticate = %q, want absent after successful auth", got)
		}
	})
}
