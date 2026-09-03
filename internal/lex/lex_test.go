package lex

import "testing"

func TestReconstructExact(t *testing.T) {
	for _, cmd := range []string{
		"", "ls", "rm -rf /", "a && b || c ; d",
		"echo 'x y' \"z\" `w`", "  spaced   out  ",
		"2>/dev/null", "echo 'unclosed", "a\tb\nc",
	} {
		if got := Reconstruct(Lex(cmd)); got != cmd {
			t.Errorf("reconstruct(%q) = %q", cmd, got)
		}
	}
}

func TestTokensNeverEmpty(t *testing.T) {
	for _, cmd := range []string{"a b", "&&", " 'q' ", "  ", "x>y", "a|b"} {
		for _, tok := range Lex(cmd) {
			if tok.Text == "" {
				t.Errorf("empty token in %q: %+v", cmd, tok)
			}
		}
	}
}

func TestWordSplit(t *testing.T) {
	want := []Token{
		{Word, "rm"}, {Sep, " "}, {Word, "-rf"}, {Sep, " "}, {Word, "/"},
	}
	got := Lex("rm -rf /")
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("tok[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestOperatorRunMerges(t *testing.T) {
	var texts []string
	for _, tok := range Lex("a && b >> c 2>&1") {
		texts = append(texts, tok.Text)
	}
	for _, want := range []string{"&&", ">>", ">&"} {
		found := false
		for _, s := range texts {
			if s == want {
				found = true
			}
		}
		if !found {
			t.Errorf("missing op %q in %v", want, texts)
		}
	}
}

func containsToken(toks []Token, want Token) bool {
	for _, tok := range toks {
		if tok == want {
			return true
		}
	}
	return false
}

func TestQuotedSpanKeptWhole(t *testing.T) {
	toks := Lex("echo 'a b c' \"d\"")
	if !containsToken(toks, Token{SQuote, "'a b c'"}) {
		t.Errorf("missing SQUOTE 'a b c': %+v", toks)
	}
	if !containsToken(toks, Token{DQuote, `"d"`}) {
		t.Errorf("missing DQUOTE \"d\": %+v", toks)
	}
}

func TestBacktickSpan(t *testing.T) {
	if !containsToken(Lex("echo `uname`"), Token{Backtick, "`uname`"}) {
		t.Error("missing BACKTICK `uname`")
	}
}

func TestIsClosed(t *testing.T) {
	if !IsClosed(Token{SQuote, "'ab'"}) {
		t.Error("closed squote reported open")
	}
	if IsClosed(Token{SQuote, "'ab"}) {
		t.Error("unclosed squote not detected")
	}
	if !IsClosed(Token{Word, "ab"}) {
		t.Error("word should count as closed")
	}
}
