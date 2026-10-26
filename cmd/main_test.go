package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestShouldRunCLIMode_KnownCommands(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected bool
	}{
		{
			name:     "query command",
			args:     []string{"query", "SELECT 1"},
			expected: true,
		},
		{
			name:     "catalogs command",
			args:     []string{"catalogs"},
			expected: true,
		},
		{
			name:     "schemas command",
			args:     []string{"schemas", "memory"},
			expected: true,
		},
		{
			name:     "tables command",
			args:     []string{"tables", "memory", "default"},
			expected: true,
		},
		{
			name:     "describe command",
			args:     []string{"describe", "test_table"},
			expected: true,
		},
		{
			name:     "explain command",
			args:     []string{"explain", "SELECT 1"},
			expected: true,
		},
		{
			name:     "interactive flag",
			args:     []string{"--interactive"},
			expected: true,
		},
		{
			name:     "with flags before command",
			args:     []string{"--format", "json", "query", "SELECT 1"},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shouldRunCLIMode(tt.args)
			if result != tt.expected {
				t.Errorf("shouldRunCLIMode(%v) = %v, expected %v", tt.args, result, tt.expected)
			}
		})
	}
}

func TestShouldRunCLIMode_UnknownCommands(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected bool
	}{
		{
			name:     "unknown single argument - should NOT trigger CLI",
			args:     []string{"unknown-command"},
			expected: false, // Critical for MCP compatibility
		},
		{
			name:     "unknown argument with flags",
			args:     []string{"--some-flag", "unknown-arg"},
			expected: false, // Critical for MCP compatibility
		},
		{
			name:     "multiple unknown arguments",
			args:     []string{"arg1", "arg2"},
			expected: false, // Critical for MCP compatibility
		},
		{
			name:     "empty args",
			args:     []string{},
			expected: false,
		},
		{
			name:     "only flags",
			args:     []string{"--format", "json"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shouldRunCLIMode(tt.args)
			if result != tt.expected {
				t.Errorf("shouldRunCLIMode(%v) = %v, expected %v", tt.args, result, tt.expected)
			}
		})
	}
}

func TestHasCLIOnlyFlags(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected bool
	}{
		{
			name:     "help flag",
			args:     []string{"--help"},
			expected: true,
		},
		{
			name:     "short help flag",
			args:     []string{"-h"},
			expected: true,
		},
		{
			name:     "version flag",
			args:     []string{"--version"},
			expected: true,
		},
		{
			name:     "short version flag",
			args:     []string{"-v"},
			expected: true,
		},
		{
			name:     "config flag",
			args:     []string{"--config", "/path/to/config"},
			expected: true,
		},
		{
			name:     "format flag",
			args:     []string{"--format", "json"},
			expected: true,
		},
		{
			name:     "interactive flag",
			args:     []string{"--interactive"},
			expected: true,
		},
		{
			name:     "no flags",
			args:     []string{"query", "SELECT 1"},
			expected: false,
		},
		{
			name:     "empty args",
			args:     []string{},
			expected: false,
		},
		{
			name:     "unknown flag",
			args:     []string{"--unknown-flag"},
			expected: false,
		},
		{
			name:     "help flag with equals",
			args:     []string{"--help=true"},
			expected: true, // Function extracts flag name before "="
		},
		{
			name:     "config flag with equals",
			args:     []string{"--config=/path/to/config"},
			expected: true, // Function extracts flag name before "="
		},
		{
			name:     "format flag with equals",
			args:     []string{"--format=json"},
			expected: true, // Function extracts flag name before "="
		},
		{
			name:     "multiple flags",
			args:     []string{"--help", "--version"},
			expected: true,
		},
		{
			name:     "flag with value",
			args:     []string{"--config", "/path/to/config"},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasCLIOnlyFlags(tt.args)
			if result != tt.expected {
				t.Errorf("hasCLIOnlyFlags(%v) = %v, expected %v", tt.args, result, tt.expected)
			}
		})
	}
}

