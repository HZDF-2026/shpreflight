// Package segments splits a token stream into command segments.
//
// A pipeline like `rg pattern src/ | head -5 2>/dev/null && echo done`
// splits at control operators (| || ; && &). Inside a segment, words after
// a redirection operator (>, >>, <, 2>&1, ...) belong to the redirect, not
// to the command, so they are never mistaken for the program to resolve.
package segments

import (
	"strings"

	"github.com/HZDF-2026/shpreflight/internal/lex"
)

var controlOps = map[string]bool{
	"|": true, "||": true, ";": true, "&&": true, "&": true,
}

// Segment is one command between control operators, with redirection
// targets kept apart from its words.
type Segment struct {
	Head       string // first word, "" when the segment has no words
	Words      []string
	Redirects  []string
	Raw        string
	Terminator string // control op that ended this segment, "" at end of input
}

func (s *Segment) PipesOut() bool { return s.Terminator == "|" }

func stripQuotes(word string) string {
	if len(word) >= 2 && word[0] == word[len(word)-1] &&
		(word[0] == '\'' || word[0] == '"' || word[0] == '`') {
		return word[1 : len(word)-1]
	}
	return word
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// SplitSegments splits the token stream at control operators. Redirect
// state is per-segment: without resetting it at flush, every word after
// "x > f && ..." would be swallowed as a redirect target and the head of
// the next segment (e.g. an "rm -rf /") would vanish from analysis.
func SplitSegments(tokens []lex.Token) []Segment {
	var segments []Segment
	var words, redirects, rawParts []string
	inRedirect := false

	flush := func(terminator string) {
		if len(words) > 0 || len(redirects) > 0 || len(rawParts) > 0 {
			head := ""
			if len(words) > 0 {
				head = words[0]
			}
			segments = append(segments, Segment{
				Head:       head,
				Words:      append([]string(nil), words...),
				Redirects:  append([]string(nil), redirects...),
				Raw:        strings.TrimSpace(strings.Join(rawParts, "")),
				Terminator: terminator,
			})
		}
		words = words[:0]
		redirects = redirects[:0]
		rawParts = rawParts[:0]
		inRedirect = false
	}

	for idx, tok := range tokens {
		switch tok.Kind {
		case lex.Sep:
			rawParts = append(rawParts, tok.Text)
		case lex.Op:
			rawParts = append(rawParts, tok.Text)
			if controlOps[tok.Text] {
				flush(tok.Text)
			} else {
				inRedirect = true
			}
		default:
			rawParts = append(rawParts, tok.Text)
			bare := stripQuotes(tok.Text)
			skipAsFdPrefix := false
			if tok.Kind == lex.Word && isDigits(bare) && idx+1 < len(tokens) {
				next := tokens[idx+1]
				if next.Kind == lex.Op && strings.ContainsAny(next.Text, "><") {
					skipAsFdPrefix = true
				}
			}
			if skipAsFdPrefix {
				continue // fd prefix of the next redirect: "out 2> err", "log 2>&1"
			}
			if inRedirect {
				redirects = append(redirects, bare)
			} else {
				words = append(words, bare)
			}
		}
	}
	flush("")
	return segments
}
