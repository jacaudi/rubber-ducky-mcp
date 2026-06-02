package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRootBareShowsHelpAndExitsZero(t *testing.T) {
	cmd := newRootCmd()
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("bare root Execute() err = %v, want nil (exit 0)", err)
	}
	if !strings.Contains(out.String(), "Available Commands") {
		t.Errorf("bare root should print help; got: %s", out.String())
	}
	// D5: no auto-stdio — help text mentions the serve subcommand instead.
	if !strings.Contains(out.String(), "serve") {
		t.Errorf("help should list the serve subcommand; got: %s", out.String())
	}
}

func TestRootVersionFlagMatchesVersionText(t *testing.T) {
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--version"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("--version Execute() err = %v", err)
	}
	got := strings.TrimRight(out.String(), "\n")
	if got != versionText() {
		t.Errorf("--version output = %q, want %q", got, versionText())
	}
}

func TestRootSubcommandsRegistered(t *testing.T) {
	cmd := newRootCmd()
	want := map[string]bool{"serve": false, "cli": false, "schema": false, "version": false}
	for _, sub := range cmd.Commands() {
		if _, ok := want[sub.Name()]; ok {
			want[sub.Name()] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("subcommand %q not registered", name)
		}
	}
}

// TestRootErrorPathKeepsStdoutClean proves pin 2: when a subcommand RunE
// errors, nothing reaches stdout (errors/usage go to stderr only).
func TestRootErrorPathKeepsStdoutClean(t *testing.T) {
	cmd := newRootCmd()
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	// `cli` with a malformed line returns errCLIFailed.
	cmd.SetIn(strings.NewReader("garbage\n"))
	cmd.SetArgs([]string{"cli"})

	_ = cmd.Execute() // returns errCLIFailed; main would exit 1.
	if out.Len() != 0 {
		t.Errorf("stdout must stay clean on error path; got: %q", out.String())
	}
}

// TestRootUnknownCommandWritesStderrNotStdout proves the M1 refinement:
// SilenceErrors=false routes an unknown-command error to stderr (a helpful
// message) while SilenceUsage=true + stderr routing keep stdout clean (pin 2).
func TestRootUnknownCommandWritesStderrNotStdout(t *testing.T) {
	cmd := newRootCmd()
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"bogus"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("unknown command should return an error")
	}
	if out.Len() != 0 {
		t.Errorf("stdout must stay clean on unknown-command; got: %q", out.String())
	}
	if !strings.Contains(errBuf.String(), "unknown command") {
		t.Errorf("stderr should describe the unknown command; got: %q", errBuf.String())
	}
}
