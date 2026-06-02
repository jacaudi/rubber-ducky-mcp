# Client setup

All snippets assume the binary `critical-thinking` is on your `$PATH`. After `go install github.com/jacaudi/critical-thinking/cmd/critical-thinking@latest`, that's `$GOPATH/bin/critical-thinking` — make sure `$GOPATH/bin` is on `$PATH`, or use the absolute path in the `command` field.

## Claude Code

### stdio

```bash
claude mcp add critical-thinking -- critical-thinking serve
```

Add `--scope user` to make it available in every project, or `--scope project` to commit it to a `.mcp.json` file in the repo. The default `local` scope keeps it private to the current project on this machine.

### Streamable HTTP

Run the server in one terminal:

```bash
critical-thinking serve --http :3000
```

Register it with Claude Code:

```bash
claude mcp add --transport http critical-thinking http://localhost:3000/mcp
```

### Verify

```bash
claude mcp list
```

Inside a Claude Code session, `/mcp` shows live status, and the `criticalthinking` tool will appear in tool listings.

## Claude Desktop

`~/Library/Application Support/Claude/claude_desktop_config.json` (macOS) or `%APPDATA%\Claude\claude_desktop_config.json` (Windows):

```json
{
  "mcpServers": {
    "critical-thinking": {
      "command": "critical-thinking",
      "args": ["serve"]
    }
  }
}
```

Restart Claude Desktop after editing.

## Codex CLI

`~/.codex/mcp.json`:

```json
{
  "mcpServers": {
    "critical-thinking": {
      "command": "critical-thinking",
      "args": ["serve"]
    }
  }
}
```

## VS Code (Continue, Cline, etc.)

Most VS Code MCP-aware extensions use the same `mcp.json` shape:

```json
{
  "mcpServers": {
    "critical-thinking": {
      "command": "critical-thinking",
      "args": ["serve"]
    }
  }
}
```

## Cursor

`~/.cursor/mcp.json`:

```json
{
  "mcpServers": {
    "critical-thinking": {
      "url": "http://localhost:3000/mcp"
    }
  }
}
```

(Cursor currently prefers HTTP transport.) Run the server separately with `critical-thinking serve --http :3000`.

## Generic HTTP (any client)

Run the server in HTTP mode and point your client at `/mcp`:

```bash
critical-thinking serve --http :3000
```

```json
{
  "mcpServers": {
    "critical-thinking": {
      "url": "http://localhost:3000/mcp"
    }
  }
}
```

For browser-based clients, set `ALLOWED_ORIGINS` to permit your origin — see [configuration.md](configuration.md).

## Docker

```bash
docker run -d --name critical-thinking -p 3000:3000 ghcr.io/jacaudi/critical-thinking:v2.0.0
```

Then use the HTTP client config above. The image binds to `0.0.0.0` automatically (via `DOCKER=true`); pair it with appropriate firewall rules in production.

## CLI (no MCP host)

Run the engine directly, without an MCP client:

```bash
# Stream thoughts: one ThoughtData JSON object per line on stdin.
printf '%s\n' '{"thought":"...","thoughtNumber":1,"totalThoughts":3,"nextThoughtNeeded":true,"confidence":0.6,"assumptions":[],"critique":"...","counterArgument":"...","nextStepRationale":"..."}' \
  | critical-thinking cli

# Structured NDJSON output instead of the transcript:
... | critical-thinking cli --json

# Print the tool contract (description + JSON Schemas) for a model to read:
critical-thinking schema
```

`critical-thinking cli` keeps one in-memory session for the process, so history, confidence, and
branches accumulate across input lines. Validation/parse errors are written to
stderr (or to stdout in `--json` mode, to keep the stream complete) and the
process continues; the exit code is `1` if any line errored, else `0`.
