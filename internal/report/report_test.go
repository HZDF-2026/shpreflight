package report

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestToJSONShape(t *testing.T) {
	rep := New("rm -rf /", "bash", []Issue{
		{Code: "RM-ROOT", Severity: "error", Kind: "danger",
			Message: "rm: recursive force-delete aimed at a root/home/glob path",
			Fix:     "restrict the target path; verify with --dry-run or -WhatIf"},
	}, []ToolInfo{{Name: "rm", Status: "missing", Path: nil}}, 0.0625)

	var d map[string]any
	if err := json.Unmarshal([]byte(rep.ToJSON()), &d); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if d["verdict"] != "fail" || d["target"] != "bash" {
		t.Errorf("verdict/target = %v/%v", d["verdict"], d["target"])
	}
	if d["errors"] != float64(1) || d["warnings"] != float64(0) {
		t.Errorf("errors/warnings = %v/%v", d["errors"], d["warnings"])
	}
	if _, ok := d["command"]; !ok {
		t.Error("missing command key")
	}
	tools := d["tools"].([]any)
	t0 := tools[0].(map[string]any)
	if t0["path"] != nil {
		t.Errorf("path should be null, got %v", t0["path"])
	}
}

func TestToJSONEmptyListsNotNull(t *testing.T) {
	rep := New("ls", "bash", nil, nil, 0)
	if strings.Contains(rep.ToJSON(), "null") {
		t.Errorf("empty lists must render as [], got: %s", rep.ToJSON())
	}
}

func TestVerdictsAndExitCodes(t *testing.T) {
	cases := []struct {
		issues []Issue
		verdict string
		exit    int
	}{
		{nil, "ok", ExitOK},
		{[]Issue{{Code: "X", Severity: "info", Kind: "syntax", Message: "m"}}, "ok", ExitOK},
		{[]Issue{{Code: "X", Severity: "warning", Kind: "syntax", Message: "m"}}, "warn", ExitWarn},
		{[]Issue{{Code: "X", Severity: "error", Kind: "syntax", Message: "m"}}, "fail", ExitFail},
		{[]Issue{
			{Code: "X", Severity: "warning", Kind: "syntax", Message: "m"},
			{Code: "Y", Severity: "error", Kind: "syntax", Message: "m"},
		}, "fail", ExitFail},
	}
	for _, c := range cases {
		rep := New("c", "bash", c.issues, nil, 0)
		if rep.Verdict != c.verdict || rep.ExitCode() != c.exit {
			t.Errorf("issues %+v: verdict=%s exit=%d, want %s/%d",
				c.issues, rep.Verdict, rep.ExitCode(), c.verdict, c.exit)
		}
	}
}

func TestToTextMentionsFix(t *testing.T) {
	rep := New("a && b", "powershell5", []Issue{
		{Code: "SEP-AND", Severity: "error", Kind: "syntax",
			Message: "operator '&&' is not supported in this shell",
			Fix:     "separate commands, or chain with ';' if order alone matters"},
	}, nil, 0)
	out := rep.ToText()
	if !strings.Contains(out, "SEP-AND") || !strings.Contains(out, "fix:") {
		t.Errorf("text output missing code/fix: %q", out)
	}
	if !strings.Contains(out, "shpreflight: fail (1 error(s), 0 warning(s)) for powershell5") {
		t.Errorf("text header wrong: %q", out)
	}
}

func TestToTextNoIssues(t *testing.T) {
	out := New("ls", "bash", nil, nil, 0).ToText()
	if !strings.Contains(out, "no issues found") {
		t.Errorf("expected 'no issues found': %q", out)
	}
}

func TestElapsedRounded(t *testing.T) {
	rep := New("ls", "bash", nil, nil, 0.123456)
	if rep.ElapsedMS != 0.123 {
		t.Errorf("ElapsedMS = %v, want 0.123", rep.ElapsedMS)
	}
}
