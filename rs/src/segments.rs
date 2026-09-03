//! Token stream -> command segments.
//!
//! A pipeline like `rg pattern src/ | head -5 2>/dev/null && echo done`
//! splits at control operators (`|`, `||`, `;`, `&&`, `&`). Inside a
//! segment, words after a redirection operator belong to the redirect, not
//! to the command, so they are never mistaken for the program to resolve.

use crate::lex::{Kind, Token};

const CONTROL_OPS: &[&str] = &["|", "||", ";", "&&", "&"];

#[derive(Clone, Debug, Default)]
pub struct Segment {
    pub head: Option<String>,
    pub words: Vec<String>,
    pub redirects: Vec<String>,
    pub raw: String,
    /// Control operator that ended this segment: `|` `||` `;` `&&` `&` or None.
    pub terminator: Option<String>,
}

impl Segment {
    pub fn pipes_out(&self) -> bool {
        self.terminator.as_deref() == Some("|")
    }
}

fn is_control(op: &str) -> bool {
    CONTROL_OPS.contains(&op)
}

fn strip_quotes(word: &str) -> String {
    let cs: Vec<char> = word.chars().collect();
    if cs.len() >= 2 && cs[0] == cs[cs.len() - 1] && QUOTE_CHARS.contains(cs[0]) {
        return cs[1..cs.len() - 1].iter().collect();
    }
    word.to_string()
}

const QUOTE_CHARS: &str = "'\"`";

fn is_digits(s: &str) -> bool {
    !s.is_empty() && s.chars().all(|c| c.is_ascii_digit())
}

struct Builder {
    words: Vec<String>,
    redirects: Vec<String>,
    raw: String,
    in_redirect: bool,
}

impl Builder {
    fn new() -> Self {
        Builder { words: Vec::new(), redirects: Vec::new(), raw: String::new(), in_redirect: false }
    }

    fn flush(&mut self, segments: &mut Vec<Segment>, terminator: Option<String>) {
        if !self.words.is_empty() || !self.redirects.is_empty() || !self.raw.is_empty() {
            segments.push(Segment {
                head: self.words.first().cloned(),
                words: self.words.clone(),
                redirects: self.redirects.clone(),
                raw: self.raw.trim().to_string(),
                terminator,
            });
        }
        self.words.clear();
        self.redirects.clear();
        self.raw.clear();
        // Redirect state is per-segment: without this reset, every word after
        // "x > f && ..." would be swallowed as a redirect target and the head
        // of the next segment (e.g. an "rm -rf /") would vanish from analysis.
        self.in_redirect = false;
    }
}

pub fn split_segments(tokens: &[Token]) -> Vec<Segment> {
    let mut segments = Vec::new();
    let mut b = Builder::new();
    for (idx, tok) in tokens.iter().enumerate() {
        match tok.kind {
            Kind::Sep => b.raw.push_str(&tok.text),
            Kind::Op => {
                b.raw.push_str(&tok.text);
                if is_control(&tok.text) {
                    b.flush(&mut segments, Some(tok.text.clone()));
                } else {
                    b.in_redirect = true;
                }
            }
            _ => {
                b.raw.push_str(&tok.text);
                let bare = strip_quotes(&tok.text);
                let next_is_redirect = tokens
                    .get(idx + 1)
                    .map_or(false, |nx| nx.kind == Kind::Op && (nx.text.contains('>') || nx.text.contains('<')));
                // fd prefix of the next redirect: "out 2> err", "log 2>&1"
                if tok.kind == Kind::Word && is_digits(&bare) && next_is_redirect {
                    continue;
                }
                if b.in_redirect {
                    b.redirects.push(bare);
                } else {
                    b.words.push(bare);
                }
            }
        }
    }
    b.flush(&mut segments, None);
    segments
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::lex::lex;

    fn segs_of(cmd: &str) -> Vec<Segment> {
        split_segments(&lex(cmd))
    }

    #[test]
    fn pipeline_segments() {
        let segs = segs_of("rg pat | head -5 | wc -l");
        let heads: Vec<&str> = segs.iter().map(|s| s.head.as_deref().unwrap()).collect();
        assert_eq!(heads, vec!["rg", "head", "wc"]);
        assert!(segs.iter().take(segs.len() - 1).all(|s| s.terminator.as_deref() == Some("|")));
        assert!(segs.last().unwrap().terminator.is_none());
    }

    #[test]
    fn segments_own_their_words() {
        // regression: flush() used to clear the shared list after append
        let segs = segs_of("rm -rf / && ls");
        assert_eq!(segs[0].words, vec!["rm", "-rf", "/"]);
        assert_eq!(segs[1].words, vec!["ls"]);
    }

    #[test]
    fn redirect_target_not_a_command() {
        let segs = segs_of("echo hi > out.txt 2> err.txt");
        assert_eq!(segs[0].words, vec!["echo", "hi"]);
        assert_eq!(segs[0].redirects, vec!["out.txt", "err.txt"]);
    }

    #[test]
    fn redirect_after_space_still_redirect() {
        // regression: SEP used to reset in_redirect
        let segs = segs_of("echo done > .env");
        assert_eq!(segs[0].redirects, vec![".env"]);
        assert!(!segs[0].words.contains(&".env".to_string()));
    }

    #[test]
    fn pipes_out_not_triggered_by_or() {
        let segs = segs_of("false || echo ok");
        assert_eq!(segs[0].terminator.as_deref(), Some("||"));
        assert!(!segs[0].pipes_out());
    }

    #[test]
    fn redirect_state_resets_at_segment_boundary() {
        // regression: in_redirect used to leak across the control operator,
        // so the segment after "x > f && ..." lost its head entirely
        let segs = segs_of("x > f.txt && rm -rf /");
        assert_eq!(segs[1].words, vec!["rm", "-rf", "/"]);
        assert_eq!(segs[1].head.as_deref(), Some("rm"));
    }

    #[test]
    fn fd_prefix_swallowed() {
        let segs = segs_of("log 2>&1");
        assert_eq!(segs[0].words, vec!["log"]);
        assert_eq!(segs[0].redirects.len(), 1);
    }
}
