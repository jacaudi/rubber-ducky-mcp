package main

import (
	"testing"
)

func TestServeCmdDefaultsToStdio(t *testing.T) {
	cmd := newServeCmd()

	var stdioCalled bool
	var httpAddr string
	cmd.stdioRun = func() { stdioCalled = true }
	cmd.httpRun = func(addr string) { httpAddr = addr }

	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !stdioCalled {
		t.Error("bare `serve` should run stdio")
	}
	if httpAddr != "" {
		t.Errorf("bare `serve` should not run HTTP; got addr %q", httpAddr)
	}
}

func TestServeCmdHTTPWhenFlagSet(t *testing.T) {
	cmd := newServeCmd()

	var stdioCalled bool
	var httpAddr string
	cmd.stdioRun = func() { stdioCalled = true }
	cmd.httpRun = func(addr string) { httpAddr = addr }

	cmd.SetArgs([]string{"--http", ":3000"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if stdioCalled {
		t.Error("`serve --http` should not run stdio")
	}
	if httpAddr != ":3000" {
		t.Errorf("httpRun addr = %q, want :3000", httpAddr)
	}
}
