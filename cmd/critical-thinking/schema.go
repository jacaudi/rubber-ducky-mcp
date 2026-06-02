package main

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/jacaudi/critical-thinking/internal/thinking"
	"github.com/spf13/cobra"
)

// toolDescriptor mirrors what MCP clients receive via tools/list, so a model
// driving the CLI sees the same contract.
type toolDescriptor struct {
	Name         string             `json:"name"`
	Description  string             `json:"description"`
	InputSchema  *jsonschema.Schema `json:"inputSchema"`
	OutputSchema *jsonschema.Schema `json:"outputSchema"`
}

// printSchema writes the criticalthinking tool contract (description + JSON
// Schemas) to w as pretty JSON. Schemas come from jsonschema.For[T](nil),
// which defaults to &jsonschema.ForOptions{} — the same call the MCP SDK makes
// internally (jsonschema.ForType(rt, &jsonschema.ForOptions{})). The
// inputSchema is therefore byte-identical to what MCP clients receive in
// tools/list. The outputSchema is what the SDK would generate for
// ThoughtResponse (and what the tool's structuredContent conforms to); the
// MCP tool itself advertises no output schema, since its handler output type
// is `any`.
func printSchema(w io.Writer) error {
	in, err := jsonschema.For[thinking.ThoughtData](nil)
	if err != nil {
		return fmt.Errorf("input schema: %w", err)
	}
	out, err := jsonschema.For[thinking.ThoughtResponse](nil)
	if err != nil {
		return fmt.Errorf("output schema: %w", err)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(toolDescriptor{
		Name:         "criticalthinking",
		Description:  thinking.ToolDescription,
		InputSchema:  in,
		OutputSchema: out,
	})
}

// newSchemaCmd prints the criticalthinking tool contract and exits. On failure
// it writes a `schema:`-prefixed diagnostic to stderr and returns the error for
// a non-zero exit; stdout carries only the schema JSON.
func newSchemaCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "schema",
		Short: "Print the criticalthinking tool contract (description + JSON Schemas)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := printSchema(cmd.OutOrStdout()); err != nil {
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "schema:", err)
				return err
			}
			return nil
		},
	}
}
