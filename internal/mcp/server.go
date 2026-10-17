package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	mcpserver "github.com/mark3labs/mcp-go/server"
	oauth "github.com/tuannvm/oauth-mcp-proxy"
	"github.com/tuannvm/mcp-trino/internal/config"
	"github.com/tuannvm/mcp-trino/internal/trino"
)

// Server represents the MCP server with all components
type Server struct {
	mcpServer   *mcpserver.MCPServer
	config      *config.TrinoConfig
	version     string
	oauthServer *oauth.Server // oauth-mcp-proxy Server (nil if OAuth disabled)
}

// NewServer creates a new MCP server instance with all components
func NewServer(trinoClient *trino.Client, trinoConfig *config.TrinoConfig, version string) *Server {
	mcpServer, oauthServer := createMCPServer(trinoClient, trinoConfig, version)

	return &Server{
		mcpServer:   mcpServer,
		config:      trinoConfig,
		version:     version,
		oauthServer: oauthServer,
	}
}

func createMCPServer(trinoClient *trino.Client, trinoConfig *config.TrinoConfig, version string) (*mcpserver.MCPServer, *oauth.Server) {
	options := []mcpserver.ServerOption{mcpserver.WithToolCapabilities(true)}

	var oauthServer *oauth.Server
	if trinoConfig.OAuthEnabled {
		oauthCfg := trinoConfigToOAuthConfig(trinoConfig)
		var err error
		oauthServer, err = oauth.NewServer(oauthCfg)
		if err != nil {
			log.Printf("ERROR: Failed to create OAuth server: %v", err)
		} else {
			options = append(options, mcpserver.WithToolHandlerMiddleware(oauthServer.Middleware()))
			log.Printf("INFO: OAuth enabled with provider: %s, mode: %s", trinoConfig.OAuthProvider, trinoConfig.OAuthMode)
		}
	}

	mcpServer := mcpserver.NewMCPServer("Trino MCP Server", version, options...)

	trinoHandlers := NewTrinoHandlers(trinoClient, trinoConfig)
	RegisterTrinoTools(mcpServer, trinoHandlers)

	return mcpServer, oauthServer
}

// ServeStdio starts the MCP server with STDIO transport
func (s *Server) ServeStdio() error {
	return mcpserver.ServeStdio(s.mcpServer)
}

// listenAddr returns the address the HTTP server binds to. MCP_HOST is the
// actual listen address (default: loopback only). Containers must set
// MCP_HOST=0.0.0.0 explicitly to be reachable through the container
// network interface.
func listenAddr(port string) string {
	host := getEnv("MCP_HOST", "127.0.0.1")
	return fmt.Sprintf("%s:%s", host, port)
}

// validateHTTPAuth enforces that a network-facing HTTP server runs with
// authentication (OAuth or credential passthrough) unless the operator
// explicitly opts in to running unauthenticated. Without this, a default
// HTTP deployment exposes the full query surface to anyone who can reach
// the port.
func (s *Server) validateHTTPAuth() error {
	if s.config.OAuthEnabled || s.config.AuthMode == "passthrough" {
		return nil
	}
	optIn, _ := parseBoolEnv("MCP_ALLOW_UNAUTHENTICATED_HTTP")
	if !optIn {
		return fmt.Errorf("refusing to start HTTP transport without authentication: " +
			"enable OAuth (OAUTH_ENABLED=true), use credential passthrough " +
			"(TRINO_AUTH_MODE=passthrough), or explicitly acknowledge the risk with " +
			"MCP_ALLOW_UNAUTHENTICATED_HTTP=true (development only)")
	}
	log.Println("WARNING: HTTP transport is running UNAUTHENTICATED " +
		"(MCP_ALLOW_UNAUTHENTICATED_HTTP=true). Anyone who can reach this endpoint " +
		"can execute queries with the configured Trino credentials.")
	return nil
}

