package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func runCheckCLI(t *testing.T, args []string, stdin string) (int, string, string) {
	t.Helper()
	var out, errBuf bytes.Buffer
	rc := run(args, strings.NewReader(stdin), &out, &errBuf)
	return rc, out.String(), errBuf.String()
}

func TestCheckText(t *testing.T) {
	rc, out, _ := runCheckCLI(t, []string{"check", "a && b", "--shell", "powershell5"}, "")
	if rc != 2 {
		t.Errorf("rc = %d, want 2", rc)
	}
	if !strings.Contains(out, "SEP-AND") {
		t.Errorf("out missing SEP-AND: %q", out)
	}
}

func TestCheckJSON(t *testing.T) {
	rc, out, _ := runCheckCLI(t,
		[]string{"check", "rm", "-rf", "/", "--shell", "bash", "--format", "json"}, "")
	if rc != 2 {
		t.Errorf("rc = %d, want 2", rc)
	}
	var d map[string]any
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if d["verdict"] != "fail" {
		t.Errorf("verdict = %v", d["verdict"])
	}
	issues := d["issues"].([]any)
	hasRMRoot := false
	for _, iss := range issues {
		m := iss.(map[string]any)
		if m["code"] == "RM-ROOT" {
			hasRMRoot = true
			if _, ok := m["fix"]; !ok {
				t.Error("RM-ROOT issue missing fix")
			}
		}
	}
	if !hasRMRoot {
		t.Errorf("RM-ROOT missing: %s", out)
	}
}

func TestCheckStdin(t *testing.T) {
	rc, out, _ := runCheckCLI(t,
		[]string{"check", "--stdin", "--shell", "cmd", "--no-path-check"}, "grep x f\n")
	if rc != 2 {
		t.Errorf("rc = %d, want 2", rc)
	}
	if !strings.Contains(out, "POSIX-CMD") {
		t.Errorf("out missing POSIX-CMD: %q", out)
	}
}

func TestCheckEmpty(t *testing.T) {
	rc, _, errOut := runCheckCLI(t, []string{"check"}, "")
	if rc != 3 || !strings.Contains(errOut, "no command given") {
		t.Errorf("rc = %d, err = %q", rc, errOut)
	}
	rc, _, errOut = runCheckCLI(t, []string{"check", "   "}, "")
	if rc != 3 || !strings.Contains(errOut, "empty command") {
		t.Errorf("rc = %d, err = %q", rc, errOut)
	}
}

func TestShellsCmd(t *testing.T) {
	rc, out, _ := runCheckCLI(t, []string{"shells"}, "")
	if rc != 0 {
		t.Errorf("rc = %d, want 0", rc)
	}
	if !strings.Contains(out, "powershell5") {
		t.Errorf("out missing powershell5: %q", out)
	}
	var d map[string]any
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(d) != 5 {
		t.Errorf("expected 5 shells, got %d", len(d))
	}
}

func TestMultiWordCommandArgument(t *testing.T) {
	rc, out, _ := runCheckCLI(t, []string{"check", "echo", "hi", "--shell", "bash"}, "")
	if rc != 0 {
		t.Errorf("rc = %d, want 0; out=%q", rc, out)
	}
}

func TestCommandFlagsPassThrough(t *testing.T) {
	// "-rf" belongs to the command, not to shpreflight
	rc, out, _ := runCheckCLI(t,
		[]string{"check", "rm", "-rf", "/", "--shell", "bash", "--no-path-check"}, "")
	if rc != 2 {
		t.Errorf("rc = %d, want 2", rc)
	}
	if !strings.Contains(out, "RM-ROOT") {
		t.Errorf("out missing RM-ROOT: %q", out)
	}
}

func TestUnknownShell(t *testing.T) {
	rc, _, errOut := runCheckCLI(t, []string{"check", "echo hi", "--shell", "fish"}, "")
	if rc != 3 {
		t.Errorf("rc = %d, want 3", rc)
	}
	if !strings.Contains(errOut, "unknown shell 'fish'") {
		t.Errorf("err = %q", errOut)
	}
}

func TestBadFormat(t *testing.T) {
	rc, _, errOut := runCheckCLI(t,
		[]string{"check", "echo hi", "--format", "yaml"}, "")
	if rc != 3 {
		t.Errorf("rc = %d, want 3", rc)
	}
	if !strings.Contains(errOut, "invalid --format choice") {
		t.Errorf("err = %q", errOut)
	}
}

func TestNoArgsUsage(t *testing.T) {
	rc, _, errOut := runCheckCLI(t, nil, "")
	if rc != 3 || !strings.Contains(errOut, "usage:") {
		t.Errorf("rc = %d, err = %q", rc, errOut)
	}
}

func TestRealSessionRegression(t *testing.T) {
	// the exact command that failed at the start of the session this tool
	// was conceived in: PS 5.1 + && + git missing from PATH
	rc, out, _ := runCheckCLI(t,
		[]string{"check", "git diff --stat && git log --oneline -5",
			"--shell", "powershell5"}, "")
	if rc != 2 {
		t.Errorf("rc = %d, want 2", rc)
	}
	if !strings.Contains(out, "SEP-AND") {
		t.Errorf("out missing SEP-AND: %q", out)
	}
}

func TestJSONFormatFlagEquals(t *testing.T) {
	rc, out, _ := runCheckCLI(t,
		[]string{"check", "--shell=bash", "--no-path-check", "--format=json", "rm -rf /"}, "")
	if rc != 2 || !strings.Contains(out, `"verdict": "fail"`) {
		t.Errorf("rc = %d, out = %q", rc, out)
	}
}
