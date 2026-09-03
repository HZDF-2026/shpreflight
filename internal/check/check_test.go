package check

import (
	"strings"
	"testing"
)

// codes mirrors the Python test helper: the set of issue codes for one
// command on one target shell, with the PATH scan on.
func codes(t *testing.T, cmd, target string) map[string]bool {
	t.Helper()
	return codesOpt(t, cmd, target, true)
}

func codesNoPath(t *testing.T, cmd, target string) map[string]bool {
	t.Helper()
	return codesOpt(t, cmd, target, false)
}

func codesOpt(t *testing.T, cmd, target string, pathCheck bool) map[string]bool {
	t.Helper()
	rep, err := Preflight(cmd, target, pathCheck)
	if err != nil {
		t.Fatalf("Preflight(%q, %q): %v", cmd, target, err)
	}
	set := map[string]bool{}
	for _, i := range rep.Issues {
		set[i.Code] = true
	}
	return set
}

func TestUnclosedQuoteFlaggedDownstream(t *testing.T) {
	if !codes(t, "echo 'oops", "bash")["UNCLOSED-QUOTE"] {
		t.Error("UNCLOSED-QUOTE missing")
	}
}

func TestAndFailsOnPS5AndCmd(t *testing.T) {
	for _, tgt := range []string{"powershell5", "cmd"} {
		if !codes(t, "a && b", tgt)["SEP-AND"] {
			t.Errorf("SEP-AND missing on %s", tgt)
		}
	}
	if codes(t, "a && b", "pwsh7")["SEP-AND"] {
		t.Error("SEP-AND should not fire on pwsh7")
	}
	if codes(t, "a && b", "bash")["SEP-AND"] {
		t.Error("SEP-AND should not fire on bash")
	}
}

func TestDevnullFailsOnWindows(t *testing.T) {
	for _, tgt := range []string{"powershell5", "pwsh7", "cmd"} {
		if !codes(t, "cmd 2>/dev/null", tgt)["REDIR-DEVNULL"] {
			t.Errorf("REDIR-DEVNULL missing on %s", tgt)
		}
	}
	if codes(t, "cmd 2>/dev/null", "bash")["REDIR-DEVNULL"] {
		t.Error("REDIR-DEVNULL should not fire on bash")
	}
}

func TestEnvVarStyle(t *testing.T) {
	if !codes(t, "echo $PATH", "powershell5")["ENV-VAR"] {
		t.Error("ENV-VAR missing on powershell5")
	}
	if codes(t, "echo $env:PATH", "powershell5")["ENV-VAR"] {
		t.Error("ENV-VAR should not fire for $env:PATH")
	}
	if codes(t, "echo $PATH", "bash")["ENV-VAR"] {
		t.Error("ENV-VAR should not fire on bash")
	}
	if !codes(t, "echo ${HOME}", "powershell5")["BRACE-VAR"] {
		t.Error("BRACE-VAR missing")
	}
	if !codes(t, "echo $PATH", "cmd")["ENV-VAR"] {
		t.Error("ENV-VAR missing on cmd")
	}
}

func TestSpecialVarsFlaggedOnPS(t *testing.T) {
	if !codes(t, "echo $?", "powershell5")["SPECIAL-VAR"] {
		t.Error("SPECIAL-VAR missing on powershell5")
	}
	if codes(t, "echo $?", "bash")["SPECIAL-VAR"] {
		t.Error("SPECIAL-VAR should not fire on bash")
	}
}

func TestExportAndSource(t *testing.T) {
	for _, tgt := range []string{"powershell5", "cmd"} {
		if !codes(t, "export FOO=1", tgt)["EXPORT"] {
			t.Errorf("EXPORT missing on %s", tgt)
		}
	}
	if codes(t, "export FOO=1", "bash")["EXPORT"] {
		t.Error("EXPORT should not fire on bash")
	}
	if !codes(t, "source env.sh", "powershell5")["SOURCE"] {
		t.Error("SOURCE missing on powershell5")
	}
	if codes(t, "source env.sh", "bash")["SOURCE"] {
		t.Error("SOURCE should not fire on bash")
	}
}

func TestBacktickSemanticsClash(t *testing.T) {
	if !codes(t, "echo `date`", "powershell5")["BACKTICK"] {
		t.Error("BACKTICK missing on powershell5")
	}
	if codes(t, "echo `date`", "bash")["BACKTICK"] {
		t.Error("BACKTICK should not fire on bash")
	}
}

