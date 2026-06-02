# Migration

Cumulative breaking-change log for `critical-thinking`. Most recent changes first.

## v2.0.0 — Flag CLI replaced by Cobra subcommands

The invocation surface moved from flags to subcommands. Every capability is unchanged —
only how you invoke it changed. Bare `critical-thinking` now prints help (it no longer
starts stdio automatically); use `critical-thinking serve`.

| v1.x | v2.0.0 |
|---|---|
| `critical-thinking` (bare → stdio) | `critical-thinking serve` |
| `critical-thinking -http :3000` | `critical-thinking serve --http :3000` |
| `critical-thinking -cli` | `critical-thinking cli` |
| `critical-thinking -cli -json` | `critical-thinking cli --json` |
| `critical-thinking schema` | `critical-thinking schema` (unchanged) |
| (none) | `critical-thinking version` / `critical-thinking --version` |

**`mcp.json` / host config:** stdio entries that used `"command": "critical-thinking"` with
no args must add `"args": ["serve"]` (TOML: `args = ["serve"]`). URL-based HTTP entries
(`"url": "http://localhost:3000/mcp"`) are unchanged; just launch the server with
`critical-thinking serve --http :3000` instead of `-http :3000`.

`claude mcp add` stdio registration becomes
`claude mcp add critical-thinking -- critical-thinking serve`.

No engine, field, schema, cap, resource, or transport behavior changed in this release.

## Repo rename: `critical-thinking-mcp` → `critical-thinking`

The repo was renamed to drop the `-mcp` suffix and align with the upstream `sequentialthinking` MCP server naming. The image, binary, and tool name were already unsuffixed; the repo and module path now match.

- **GitHub repo** is now `jacaudi/critical-thinking`. GitHub redirects keep the old `critical-thinking-mcp` URL working.
- **Go module path** is now `github.com/jacaudi/critical-thinking`. Update import paths if you depend on `internal/thinking` from outside this repo — `go install` against the old path will fail (Go module paths do not redirect).
- **`go install`** is now `go install github.com/jacaudi/critical-thinking/cmd/critical-thinking@latest`. The binary still lands at `$GOPATH/bin/critical-thinking`.
- **Docker image, binary name, MCP `Implementation.Name`, server log line, and `mcp.json` server alias** all unchanged: `critical-thinking`.

## Repo rename: `critical-thinking-plugin` → `critical-thinking-mcp`

The repo was renamed to drop the `-plugin` suffix, since the Claude Code plugin scaffolding was removed and the project is now solely an MCP server.

- **GitHub repo** is now `jacaudi/critical-thinking-mcp`. GitHub redirects keep the old URL working, but new bookmarks and CI references should use the new name.
- **Go module path** is now `github.com/jacaudi/critical-thinking-mcp`. Update import paths if you depend on `internal/thinking` from outside this repo.
- **`go install`** is now `go install github.com/jacaudi/critical-thinking-mcp/cmd/critical-thinking@latest`. The binary still lands at `$GOPATH/bin/critical-thinking`.
- **Docker image** unchanged: `ghcr.io/jacaudi/critical-thinking:<tag>` (the image name was already decoupled from the repo name).
- **Binary, MCP `Implementation.Name`, and server log line** unchanged: `critical-thinking`.
- **Client-side server aliases** (the key under `mcpServers` in `mcp.json`) are user-controlled and unaffected.

## Project rename: `rubber-ducky-mcp` → `critical-thinking`

The whole project was renamed to align with the discipline it teaches. Specifics:

- **GitHub repo** moved to `jacaudi/critical-thinking-plugin` (the `-plugin` suffix only appears in the repo URL and top-level README title).
- **Go module path** is now `github.com/jacaudi/critical-thinking-plugin` (follows the repo URL). Update import paths if you depend on `internal/thinking` from outside this repo.
- **Entry point moved to `./cmd/critical-thinking/`.** `go install` is now `go install github.com/jacaudi/critical-thinking-plugin/cmd/critical-thinking@latest`. The binary still lands at `$GOPATH/bin/critical-thinking`. Build commands updated everywhere (Dockerfile, CI action, dev docs).
- **Binary name** is now `critical-thinking` (was `rubber-ducky-mcp`). Update `mcp.json` `command` fields and any shell scripts.
- **Docker image** is now `ghcr.io/jacaudi/critical-thinking:<tag>` (was `ghcr.io/jacaudi/rubber-ducky-mcp:<tag>`). The previous image tags remain accessible at the old name until removed; new releases publish only to the new name.
- **MCP `Implementation.Name`** is now `critical-thinking` (was `rubber-ducky-mcp`). Affects what hosts display in their MCP server lists.
- **Server log line** prefix updated to `critical-thinking`.
- **Client-side server aliases** (the key under `mcpServers` in `mcp.json`) are user-controlled and unaffected. Suggested key in docs is now `"critical-thinking"`.

