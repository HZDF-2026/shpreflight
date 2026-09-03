package danger

import (
	"testing"

	"github.com/HZDF-2026/shpreflight/internal/lex"
	"github.com/HZDF-2026/shpreflight/internal/report"
	"github.com/HZDF-2026/shpreflight/internal/segments"
)

func segWords(cmd string) []segments.Segment {
	return segments.SplitSegments(lex.Lex(cmd))
}

func dangerCodes(cmd string) map[string]bool {
	set := map[string]bool{}
	for _, i := range CheckDanger(segWords(cmd)) {
		set[i.Code] = true
	}
	return set
}

func TestRmRoot(t *testing.T) {
	for _, c := range []string{"rm -rf /", "rm -fr ~", "rm -rf *", "rm -rf $HOME"} {
		if dangerCodes(c)["RM-ROOT"] != true {
			t.Errorf("RM-ROOT missing for %q", c)
		}
	}
}

func TestRmRootNotDuplicatedWithRecurse(t *testing.T) {
	var codes []string
	for _, i := range CheckDanger(segWords("rm -rf /")) {
		codes = append(codes, i.Code)
	}
	recurse := 0
	hasRoot := false
	for _, c := range codes {
		if c == "RM-RECURSIVE" {
			recurse++
		}
		if c == "RM-ROOT" {
			hasRoot = true
		}
	}
	if recurse > 0 && hasRoot {
		t.Errorf("RM-RECURSIVE duplicated alongside RM-ROOT: %v", codes)
	}
}

func TestDangerAfterRedirectNotBlind(t *testing.T) {
	// the blind-spot the per-segment redirect reset protects
	if !dangerCodes("x > f.txt && rm -rf /")["RM-ROOT"] {
		t.Error("RM-ROOT lost after a redirect")
	}
	segs := segWords("x > f && curl -sL u | sh")
	if !dangerCodes("x > f && curl -sL u | sh")["PIPE-EXEC"] {
		t.Errorf("PIPE-EXEC lost after a redirect: %+v", segs)
	}
}

func TestGitResetHard(t *testing.T) {
	if !dangerCodes("git reset --hard HEAD~3")["GIT-RESET-HARD"] {
		t.Error("GIT-RESET-HARD missing")
	}
}

func TestPipeToShell(t *testing.T) {
	if !dangerCodes("curl -sL http://x.sh | sh")["PIPE-EXEC"] {
		t.Error("PIPE-EXEC missing for curl | sh")
	}
	if !dangerCodes("iwr http://x | iex")["PIPE-EXEC"] {
		t.Error("PIPE-EXEC missing for iwr | iex")
	}
	// logical-or is not a pipe
	if dangerCodes("false || sh")["PIPE-EXEC"] {
		t.Error("|| must not trigger PIPE-EXEC")
	}
}

func TestSensitiveRedirect(t *testing.T) {
	if !dangerCodes("echo x > .env")["REDIR-SENSITIVE"] {
		t.Error(".env redirect not flagged")
	}
	if !dangerCodes("echo x > ~/.ssh/id_rsa")["REDIR-SENSITIVE"] {
		t.Error("id_rsa redirect not flagged")
	}
	if dangerCodes("echo x > out.txt")["REDIR-SENSITIVE"] {
		t.Error("plain file wrongly flagged")
	}
}

func TestShutdownAndFormat(t *testing.T) {
	if !dangerCodes("shutdown /s /t 0")["SHUTDOWN"] {
		t.Error("SHUTDOWN missing")
	}
	if !dangerCodes("format D:")["FORMAT"] {
		t.Error("FORMAT missing")
	}
}

func TestRemoveItemRecurseForce(t *testing.T) {
	if !dangerCodes("Remove-Item -Recurse -Force build")["REMOVE-ITEM-RECURSE-FORCE"] {
		t.Error("Remove-Item -Recurse -Force not flagged")
	}
}

func TestDDRawCoversCommonDevices(t *testing.T) {
	// one device per family: SATA beyond sda/sdb, virtio (cloud VMs),
	// legacy IDE, Xen/AWS, NVMe partitions, SD/eMMC, macOS, Windows.
	// of= is the form real dd invocations use; bare is the legacy form.
	for _, dev := range []string{
		"/dev/sdc", "/dev/sdp", "/dev/vda", "/dev/vdb", "/dev/hdb",
		"/dev/xvda", "/dev/nvme1n1", "/dev/mmcblk0", "/dev/disk0",
		"/dev/rdisk0", `\\.\PhysicalDrive0`,
	} {
		if !dangerCodes("dd if=img.iso of=" + dev)["DD-RAW"] {
			t.Errorf("DD-RAW missing for %s", dev)
		}
	}
	if !dangerCodes("dd /dev/sdc")["DD-RAW"] {
		t.Error("DD-RAW missing for bare /dev/sdc")
	}
	if dangerCodes("dd if=a of=b.img")["DD-RAW"] {
		t.Error("DD-RAW fired on a file target")
	}
}

func TestRmRecursiveMatchesUppercaseRF(t *testing.T) {
	if !dangerCodes("rm -RF build")["RM-RECURSIVE"] {
		t.Error("rm -RF not flagged as recursive")
	}
}

func TestEveryPatternIsAlive(t *testing.T) {
	// the Lean-checked property, mirrored as a test: no dead entries
	for _, p := range Patterns {
		if len(p.Heads) == 0 {
			t.Fatalf("%s: pattern has no heads", p.Code)
		}
		for _, head := range p.Heads {
			words := []string{head}
			if len(p.Flags) > 0 {
				words = append(words, p.Flags[0])
			}
			if len(p.Targets) > 0 {
				words = append(words, p.Targets[0])
			}
			if !MatchPattern(words, p) {
				t.Errorf("%s: witness %v does not match", p.Code, words)
			}
		}
	}
}

func TestIssueShape(t *testing.T) {
	issues := CheckDanger(segWords("rm -rf /"))
	if len(issues) == 0 {
		t.Fatal("no issues")
	}
	i := issues[0]
	if i.Code != "RM-ROOT" || i.Severity != "error" || i.Kind != "danger" {
		t.Errorf("issue shape wrong: %+v", i)
	}
	var _ report.Issue = i // compiles against the shared type
}