func TestCleanArgs(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected []string
	}{
		{
			name:     "strip --cli flag",
			args:     []string{"--cli", "query", "SELECT 1"},
			expected: []string{"query", "SELECT 1"},
		},
		{
			name:     "strip --mcp flag",
			args:     []string{"--mcp", "query", "SELECT 1"},
			expected: []string{"query", "SELECT 1"},
		},
		{
			name:     "flags after subcommand preserved",
			args:     []string{"query", "--format", "json", "SELECT 1"},
			expected: []string{"query", "--format", "json", "SELECT 1"},
		},
		{
			name:     "only mode flags",
			args:     []string{"--cli"},
			expected: []string{},
		},
		{
			name:     "no mode flags",
			args:     []string{"query", "SELECT 1"},
			expected: []string{"query", "SELECT 1"},
		},
		{
			name:     "flags before subcommand preserved",
			args:     []string{"--format", "json", "query", "SELECT 1"},
			expected: []string{"--format", "json", "query", "SELECT 1"},
		},
		{
			name:     "empty args",
			args:     []string{},
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cleanArgs(tt.args)
			if len(result) != len(tt.expected) {
				t.Fatalf("cleanArgs(%v) length = %d, expected %d", tt.args, len(result), len(tt.expected))
			}
			for i := range result {
				if result[i] != tt.expected[i] {
					t.Errorf("cleanArgs(%v)[%d] = %v, expected %v", tt.args, i, result[i], tt.expected[i])
				}
			}
		})
	}
}

func TestGetEnv(t *testing.T) {
	// Save and restore original env
	originalValue := os.Getenv("TEST_GET_ENV_VAR")
	defer func() {
		if originalValue != "" {
			_ = os.Setenv("TEST_GET_ENV_VAR", originalValue)
		} else {
			_ = os.Unsetenv("TEST_GET_ENV_VAR")
		}
	}()

	t.Run("environment variable is set", func(t *testing.T) {
		_ = os.Setenv("TEST_GET_ENV_VAR", "test_value")
		result := getEnv("TEST_GET_ENV_VAR", "default")
		if result != "test_value" {
			t.Errorf("getEnv() = %v, expected 'test_value'", result)
		}
	})

	t.Run("environment variable is not set", func(t *testing.T) {
		_ = os.Unsetenv("TEST_GET_ENV_VAR")
		result := getEnv("TEST_GET_ENV_VAR", "default_value")
		if result != "default_value" {
			t.Errorf("getEnv() = %v, expected 'default_value'", result)
		}
	})

	t.Run("empty environment variable returns empty string", func(t *testing.T) {
		_ = os.Setenv("TEST_GET_ENV_VAR", "")
		result := getEnv("TEST_GET_ENV_VAR", "default_value")
		if result != "" {
			t.Errorf("getEnv() = %v, expected '' (empty string for set but empty env var)", result)
		}
	})
}

func TestIsTTY(t *testing.T) {
	// This is a simple smoke test - we can't easily test TTY detection
	// in all environments, but we can verify it returns a boolean
	result := isTTY()
	if result != true && result != false {
		t.Errorf("isTTY() returned non-boolean value")
	}
}

// --- Structured help output tests ---

func TestMainHelp_StructuredSections(t *testing.T) {
	var buf bytes.Buffer
	printMainHelp(&buf)
	output := buf.String()

	requiredSections := []string{
		"NAME", "SYNOPSIS", "DESCRIPTION", "COMMANDS",
		"FLAGS", "EXAMPLES", "ENVIRONMENT", "CONFIGURATION",
	}
	for _, section := range requiredSections {
		if !strings.Contains(output, section) {
			t.Errorf("main help missing required section: %s", section)
		}
	}

	// Verify all subcommands are documented
	commands := []string{"query", "catalogs", "schemas", "tables", "describe", "explain", "interactive", "config"}
	for _, cmd := range commands {
		if !strings.Contains(output, cmd) {
			t.Errorf("main help missing command: %s", cmd)
		}
	}

	// Verify all env vars are documented
	envVars := []string{
		"TRINO_HOST", "TRINO_PORT", "TRINO_USER", "TRINO_PASSWORD",
		"TRINO_CATALOG", "TRINO_SCHEMA", "TRINO_PROFILE",
		"TRINO_QUERY_TIMEOUT", "NO_COLOR",
	}
	for _, env := range envVars {
		if !strings.Contains(output, env) {
			t.Errorf("main help missing env var: %s", env)
		}
	}
}

