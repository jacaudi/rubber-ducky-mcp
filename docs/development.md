# Development

## Toolchain

Go 1.26+. No other build dependencies (the MCP SDK is a Go module).

## Build

```bash
go build -ldflags "-X main.version=$(git describe --tags --always)" -o critical-thinking ./cmd/critical-thinking
```

The `-X main.version=...` flag stamps the build with a version string surfaced via `/health` and the MCP `Implementation.Version`.

## Test

```bash
go test -race -count=1 ./...   # full suite, race detector on, no test cache
go vet ./...                    # static checks
gofmt -d .                      # diff against gofmt; empty output = clean
```

`-race` is the standard mode for this project. Concurrency invariants in the HTTP path are non-trivial (per-session factory closures, session registry mutex) and `go test` without `-race` will not catch their regressions.

## Debugging with MCP Inspector

The fastest way to manually exercise the tool is the official [MCP Inspector](https://github.com/modelcontextprotocol/inspector):

```bash
# stdio
npx @modelcontextprotocol/inspector critical-thinking

# HTTP
critical-thinking -http :3000 &
npx @modelcontextprotocol/inspector --uri http://localhost:3000/mcp
```

The inspector lets you call `criticalthinking` interactively, watch the rendered transcript, and read the `thinking://current` resource without writing client code.

## Project layout

```
.
├── cmd/critical-thinking/
│   ├── main.go                   # MCP transport adapter (stdio + HTTP)
│   └── main_test.go              # cross-session isolation integration test
├── internal/thinking/
│   ├── description.go            # tool description (the prompt-engineering contract)
│   ├── schema.go                 # ThoughtData / ThoughtResponse + Validate()
│   ├── server.go                 # SequentialThinkingServer state machine
│   └── *_test.go                 # unit tests
├── Dockerfile                    # multi-stage, distroless final
```

The `internal/thinking` package has zero dependency on the MCP SDK — `cmd/critical-thinking/main.go` is the only adapter. That keeps the state machine fully unit-testable.

## Release workflow

CI runs on push and PR via GitHub Actions: `vet`, `gofmt`, `go test -race -count=1 ./...`, and a Docker build. Releases are tag-driven; tagging `vX.Y.Z` triggers the release workflow which builds and pushes the multi-arch Docker image to `ghcr.io/jacaudi/critical-thinking:vX.Y.Z` and updates `:latest`.

## Treating the description as a protocol

The string in [`internal/thinking/description.go`](../internal/thinking/description.go) is the contract every client agent reads. Treat changes there like wire-format changes: bump the package version and add an entry to [migration.md](migration.md). Field renames, length-cap changes, or removed guidance can all silently break agent behavior.
