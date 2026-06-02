# Critical Thinking

A Model Context Protocol server for **critical, narrated, sequential thinking**. Think one step at a time, out loud — with required confidence calibration and adversarial self-critique on every thought.

It fuses three disciplines:

1. **Sequential thinking** — break problems into ordered, numbered steps; revise; branch.
2. **Thinking out loud** — explain each thought in first-person, exploratory voice. Putting half-formed reasoning into words is itself the double-check on it.
3. **Critical self-examination** — every thought is paired with confidence, assumptions, critique, and a counter-argument.

The single tool is `criticalthinking`. Every call must include the four critical-thinking fields — there is no opt-out, by design.

## Install

```bash
go install github.com/jacaudi/critical-thinking/cmd/critical-thinking@latest
# or
docker pull ghcr.io/jacaudi/critical-thinking:v2.0.0
```

The Go install lands the binary at `$GOPATH/bin/critical-thinking`.

## Run

```bash
# stdio (default; for Claude Desktop, Codex CLI, VS Code, etc.)
critical-thinking serve

# Streamable HTTP
critical-thinking serve --http :3000

# Docker (HTTP on :3000)
docker run --rm -p 3000:3000 ghcr.io/jacaudi/critical-thinking:v2.0.0

# CLI mode — pipe NDJSON ThoughtData directly, no MCP host required
critical-thinking cli             # prints narrated transcript to stdout
critical-thinking cli --json      # prints structured ThoughtResponse as NDJSON
critical-thinking schema          # prints the tool contract (description + JSON Schemas) and exits
```

See [docs/clients.md#cli-no-mcp-host](docs/clients.md#cli-no-mcp-host) for CLI usage details and error-handling behaviour.

## One-call example

Request:

```json
{
  "thought": "I think we should normalize first because reads dominate writes.",
  "thoughtNumber": 1, "totalThoughts": 3, "nextThoughtNeeded": true,
  "confidence": 0.6,
  "assumptions": ["read:write ratio is ~10:1"],
  "critique": "Drifted into solution mode without confirming the ratio.",
  "counterArgument": "If writes dominate, monolith-first is simpler.",
  "nextStepRationale": "Verify the read:write ratio before committing to normalization."
}
```

Response (`structuredContent`):

```json
{ "branches": [], "thoughtHistoryLength": 1, "sessionConfidence": 0.6 }
```

The `text` content is a rendered transcript in first-person, exploratory voice. Subsequent calls can omit `thoughtNumber` (auto-assigned) and `totalThoughts` (inherited). Every critical-thinking field has a server-side length cap to enforce one-tight-sentence discipline. The full contract lives in the tool description itself.

## Client setup

### Claude Code

Add the server with the `claude` CLI — pick one transport.

**stdio** (the binary runs as a subprocess of Claude Code):

```bash
claude mcp add critical-thinking -- critical-thinking serve
```

**Streamable HTTP** (run the server separately, point Claude Code at the URL):

```bash
critical-thinking serve --http :3000 &
claude mcp add --transport http critical-thinking http://localhost:3000/mcp
```

Scope it with `--scope user` (available in every project for your user) or `--scope project` (committed to `.mcp.json` in the repo). Default scope is `local` (this project, your machine only).

Verify with `claude mcp list`. Inside a session, `/mcp` shows server status and tools.

### Other clients

`mcp.json` (Claude Desktop / Codex CLI / VS Code):

```json
{
  "mcpServers": {
    "critical-thinking": { "command": "critical-thinking", "args": ["serve"] }
  }
}
```

Or HTTP:

```json
{
  "mcpServers": {
    "critical-thinking": { "url": "http://localhost:3000/mcp" }
  }
}
```

More client recipes in [docs/clients.md](docs/clients.md).

## Resources

The server exposes `thinking://current` — a per-session JSON snapshot of the full thought history (trunk + branches, all critical-thinking fields preserved).

## Documentation

- [docs/configuration.md](docs/configuration.md) — env vars, HTTP endpoints, session lifecycle
- [docs/clients.md](docs/clients.md) — Claude Desktop, Codex CLI, VS Code, Cursor recipes
- [docs/development.md](docs/development.md) — building, testing, debugging with MCP Inspector
- [docs/migration.md](docs/migration.md) — breaking changes since `http-sequential-thinking`

## License

[MIT](LICENSE).
