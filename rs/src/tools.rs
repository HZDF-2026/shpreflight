//! PATH resolution for the heads of every segment.
//!
//! Resolves what the shell itself would resolve — is the first word of each
//! command actually executable here? One readdir pass over the PATH builds
//! a name cache; every lookup after that is a set membership test, so
//! checking a five-segment pipeline costs the same as checking one command.

use std::collections::HashSet;
use std::sync::OnceLock;

use crate::segments::Segment;

/// Heads that are shell builtins or Windows internal commands: no PATH entry
/// to resolve, on either family of shells.
pub const BUILTINS: &[&str] = &[
    "if", "then", "else", "elif", "fi", "for", "while", "do", "done",
    "case", "esac", "function", "in", "export", "source", "set", "unset",
    "alias", "exit", "return", "cd", "chdir", "pushd", "popd", "echo",
    "read", "shift", "true", "false", "eval", "exec", "trap", "local",
    "declare", "readonly",
    // Windows cmd.exe / PowerShell internal commands
    "mkdir", "rmdir", "rd", "del", "erase", "copy", "xcopy", "move", "ren",
    "type", "cls", "dir", "ver", "vol", "call", "start", "title", "color",
    "prompt", "setlocal", "endlocal", "break", "md",
];

#[cfg(windows)]
const EXTS: &[&str] = &[".exe", ".cmd", ".bat", ".com"];
#[cfg(not(windows))]
const EXTS: &[&str] = &[];

pub fn path_names() -> &'static HashSet<String> {
    static CACHE: OnceLock<HashSet<String>> = OnceLock::new();
    CACHE.get_or_init(|| scan_path())
}

fn scan_path() -> HashSet<String> {
    let mut names = HashSet::new();
    let path = match std::env::var("PATH") {
        Ok(p) => p,
        Err(_) => return names,
    };
    let sep = if cfg!(windows) { ';' } else { ':' };
    for dir in path.split(sep) {
        if dir.is_empty() {
            continue;
        }
        if let Ok(entries) = std::fs::read_dir(dir) {
            for entry in entries.flatten() {
                let n = entry.file_name().to_string_lossy().into_owned();
                names.insert(n.clone());
                for ext in EXTS {
                    if let Some(stem) = n.strip_suffix(ext) {
                        names.insert(stem.to_string());
                        break;
                    }
                }
            }
        }
    }
    names
}

#[derive(Clone, Debug, PartialEq)]
pub struct ToolInfo {
    pub name: String,
    pub status: &'static str,
    pub path: Option<String>,
}

pub fn check_tools(segments: &[Segment], path_check: bool) -> Vec<ToolInfo> {
    if !path_check {
        return Vec::new();
    }
    let names = path_names();
    check_tools_cached(segments, names)
}

pub fn check_tools_cached(segments: &[Segment], names: &HashSet<String>) -> Vec<ToolInfo> {
    let mut results: Vec<ToolInfo> = Vec::new();
    let mut seen: HashSet<String> = HashSet::new();
    for seg in segments {
        let head = match seg.head.as_deref() {
            Some(h) if !h.is_empty() => h,
            _ => continue,
        };
        if BUILTINS.contains(&head) || seen.contains(head) {
            continue;
        }
        seen.insert(head.to_string());
        let found = names.contains(head);
        results.push(ToolInfo {
            name: head.to_string(),
            status: if found { "found" } else { "missing" },
            path: if found { Some(head.to_string()) } else { None },
        });
    }
    results
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::lex::lex;
    use crate::segments::split_segments;

    fn segs_of(cmd: &str) -> Vec<Segment> {
        split_segments(&lex(cmd))
    }

    fn names(list: &[&str]) -> HashSet<String> {
        list.iter().map(|s| s.to_string()).collect()
    }

    #[test]
    fn skips_builtins_and_dedups() {
        let ns = names(&["git", "rg"]);
        let tools = check_tools_cached(&segs_of("cd x && git st && git log"), &ns);
        assert_eq!(tools.len(), 1);
        assert_eq!(tools[0].name, "git");
        assert_eq!(tools[0].status, "found");
    }

    #[test]
    fn missing_reported() {
        let ns = names(&["git"]);
        let tools = check_tools_cached(&segs_of("zzz_tool"), &ns);
        assert_eq!(tools.len(), 1);
        assert_eq!(tools[0].status, "missing");
        assert_eq!(tools[0].path, None);
    }

    #[test]
    fn order_preserved() {
        let ns = names(&["b", "a"]);
        let tools = check_tools_cached(&segs_of("a | b"), &ns);
        let order: Vec<&str> = tools.iter().map(|t| t.name.as_str()).collect();
        assert_eq!(order, vec!["a", "b"]);
    }

    #[test]
    fn path_check_off() {
        assert!(check_tools(&segs_of("zzz"), false).is_empty());
    }
}
