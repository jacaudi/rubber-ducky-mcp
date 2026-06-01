package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jacaudi/critical-thinking/internal/thinking"
)

func TestRunCLIJSONOutput(t *testing.T) {
	in := `{"thought":"x","thoughtNumber":1,"totalThoughts":1,"nextThoughtNeeded":false,"confidence":0.5,"assumptions":[],"critique":"c","counterArgument":"ca"}` + "\n"
	var out, errb bytes.Buffer
	code := runCLI(strings.NewReader(in), &out, &errb, true)
	if code != 0 {
		t.Fatalf("exit = %d; stderr = %s", code, errb.String())
	}
	var resp thinking.ThoughtResponse
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &resp); err != nil {
		t.Fatalf("stdout is not NDJSON ThoughtResponse: %v\n%s", err, out.String())
	}
	if resp.ThoughtNumber != 1 || resp.ThoughtHistoryLength != 1 {
		t.Errorf("resp = %+v", resp)
	}
}

func TestRunCLITranscriptAndAccumulation(t *testing.T) {
	in := strings.Join([]string{
		`{"thought":"first","thoughtNumber":1,"totalThoughts":2,"nextThoughtNeeded":true,"confidence":0.5,"assumptions":[],"critique":"c","counterArgument":"ca","nextStepRationale":"continue"}`,
		`{"thought":"second","thoughtNumber":2,"totalThoughts":2,"nextThoughtNeeded":false,"confidence":0.7,"assumptions":[],"critique":"c2","counterArgument":"ca2"}`,
	}, "\n") + "\n"

	var out, errb bytes.Buffer
	code := runCLI(strings.NewReader(in), &out, &errb, false)
	if code != 0 {
		t.Fatalf("exit = %d; stderr = %s", code, errb.String())
	}
	s := out.String()
	if !strings.Contains(s, "Thought 1 of 2") || !strings.Contains(s, "Thought 2 of 2") {
		t.Errorf("missing thought headers:\n%s", s)
	}
	if !strings.Contains(s, "across 2 thoughts") {
		t.Errorf("expected accumulated footer 'across 2 thoughts':\n%s", s)
	}
}

func TestRunCLIMalformedLineContinues(t *testing.T) {
	in := "{not json\n" +
		`{"thought":"ok","thoughtNumber":1,"totalThoughts":1,"nextThoughtNeeded":false,"confidence":0.5,"assumptions":[],"critique":"c","counterArgument":"ca"}` + "\n"
	var out, errb bytes.Buffer
	code := runCLI(strings.NewReader(in), &out, &errb, false)
	if code != 1 {
		t.Errorf("exit = %d; want 1", code)
	}
	if !strings.Contains(errb.String(), "line 1") {
		t.Errorf("stderr should reference line 1: %q", errb.String())
	}
	if !strings.Contains(out.String(), "Thought 1 of 1") {
		t.Errorf("a valid line after a bad one must still render:\n%s", out.String())
	}
}

func TestRunCLIValidationErrorRouting(t *testing.T) {
	// Missing required "critique" → validation error result (IsError).
	bad := `{"thought":"x","thoughtNumber":1,"totalThoughts":1,"nextThoughtNeeded":false,"confidence":0.5,"assumptions":[],"counterArgument":"ca"}` + "\n"

	// default mode: error JSON to stderr, nothing to stdout.
	var out, errb bytes.Buffer
	if code := runCLI(strings.NewReader(bad), &out, &errb, false); code != 1 {
		t.Errorf("default mode exit = %d; want 1", code)
	}
	if out.Len() != 0 || !strings.Contains(errb.String(), `"status":"failed"`) {
		t.Errorf("default: want error JSON on stderr only; out=%q err=%q", out.String(), errb.String())
	}

	// -json mode: error JSON to stdout (keeps the NDJSON stream complete).
	var out2, errb2 bytes.Buffer
	if code := runCLI(strings.NewReader(bad), &out2, &errb2, true); code != 1 {
		t.Errorf("json mode exit = %d; want 1", code)
	}
	if !strings.Contains(out2.String(), `"status":"failed"`) || errb2.Len() != 0 {
		t.Errorf("json: error JSON should go to stdout only; out=%q err=%q", out2.String(), errb2.String())
	}
}

func TestRunCLIBlankAndEmpty(t *testing.T) {
	var out, errb bytes.Buffer
	if code := runCLI(strings.NewReader("\n   \n"), &out, &errb, false); code != 0 || out.Len() != 0 {
		t.Errorf("blank/empty: code=%d out=%q", code, out.String())
	}
}

// TestCliCmdExitsNonZeroOnAnyFailureAfterProcessingAll proves pin 1 at the
// subcommand layer: a bad line followed by a good line returns errCLIFailed
// (→ exit 1 in main) AND still emits the good line's output.
func TestCliCmdExitsNonZeroOnAnyFailureAfterProcessingAll(t *testing.T) {
	cmd := newCliCmd()
	cmd.SetIn(strings.NewReader("garbage\n" + `{"thought":"t","thoughtNumber":1,"totalThoughts":3,"nextThoughtNeeded":true,"confidence":0.5,"assumptions":[],"critique":"c","counterArgument":"x","nextStepRationale":"n"}` + "\n"))
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"--json"})

	err := cmd.Execute()
	if !errors.Is(err, errCLIFailed) {
		t.Fatalf("Execute() err = %v, want errCLIFailed", err)
	}
	if !strings.Contains(out.String(), `"thoughtHistoryLength"`) {
		t.Errorf("good line not processed (fail-fast?): %s", out.String())
	}
}

func TestCliCmdSuccessReturnsNil(t *testing.T) {
	cmd := newCliCmd()
	cmd.SetIn(strings.NewReader(`{"thought":"t","thoughtNumber":1,"totalThoughts":3,"nextThoughtNeeded":true,"confidence":0.5,"assumptions":[],"critique":"c","counterArgument":"x","nextStepRationale":"n"}` + "\n"))
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() err = %v, want nil", err)
	}
}
