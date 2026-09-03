// Package danger detects destructive operations over command segments.
//
// Matching is structural (head + flags + targets), not regex-over-strings,
// so the pattern table is data and its recall over the table itself is
// machine-checked in Lean 4 (proofs/Shpreflight.lean): every pattern in
// the table is matched by at least one concrete command — the detector
// cannot have a dead entry, i.e. known-dangerous commands are never
// silently unmatchable.
package danger

import (
	"fmt"
	"strings"

	"github.com/HZDF-2026/shpreflight/internal/report"
	"github.com/HZDF-2026/shpreflight/internal/segments"
)

var shellHeads = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "pwsh": true,
	"powershell": true, "dash": true,
}

var execHeads = map[string]bool{
	"iex": true, "Invoke-Expression": true, "eval": true,
}

var sensitiveBases = []string{
	".env", ".env.local", ".env.production", "secrets",
	"id_rsa", "id_ed25519", "credentials", "credentials.json",
	".npmrc", ".netrc", ".aws/credentials",
}

// Pattern is one runtime danger pattern, built from the generated table.
type Pattern struct {
	Code     string
	Severity string
	Heads    []string
	Flags    []string
	Targets  []string
	Message  string
	Fix      string
}

// Patterns mirrors shpreflight.danger.PATTERNS entry for entry.
var Patterns = buildPatterns()

func buildPatterns() []Pattern {
	ps := make([]Pattern, 0, len(patternTable))
	for _, d := range patternTable {
		ps = append(ps, Pattern{
			Code: d.code, Severity: d.severity,
			Heads: d.heads, Flags: d.flags, Targets: d.targets,
			Message: d.message, Fix: d.fix,
		})
	}
	return ps
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func anyIn(rest, set []string) bool {
	for _, r := range rest {
		if contains(set, r) {
			return true
		}
	}
	return false
}

// MatchPattern is the structural match: head, then any-of flags, then
// any-of targets.
func MatchPattern(words []string, p Pattern) bool {
	if len(words) == 0 {
		return false
	}
	if !contains(p.Heads, words[0]) {
		return false
	}
	rest := words[1:]
	if len(p.Flags) > 0 && !anyIn(rest, p.Flags) {
		return false
	}
	if len(p.Targets) > 0 && !anyIn(rest, p.Targets) {
		return false
	}
	return true
}

func basename(path string) string {
	for _, sep := range []string{"/", "\\"} {
		if i := strings.LastIndex(path, sep); i >= 0 {
			path = path[i+1:]
		}
	}
	return path
}

// CheckDanger scans segments for destructive operations.
func CheckDanger(segs []segments.Segment) []report.Issue {
	var issues []report.Issue
	pipeReceiver := false
	for _, seg := range segs {
		head := seg.Head
		if head != "" {
			if pipeReceiver && (shellHeads[head] || execHeads[head]) {
				issues = append(issues, report.Issue{
					Code:     "PIPE-EXEC",
					Severity: "error",
					Kind:     "danger",
					Message: fmt.Sprintf("pipe feeds untrusted input into '%s' — remote-code-execution pattern (curl ... | sh)", head),
					Fix:      "download first, inspect, then run",
				})
			}
			if pipeReceiver && head == "npm" && contains(seg.Words, "publish") {
				issues = append(issues, report.Issue{
					Code:     "NPM-PUBLISH",
					Severity: "warning",
					Kind:     "danger",
					Message:  "publishing to the npm registry makes the package public",
					Fix:      "use npm publish --dry-run until content is final",
				})
			}
		}
		hitCodes := map[string]bool{}
		for _, p := range Patterns {
			if MatchPattern(seg.Words, p) {
				// RM-ROOT already says everything about this segment's rm
				if p.Code == "RM-RECURSIVE" && hitCodes["RM-ROOT"] {
					continue
				}
				hitCodes[p.Code] = true
				issues = append(issues, report.Issue{
					Code:     p.Code,
					Severity: p.Severity,
					Kind:     "danger",
					Message:  fmt.Sprintf("%s: %s", seg.Words[0], p.Message),
					Fix:      p.Fix,
				})
			}
		}
		for _, redir := range seg.Redirects {
			if contains(sensitiveBases, basename(redir)) ||
				strings.HasSuffix(redir, ".key") || strings.HasSuffix(redir, ".pem") {
				issues = append(issues, report.Issue{
					Code:     "REDIR-SENSITIVE",
					Severity: "warning",
					Kind:     "danger",
					Message:  fmt.Sprintf("overwriting sensitive file '%s' via redirection", redir),
					Fix:      "write to a temp file and diff first",
				})
			}
		}
		pipeReceiver = seg.PipesOut()
	}
	return issues
}