func TestSubcommandHelp_AllCommands(t *testing.T) {
	commands := []string{"query", "catalogs", "schemas", "tables", "describe", "explain", "config", "interactive"}

	for _, cmd := range commands {
		t.Run(cmd, func(t *testing.T) {
			var buf bytes.Buffer
			printSubcommandHelp(&buf, cmd)
			output := buf.String()

			// Every subcommand help must have NAME, SYNOPSIS, DESCRIPTION, EXAMPLES
			for _, section := range []string{"NAME", "SYNOPSIS", "DESCRIPTION"} {
				if !strings.Contains(output, section) {
					t.Errorf("%s --help missing section: %s", cmd, section)
				}
			}

			// Must mention the command name
			if !strings.Contains(output, "mcp-trino "+cmd) {
				t.Errorf("%s --help doesn't mention 'mcp-trino %s'", cmd, cmd)
			}
		})
	}
}

func TestSubcommandHelp_Unknown(t *testing.T) {
	var buf bytes.Buffer
	printSubcommandHelp(&buf, "nonexistent")
	output := buf.String()
	if !strings.Contains(output, "No help available") {
		t.Errorf("expected 'No help available' for unknown command, got: %s", output)
	}
}

// --- Exit code / usageError tests ---

func TestRunCLI_UsageError_UnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runCLI(&stdout, &stderr, []string{"qury"})
	if err == nil {
		t.Fatal("expected error for unknown command")
	}
	if _, ok := err.(*usageError); !ok {
		t.Errorf("expected *usageError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "unknown command") {
		t.Errorf("expected 'unknown command' in error, got: %v", err)
	}
}

func TestRunCLI_UsageError_InvalidFormat(t *testing.T) {
	// Hermetic: fixture config so the test doesn't read the developer's real
	// ~/.config/trino file, and pinned auth env so ambient TRINO_AUTH_MODE or
	// OAUTH_ENABLED exports cannot fail config loading before flag validation
	t.Setenv("TRINO_AUTH_MODE", "standard")
	t.Setenv("OAUTH_ENABLED", "false")
	tmpDir := t.TempDir()
	configPath := tmpDir + "/config.json"
	fixture := `{"current":"p","profiles":{"p":{"host":"h","port":8080,"user":"u"}}}`
	if err := os.WriteFile(configPath, []byte(fixture), 0600); err != nil {
		t.Fatalf("failed to write fixture config: %v", err)
	}

	var stdout, stderr bytes.Buffer
	err := runCLI(&stdout, &stderr, []string{"--config", configPath, "--format", "xml", "catalogs"})
	if err == nil {
		t.Fatal("expected error for invalid format")
	}
	if _, ok := err.(*usageError); !ok {
		t.Errorf("expected *usageError for invalid format, got %T: %v", err, err)
	}
}

func TestRunCLI_UsageError_QueryNoArg(t *testing.T) {
	// Hermetic: fixture config + pinned auth env (see TestRunCLI_UsageError_InvalidFormat)
	t.Setenv("TRINO_AUTH_MODE", "standard")
	t.Setenv("OAUTH_ENABLED", "false")
	tmpDir := t.TempDir()
	configPath := tmpDir + "/config.json"
	fixture := `{"current":"p","profiles":{"p":{"host":"h","port":8080,"user":"u"}}}`
	if err := os.WriteFile(configPath, []byte(fixture), 0600); err != nil {
		t.Fatalf("failed to write fixture config: %v", err)
	}

	var stdout, stderr bytes.Buffer
	err := runCLI(&stdout, &stderr, []string{"--config", configPath, "query"})
	if err == nil {
		t.Fatal("expected error for query with no SQL arg")
	}
	if _, ok := err.(*usageError); !ok {
		t.Errorf("expected *usageError for query with no arg, got %T: %v", err, err)
	}
}

