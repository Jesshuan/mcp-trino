# Per-User Credential Passthrough (LDAP / Trino Authentication)

`TRINO_AUTH_MODE=passthrough` forwards each client's HTTP Basic credentials
(user/password) to Trino, so **Trino's own authentication — typically LDAP —
becomes the single source of truth**. Queries run as the real end user, and
Trino-side access control and audit apply per person.

This is an alternative to OAuth (see [oauth.md](oauth.md)), not an addition:
pick one authentication model per deployment.

## When to use which mode

| | `standard` (default) | `passthrough` | OAuth |
|---|---|---|---|
| Trino sees | one service account | the real user | service account (or impersonated user) |
| Trino ACLs | one shared blob | per-user | per-user (with impersonation) |
| Client sends | nothing | user/password (Basic) | ephemeral token (Bearer) |
| Trino auth backend | any | **password-based (LDAP)** | any |
| Transport | stdio or http | **http only** | http |

## Architecture

```
Claude Code ──HTTPS + Basic auth──▶ mcp-trino ──per-user connection──▶ Trino (LDAP)
   Alice                              (never logs credentials)              │
   Bob                                                                        │
                                    Trino authenticates the REAL user ───────┘
```

- The client sends `Authorization: Basic <base64(user:password)>` on every
  MCP HTTP request.
- mcp-trino parses the header once, opens (and caches) a **per-user Trino
  connection pool** using those credentials, and never logs them.
- Missing or malformed credentials get `401` with a
  `WWW-Authenticate: Basic realm="trino"` challenge.
- Trino rejects bad credentials at query time — mcp-trino adds no user
  database of its own.

> **Token caveat**: an OAuth/IdP ephemeral token cannot be forwarded to an
> LDAP-configured Trino — LDAP only validates passwords. If your users
> authenticate with tokens, use [OAuth mode](oauth.md) instead.

## Quick start

```bash
export TRINO_AUTH_MODE=passthrough
export MCP_TRANSPORT=http          # required: Basic credentials are HTTP headers
export TRINO_HOST=trino.internal
export TRINO_SCHEME=https
export TRINO_SSL_INSECURE=false    # LDAP passwords must travel over verified TLS

# No TRINO_PASSWORD needed: users supply their own credentials.
# TRINO_USER is still used as a fallback service account if set.
mcp-trino
```

Client configuration (Claude Code style — custom headers on the HTTP MCP
server):

```json
{
  "mcpServers": {
    "trino-ldap": {
      "url": "https://your-server-endpoint:8443/mcp",
      "headers": {
        "Authorization": "Basic <base64 of user:password>"
      }
    }
  }
}
```

## Testing with the CLI

The CLI has no HTTP layer to carry Basic headers, so **each invocation is its
own identity**: the credentials you run the command with are forwarded to
Trino directly. `TRINO_USER` is *not* required.

```bash
export TRINO_AUTH_MODE=passthrough

# Interactive: prompts "Trino user:" then "Trino password:" (masked)
mcp-trino query 'SHOW SCHEMAS FROM tpch'

# Explicit credentials — script/CI friendly
mcp-trino --user alice --password '...' query 'SELECT 1'

# REPL: credentials prompted once, every query runs as that identity
mcp-trino interactive
```

A passthrough profile simply omits `user`:

```yaml
# ~/.config/trino/config.yaml
current: ldap-dev

profiles:
  ldap-dev:
    host: trino.internal
    port: 8443
    scheme: https
    ssl:
      enabled: true
      insecure: false   # dev/self-signed only — verify certs in production
```

Scripted (non-TTY) runs without credentials fail explicitly instead of
hanging on a prompt:

```
Error: TRINO_AUTH_MODE=passthrough requires per-invocation credentials:
pass --user/--password or set TRINO_USER/TRINO_PASSWORD
(no interactive terminal available)
```

Verify the negative case against a password-authenticated Trino: wrong
credentials return `401 Unauthorized: Access Denied: Invalid credentials`,
proving the identity really comes from the invocation and not from a shared
account.

## Configuration

| Variable | Default | Notes |
|---|---|---|
| `TRINO_AUTH_MODE` | `standard` | `standard` = shared service account; `passthrough` = per-user Basic credentials |
| `MCP_TRANSPORT` | `stdio` | Must be `http` for passthrough |
| `TRINO_USER`/`TRINO_PASSWORD` | — | Optional in passthrough mode: used only for the startup connection test (and CLI commands) |
| `HTTPS_CERT_FILE` / `HTTPS_KEY_FILE` | — | Provide TLS, or put a TLS-terminating proxy in front; the server warns loudly otherwise |

**Mutually exclusive** (the server refuses to start):
- `TRINO_AUTH_MODE=passthrough` + `OAUTH_ENABLED=true`
- `TRINO_AUTH_MODE=passthrough` + `TRINO_ENABLE_IMPERSONATION=true`
  (redundant: queries already run as the authenticated user)

## Behavior details

- **Per-user connection cache**: pools are keyed by username (max 20, LRU
  eviction, 15-minute idle expiry, 5-minute connection lifetime). Rapid tool
  calls from the same user reuse one pool instead of re-binding to LDAP each
  time.
- **Credential rotation**: if the same username presents a different
  password, the cached pool is dropped and rebuilt.
- **Query attribution**: `X-Trino-Client-Tags` / `X-Trino-Client-Info` carry
  the authenticated username, so Trino audit shows real users.
- **CLI mode**: each CLI invocation is its own identity — there is no shared
  service account and `TRINO_USER` is not required. Credentials come from the
  invocation itself: `--user`/`--password` flags, `TRINO_USER`/`TRINO_PASSWORD`
  environment variables, or an interactive prompt (username plain, password
  masked) when run from a terminal. Scripted (non-TTY) runs without
  credentials fail with an explicit error.
- **Startup guard**: passthrough counts as an authenticated deployment — no
  `MCP_ALLOW_UNAUTHENTICATED_HTTP` opt-in is needed.

## Security requirements

- **TLS is mandatory in practice.** User passwords transit mcp-trino on every
  request. Terminate TLS at mcp-trino (`HTTPS_CERT_FILE`/`HTTPS_KEY_FILE`) or
  at a proxy in front (e.g. `https://your-endpoint:8443` → `mcp-trino:8080`).
  The server logs a loud warning when running passthrough over plain HTTP.
- **Never put credentials in tool arguments or URLs** — they must travel only
  in the `Authorization` header. Tool arguments can end up in LLM transcripts.
- **Treat the server as a credential-handling system**: it holds user
  passwords in memory for the lifetime of the connection cache. Harden the
  host accordingly.
- **Trino TLS verification** must stay enabled (`TRINO_SSL_INSECURE=false`,
  the default) — credentials forwarded over an unverified TLS connection can
  be captured by a man-in-the-middle.
