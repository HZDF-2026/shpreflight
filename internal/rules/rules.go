// Package rules implements dialect-compatibility checks: will this command
// actually run on the target shell?
//
// Each rule is triggered by concrete token evidence and fires only for the
// shells where it genuinely breaks. Severity is "error" when the command
// will fail (or silently do the wrong thing) and "warning" when it merely
// smells wrong.
package rules

import (
	"fmt"
	"strings"

	"github.com/HZDF-2026/shpreflight/internal/lex"
	"github.com/HZDF-2026/shpreflight/internal/report"
	"github.com/HZDF-2026/shpreflight/internal/segments"
	"github.com/HZDF-2026/shpreflight/internal/shells"
)

var specialVars = []string{
	"$?", "$!", "$$", "$#", "$@", "$*",
	"$0", "$1", "$2", "$3", "$4", "$5", "$6", "$7", "$8", "$9",
}

// POSIX commands with no Windows PowerShell 5.1 equivalent alias, plus the
// native replacement agents should switch to.
var posixToPS = map[string]string{
	"grep":    "Select-String (sls)",
	"sed":     "-replace operator",
	"awk":     "ForEach-Object + split",
	"find":    "Get-ChildItem -Recurse",
	"head":    "Select-Object -First N",
	"tail":    "Get-Content -Tail N",
	"touch":   "New-Item -ItemType File",
	"chmod":   "icacls",
	"chown":   "icacls",
	"which":   "Get-Command",
	"xargs":   "ForEach-Object",
	"df":      "Get-PSDrive",
	"du":      "Get-ChildItem -Recurse | Measure-Object Length -Sum",
	"wc":      "Measure-Object -Line/-Word/-Character",
	"tr":      "-replace / .ToLower",
	"cut":     ".Split()",
	"uniq":    "Select-Object -Unique",
	"uname":   "$env:OS",
	"env":     "Get-ChildItem env:",
	"basename": "Split-Path -Leaf",
	"dirname": "Split-Path -Parent",
	"ln":      "New-Item -ItemType SymbolicLink",
	"readlink": "(Get-Item).Target",
	"mktemp":  "New-TemporaryFile",
	"stat":    "Get-Item",
	"less":    "more.com / Out-Host -Paging",
	"open":    "Start-Process (alias on macOS only)",
	"make":    "external: install via winget",
}

var rmShortFlags = []string{"-r", "-f", "-rf", "-fr", "-Rf", "-R"}

func isLetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isIdentStart(c byte) bool {
	return c == '_' || isLetter(c)
}

func isIdentByte(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9')
}

// varRe fullmatches \$[A-Za-z_][A-Za-z0-9_]*$ — a bare bash variable.
func varRe(s string) bool {
	if len(s) < 2 || s[0] != '$' || !isIdentStart(s[1]) {
		return false
	}
	for i := 2; i < len(s); i++ {
		if !isIdentByte(s[i]) {
			return false
		}
	}
	return true
}

// braceVarRe fullmatches \$\{[A-Za-z_][A-Za-z0-9_]*\}$.
func braceVarRe(s string) bool {
	if len(s) < 4 || !strings.HasPrefix(s, "${") || !strings.HasSuffix(s, "}") {
		return false
	}
	inner := s[2 : len(s)-1]
	if !isIdentStart(inner[0]) {
		return false
	}
	for i := 1; i < len(inner); i++ {
		if !isIdentByte(inner[i]) {
			return false
		}
	}
	return true
}

// cmletRe fullmatches [A-Z][A-Za-z]+-[A-Z][A-Za-z]+$ — a PowerShell cmdlet
// name like Get-ChildItem.
func cmletRe(s string) bool {
	n := len(s)
	if n < 5 || s[0] < 'A' || s[0] > 'Z' {
		return false
	}
	i := 1
	letters := 0
	for i < n && isLetter(s[i]) {
		i++
		letters++
	}
	if letters == 0 || i >= n || s[i] != '-' {
		return false
	}
	i++
	if i >= n || s[i] < 'A' || s[i] > 'Z' {
		return false
	}
	i++
	letters = 0
	for i < n && isLetter(s[i]) {
		i++
		letters++
	}
	return i == n && letters > 0
}