## Tool description rewritten — "Thinking out loud" replaces the rubber-duck framing

The verbatim description registered on the `criticalthinking` tool was rewritten. Discipline #2 changed from "Rubber-duck narration" to "Thinking out loud." The mechanism is unchanged (first-person, exploratory voice; hedges and self-corrections welcome) but the framing is now: putting half-formed reasoning into words is itself the double-check on it. No field semantics changed; no caps changed; no required fields added or removed. Per the protocol-level treatment of `description.go`, this is a behavior-affecting change for client agents that read the tool description and adjust their voice.

## From `0.6.x` (post-rewrite, prior to length-cap and optional-field work)

### Length caps on critical fields

Server-side rune-counted maxLength on the four critical-thinking fields. Over-cap requests return `IsError: true`.

| Field | Cap (runes) | Notes |
|---|---:|---|
| `critique` | 280 | Always enforced |
| `counterArgument` | 280 | Always enforced |
| `assumptions[i]` | 200 | Per-entry |
| `nextStepRationale` | 200 | Only enforced when `nextThoughtNeeded=true` |

The caps are intentionally tight. They force one-tight-sentence-per-field discipline; padded prose returns an error rather than being silently accepted. If a critique genuinely needs more than 280 chars, split the thinking across two `criticalthinking` calls — that's the design intent.

### `thoughtNumber` and `totalThoughts` are now optional after the first thought

- Omit `thoughtNumber` to let the server auto-assign:
  - **Trunk** thoughts: `len(history)+1`.
  - **Branch** thoughts (when `branchFromThought` and `branchId` are set): position within the branch (1, 2, 3, …) — **not** a global ordinal.
  - **Revisions** (when `isRevision` and `revisesThought` are set): next trunk slot.
- Omit `totalThoughts` to inherit the most recent **trunk** thought's value. Branch thoughts are explicitly skipped during inheritance so a branch's auto-bumped `totalThoughts` cannot contaminate the trunk's running estimate.
- The first trunk thought of a session must still send `totalThoughts` explicitly; omitting it returns `IsError: true`.
- Sending values explicitly is still accepted and overrides — useful for unambiguous revisions or when a client treats `thoughtNumber` as a global ordinal.

### Response no longer echoes `thoughtNumber` / `totalThoughts` / `nextThoughtNeeded`

The caller already sent these — echoing them was pure redundancy. The response now contains only:

```json
{
  "branches": [...],
  "thoughtHistoryLength": N,
  "sessionConfidence": 0.X,
  "branchConfidences": { ... }
}
```

Read the full per-thought state from the `thinking://current` resource if needed.

### Binary, server name, and log line renamed to `rubber-ducky-mcp`

For consistency with the Go module path. The Docker image build artifact and the MCP `Implementation.Name` field are now `rubber-ducky-mcp` (was `rubber-ducky-thinking`). Update `mcp.json` `command` fields and any shell-script references. Client-side server *aliases* (the key under `mcpServers`) are user-controlled and unaffected.

## From `http-sequential-thinking` (Node predecessor)

This Go server is the successor to `jacaudi/http-sequential-thinking`. The original differences:

- **Tool renamed:** `sequentialthinking` → `criticalthinking`. Update `mcp.json` references.
- **Required new fields:** every call must send `confidence`, `assumptions`, `critique`, `counterArgument`. Calls missing these fail with `IsError: true`.
- **`nextStepRationale` required when `nextThoughtNeeded: true`.**
- **Web UI removed.** Use [MCP Inspector](https://github.com/modelcontextprotocol/inspector) or `curl` for manual testing.
- **CORS default tightened.** Set `ALLOWED_ORIGINS` explicitly to allow browser clients.

The original `http-sequential-thinking` had no notion of confidence calibration, assumptions, critique, or counter-argument. That's the deliberate philosophical break: the original tool was infrastructure for sequential prompting; this one is infrastructure for critical sequential prompting.