func TestRunCLI_UsageError_DescribeNoArg(t *testing.T) {
	// Hermetic: fixture config + pinned auth env (see TestRunCLI_UsageError_InvalidFormat)
	t.Setenv("TRINO_AUTH_MODE", "standard")
	t.Setenv("OAUTH_ENABLED", "false")
	tmpDir := t.TempDir()
	configPath := tmpDir + "/config.json"
	fixture := `{"current":"p","profiles":{"p":{"host":"h","port":8080,"user":"u"}}}`
	if err := os.WriteFile(configPath, []byte(fixture), 0600); err != nil {
		t.Fatalf("failed to write fixture config: %v", err)
	}

	var stdout, stderr bytes.Buffer
	err := runCLI(&stdout, &stderr, []string{"--config", configPath, "describe"})
	if err == nil {
		t.Fatal("expected error for describe with no table arg")
	}
	if _, ok := err.(*usageError); !ok {
		t.Errorf("expected *usageError for describe with no arg, got %T: %v", err, err)
	}
}

func TestRunCLI_UsageError_ConfigProfileNoSubcommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runCLI(&stdout, &stderr, []string{"config", "profile"})
	if err == nil {
		t.Fatal("expected error for config profile with no subcommand")
	}
	if _, ok := err.(*usageError); !ok {
		t.Errorf("expected *usageError, got %T: %v", err, err)
	}
}

func TestRunCLI_NoError_HelpFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runCLI(&stdout, &stderr, []string{"--help"})
	if err != nil {
		t.Errorf("--help should not return error, got: %v", err)
	}
	if !strings.Contains(stderr.String(), "NAME") {
		t.Error("--help should print structured help to stderr")
	}
}

func TestRunCLI_NoError_VersionFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runCLI(&stdout, &stderr, []string{"--version"})
	if err != nil {
		t.Errorf("--version should not return error, got: %v", err)
	}
	if !strings.Contains(stdout.String(), "mcp-trino") {
		t.Error("--version should print version to stdout")
	}
}

func TestRunCLI_NoError_SubcommandHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runCLI(&stdout, &stderr, []string{"query", "--help"})
	if err != nil {
		t.Errorf("query --help should not return error, got: %v", err)
	}
	if !strings.Contains(stderr.String(), "SYNOPSIS") {
		t.Error("query --help should print structured help")
	}
}

func TestRunCLI_MissingHost(t *testing.T) {
	// Pin auth env so ambient TRINO_AUTH_MODE/OAUTH_ENABLED exports cannot
	// alter which validation fires; the test targets the host requirement
	t.Setenv("TRINO_AUTH_MODE", "standard")
	t.Setenv("OAUTH_ENABLED", "false")
	// Use a profile that doesn't set host to override defaults
	tmpDir := t.TempDir()
	configPath := tmpDir + "/config.json"
	_ = os.WriteFile(configPath, []byte(`{"current":"empty","profiles":{"empty":{"port":8080,"user":"u"}}}`), 0600)

	_ = os.Unsetenv("TRINO_HOST")
	_ = os.Unsetenv("TRINO_USER")

	var stdout, stderr bytes.Buffer
	err := runCLI(&stdout, &stderr, []string{"--config", configPath, "catalogs"})
	if err == nil {
		t.Fatal("expected error when host is missing")
	}
	if !strings.Contains(err.Error(), "host") {
		t.Errorf("expected 'host' validation error, got: %v", err)
	}
}

// replaceStdin points os.Stdin at /dev/null for the duration of the test,
// guaranteeing non-terminal stdin regardless of how go test was invoked.
func replaceStdin(t *testing.T) {
	t.Helper()
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("failed to open /dev/null: %v", err)
	}
	oldStdin := os.Stdin
	os.Stdin = devNull
	t.Cleanup(func() {
		os.Stdin = oldStdin
		_ = devNull.Close()
	})
}

// TestRunCLI_PassthroughNoCredentialsNonTTY verifies that passthrough mode
// without any credentials fails with a passthrough-specific error instead of
// the generic "user not set" (non-interactive context: no prompt possible).
func TestRunCLI_PassthroughNoCredentialsNonTTY(t *testing.T) {
	replaceStdin(t)

	tmpDir := t.TempDir()
	configPath := tmpDir + "/config.json"
	fixture := `{"current":"p","profiles":{"p":{"host":"h","port":8443}}}` // no user in profile
	if err := os.WriteFile(configPath, []byte(fixture), 0600); err != nil {
		t.Fatalf("failed to write fixture config: %v", err)
	}

	t.Setenv("TRINO_AUTH_MODE", "passthrough")
	t.Setenv("TRINO_USER", "")
	t.Setenv("TRINO_PASSWORD", "")
	t.Setenv("TRINO_ACCESS_TOKEN", "")

	var stdout, stderr bytes.Buffer
	err := runCLI(&stdout, &stderr, []string{"--config", configPath, "catalogs"})
	if err == nil {
		t.Fatal("expected error when passthrough mode has no credentials in non-TTY context")
	}
	if !strings.Contains(err.Error(), "passthrough") {
		t.Errorf("expected passthrough-specific error, got: %v", err)
	}
}

