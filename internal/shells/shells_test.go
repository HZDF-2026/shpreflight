package shells

import (
	"runtime"
	"testing"
)

func TestAllRegistered(t *testing.T) {
	want := []string{"powershell5", "pwsh7", "bash", "cmd", "sh"}
	if len(Shells) != len(want) {
		t.Fatalf("len = %d, want %d", len(Shells), len(want))
	}
	for i, s := range Shells {
		if s.Key != want[i] {
			t.Errorf("Shells[%d].Key = %q, want %q", i, s.Key, want[i])
		}
	}
}

func TestAutoResolves(t *testing.T) {
	for _, input := range []string{"", "auto"} {
		got, err := ResolveTarget(input)
		if err != nil {
			t.Fatalf("ResolveTarget(%q): %v", input, err)
		}
		known := false
		for _, s := range Shells {
			if s.Key == got {
				known = true
			}
		}
		if !known {
			t.Errorf("ResolveTarget(%q) = %q, not a registered shell", input, got)
		}
	}
}

func TestUnknownShellRejected(t *testing.T) {
	if _, err := ResolveTarget("fish"); err == nil {
		t.Error("fish should be rejected")
	}
}

func TestDefaultShellMatchesPlatform(t *testing.T) {
	got := DefaultShell()
	want := "bash"
	if runtime.GOOS == "windows" {
		want = "powershell5"
	}
	if got != want {
		t.Errorf("DefaultShell() = %q, want %q", got, want)
	}
}
