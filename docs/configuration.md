# Configuration

## Environment variables

| Env var | Default | Purpose |
|---|---|---|
| `ALLOWED_ORIGINS` | (empty) | Comma-separated list of browser origins permitted to call `/mcp`. Wired into both the outer CORS layer and the SDK's CSRF protection (`http.CrossOriginProtection.AddTrustedOrigin`). Default rejects all browser origins. Non-browser callers (no `Origin` / no `Sec-Fetch-Site` header) are unaffected. |
| `DOCKER` | unset | When `true`, HTTP server binds to `0.0.0.0` instead of `127.0.0.1`. Set automatically in the published Docker image. |
| `DISABLE_THOUGHT_LOGGING` | unset | Reserved for the future structured-log gate. The current server emits no per-thought logs by default. |

## Transports

### Stdio (default)

```bash
critical-thinking serve
```

One process serves one session. There is no cross-stream isolation concern because there is no second stream — the process IS the session. Use this for direct integration with MCP hosts (Claude Desktop, Codex CLI, VS Code).

### Streamable HTTP

```bash
critical-thinking serve --http :3000
```

The HTTP server binds to `127.0.0.1` by default (or `0.0.0.0` when `DOCKER=true`). Each session gets its own `*mcp.Server` with its own `SequentialThinkingServer`, constructed inside a factory closure — there is no map keyed by session ID anywhere, by design. The closure scope is the cross-session isolation invariant.

## HTTP endpoints

| Path | Methods | Purpose |
|---|---|---|
| `/mcp` | `POST`, `GET`, `DELETE` | Main MCP endpoint (Streamable HTTP) |
| `/health` | `GET` | Returns `{status, transport, sessionsCreated, version}`. `sessionsCreated` is a **lifetime** counter of sessions ever created in this process; it is NOT pruned when the SDK closes idle sessions. Treat it as a creation counter, not an active-session gauge. |

## Session lifecycle

Sessions are in-memory only. Idle sessions expire after **1 hour**, enforced by the SDK via `StreamableHTTPOptions.SessionTimeout`. When the SDK closes a session, the bound `*mcp.Server` (and the `*SequentialThinkingServer` it captures) becomes unreachable and is released for GC.

There is no callback fired when the SDK closes a session, so the in-process registry that powers `/health.sessionsCreated` drifts upward — that's intentional. If you need an accurate active-session count, get it from your reverse proxy or load balancer, not from this server.

## CORS and CSRF

When `ALLOWED_ORIGINS` is empty, browser requests with an `Origin` header are rejected with HTTP 403. Non-browser clients (no `Origin`, no `Sec-Fetch-Site`) bypass the check entirely. When set, matching origins receive `Access-Control-Allow-Origin: <origin>`, `Access-Control-Allow-Credentials: true`, `Access-Control-Expose-Headers: mcp-session-id`, and a `Vary: Origin` header for cache-poisoning mitigation.

The same origin list is registered with the SDK's CSRF protection (`http.CrossOriginProtection.AddTrustedOrigin`) so the SDK's same-origin policy doesn't double-reject permitted browser callers.