// curlFlags reproduces re.match(r"^-[a-zA-Z]+|^--[a-zA-Z-]+$"): curl-style
// flags, which the Invoke-WebRequest alias rejects outright.
func curlFlags(w string) bool {
	if strings.HasPrefix(w, "--") {
		rest := w[2:]
		if rest == "" {
			return false
		}
		for i := 0; i < len(rest); i++ {
			if !isLetter(rest[i]) && rest[i] != '-' {
				return false
			}
		}
		return true
	}
	if strings.HasPrefix(w, "-") {
		return len(w) >= 2 && isLetter(w[1])
	}
	return false
}

// pyRepr renders s the way Python's repr() would, for messages that embed
// a quoted fragment.
func pyRepr(s string) string {
	hasSingle := strings.ContainsRune(s, '\'')
	hasDouble := strings.ContainsRune(s, '"')
	quote := byte('\'')
	if hasSingle && !hasDouble {
		quote = '"'
	}
	var b strings.Builder
	b.WriteByte(quote)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\\':
			b.WriteString(`\\`)
		case quote:
			b.WriteByte('\\')
			b.WriteByte(c)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte(quote)
	return b.String()
}

func head12(s string) string {
	r := []rune(s)
	if len(r) > 12 {
		r = r[:12]
	}
	return string(r)
}

func anyCurlFlags(words []string) bool {
	for _, w := range words {
		if curlFlags(w) {
			return true
		}
	}
	return false
}

func anyIn(words, set []string) bool {
	for _, w := range words {
		for _, s := range set {
			if w == s {
				return true
			}
		}
	}
	return false
}

func isCmdHead(head string) bool {
	return head == "powershell5" || head == "cmd"
}

