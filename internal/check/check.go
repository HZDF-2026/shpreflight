// Package check is the preflight orchestration:
// lex -> segment -> dialect + danger + tools.
package check

import (
	"fmt"
	"time"

	"github.com/HZDF-2026/shpreflight/internal/danger"
	"github.com/HZDF-2026/shpreflight/internal/lex"
	"github.com/HZDF-2026/shpreflight/internal/report"
	"github.com/HZDF-2026/shpreflight/internal/rules"
	"github.com/HZDF-2026/shpreflight/internal/segments"
	"github.com/HZDF-2026/shpreflight/internal/shells"
	"github.com/HZDF-2026/shpreflight/internal/tools"
)

// noDowngrade lists POSIX names whose Windows PATH namesakes do something
// different, so a PATH hit is not evidence the command will do what the
// agent meant.
var noDowngrade = map[string]bool{"find": true}

// Preflight runs the full diagnostic over one command.
func Preflight(cmd string, target string, pathCheck bool) (*report.Report, error) {
	start := time.Now()
	shell, err := shells.ResolveTarget(target)
	if err != nil {
		return nil, err
	}
	tokens := lex.Lex(cmd)
	segs := segments.SplitSegments(tokens)

	issues := []report.Issue{}
	issues = append(issues, rules.CheckDialect(tokens, segs, shell)...)
	issues = append(issues, danger.CheckDanger(segs)...)
	toolInfos := tools.CheckTools(segs, pathCheck)

	// A tool reported by rules as POSIX-only but actually present on PATH
	// (Git Bash in PATH etc.) downgrades from hard failure to a note:
	// the command will run, just not natively.
	if len(toolInfos) > 0 {
		found := map[string]bool{}
		for _, tl := range toolInfos {
			if tl.Status == "found" {
				found[tl.Name] = true
			}
		}
		for i := range issues {
			iss := &issues[i]
			if iss.Code == "POSIX-CMD" && found[iss.Tool] && !noDowngrade[iss.Tool] {
				iss.Severity = "info"
				iss.Message += " (present on PATH, non-native)"
			}
		}
	}

	// Anything reported missing on PATH gets an explicit issue, since
	// 'command not found' is the single largest failure mode for agents.
	if pathCheck {
		for _, tl := range toolInfos {
			if tl.Status == "missing" && !tools.Builtins[tl.Name] {
				if hasPosixCmdIssue(issues, tl.Name) {
					continue
				}
				issues = append(issues, report.Issue{
					Code:     "TOOL-NOT-FOUND",
					Severity: "error",
					Kind:     "tool",
					Message:  fmt.Sprintf("'%s' was not found on PATH — this is a 'command not found' failure", tl.Name),
					Fix:      "install it, or use the native equivalent",
					Tool:     tl.Name,
				})
			}
		}
	}

	elapsed := float64(time.Since(start).Nanoseconds()) / 1e6
	return report.New(cmd, shell, issues, toolInfos, elapsed), nil
}

func hasPosixCmdIssue(issues []report.Issue, name string) bool {
	for _, i := range issues {
		if i.Code == "POSIX-CMD" && i.Tool == name {
			return true
		}
	}
	return false
}
