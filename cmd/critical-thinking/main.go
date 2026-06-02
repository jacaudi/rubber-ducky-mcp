package main

import "os"

// Injected at build time via -ldflags (see taskfile.yml / .goreleaser.yaml / Dockerfile).
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}
