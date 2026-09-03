// Package lex implements the single-pass shell-command lexer.
//
// Design invariant (machine-checked in Lean 4, see proofs/):
//
//	concat(token text for token in lex(cmd)) == cmd
//
// Every input character lands in exactly one token — no gaps, no overlaps.
// That is what makes the downstream rule engine trustworthy: a destructive
// fragment can never hide in a lexing blind spot.
package lex

import "strings"

const (
	Word     = "WORD"
	Sep      = "SEP"
	Op       = "OP"
	SQuote   = "SQUOTE"
	DQuote   = "DQUOTE"
	Backtick = "BACKTICK"
)

const (
	opChars    = "&|;<>"
	whitespace = " \t\r\n"
	wordStop   = opChars + whitespace + "'\"`"
)

// Token is one lexeme: its kind and verbatim text.
type Token struct {
	Kind string
	Text string
}

func quotedSpan(cmd string, i int) int {
	q := cmd[i]
	j := strings.IndexByte(cmd[i+1:], q)
	if j < 0 {
		return len(cmd)
	}
	return i + 1 + j + 1
}

func isWhitespace(c byte) bool { return strings.IndexByte(whitespace, c) >= 0 }
func isOpChar(c byte) bool     { return strings.IndexByte(opChars, c) >= 0 }

// Lex splits cmd into tokens. Byte-level scanning is equivalent to
// code-point scanning: every stop character is ASCII, and UTF-8
// continuation bytes never collide with them.
func Lex(cmd string) []Token {
	tokens := make([]Token, 0, 8)
	i, n := 0, len(cmd)
	for i < n {
		ch := cmd[i]
		switch {
		case isWhitespace(ch):
			j := i + 1
			for j < n && isWhitespace(cmd[j]) {
				j++
			}
			tokens = append(tokens, Token{Sep, cmd[i:j]})
			i = j
		case ch == '\'' || ch == '"' || ch == '`':
			kind := SQuote
			if ch == '"' {
				kind = DQuote
			} else if ch == '`' {
				kind = Backtick
			}
			j := quotedSpan(cmd, i)
			tokens = append(tokens, Token{kind, cmd[i:j]})
			i = j
		case isOpChar(ch):
			j := i + 1
			for j < n && isOpChar(cmd[j]) {
				j++
			}
			tokens = append(tokens, Token{Op, cmd[i:j]})
			i = j
		default:
			j := i + 1
			for j < n && strings.IndexByte(wordStop, cmd[j]) < 0 {
				j++
			}
			tokens = append(tokens, Token{Word, cmd[i:j]})
			i = j
		}
	}
	return tokens
}

// IsClosed reports whether a quoted token carries its closing quote.
func IsClosed(t Token) bool {
	switch t.Kind {
	case SQuote, DQuote, Backtick:
		return len(t.Text) >= 2 && t.Text[0] == t.Text[len(t.Text)-1]
	}
	return true
}

// Reconstruct concatenates token texts; equals the input for Lex output.
func Reconstruct(tokens []Token) string {
	var b strings.Builder
	for _, t := range tokens {
		b.WriteString(t.Text)
	}
	return b.String()
}