// ServeHTTP starts the MCP server with HTTP transport
func (s *Server) ServeHTTP(port string) error {
	addr := listenAddr(port)

	if err := s.validateHTTPAuth(); err != nil {
		return err
	}

	log.Println("Setting up StreamableHTTP server...")

	var streamableServer *mcpserver.StreamableHTTPServer
	if s.config.OAuthEnabled {
		streamableServer = mcpserver.NewStreamableHTTPServer(
			s.mcpServer,
			mcpserver.WithEndpointPath("/mcp"),
			mcpserver.WithHTTPContextFunc(oauth.CreateHTTPContextFunc()),
			mcpserver.WithStateLess(false),
		)
	} else {
		streamableServer = mcpserver.NewStreamableHTTPServer(
			s.mcpServer,
			mcpserver.WithEndpointPath("/mcp"),
			mcpserver.WithStateLess(false),
		)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/status", s.handleStatus)

	if s.config.OAuthEnabled && s.oauthServer != nil {
		s.oauthServer.RegisterHandlers(mux)
		log.Printf("INFO: OAuth enabled - mode: %s, provider: %s", s.config.OAuthMode, s.config.OAuthProvider)
	}

	mcpHandler := s.createMCPHandler(streamableServer)
	mux.HandleFunc("/mcp", mcpHandler)
	mux.HandleFunc("/sse", mcpHandler)

	httpServer := &http.Server{Addr: addr, Handler: mux}

	done := make(chan bool, 1)
	go s.handleSignals(done)

	go func() {
		certFile := getEnv("HTTPS_CERT_FILE", "")
		keyFile := getEnv("HTTPS_KEY_FILE", "")

		// User passwords transit the server in passthrough mode: warn loudly
		// when they would travel in cleartext (no local TLS, no mention of a
		// TLS-terminating proxy in front).
		if s.config.AuthMode == "passthrough" && certFile == "" && keyFile == "" {
			log.Println("WARNING: Passthrough mode over plain HTTP: user passwords are transmitted in cleartext. " +
				"Configure HTTPS_CERT_FILE/HTTPS_KEY_FILE or place a TLS-terminating proxy in front of this server.")
		}

		mcpHost := getEnv("MCP_HOST", "localhost")
		mcpPort := getEnv("MCP_PORT", "8080")
		scheme := s.getScheme()
		mcpURL := getEnv("MCP_URL", fmt.Sprintf("%s://%s:%s", scheme, mcpHost, mcpPort))

		if certFile != "" && keyFile != "" {
			oauthStatus := s.getOAuthStatus()

			log.Printf("Starting HTTPS server on %s%s", addr, oauthStatus)
			log.Printf("  - Modern endpoint: %s/mcp", mcpURL)
			log.Printf("  - Legacy endpoint: %s/sse (backward compatibility)", mcpURL)
			log.Printf("  - OAuth metadata: %s/.well-known/oauth-authorization-server", mcpURL)
			log.Printf("  - OAuth metadata (legacy): %s/.well-known/oauth-metadata", mcpURL)
			if s.config.OAuthEnabled {
				log.Printf("  - OAuth callback: %s/oauth/callback", mcpURL)
				log.Printf("  - OAuth callback (Claude Code): %s/callback (redirects to /oauth/callback)", mcpURL)
			}

			if err := httpServer.ListenAndServeTLS(certFile, keyFile); err != nil && err != http.ErrServerClosed {
				log.Fatalf("HTTPS server error: %v", err)
			}
		} else {
			oauthStatus := s.getOAuthStatusWithWarning()

			log.Printf("Starting HTTP server on %s%s", addr, oauthStatus)
			log.Printf("  - Modern endpoint: %s/mcp", mcpURL)
			log.Printf("  - Legacy endpoint: %s/sse (backward compatibility)", mcpURL)
			log.Printf("  - OAuth metadata: %s/.well-known/oauth-authorization-server", mcpURL)
			log.Printf("  - OAuth metadata (legacy): %s/.well-known/oauth-metadata", mcpURL)
			if s.config.OAuthEnabled {
				log.Printf("  - OAuth callback: %s/oauth/callback", mcpURL)
				log.Printf("  - OAuth callback (Claude Code): %s/callback (redirects to /oauth/callback)", mcpURL)
			}

			if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Fatalf("HTTP server error: %v", err)
			}
		}
	}()

	<-done
	log.Println("Shutting down HTTP server...")

	// Allow 30 seconds for graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	log.Println("Waiting for active connections to finish (max 30 seconds)...")
	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("HTTP server forced shutdown after timeout: %v", err)
		return httpServer.Close()
	}
	log.Println("HTTP server shutdown completed gracefully")
	return nil
}