// TestRunCLI_PassthroughWithUserProceeds verifies that passthrough mode with
// per-invocation credentials gets past the user requirement and attempts the
// connection (fails here on an unresolvable host, which is expected).
func TestRunCLI_PassthroughWithUserProceeds(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := tmpDir + "/config.json"
	fixture := `{"current":"p","profiles":{"p":{"host":"invalid-test-host.invalid","port":8443}}}`
	if err := os.WriteFile(configPath, []byte(fixture), 0600); err != nil {
		t.Fatalf("failed to write fixture config: %v", err)
	}

	t.Setenv("TRINO_AUTH_MODE", "passthrough")
	t.Setenv("TRINO_USER", "invoked-user")
	t.Setenv("TRINO_PASSWORD", "invoked-password")
	t.Setenv("TRINO_ACCESS_TOKEN", "")

	var stdout, stderr bytes.Buffer
	err := runCLI(&stdout, &stderr, []string{"--config", configPath, "catalogs"})
	if err == nil {
		t.Fatal("expected connection failure against invalid host")
	}
	if !strings.Contains(err.Error(), "failed") {
		t.Errorf("expected connection error, got: %v", err)
	}
}

// TestRunCLI_PassthroughWithTokenProceeds verifies that in passthrough mode a
// bearer token (TRINO_ACCESS_TOKEN / --access-token) replaces Basic
// credentials entirely: no user is needed and the request proceeds.
func TestRunCLI_PassthroughWithTokenProceeds(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := tmpDir + "/config.json"
	fixture := `{"current":"p","profiles":{"p":{"host":"invalid-test-host.invalid","port":8443}}}`
	if err := os.WriteFile(configPath, []byte(fixture), 0600); err != nil {
		t.Fatalf("failed to write fixture config: %v", err)
	}

	t.Setenv("TRINO_AUTH_MODE", "passthrough")
	t.Setenv("TRINO_USER", "")
	t.Setenv("TRINO_PASSWORD", "")
	t.Setenv("TRINO_ACCESS_TOKEN", "aaa.eyJzdWIiOiJhbGljZSJ0.sig")

	var stdout, stderr bytes.Buffer
	err := runCLI(&stdout, &stderr, []string{"--config", configPath, "catalogs"})
	if err == nil {
		t.Fatal("expected connection failure against invalid host")
	}
	if strings.Contains(err.Error(), "user not set") || strings.Contains(err.Error(), "requires per-invocation credentials") {
		t.Errorf("token must satisfy the credential requirement, got: %v", err)
	}
	if !strings.Contains(err.Error(), "failed") {
		t.Errorf("expected connection error, got: %v", err)
	}
}

// TestRunCLI_StandardModeStillRequiresUser verifies standard mode keeps the
// TRINO_USER requirement (passthrough relaxation must not leak into it).
func TestRunCLI_StandardModeStillRequiresUser(t *testing.T) {
	replaceStdin(t)

	tmpDir := t.TempDir()
	configPath := tmpDir + "/config.json"
	fixture := `{"current":"p","profiles":{"p":{"host":"h","port":8443}}}` // no user
	if err := os.WriteFile(configPath, []byte(fixture), 0600); err != nil {
		t.Fatalf("failed to write fixture config: %v", err)
	}

	t.Setenv("TRINO_AUTH_MODE", "")
	t.Setenv("TRINO_USER", "")

	var stdout, stderr bytes.Buffer
	err := runCLI(&stdout, &stderr, []string{"--config", configPath, "catalogs"})
	if err == nil {
		t.Fatal("expected 'user not set' error in standard mode without user")
	}
	if !strings.Contains(err.Error(), "user not set") {
		t.Errorf("expected 'user not set' error, got: %v", err)
	}
}
