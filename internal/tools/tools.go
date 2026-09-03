// Package tools resolves the heads of every segment against PATH.
//
// One ReadDir pass over the PATH builds a name cache; every lookup after
// that is a set membership test, so checking a five-segment pipeline costs
// the same as checking one command.
package tools

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/HZDF-2026/shpreflight/internal/report"
	"github.com/HZDF-2026/shpreflight/internal/segments"
)

// Builtins are shell builtins or Windows internal commands: no PATH entry
// to resolve, on either family of shells.
var Builtins = map[string]bool{
	"if": true, "then": true, "else": true, "elif": true, "fi": true,
	"for": true, "while": true, "do": true, "done": true,
	"case": true, "esac": true, "function": true, "in": true,
	"export": true, "source": true, "set": true, "unset": true,
	"alias": true, "exit": true, "return": true, "cd": true, "chdir": true,
	"pushd": true, "popd": true, "echo": true, "read": true, "shift": true,
	"true": true, "false": true, "eval": true, "exec": true, "trap": true,
	"local": true, "declare": true, "readonly": true,
	// Windows cmd.exe / PowerShell internal commands
	"mkdir": true, "rmdir": true, "rd": true, "del": true, "erase": true,
	"copy": true, "xcopy": true, "move": true, "ren": true,
	"type": true, "cls": true, "dir": true, "ver": true, "vol": true,
	"call": true, "start": true, "title": true, "color": true,
	"prompt": true, "setlocal": true, "endlocal": true, "break": true,
	"md": true,
}

var cache map[string]bool

func pathExts() []string {
	if runtime.GOOS == "windows" {
		return []string{".exe", ".cmd", ".bat", ".com"}
	}
	return nil
}

// resetCache drops the PATH name cache (tests swap PATH between cases).
func resetCache() { cache = nil }

func pathNames() map[string]bool {
	if cache != nil {
		return cache
	}
	names := map[string]bool{}
	exts := pathExts()
	for _, d := range filepath.SplitList(os.Getenv("PATH")) {
		entries, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		for _, e := range entries {
			n := e.Name()
			names[n] = true
			for _, ext := range exts {
				if strings.HasSuffix(n, ext) {
					names[n[:len(n)-len(ext)]] = true
					break
				}
			}
		}
	}
	cache = names
	return names
}

// CheckTools resolves each segment head against the PATH cache.
func CheckTools(segs []segments.Segment, pathCheck bool) []report.ToolInfo {
	if !pathCheck {
		return nil
	}
	names := pathNames()
	results := map[string]report.ToolInfo{}
	var order []string
	for _, seg := range segs {
		head := seg.Head
		if head == "" || Builtins[head] {
			continue
		}
		if _, done := results[head]; done {
			continue
		}
		var path *string
		if names[head] {
			p := head
			path = &p
		}
		status := "missing"
		if names[head] {
			status = "found"
		}
		results[head] = report.ToolInfo{Name: head, Status: status, Path: path}
		order = append(order, head)
	}
	out := make([]report.ToolInfo, 0, len(order))
	for _, h := range order {
		out = append(out, results[h])
	}
	return out
}