// createMCPHandler creates the shared MCP handler function
func (s *Server) createMCPHandler(streamableServer *mcpserver.StreamableHTTPServer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// No CORS headers: this server serves MCP clients (Claude Code,
		// agents, IDEs), not browsers. Emitting Access-Control-Allow-Origin
		// would let any website read responses from a victim's browser
		// (drive-by data exfiltration). Browser-based clients, if ever
		// needed, require an explicit CORS allowlist (not yet implemented).

		log.Printf("MCP %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)

		if s.config.OAuthEnabled {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
				log.Printf("OAuth: No bearer token provided, returning 401 with discovery info")

				mcpHost := getEnv("MCP_HOST", "localhost")
				mcpPort := getEnv("MCP_PORT", "8080")
				scheme := s.getScheme()
				mcpURL := getEnv("MCP_URL", fmt.Sprintf("%s://%s:%s", scheme, mcpHost, mcpPort))

				w.Header().Add("WWW-Authenticate", `Bearer realm="OAuth", error="invalid_token", error_description="Missing or invalid access token"`)
				w.Header().Add("WWW-Authenticate", fmt.Sprintf(`resource_metadata="%s/.well-known/oauth-protected-resource"`, mcpURL))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)

				errorResponse := map[string]string{
					"error":             "invalid_token",
					"error_description": "Missing or invalid access token",
				}
				if err := json.NewEncoder(w).Encode(errorResponse); err != nil {
					log.Printf("Error encoding OAuth error response: %v", err)
				}
				return
			}

			contextFunc := oauth.CreateHTTPContextFunc()
			ctx := contextFunc(r.Context(), r)
			r = r.WithContext(ctx)
		}

		// Passthrough mode: extract HTTP Basic credentials and attach them to
		// the request context. They are used to open a per-user Trino
		// connection and are never logged.
		if s.config.AuthMode == "passthrough" {
			user, password, ok := r.BasicAuth()
			if !ok {
				log.Printf("Passthrough: no Basic credentials from %s, returning 401", r.RemoteAddr)
				w.Header().Add("WWW-Authenticate", `Basic realm="trino", charset="UTF-8"`)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				errorResponse := map[string]string{
					"error":             "invalid_credentials",
					"error_description": "HTTP Basic authentication required (credentials are forwarded to Trino)",
				}
				if err := json.NewEncoder(w).Encode(errorResponse); err != nil {
					log.Printf("Error encoding auth error response: %v", err)
				}
				return
			}
			ctx := trino.WithBasicCredentials(r.Context(), user, password)
			r = r.WithContext(ctx)
		}

		streamableServer.ServeHTTP(w, r)
	}
}

// handleStatus handles the status endpoint
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, `{"status":"ok","version":"%s"}`, s.version)
}

// handleSignals handles graceful shutdown signals
func (s *Server) handleSignals(done chan<- bool) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	done <- true
}

func (s *Server) getScheme() string {
	certFile := getEnv("HTTPS_CERT_FILE", "")
	keyFile := getEnv("HTTPS_KEY_FILE", "")

	if certFile != "" && keyFile != "" {
		return "https"
	}
	return "http"
}

func (s *Server) getOAuthStatus() string {
	if s.config.OAuthEnabled {
		return " (OAuth enabled)"
	}
	return " (OAuth disabled)"
}

// getOAuthStatusWithWarning returns OAuth status with warning for HTTP
func (s *Server) getOAuthStatusWithWarning() string {
	if s.config.OAuthEnabled {
		return " (OAuth enabled - WARNING: HTTPS recommended for production)"
	}
	return " (OAuth disabled)"
}


func trinoConfigToOAuthConfig(cfg *config.TrinoConfig) *oauth.Config {
	serverURL := getEnv("MCP_URL", "")
	if serverURL == "" {
		mcpHost := getEnv("MCP_HOST", "localhost")
		mcpPort := getEnv("MCP_PORT", "8080")
		scheme := "http"
		if getEnv("HTTPS_CERT_FILE", "") != "" && getEnv("HTTPS_KEY_FILE", "") != "" {
			scheme = "https"
		}
		serverURL = fmt.Sprintf("%s://%s:%s", scheme, mcpHost, mcpPort)
	}

	return &oauth.Config{
		Mode:         cfg.OAuthMode,
		Provider:     cfg.OAuthProvider,
		RedirectURIs: cfg.OAuthRedirectURIs,
		Issuer:       cfg.OIDCIssuer,
		Audience:     cfg.OIDCAudience,
		ClientID:     cfg.OIDCClientID,
		ClientSecret: cfg.OIDCClientSecret,
		ServerURL:    serverURL,
		JWTSecret:    []byte(cfg.JWTSecret),
	}
}

// getEnv gets environment variable with default value
func getEnv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}

// parseBoolEnv reads a boolean environment variable (default: false)
func parseBoolEnv(key string) (bool, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return false, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("invalid %s value '%s': must be a boolean", key, v)
	}
	return b, nil
}
