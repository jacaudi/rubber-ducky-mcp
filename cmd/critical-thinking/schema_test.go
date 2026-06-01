package main

import (
	"bytes"
	"testing"
)

// TestPrintSchema is relocated from main_test.go; printSchema now lives in schema.go.
func TestPrintSchema(t *testing.T) {
	var buf bytes.Buffer
	if err := printSchema(&buf); err != nil {
		t.Fatalf("printSchema: %v", err)
	}
	got := buf.String()
	for _, want := range []string{`"name": "criticalthinking"`, `"inputSchema"`, `"outputSchema"`} {
		if !bytes.Contains(buf.Bytes(), []byte(want)) {
			t.Errorf("schema output missing %q\ngot: %s", want, got)
		}
	}
}

// TestSchemaCmdMatchesPrintSchema asserts the subcommand emits exactly what
// printSchema writes (pin: schema output matches printSchema).
func TestSchemaCmdMatchesPrintSchema(t *testing.T) {
	var want bytes.Buffer
	if err := printSchema(&want); err != nil {
		t.Fatalf("printSchema: %v", err)
	}

	cmd := newSchemaCmd()
	var got bytes.Buffer
	cmd.SetOut(&got)
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got.String() != want.String() {
		t.Errorf("schema subcommand output != printSchema output\ngot:  %s\nwant: %s", got.String(), want.String())
	}
}
