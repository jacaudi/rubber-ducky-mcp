package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestVersionTextContainsAllFields(t *testing.T) {
	txt := versionText()
	for _, want := range []string{version, commit, date} {
		if !strings.Contains(txt, want) {
			t.Errorf("versionText() = %q, missing %q", txt, want)
		}
	}
	if strings.HasSuffix(txt, "\n") {
		t.Errorf("versionText() must not end with a newline; got %q", txt)
	}
}

func TestVersionCmdTextOutput(t *testing.T) {
	cmd := newVersionCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	got := strings.TrimRight(out.String(), "\n")
	if got != versionText() {
		t.Errorf("version output = %q, want %q", got, versionText())
	}
}

func TestVersionCmdJSONOutput(t *testing.T) {
	cmd := newVersionCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	var got struct {
		Version string `json:"version"`
		Commit  string `json:"commit"`
		Date    string `json:"date"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\nbody=%s", err, out.String())
	}
	if got.Version != version || got.Commit != commit || got.Date != date {
		t.Errorf("json = %+v, want {%s %s %s}", got, version, commit, date)
	}
}
