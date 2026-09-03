//! Single-pass shell-command lexer.
//!
//! Design invariant (machine-checked in Lean 4, see proofs/):
//! `concat(token text for token in lex(cmd)) == cmd`
//! Every input character lands in exactly one token — no gaps, no overlaps.
//! That is what makes the downstream rule engine trustworthy: a destructive
//! fragment can never hide in a lexing blind spot.

#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub enum Kind {
    Word,
    Sep,
    Op,
    Squote,
    Dquote,
    Backtick,
}

#[derive(Clone, Debug)]
pub struct Token {
    pub kind: Kind,
    pub text: String,
}

const OP_CHARS: &str = "&|;<>";
const WHITESPACE: &str = " \t\r\n";
const QUOTES: &str = "'\"`";

fn is_op_char(c: char) -> bool {
    OP_CHARS.contains(c)
}

fn is_ws(c: char) -> bool {
    WHITESPACE.contains(c)
}

fn is_word_stop(c: char) -> bool {
    is_op_char(c) || is_ws(c) || QUOTES.contains(c)
}

fn kind_of_quote(c: char) -> Kind {
    match c {
        '\'' => Kind::Squote,
        '"' => Kind::Dquote,
        _ => Kind::Backtick,
    }
}

pub fn lex(cmd: &str) -> Vec<Token> {
    let c: Vec<char> = cmd.chars().collect();
    let n = c.len();
    let mut tokens = Vec::new();
    let mut i = 0;
    while i < n {
        let ch = c[i];
        if is_ws(ch) {
            let mut j = i + 1;
            while j < n && is_ws(c[j]) {
                j += 1;
            }
            tokens.push(Token { kind: Kind::Sep, text: c[i..j].iter().collect() });
            i = j;
        } else if QUOTES.contains(ch) {
            let kind = kind_of_quote(ch);
            let mut j = i + 1;
            while j < n && c[j] != ch {
                j += 1;
            }
            let end = if j == n { n } else { j + 1 };
            tokens.push(Token { kind, text: c[i..end].iter().collect() });
            i = end;
        } else if is_op_char(ch) {
            let mut j = i + 1;
            while j < n && is_op_char(c[j]) {
                j += 1;
            }
            tokens.push(Token { kind: Kind::Op, text: c[i..j].iter().collect() });
            i = j;
        } else {
            let mut j = i + 1;
            while j < n && !is_word_stop(c[j]) {
                j += 1;
            }
            tokens.push(Token { kind: Kind::Word, text: c[i..j].iter().collect() });
            i = j;
        }
    }
    tokens
}

/// A quoted span is closed when it starts and ends with the same quote char
/// and has at least 2 characters.
pub fn is_closed(tok: &Token) -> bool {
    if !matches!(tok.kind, Kind::Squote | Kind::Dquote | Kind::Backtick) {
        return true;
    }
    let cs: Vec<char> = tok.text.chars().collect();
    cs.len() >= 2 && cs[0] == cs[cs.len() - 1]
}

pub fn reconstruct(tokens: &[Token]) -> String {
    let mut s = String::new();
    for t in tokens {
        s.push_str(&t.text);
    }
    s
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn reconstruct_exact() {
        for cmd in [
            "", "ls", "rm -rf /", "a && b || c ; d", "echo 'x y' \"z\" `w`",
            "  spaced   out  ", "2>/dev/null", "echo 'unclosed", "a\tb\nc",
        ] {
            assert_eq!(reconstruct(&lex(cmd)), cmd);
        }
    }

    #[test]
    fn tokens_never_empty() {
        for cmd in ["a b", "&&", " 'q' ", "  ", "x>y", "a|b"] {
            assert!(lex(cmd).iter().all(|t| !t.text.is_empty()));
        }
    }

    #[test]
    fn word_split() {
        let toks = lex("rm -rf /");
        let pairs: Vec<(Kind, &str)> = toks.iter().map(|t| (t.kind, t.text.as_str())).collect();
        assert_eq!(
            pairs,
            vec![
                (Kind::Word, "rm"),
                (Kind::Sep, " "),
                (Kind::Word, "-rf"),
                (Kind::Sep, " "),
                (Kind::Word, "/"),
            ]
        );
    }

    #[test]
    fn operator_run_merges() {
        let toks = lex("a && b >> c 2>&1");
        let texts: Vec<&str> = toks.iter().map(|t| t.text.as_str()).collect();
        assert!(texts.contains(&"&&"));
        assert!(texts.contains(&">>"));
        assert!(texts.contains(&">&"));
    }

    #[test]
    fn quoted_span_kept_whole() {
        let toks = lex("echo 'a b c' \"d\"");
        assert!(toks.iter().any(|t| t.kind == Kind::Squote && t.text == "'a b c'"));
        assert!(toks.iter().any(|t| t.kind == Kind::Dquote && t.text == "\"d\""));
    }

    #[test]
    fn backtick_span() {
        assert!(lex("echo `uname`").iter().any(|t| t.kind == Kind::Backtick && t.text == "`uname`"));
    }

    #[test]
    fn closed_detection() {
        assert!(is_closed(&Token { kind: Kind::Squote, text: "'x'".into() }));
        assert!(!is_closed(&Token { kind: Kind::Squote, text: "'x".into() }));
        assert!(is_closed(&Token { kind: Kind::Word, text: "x".into() }));
    }
}
