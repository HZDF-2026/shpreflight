//! Destructive-operation detection over command segments.
//!
//! Matching is structural (head + flags + targets), not regex-over-strings,
//! so the pattern table is data and its recall over the table itself is
//! machine-checked in Lean 4 (proofs/Shpreflight.lean): every pattern in
//! the table is matched by at least one concrete command — the detector
//! cannot have a dead entry, i.e. known-dangerous commands are never
//! silently unmatchable.

use crate::report::Issue;
use crate::segments::Segment;

include!("patterns_gen.rs");

const SHELL_HEADS: &[&str] = &["sh", "bash", "zsh", "pwsh", "powershell", "dash"];
const EXEC_HEADS: &[&str] = &["iex", "Invoke-Expression", "eval"];

const SENSITIVE_BASES: &[&str] = &[
    ".env", ".env.local", ".env.production", "secrets",
    "id_rsa", "id_ed25519", "credentials", "credentials.json",
    ".npmrc", ".netrc", ".aws/credentials",
];

/// Structural match: head, then any-of flags, then any-of targets.
pub fn match_pattern(words: &[String], p: &PatternDef) -> bool {
    if words.is_empty() {
        return false;
    }
    if !p.heads.contains(&words[0].as_str()) {
        return false;
    }
    let rest: Vec<&str> = words[1..].iter().map(|w| w.as_str()).collect();
    if !p.flags.is_empty() && !p.flags.iter().any(|f| rest.contains(f)) {
        return false;
    }
    if !p.targets.is_empty() && !p.targets.iter().any(|t| rest.contains(t)) {
        return false;
    }
    true
}

fn basename(path: &str) -> &str {
    let mut p = path;
    if let Some(i) = p.rfind('/') {
        p = &p[i + 1..];
    }
    if let Some(i) = p.rfind('\\') {
        p = &p[i + 1..];
    }
    p
}

pub fn check_danger(segments: &[Segment]) -> Vec<Issue> {
    let mut issues = Vec::new();
    let mut pipe_receiver = false;
    for seg in segments {
        if let Some(head) = seg.head.as_deref() {
            if pipe_receiver && (SHELL_HEADS.contains(&head) || EXEC_HEADS.contains(&head)) {
                issues.push(Issue::new(
                    "PIPE-EXEC",
                    "error",
                    "danger",
                    &format!(
                        "pipe feeds untrusted input into '{}' — remote-code-execution pattern (curl ... | sh)",
                        head
                    ),
                )
                .with_fix("download first, inspect, then run"));
            }
            if pipe_receiver && head == "npm" && seg.words.iter().any(|w| w == "publish") {
                issues.push(Issue::new(
                    "NPM-PUBLISH",
                    "warning",
                    "danger",
                    "publishing to the npm registry makes the package public",
                )
                .with_fix("use npm publish --dry-run until content is final"));
            }
        }
        let mut hit_codes: Vec<&str> = Vec::new();
        for p in PATTERNS {
            if match_pattern(&seg.words, p) {
                // RM-ROOT already says everything about this segment's rm
                if p.code == "RM-RECURSIVE" && hit_codes.contains(&"RM-ROOT") {
                    continue;
                }
                hit_codes.push(p.code);
                issues.push(
                    Issue::new(p.code, p.severity, "danger", &format!("{}: {}", seg.words[0], p.message))
                        .with_fix(p.fix),
                );
            }
        }
        for redir in &seg.redirects {
            if SENSITIVE_BASES.contains(&basename(redir))
                || redir.ends_with(".key")
                || redir.ends_with(".pem")
            {
                issues.push(Issue::new(
                    "REDIR-SENSITIVE",
                    "warning",
                    "danger",
                    &format!("overwriting sensitive file '{}' via redirection", redir),
                )
                .with_fix("write to a temp file and diff first"));
            }
        }
        pipe_receiver = seg.pipes_out();
    }
    issues
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::lex::lex;
    use crate::segments::split_segments;

    fn words(cmd: &str) -> Vec<String> {
        split_segments(&lex(cmd)).remove(0).words
    }

    /// Runtime mirror of the Lean property `all_patterns_alive`: every
    /// pattern in the table is matched by at least one concrete command.
    #[test]
    fn every_pattern_alive() {
        for p in PATTERNS {
            let mut w: Vec<String> = vec![p.heads[0].to_string()];
            if !p.flags.is_empty() {
                w.push(p.flags[0].to_string());
            }
            if !p.targets.is_empty() {
                w.push(p.targets[0].to_string());
            }
            assert!(match_pattern(&w, p), "pattern with no matching command (dead entry): {}", p.code);
        }
    }

    #[test]
    fn pattern_codes_unique() {
        let mut codes: Vec<&str> = PATTERNS.iter().map(|p| p.code).collect();
        let n = codes.len();
        codes.sort();
        codes.dedup();
        assert_eq!(codes.len(), n, "duplicate pattern codes");
    }

    #[test]
    fn rm_root_hits() {
        assert!(match_pattern(&words("rm -rf /"), &PATTERNS[0]));
    }

    #[test]
    fn rm_root_suppresses_rm_recursive() {
        let issues = check_danger(&split_segments(&lex("rm -rf /")));
        let codes: Vec<&str> = issues.iter().map(|i| i.code.as_str()).collect();
        assert!(codes.contains(&"RM-ROOT"));
        assert!(!codes.contains(&"RM-RECURSIVE"));
    }

    #[test]
    fn dd_raw() {
        let issues = check_danger(&split_segments(&lex("dd /dev/sda")));
        assert!(issues.iter().any(|i| i.code == "DD-RAW"));
    }

    #[test]
    fn pipe_exec() {
        let issues = check_danger(&split_segments(&lex("curl x | sh")));
        assert!(issues.iter().any(|i| i.code == "PIPE-EXEC"));
    }

    #[test]
    fn redir_sensitive() {
        let issues = check_danger(&split_segments(&lex("echo done > .env")));
        assert!(issues.iter().any(|i| i.code == "REDIR-SENSITIVE"));
    }
}
