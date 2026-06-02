package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

// versionText is the single source of truth for the human-readable version
// string. Both the `version` subcommand and the root `--version` flag print it
// verbatim, so they can never drift. It does NOT end with a newline; callers
// add one (Fprintln / a version template).
func versionText() string {
	return fmt.Sprintf("critical-thinking %s (commit %s, built %s)", version, commit, date)
}

// versionInfo is the structured form emitted by `version --json`.
type versionInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}

// newVersionCmd prints version/commit/date. Text by default; structured JSON
// with --json. The structured form lives ONLY here, not on the root --version
// flag (D7).
func newVersionCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print version, commit, and build date",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(versionInfo{Version: version, Commit: commit, Date: date})
			}
			_, err := fmt.Fprintln(cmd.OutOrStdout(), versionText())
			return err
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit version info as JSON")
	return cmd
}
