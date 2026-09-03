// Package shells holds the shell dialect registry and target detection.
package shells

import (
	"fmt"
	"runtime"
)

// Shell is one supported dialect.
type Shell struct {
	Key  string
	Desc string
}

var Shells = []Shell{
	{"powershell5", "Windows PowerShell 5.1 (powershell.exe)"},
	{"pwsh7", "PowerShell 7+ (pwsh.exe)"},
	{"bash", "Bash (Git Bash / WSL / Unix)"},
	{"cmd", "Windows cmd.exe"},
	{"sh", "POSIX sh"},
}

// DefaultShell is the shell an agent is most likely driving here.
func DefaultShell() string {
	if runtime.GOOS == "windows" {
		return "powershell5"
	}
	return "bash"
}

// ResolveTarget maps "auto"/"" to the platform default and validates.
func ResolveTarget(shell string) (string, error) {
	if shell == "" || shell == "auto" {
		return DefaultShell(), nil
	}
	for _, s := range Shells {
		if s.Key == shell {
			return shell, nil
		}
	}
	return "", fmt.Errorf("unknown shell '%s'; see `shpreflight shells`", shell)
}

func IsPowerShell(target string) bool {
	return target == "powershell5" || target == "pwsh7"
}

func IsWindowsShell(target string) bool {
	return IsPowerShell(target) || target == "cmd"
}
