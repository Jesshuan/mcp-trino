package trino

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/trinodb/trino-go-client/trino"

	"github.com/tuannvm/mcp-trino/internal/config"
)

// ensureTestCustomClient registers the custom HTTP client that buildDSN
// references, so sql.Open can create connectors without a real NewClient.
func ensureTestCustomClient(t *testing.T) {
	t.Helper()
	err := trino.RegisterCustomClient("mcp-trino", http.DefaultClient)
	if err != nil && !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("failed to register custom client: %v", err)
	}
}

func TestWithBasicCredentials(t *testing.T) {
	tests := []struct {
		name     string
		user     string
		password string
	}{
		{"standard credentials", "alice", "password"},
		{"credentials with special characters", "alice@example.com", "p@ss:w/ord?"},
		{"empty password", "bob", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := WithBasicCredentials(context.Background(), tt.user, tt.password)

			user, password, ok := GetBasicCredentials(ctx)
			if !ok {
				t.Fatal("GetBasicCredentials() ok = false, want true")
			}
			if user != tt.user {
				t.Errorf("user = %q, want %q", user, tt.user)
			}
			if password != tt.password {
				t.Errorf("password = %q, want %q", password, tt.password)
			}
		})
	}
}

func TestGetBasicCredentials_NotSet(t *testing.T) {
	user, password, ok := GetBasicCredentials(context.Background())
	if ok {
		t.Error("GetBasicCredentials() ok = true, want false")
	}
	if user != "" || password != "" {
		t.Errorf("GetBasicCredentials() = (%q, %q), want empty values", user, password)
	}
}

func TestBasicCredentialsPreservedThroughTimeout(t *testing.T) {
	// Simulate what ExecuteQueryWithContext does: wrap the request context
	// in a timeout — credentials must survive.
	ctx := WithBasicCredentials(context.Background(), "alice", "password")
	timeoutCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()

	user, _, ok := GetBasicCredentials(timeoutCtx)
	if !ok || user != "alice" {
		t.Errorf("GetBasicCredentials() from timeout context = (%q, %v), want (alice, true)", user, ok)
	}
}

func TestBuildDSN(t *testing.T) {
	cfg := &config.TrinoConfig{
		Host:        "trino.example.com",
		Port:        8443,
		Catalog:     "memory",
		Schema:      "default",
		Scheme:      "https",
		SSL:         true,
		SSLInsecure: false,
	}

	dsn := buildDSN(cfg, "alice", "p@ss:w/ord?")

	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("buildDSN() produced unparseable DSN %q: %v", dsn, err)
	}
	if u.Scheme != "https" {
		t.Errorf("scheme = %q, want https", u.Scheme)
	}
	if u.Host != "trino.example.com:8443" {
		t.Errorf("host = %q, want trino.example.com:8443", u.Host)
	}
	if got := u.User.Username(); got != "alice" {
		t.Errorf("username = %q, want alice", got)
	}
	password, _ := u.User.Password()
	if password != "p@ss:w/ord?" {
		t.Errorf("password = %q, want original password (must round-trip special characters)", password)
	}

	query := u.Query()
	if got := query.Get("catalog"); got != "memory" {
		t.Errorf("catalog param = %q, want memory", got)
	}
	if got := query.Get("schema"); got != "default" {
		t.Errorf("schema param = %q, want default", got)
	}
	if got := query.Get("SSL"); got != "true" {
		t.Errorf("SSL param = %q, want true", got)
	}
	if got := query.Get("SSLInsecure"); got != "false" {
		t.Errorf("SSLInsecure param = %q, want false", got)
	}
	if got := query.Get("custom_client"); got != "mcp-trino" {
		t.Errorf("custom_client param = %q, want mcp-trino", got)
	}
}

func passthroughTestConfig() *config.TrinoConfig {
	return &config.TrinoConfig{
		AuthMode:     "passthrough",
		Host:         "localhost",
		Port:         8080,
		Catalog:      "memory",
		Schema:       "default",
		Scheme:       "https",
		SSL:          true,
		SSLInsecure:  false,
		QueryTimeout: time.Second,
		MaxRows:      100,
	}
}

