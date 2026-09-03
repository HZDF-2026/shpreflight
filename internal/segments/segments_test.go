package segments

import (
	"reflect"
	"testing"

	"github.com/HZDF-2026/shpreflight/internal/lex"
)

func TestPipelineSegments(t *testing.T) {
	segs := SplitSegments(lex.Lex("rg pat | head -5 | wc -l"))
	wantHeads := []string{"rg", "head", "wc"}
	if len(segs) != len(wantHeads) {
		t.Fatalf("len = %d, want %d: %+v", len(segs), len(wantHeads), segs)
	}
	for i, s := range segs {
		if s.Head != wantHeads[i] {
			t.Errorf("segs[%d].Head = %q, want %q", i, s.Head, wantHeads[i])
		}
	}
	for i, s := range segs[:len(segs)-1] {
		if s.Terminator != "|" {
			t.Errorf("segs[%d].Terminator = %q, want |", i, s.Terminator)
		}
	}
	if segs[len(segs)-1].Terminator != "" {
		t.Errorf("last Terminator = %q, want empty", segs[len(segs)-1].Terminator)
	}
}

func TestSegmentsOwnTheirWords(t *testing.T) {
	// regression: flush() used to clear the shared list after append
	segs := SplitSegments(lex.Lex("rm -rf / && ls"))
	if !reflect.DeepEqual(segs[0].Words, []string{"rm", "-rf", "/"}) {
		t.Errorf("segs[0].Words = %v", segs[0].Words)
	}
	if !reflect.DeepEqual(segs[1].Words, []string{"ls"}) {
		t.Errorf("segs[1].Words = %v", segs[1].Words)
	}
}

func TestRedirectTargetNotACommand(t *testing.T) {
	segs := SplitSegments(lex.Lex("echo hi > out.txt 2> err.txt"))
	if !reflect.DeepEqual(segs[0].Words, []string{"echo", "hi"}) {
		t.Errorf("Words = %v", segs[0].Words)
	}
	if !reflect.DeepEqual(segs[0].Redirects, []string{"out.txt", "err.txt"}) {
		t.Errorf("Redirects = %v", segs[0].Redirects)
	}
}

func TestRedirectAfterSpaceStillRedirect(t *testing.T) {
	// regression: SEP used to reset in_redirect
	segs := SplitSegments(lex.Lex("echo done > .env"))
	if !reflect.DeepEqual(segs[0].Redirects, []string{".env"}) {
		t.Errorf("Redirects = %v", segs[0].Redirects)
	}
	for _, w := range segs[0].Words {
		if w == ".env" {
			t.Errorf(".env leaked into Words: %v", segs[0].Words)
		}
	}
}

func TestPipesOutNotTriggeredByOr(t *testing.T) {
	segs := SplitSegments(lex.Lex("false || echo ok"))
	if segs[0].Terminator != "||" {
		t.Errorf("Terminator = %q, want ||", segs[0].Terminator)
	}
	if segs[0].PipesOut() {
		t.Error("|| must not count as a pipe")
	}
}

func TestRedirectStateResetsAtSegmentBoundary(t *testing.T) {
	// regression: in_redirect used to leak across the control operator,
	// so the segment after "x > f && ..." lost its head entirely
	segs := SplitSegments(lex.Lex("x > f.txt && rm -rf /"))
	if !reflect.DeepEqual(segs[1].Words, []string{"rm", "-rf", "/"}) {
		t.Errorf("segs[1].Words = %v", segs[1].Words)
	}
	if segs[1].Head != "rm" {
		t.Errorf("segs[1].Head = %q, want rm", segs[1].Head)
	}
}
