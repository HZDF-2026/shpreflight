package tools

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/HZDF-2026/shpreflight/internal/lex"
	"github.com/HZDF-2026/shpreflight/internal/segments"
)

func segHeads(cmd string) []segments.Segment {
	return segments.SplitSegments(lex.Lex(cmd))
}

func withPath(t *testing.T, dir string) {
	t.Helper()
	old := os.Getenv("PATH")
	os.Setenv("PATH", dir)
	resetCache()
	t.Cleanup(func() {
		os.Setenv("PATH", old)
		resetCache()
	})
}

func TestMissingToolReported(t *testing.T) {
	withPath(t, t.TempDir())
	infos := CheckTools(segHeads("definitely-not-a-real-cmd-xyz --v"), true)
	if len(infos) != 1 || infos[0].Status != "missing" {
		t.Fatalf("infos = %+v", infos)
	}
	if infos[0].Path != nil {
		t.Errorf("path should be nil, got %v", *infos[0].Path)
	}
}

func TestNoPathCheck(t *testing.T) {
	infos := CheckTools(segHeads("definitely-not-a-real-cmd-xyz --v"), false)
	if len(infos) != 0 {
		t.Fatalf("expected no tool scan, got %+v", infos)
	}
}

func TestWindowsBuiltinNotScanned(t *testing.T) {
	withPath(t, t.TempDir())
	infos := CheckTools(segHeads("mkdir newdir"), true)
	if len(infos) != 0 {
		t.Fatalf("builtin should be skipped, got %+v", infos)
	}
}

func TestFoundToolWithExeExtension(t *testing.T) {
	dir := t.TempDir()
	name := "fakecmd-xyz"
	if runtime.GOOS == "windows" {
		if err := os.WriteFile(filepath.Join(dir, name+".exe"), []byte{}, 0o755); err != nil {
			t.Fatal(err)
		}
	} else {
		if err := os.WriteFile(filepath.Join(dir, name), []byte{}, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	withPath(t, dir)
	infos := CheckTools(segHeads(name+" --v"), true)
	if len(infos) != 1 || infos[0].Status != "found" {
		t.Fatalf("infos = %+v", infos)
	}
	if infos[0].Path == nil || *infos[0].Path != name {
		t.Errorf("path = %v, want %q", infos[0].Path, name)
	}
}

func TestDuplicateHeadsResolvedOnce(t *testing.T) {
	withPath(t, t.TempDir())
	infos := CheckTools(segHeads("foo --v | foo --v | foo"), true)
	if len(infos) != 1 || infos[0].Name != "foo" {
		t.Fatalf("infos = %+v", infos)
	}
}

func TestCacheReusedAcrossCalls(t *testing.T) {
	dir := t.TempDir()
	name := "fakecmd-cache"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte{}, 0o755); err != nil {
		t.Fatal(err)
	}
	withPath(t, dir)
	first := pathNames()
	if len(first) == 0 {
		t.Fatal("empty PATH cache")
	}
	first["__probe__"] = true
	if !pathNames()["__probe__"] {
		t.Error("cache not reused across calls")
	}
}
