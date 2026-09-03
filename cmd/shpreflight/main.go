// Command shpreflight runs pre-flight diagnostics for agent-generated
// shell commands: dialect compatibility, missing tools, destructive
// operations — before the command runs.
//
// Exit codes are machine-readable so agents can branch on them:
//
//	0 ok, 1 warn, 2 fail, 3 usage error.
package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/HZDF-2026/shpreflight/internal/check"
	"github.com/HZDF-2026/shpreflight/internal/shells"
)

const usage = `usage: shpreflight <command> [options]

commands:
  check    diagnose a command before running it
  shells   list supported shell dialects

check options:
  --command words...    the shell command to check (quote it, or use --stdin)
  --stdin               read the command from stdin instead
  --shell SHELL         target shell: auto (default), powershell5, pwsh7, bash, cmd, sh
  --format FORMAT       output for humans (text, default) or agents (json)
  --no-path-check       skip PATH resolution of command heads`

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, usage)
		return 3
	}
	switch args[0] {
	case "check":
		return runCheck(args[1:], stdin, stdout, stderr)
	case "shells":
		return runShells(stdout)
	case "-h", "--help", "help":
		fmt.Fprintln(stdout, usage)
		return 0
	}
	fmt.Fprintf(stderr, "error: unknown command %q\n", args[0])
	fmt.Fprintln(stderr, usage)
	return 3
}

func runShells(stdout io.Writer) int {
	var b strings.Builder
	b.WriteString("{\n")
	for i, s := range shells.Shells {
		comma := ","
		if i == len(shells.Shells)-1 {
			comma = ""
		}
		fmt.Fprintf(&b, "  %q: %q%s\n", s.Key, s.Desc, comma)
	}
	b.WriteString("}")
	fmt.Fprintln(stdout, b.String())
	return 0
}

// parseCheckArgs splits flags from positional command words. Only exact
// matches of known flags are options; anything else (including "-rf",
// "--flag-of-the-command") is part of the command, matching how the Python
// reference accepts intermixed positionals.
func parseCheckArgs(args []string) (positionals []string, shell, format string, useStdin, noPathCheck bool, err error) {
	shell, format = "auto", "text"
	i := 0
	for i < len(args) {
		a := args[i]
		switch {
		case a == "--stdin":
			useStdin = true
		case a == "--no-path-check":
			noPathCheck = true
		case a == "--shell" || a == "--format":
			if i+1 >= len(args) {
				return nil, "", "", false, false, fmt.Errorf("%s requires a value", a)
			}
			i++
			val := args[i]
			if a == "--shell" {
				shell = val
			} else {
				format = val
			}
		case strings.HasPrefix(a, "--shell="):
			shell = a[len("--shell="):]
		case strings.HasPrefix(a, "--format="):
			format = a[len("--format="):]
		case a == "--":
			positionals = append(positionals, args[i+1:]...)
			i = len(args)
			continue
		default:
			positionals = append(positionals, a)
		}
		i++
	}
	if format != "text" && format != "json" {
		return nil, "", "", false, false,
			fmt.Errorf("invalid --format choice: %q (choose from 'text', 'json')", format)
	}
	return positionals, shell, format, useStdin, noPathCheck, nil
}

func runCheck(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	positionals, shell, format, useStdin, noPathCheck, err := parseCheckArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 3
	}

	var cmd string
	switch {
	case useStdin:
		data, rerr := io.ReadAll(stdin)
		if rerr != nil {
			fmt.Fprintf(stderr, "error: %v\n", rerr)
			return 3
		}
		cmd = strings.TrimSpace(string(data))
	case len(positionals) > 0:
		cmd = strings.TrimSpace(strings.Join(positionals, " "))
	default:
		fmt.Fprintln(stderr, "error: no command given")
		return 3
	}
	if cmd == "" {
		fmt.Fprintln(stderr, "error: empty command")
		return 3
	}

	rep, perr := check.Preflight(cmd, shell, !noPathCheck)
	if perr != nil {
		fmt.Fprintf(stderr, "error: %v\n", perr)
		return 3
	}
	if format == "json" {
		fmt.Fprintln(stdout, rep.ToJSON())
	} else {
		fmt.Fprintln(stdout, rep.ToText())
	}
	return rep.ExitCode()
}