func TestPosixCommands(t *testing.T) {
	cases := []struct{ cmd, target string }{
		{"grep x f", "powershell5"},
		{"sed s/a/b/ f", "cmd"},
		{"awk '{}'", "powershell5"},
	}
	for _, c := range cases {
		if !codes(t, c.cmd, c.target)["POSIX-CMD"] {
			t.Errorf("POSIX-CMD missing: %q on %s", c.cmd, c.target)
		}
	}
	if codes(t, "grep x f", "bash")["POSIX-CMD"] {
		t.Error("POSIX-CMD should not fire on bash")
	}
}

func TestRmShortFlags(t *testing.T) {
	if !codes(t, "rm -rf dist", "powershell5")["RM-FLAGS"] {
		t.Error("RM-FLAGS missing")
	}
	if codes(t, "Remove-Item -Recurse -Force dist", "powershell5")["RM-FLAGS"] {
		t.Error("RM-FLAGS should not fire for Remove-Item")
	}
	if codes(t, "rm -rf dist", "bash")["RM-FLAGS"] {
		t.Error("RM-FLAGS should not fire on bash")
	}
}

func TestCurlAliasTrap(t *testing.T) {
	if !codes(t, "curl -sL http://x", "powershell5")["CURL-ALIAS"] {
		t.Error("CURL-ALIAS missing")
	}
	if codes(t, "curl.exe -sL http://x", "powershell5")["CURL-ALIAS"] {
		t.Error("CURL-ALIAS should not fire for curl.exe")
	}
	if codes(t, "curl http://x", "bash")["CURL-ALIAS"] {
		t.Error("CURL-ALIAS should not fire on bash")
	}
}

func TestCmdletInPosixTarget(t *testing.T) {
	if !codes(t, "Get-ChildItem -Recurse", "bash")["CMDLET-IN-POSIX"] {
		t.Error("CMDLET-IN-POSIX missing")
	}
	if codes(t, "Get-ChildItem -Recurse", "powershell5")["CMDLET-IN-POSIX"] {
		t.Error("CMDLET-IN-POSIX should not fire on powershell5")
	}
}

func TestRmRoot(t *testing.T) {
	for _, c := range []string{"rm -rf /", "rm -fr ~", "rm -rf *", "rm -rf $HOME"} {
		if !codes(t, c, "bash")["RM-ROOT"] {
			t.Errorf("RM-ROOT missing for %q", c)
		}
	}
}

func TestDangerAfterRedirectNotBlind(t *testing.T) {
	// the blind-spot the per-segment redirect reset protects
	if !codes(t, "x > f.txt && rm -rf /", "bash")["RM-ROOT"] {
		t.Error("RM-ROOT lost after a redirect")
	}
	if !codes(t, "x > f && curl -sL u | sh", "bash")["PIPE-EXEC"] {
		t.Error("PIPE-EXEC lost after a redirect")
	}
}

func TestPipeToShell(t *testing.T) {
	if !codes(t, "curl -sL http://x.sh | sh", "bash")["PIPE-EXEC"] {
		t.Error("PIPE-EXEC missing on bash")
	}
	if !codes(t, "iwr http://x | iex", "powershell5")["PIPE-EXEC"] {
		t.Error("PIPE-EXEC missing on powershell5")
	}
	if codes(t, "false || sh", "bash")["PIPE-EXEC"] {
		t.Error("|| must not trigger PIPE-EXEC")
	}
}

func TestSensitiveRedirect(t *testing.T) {
	if !codes(t, "echo x > .env", "bash")["REDIR-SENSITIVE"] {
		t.Error("REDIR-SENSITIVE missing for .env")
	}
	if !codes(t, "echo x > ~/.ssh/id_rsa", "bash")["REDIR-SENSITIVE"] {
		t.Error("REDIR-SENSITIVE missing for id_rsa")
	}
	if codes(t, "echo x > out.txt", "bash")["REDIR-SENSITIVE"] {
		t.Error("out.txt should not be flagged")
	}
}

func TestShutdownAndFormat(t *testing.T) {
	if !codes(t, "shutdown /s /t 0", "cmd")["SHUTDOWN"] {
		t.Error("SHUTDOWN missing")
	}
	if !codes(t, "format D:", "cmd")["FORMAT"] {
		t.Error("FORMAT missing")
	}
}