func TestDbForContext_StandardModeUsesSharedPool(t *testing.T) {
	ensureTestCustomClient(t)

	shared, err := sql.Open("trino", "https://svc@localhost:8080/?catalog=memory&schema=default")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer func() { _ = shared.Close() }()

	c := &Client{db: shared, config: &config.TrinoConfig{AuthMode: "standard"}}

	db, err := c.dbForContext(context.Background())
	if err != nil {
		t.Fatalf("dbForContext() error = %v", err)
	}
	if db != shared {
		t.Error("dbForContext() returned a different pool, want the shared service-account pool in standard mode")
	}
}

func TestDbForContext_PassthroughRequiresCredentials(t *testing.T) {
	c := &Client{config: passthroughTestConfig()}

	_, err := c.dbForContext(context.Background())
	if err == nil {
		t.Fatal("dbForContext() expected error without credentials, got nil")
	}
	if !strings.Contains(err.Error(), "authentication required") {
		t.Errorf("dbForContext() error = %v, want 'authentication required'", err)
	}
}

func TestDbForContext_CachesPerUserAndRotatesCredentials(t *testing.T) {
	ensureTestCustomClient(t)

	c := &Client{config: passthroughTestConfig()}
	defer func() { _ = c.Close() }()

	// Same credentials twice: the pool must be cached, not re-opened
	ctxAlice := WithBasicCredentials(context.Background(), "alice", "pw1")
	db1, err := c.dbForContext(ctxAlice)
	if err != nil {
		t.Fatalf("dbForContext() error = %v", err)
	}
	db2, err := c.dbForContext(ctxAlice)
	if err != nil {
		t.Fatalf("dbForContext() second call error = %v", err)
	}
	if db1 != db2 {
		t.Error("dbForContext() re-opened the pool for the same user, want cached pool")
	}

	// Rotated credentials for the same username: the stale pool must be replaced
	ctxAliceRotated := WithBasicCredentials(context.Background(), "alice", "pw2")
	db3, err := c.dbForContext(ctxAliceRotated)
	if err != nil {
		t.Fatalf("dbForContext() rotated credentials error = %v", err)
	}
	if db3 == db1 {
		t.Error("dbForContext() reused the pool after credential rotation, want a fresh pool")
	}

	// Different user: separate pool
	ctxBob := WithBasicCredentials(context.Background(), "bob", "pw")
	db4, err := c.dbForContext(ctxBob)
	if err != nil {
		t.Fatalf("dbForContext() second user error = %v", err)
	}
	if db4 == db3 {
		t.Error("dbForContext() shared a pool between users, want per-user pools")
	}

	c.userConnsMu.Lock()
	if len(c.userConns) != 2 {
		t.Errorf("user connection cache size = %d, want 2 (alice, bob)", len(c.userConns))
	}
	c.userConnsMu.Unlock()
}

func TestDbForContext_EvictsLeastRecentlyUsed(t *testing.T) {
	ensureTestCustomClient(t)

	c := &Client{config: passthroughTestConfig()}
	defer func() { _ = c.Close() }()

	// Fill the cache beyond its cap; pools are used sequentially so the
	// first user is the least recently used and must be evicted.
	for i := 0; i <= maxUserConns; i++ {
		user := fmt.Sprintf("user%02d", i)
		ctx := WithBasicCredentials(context.Background(), user, "pw")
		if _, err := c.dbForContext(ctx); err != nil {
			t.Fatalf("dbForContext() error for %s: %v", user, err)
		}
	}

	c.userConnsMu.Lock()
	defer c.userConnsMu.Unlock()
	if len(c.userConns) != maxUserConns {
		t.Errorf("user connection cache size = %d, want %d (capped)", len(c.userConns), maxUserConns)
	}
	if _, exists := c.userConns["user00"]; exists {
		t.Error("least recently used pool (user00) was not evicted")
	}
	if _, exists := c.userConns[fmt.Sprintf("user%02d", maxUserConns)]; !exists {
		t.Errorf("most recent pool (user%02d) must remain cached", maxUserConns)
	}
}

func TestExecuteQueryWithContext_PassthroughRequiresCredentials(t *testing.T) {
	cfg := passthroughTestConfig()
	c := &Client{config: cfg, timeout: cfg.QueryTimeout}

	// Read-only check passes first; the credential requirement must reject next
	_, err := c.ExecuteQueryWithContext(context.Background(), "SELECT 1")
	if err == nil {
		t.Fatal("ExecuteQueryWithContext() expected error without credentials, got nil")
	}
	if !strings.Contains(err.Error(), "authentication required") {
		t.Errorf("ExecuteQueryWithContext() error = %v, want 'authentication required'", err)
	}
}
