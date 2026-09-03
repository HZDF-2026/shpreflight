//! Preflight orchestration: lex -> segment -> dialect + danger + tools.

use std::time::Instant;

use crate::danger::check_danger;
use crate::lex::lex;
use crate::report::{Issue, Report};
use crate::rules::check_dialect;
use crate::segments::split_segments;
use crate::shells::resolve_target;
use crate::tools::{check_tools, BUILTINS};

/// POSIX names whose Windows PATH namesakes do something different, so a
/// PATH hit is not evidence the command will do what the agent meant.
const NO_DOWNGRADE: &[&str] = &["find"];

pub fn preflight(cmd: &str, target: Option<&str>, path_check: bool) -> Result<Report, String> {
    let start = Instant::now();
    let shell = resolve_target(target)?;
    let tokens = lex(cmd);
    let segments = split_segments(&tokens);

    let mut issues: Vec<Issue> = Vec::new();
    issues.extend(check_dialect(&tokens, &segments, &shell));
    issues.extend(check_danger(&segments));
    let tools = check_tools(&segments, path_check);

    // A tool reported by rules as POSIX-only but actually present on PATH
    // (Git Bash in PATH etc.) downgrades from hard failure to a note:
    // the command will run, just not natively.
    if !tools.is_empty() {
        let found: Vec<&str> = tools
            .iter()
            .filter(|t| t.status == "found")
            .map(|t| t.name.as_str())
            .collect();
        for issue in issues.iter_mut() {
            if issue.code == "POSIX-CMD" {
                if let Some(tool) = issue.tool.as_deref() {
                    if found.contains(&tool) && !NO_DOWNGRADE.contains(&tool) {
                        issue.severity = "info".to_string();
                        issue.message.push_str(" (present on PATH, non-native)");
                    }
                }
            }
        }
    }

    // Anything reported missing on PATH gets an explicit issue, since
    // 'command not found' is the single largest failure mode for agents.
    if path_check {
        for t in &tools {
            if t.status == "missing" && !BUILTINS.contains(&t.name.as_str()) {
                if issues.iter().any(|i| i.code == "POSIX-CMD" && i.tool.as_deref() == Some(t.name.as_str())) {
                    continue;
                }
                issues.push(
                    Issue::new(
                        "TOOL-NOT-FOUND",
                        "error",
                        "tool",
                        &format!(
                            "'{}' was not found on PATH — this is a 'command not found' failure",
                            t.name
                        ),
                    )
                    .with_fix("install it, or use the native equivalent")
                    .with_tool(&t.name),
                );
            }
        }
    }

    let elapsed_ms = start.elapsed().as_secs_f64() * 1000.0;
    Ok(Report::new(cmd, &shell, issues, tools, elapsed_ms))
}

#[cfg(test)]
mod tests {
    use super::*;

    fn codes(cmd: &str, target: &str) -> Vec<String> {
        preflight(cmd, Some(target), false).unwrap().issues.iter().map(|i| i.code.clone()).collect()
    }

    #[test]
    fn ok_command() {
        let rep = preflight("echo hi", None, false).unwrap();
        assert_eq!(rep.verdict(), "ok");
        assert!(rep.issues.is_empty());
    }

    #[test]
    fn unclosed_quote() {
        assert!(codes("echo 'oops", "bash").contains(&"UNCLOSED-QUOTE".to_string()));
    }

    #[test]
    fn rm_root_fails() {
        let rep = preflight("rm -rf /", Some("bash"), false).unwrap();
        assert_eq!(rep.verdict(), "fail");
        assert!(rep.issues.iter().any(|i| i.code == "RM-ROOT"));
    }

    #[test]
    fn tool_not_found() {
        let rep = preflight("definitely_not_a_tool_xyz", Some("bash"), true).unwrap();
        assert!(rep.issues.iter().any(|i| i.code == "TOOL-NOT-FOUND"));
    }

    #[test]
    fn unknown_shell_is_error() {
        assert!(preflight("ls", Some("fish"), false).is_err());
    }

    #[test]
    fn posix_cmd_downgrade_needs_path_hit() {
        // without path check there is no downgrade
        let rep = preflight("grep foo bar.txt", Some("powershell5"), false).unwrap();
        assert!(rep.issues.iter().any(|i| i.code == "POSIX-CMD" && i.severity == "error"));
    }

    #[test]
    fn posix_cmd_not_duplicated_as_tool_not_found() {
        // with a PATH cache lacking grep: POSIX-CMD present, TOOL-NOT-FOUND absent
        let rep = preflight("grep foo bar.txt", Some("powershell5"), true).unwrap();
        assert!(rep.issues.iter().any(|i| i.code == "POSIX-CMD"));
        assert!(!rep.issues.iter().any(|i| i.code == "TOOL-NOT-FOUND" && i.tool.as_deref() == Some("grep")));
    }
}