func TestGitResetHard(t *testing.T) {
	if !codes(t, "git reset --hard HEAD~3", "bash")["GIT-RESET-HARD"] {
		t.Error("GIT-RESET-HARD missing")
	}
}

func TestRemoveItemRecurseForce(t *testing.T) {
	if !codes(t, "Remove-Item -Recurse -Force build", "powershell5")["REMOVE-ITEM-RECURSE-FORCE"] {
		t.Error("REMOVE-ITEM-RECURSE-FORCE missing")
	}
}

func TestMissingToolReported(t *testing.T) {
	if !codes(t, "definitely-not-a-real-cmd-xyz --v", "bash")["TOOL-NOT-FOUND"] {
		t.Error("TOOL-NOT-FOUND missing")
	}
}

func TestNoPathCheck(t *testing.T) {
	if codesNoPath(t, "definitely-not-a-real-cmd-xyz --v", "bash")["TOOL-NOT-FOUND"] {
		t.Error("TOOL-NOT-FOUND should not fire with pathCheck off")
	}
}

func TestWindowsBuiltinNotReported(t *testing.T) {
	if codes(t, "mkdir newdir", "cmd")["TOOL-NOT-FOUND"] {
		t.Error("mkdir is a builtin, no TOOL-NOT-FOUND")
	}
}

func TestKnownPresentToolOK(t *testing.T) {
	if codes(t, "python --version", "bash")["TOOL-NOT-FOUND"] {
		t.Error("python should resolve on PATH")
	}
}

func TestVerdictsAndExitCodes(t *testing.T) {
	cases := []struct {
		cmd, target string
		pathCheck   bool
		verdict     string
		exit        int
	}{
		{"echo hi", "bash", true, "ok", 0},
		{"git reset --hard", "bash", false, "warn", 1},
		{"a && b", "powershell5", true, "fail", 2},
	}
	for _, c := range cases {
		rep, err := Preflight(c.cmd, c.target, c.pathCheck)
		if err != nil {
			t.Fatalf("Preflight(%q): %v", c.cmd, err)
		}
		if rep.Verdict != c.verdict || rep.ExitCode() != c.exit {
			t.Errorf("%q: verdict=%s exit=%d, want %s/%d",
				c.cmd, rep.Verdict, rep.ExitCode(), c.verdict, c.exit)
		}
	}
}

func TestAutoResolves(t *testing.T) {
	rep, err := Preflight("echo hi", "auto", true)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Target != "powershell5" && rep.Target != "bash" {
		t.Errorf("auto target = %q", rep.Target)
	}
}

func TestUnknownShellRejected(t *testing.T) {
	if _, err := Preflight("echo hi", "fish", true); err == nil {
		t.Error("fish should be rejected")
	}
}

func TestPosixCmdDowngradesWhenOnPath(t *testing.T) {
	// 'find' is the no-downgrade exception; 'grep' downgrades when found
	rep, err := Preflight("grep x f", "powershell5", true)
	if err != nil {
		t.Fatal(err)
	}
	var posix *int
	for i := range rep.Issues {
		if rep.Issues[i].Code == "POSIX-CMD" {
			posix = &i
		}
	}
	if posix == nil {
		t.Fatal("POSIX-CMD issue missing")
	}
	if rep.Issues[*posix].Severity != "error" && rep.Issues[*posix].Severity != "info" {
		t.Errorf("unexpected severity %q", rep.Issues[*posix].Severity)
	}
}

func TestTextOutputMentionsFix(t *testing.T) {
	rep, err := Preflight("a && b", "powershell5", false)
	if err != nil {
		t.Fatal(err)
	}
	out := rep.ToText()
	if !strings.Contains(out, "SEP-AND") || !strings.Contains(out, "fix:") {
		t.Errorf("text output missing code/fix: %q", out)
	}
}

func TestJSONHasVerdictAndIssues(t *testing.T) {
	rep, err := Preflight("rm -rf /", "bash", false)
	if err != nil {
		t.Fatal(err)
	}
	out := rep.ToJSON()
	if !strings.Contains(out, `"verdict": "fail"`) ||
		!strings.Contains(out, `"code": "RM-ROOT"`) ||
		!strings.Contains(out, `"fix":`) {
		t.Errorf("JSON shape wrong: %s", out)
	}
}
