// Package report holds the findings model and rendering: JSON for agents,
// text for humans.
package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

const (
	VerdictOK   = "ok"
	VerdictWarn = "warn"
	VerdictFail = "fail"

	ExitOK   = 0
	ExitWarn = 1
	ExitFail = 2
)

var severityRank = map[string]int{"error": 2, "warning": 1, "info": 0}

// Issue is one finding.
type Issue struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Kind     string `json:"kind"`
	Message  string `json:"message"`
	Fix      string `json:"fix,omitempty"`
	Tool     string `json:"tool,omitempty"`
}

// ToolInfo is the PATH resolution result for one command head.
type ToolInfo struct {
	Name   string  `json:"name"`
	Status string  `json:"status"`
	Path   *string `json:"path"`
}

// Report is the full pre-flight result.
type Report struct {
	Command   string     `json:"command"`
	Target    string     `json:"target"`
	Verdict   string     `json:"verdict"`
	Errors    int        `json:"errors"`
	Warnings  int        `json:"warnings"`
	Issues    []Issue    `json:"issues"`
	Tools     []ToolInfo `json:"tools"`
	ElapsedMS float64    `json:"elapsed_ms"`
}

var verdictByRank = [...]string{VerdictOK, VerdictWarn, VerdictFail}

// New assembles a Report, deriving verdict and counts from the issues.
func New(command, target string, issues []Issue, tools []ToolInfo, elapsedMS float64) *Report {
	if issues == nil {
		issues = []Issue{}
	}
	if tools == nil {
		tools = []ToolInfo{}
	}
	worst := 0
	errors, warnings := 0, 0
	for _, i := range issues {
		if rank := severityRank[i.Severity]; rank > worst {
			worst = rank
		}
		switch i.Severity {
		case "error":
			errors++
		case "warning":
			warnings++
		}
	}
	return &Report{
		Command:   command,
		Target:    target,
		Verdict:   verdictByRank[worst],
		Errors:    errors,
		Warnings:  warnings,
		Issues:    issues,
		Tools:     tools,
		ElapsedMS: math.Round(elapsedMS*1000) / 1000,
	}
}

// ExitCode is machine-readable so agents can branch on it:
// 0 ok, 1 warn, 2 fail.
func (r *Report) ExitCode() int {
	switch r.Verdict {
	case VerdictWarn:
		return ExitWarn
	case VerdictFail:
		return ExitFail
	}
	return ExitOK
}

// ToJSON renders the agent-facing report. Field order and escaping match
// the Python reference implementation (indent 2, non-ASCII kept raw).
func (r *Report) ToJSON() string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(r); err != nil {
		return ""
	}
	return strings.TrimRight(buf.String(), "\n")
}

// ToText renders the human-facing summary.
func (r *Report) ToText() string {
	var b strings.Builder
	fmt.Fprintf(&b, "shpreflight: %s (%d error(s), %d warning(s)) for %s\n",
		r.Verdict, r.Errors, r.Warnings, r.Target)
	for _, i := range r.Issues {
		fmt.Fprintf(&b, "  %s [%s] %s: %s\n", i.Code, i.Severity, i.Kind, i.Message)
		if i.Fix != "" {
			fmt.Fprintf(&b, "    fix: %s\n", i.Fix)
		}
	}
	for _, tl := range r.Tools {
		if tl.Status == "missing" {
			fmt.Fprintf(&b, "  %s: not found on PATH\n", tl.Name)
		}
	}
	if len(r.Issues) == 0 {
		b.WriteString("  no issues found\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
