package main

import (
	"testing"

	"github.com/spf13/cobra"
)

// TestCobraImportable is a temporary anchor so `go mod tidy` keeps the cobra
// dependency before any production code imports it. Deleted in Task 2.
func TestCobraImportable(t *testing.T) {
	c := &cobra.Command{Use: "anchor"}
	if c.Use != "anchor" {
		t.Fatalf("Use = %q, want anchor", c.Use)
	}
}
