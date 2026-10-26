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
| Client sends | nothing | user/password (Basic) or Bearer token (JWT) | ephemeral token (Bearer) |
| Trino auth backend | any | **password (LDAP)** or **Bearer-token auth (JWT, OAUTH2/OIDC)** | any |
| Transport | stdio or http | **http only** | http |

## Architecture

```
Claude Code ──HTTPS + Basic auth──▶ mcp-trino ──per-user connection──▶ Trino (LDAP)
   Alice                              (never logs credentials)              │
   Bob                                                                        │
                                    Trino authenticates the REAL user ───────┘
```

- The client sends `Authorization: Basic <base64(user:password)>` or
  `Authorization: Bearer <jwt>` on every MCP HTTP request.
- mcp-trino parses the header once, opens (and caches) a **per-user Trino
  connection pool** using those credentials, and never logs them.
- Missing or malformed credentials get `401` with both
  `WWW-Authenticate: Basic` and `WWW-Authenticate: Bearer` challenges.
- Trino rejects bad credentials at query time — mcp-trino adds no user
  database of its own.

> **Token caveat**: which credentials passthrough can forward depends on how
> Trino authenticates. `Authorization: Basic` reaches a **password-based
> (LDAP)** Trino; `Authorization: Bearer` (JWT) reaches a Trino configured
> with **JWT/OIDC authentication** — see [Bearer token passthrough](#bearer-token-passthrough-jwt)
> below. mcp-trino validates nothing itself; if you want tokens *validated at
> the MCP layer* before they reach Trino, use [OAuth mode](oauth.md) instead.

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

## Bearer token passthrough (JWT)

Passthrough mode also accepts `Authorization: Bearer <jwt>` alongside Basic.
This fits **AI agents and programmatic clients that obtain ephemeral tokens
from an IdP at runtime** (OIDC/OAuth2 login, service-account token exchange):
the token is set per request — or refreshed by the client lifecycle — and
forwarded to Trino, so no long-lived Basic credentials are embedded in client
configs.

```json
{
  "mcpServers": {
    "trino-oidc": {
      "url": "https://your-server-endpoint:8443/mcp",
      "headers": {
        "Authorization": "Bearer <access-token>"
      }
    }
  }
}
```

**What mcp-trino does and does not do with the token:**

- It **never validates the token** — no signature, expiry, issuer, or audience
  checks. Trino's JWT/OIDC authenticator is the validator; mcp-trino is a
  forwarding pipe. Rejected/expired tokens surface Trino's auth error.
- It **never refreshes tokens** — the client owns the lifecycle. A refreshed
  token simply arrives on the next request and the per-user connection pool is
  rotated automatically.
- It **derives the connection identity from the token**: the DSN carries only
  the `accessToken` parameter (no user header), so Trino maps the token to
  the user with whichever authenticator validates it (the token's `sub`
  claim by default; see `principal-field` in the Trino authentication docs).
  Connection pools are keyed by the token's **unverified** `sub` claim (used
  for pool keying and query attribution only, never for authorization).

**Trino side** must accept `Authorization: Bearer` tokens. In practice two
authenticator setups do, and both work unchanged — mcp-trino only forwards
the header and is agnostic to which one validates the token:

- **JWT authenticator** — Trino validates the JWT itself against a JWKS:

  ```properties
  http-server.authentication.type=PASSWORD,JWT
  http-server.authentication.jwt.key-file=https://your-idp/.well-known/jwks.json
  ```

- **OAUTH2 authenticator** (OIDC) — Trino delegates validation to the OIDC
  provider; access tokens issued by that provider are accepted when presented
  directly, the same way the official Trino CLI's `--access-token` flag works:

  ```properties
  http-server.authentication.type=PASSWORD,OAUTH2
  http-server.authentication.oauth2.issuer=https://your-idp
  http-server.authentication.oauth2.client-id=...
  http-server.authentication.oauth2.client-secret=...
  ```

**CLI testing** uses the same plumbing — `--access-token` or
`TRINO_ACCESS_TOKEN` replaces user/password entirely (no prompt, no
`TRINO_USER` needed):

```bash
TRINO_AUTH_MODE=passthrough TRINO_ACCESS_TOKEN=eyJ... mcp-trino query 'SELECT 1'
TRINO_AUTH_MODE=passthrough mcp-trino --access-token "$TOKEN" query 'SELECT 1'
```

Tokens are still secrets: prefer the environment variable or a secrets CLI
over `--access-token` to keep them out of shell history.

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
| `TRINO_AUTH_MODE` | `standard` | `standard` = shared service account; `passthrough` = per-user Basic or Bearer credentials |
| `MCP_TRANSPORT` | `stdio` | Must be `http` for passthrough |
| `TRINO_USER`/`TRINO_PASSWORD` | — | Optional in passthrough mode: used only for the startup connection test (and CLI commands) |
| `TRINO_ACCESS_TOKEN` | — | CLI only: bearer token (JWT) replacing user/password; on the server side tokens arrive per request in the `Authorization` header |
| `HTTPS_CERT_FILE` / `HTTPS_KEY_FILE` | — | Provide TLS, or put a TLS-terminating proxy in front; the server warns loudly otherwise |

**Mutually exclusive** (the server refuses to start):
- `TRINO_AUTH_MODE=passthrough` + `OAUTH_ENABLED=true`
- `TRINO_AUTH_MODE=passthrough` + `TRINO_ENABLE_IMPERSONATION=true`
  (redundant: queries already run as the authenticated user)

## Behavior details

- **Per-user connection cache**: Basic pools are keyed by username, bearer
  pools by the token's unverified `sub` claim (max 20, LRU eviction,
  15-minute idle expiry, 5-minute connection lifetime). Rapid tool calls from
  the same user reuse one pool instead of re-binding each time.
- **Credential rotation**: if the same username presents a different
  password, or the same subject presents a different (refreshed) token, the
  cached pool is dropped and rebuilt.
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
- **Bearer tokens are forwarded, not validated.** Anyone who can reach the
  MCP endpoint can present *any* token; only Trino decides whether it is
  acceptable. Put network-level access control in front of mcp-trino and keep
  Trino's JWT authenticator strict (issuer, audience, signature).
- **Never put credentials in tool arguments or URLs** — they must travel only
  in the `Authorization` header. Tool arguments can end up in LLM transcripts.
- **Treat the server as a credential-handling system**: it holds user
  passwords in memory for the lifetime of the connection cache. Harden the
  host accordingly.
- **Trino TLS verification** must stay enabled (`TRINO_SSL_INSECURE=false`,
  the default) — credentials forwarded over an unverified TLS connection can
  be captured by a man-in-the-middle.