// CheckDialect runs every dialect rule over the token stream.
func CheckDialect(tokens []lex.Token, segs []segments.Segment, target string) []report.Issue {
	var issues []report.Issue
	ps := shells.IsPowerShell(target)
	win := shells.IsWindowsShell(target)
	onBash := target == "bash" || target == "sh"

	for _, tok := range tokens {
		if (tok.Kind == lex.SQuote || tok.Kind == lex.DQuote || tok.Kind == lex.Backtick) &&
			!lex.IsClosed(tok) {
			issues = append(issues, report.Issue{
				Code:     "UNCLOSED-QUOTE",
				Severity: "error",
				Kind:     "syntax",
				Message:  fmt.Sprintf("unclosed quote starting at %s — the shell will hang or fail", pyRepr(head12(tok.Text))),
				Fix:      "close the quote",
			})
		}
	}

	for _, tok := range tokens {
		switch tok.Kind {
		case lex.Op:
			switch {
			case tok.Text == "&&" && isCmdHead(target):
				issues = append(issues, report.Issue{
					Code:     "SEP-AND",
					Severity: "error",
					Kind:     "syntax",
					Message:  "operator '&&' is not supported in this shell",
					Fix:      "separate commands, or chain with ';' if order alone matters",
				})
			case tok.Text == "||" && isCmdHead(target):
				issues = append(issues, report.Issue{
					Code:     "SEP-OR",
					Severity: "error",
					Kind:     "syntax",
					Message:  "operator '||' is not supported in this shell",
					Fix:      "separate commands and check exit codes explicitly",
				})
			}
		case lex.Word:
			text := tok.Text
			switch {
			case text == "/dev/null" && win:
				issues = append(issues, report.Issue{
					Code:     "REDIR-DEVNULL",
					Severity: "error",
					Kind:     "syntax",
					Message:  "redirection to /dev/null — path does not exist on Windows",
					Fix:      "redirect to NUL instead:  2>NUL",
				})
			case (ps || target == "cmd") && varRe(text):
				issues = append(issues, report.Issue{
					Code:     "ENV-VAR",
					Severity: "error",
					Kind:     "syntax",
					Message:  fmt.Sprintf("bash-style variable %s — PowerShell reads it as an unset PS variable", text),
					Fix:      fmt.Sprintf("use the environment: $env:%s", text[1:]),
				})
			case ps && braceVarRe(text):
				name := text[2 : len(text)-1]
				issues = append(issues, report.Issue{
					Code:     "BRACE-VAR",
					Severity: "error",
					Kind:     "syntax",
					Message:  "bash-style ${var} expansion is not PowerShell syntax",
					Fix:      fmt.Sprintf("use $env:%s", name),
				})
			case ps && inSlice(specialVars, text):
				issues = append(issues, report.Issue{
					Code:     "SPECIAL-VAR",
					Severity: "error",
					Kind:     "syntax",
					Message: fmt.Sprintf("%s means one thing in bash and another in PowerShell (e.g. bash $? is the numeric exit code, PS $? is a bool)", text),
					Fix:      "use $LASTEXITCODE for native exit codes",
				})
			case text == "export" && (ps || target == "cmd"):
				issues = append(issues, report.Issue{
					Code:     "EXPORT",
					Severity: "error",
					Kind:     "syntax",
					Message:  "'export' is not available in this shell",
					Fix:      `PowerShell: $env:NAME = "value"   cmd: set NAME=value`,
				})
			case text == "source" && ps:
				issues = append(issues, report.Issue{
					Code:     "SOURCE",
					Severity: "error",
					Kind:     "syntax",
					Message:  "'source' is not available in PowerShell",
					Fix:      "dot-source it:  . ./script.ps1",
				})
			}
		case lex.Backtick:
			if ps || target == "cmd" {
				issues = append(issues, report.Issue{
					Code:     "BACKTICK",
					Severity: "error",
					Kind:     "syntax",
					Message:  "backticks mean command substitution in bash but are the escape character in PowerShell — the command will do something else entirely",
					Fix:      "use $(...) for subexpressions in PowerShell",
				})
			}
		}
	}

	for _, seg := range segs {
		head := seg.Head
		if head == "" {
			continue
		}
		rest := seg.Words[1:]
		if ps && head == "curl" && anyCurlFlags(rest) {
			issues = append(issues, report.Issue{
				Code:     "CURL-ALIAS",
				Severity: "error",
				Kind:     "tool",
				Message:  "'curl' is an alias for Invoke-WebRequest in Windows PowerShell 5.1 and rejects unix curl flags",
				Fix:      "call the real binary: curl.exe -sL ...",
			})
		}
		if ps && (head == "rm" || head == "del" || head == "erase" || head == "rd") &&
			anyIn(rest, rmShortFlags) {
			issues = append(issues, report.Issue{
				Code:     "RM-FLAGS",
				Severity: "error",
				Kind:     "syntax",
				Message:  fmt.Sprintf("'%s' maps to Remove-Item in PowerShell, which has no -r/-f short flags", head),
				Fix:      "Remove-Item -Recurse -Force",
			})
		}
		if onBash && cmletRe(head) {
			issues = append(issues, report.Issue{
				Code:     "CMDLET-IN-POSIX",
				Severity: "error",
				Kind:     "tool",
				Message:  fmt.Sprintf("'%s' is a PowerShell cmdlet; it does not exist in %s", head, target),
				Fix:      "use the POSIX equivalent for this shell",
			})
		}
		if win {
			if repl, ok := posixToPS[head]; ok {
				issues = append(issues, report.Issue{
					Code:     "POSIX-CMD",
					Severity: "error",
					Kind:     "tool",
					Message:  fmt.Sprintf("'%s' is not a Windows PowerShell command", head),
					Fix:      repl,
					Tool:     head,
				})
			}
		}
	}
	return issues
}

func inSlice(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
