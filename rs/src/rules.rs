//! Dialect-compatibility rules: will this command actually run on the target?
//!
//! Each rule is triggered by concrete token evidence and fires only for the
//! shells where it genuinely breaks. Severity is "error" when the command
//! will fail (or silently do the wrong thing) and "warning" when it merely
//! smells wrong.

use crate::lex::{is_closed, Kind, Token};
use crate::report::Issue;
use crate::segments::Segment;
use crate::shells::{is_powershell, is_windows};

/// `$[A-Za-z_][A-Za-z0-9_]*` (full match) — a bare bash-style variable.
fn is_dollar_var(text: &str) -> bool {
    let cs: Vec<char> = text.chars().collect();
    if cs.len() < 2 || cs[0] != '$' {
        return false;
    }
    if !(cs[1].is_ascii_alphabetic() || cs[1] == '_') {
        return false;
    }
    cs[2..].iter().all(|c| c.is_ascii_alphanumeric() || *c == '_')
}

/// `${[A-Za-z_][A-Za-z0-9_]*}` (full match) — a braced bash-style variable.
fn is_brace_var(text: &str) -> bool {
    let cs: Vec<char> = text.chars().collect();
    if cs.len() < 4 || cs[0] != '$' || cs[1] != '{' || cs[cs.len() - 1] != '}' {
        return false;
    }
    let name: String = cs[2..cs.len() - 1].iter().collect();
    is_name(&name)
}

fn is_name(name: &str) -> bool {
    let cs: Vec<char> = name.chars().collect();
    if cs.is_empty() || !(cs[0].is_ascii_alphabetic() || cs[0] == '_') {
        return false;
    }
    cs[1..].iter().all(|c| c.is_ascii_alphanumeric() || *c == '_')
}

const SPECIAL_VARS: &[&str] = &[
    "$?", "$!", "$$", "$#", "$@", "$*", "$0", "$1", "$2",
    "$3", "$4", "$5", "$6", "$7", "$8", "$9",
];

/// `[A-Z][A-Za-z]+-[A-Z][A-Za-z]+` (full match) — a PowerShell cmdlet name.
fn is_cmdlet(s: &str) -> bool {
    let cs: Vec<char> = s.chars().collect();
    let n = cs.len();
    if n < 5 {
        return false;
    }
    if !cs[0].is_ascii_uppercase() {
        return false;
    }
    let mut i = 1;
    let mut word1 = 0;
    while i < n && (cs[i].is_ascii_alphabetic()) {
        i += 1;
        word1 += 1;
    }
    if word1 == 0 || i >= n || cs[i] != '-' {
        return false;
    }
    i += 1;
    if i >= n || !cs[i].is_ascii_uppercase() {
        return false;
    }
    let mut word2 = 0;
    while i < n && cs[i].is_ascii_alphabetic() {
        i += 1;
        word2 += 1;
    }
    word2 > 0 && i == n
}

/// POSIX commands with no Windows PowerShell 5.1 equivalent alias, plus the
/// native replacement agents should switch to.
const POSIX_TO_PS: &[(&str, &str)] = &[
    ("grep", "Select-String (sls)"),
    ("sed", "-replace operator"),
    ("awk", "ForEach-Object + split"),
    ("find", "Get-ChildItem -Recurse"),
    ("head", "Select-Object -First N"),
    ("tail", "Get-Content -Tail N"),
    ("touch", "New-Item -ItemType File"),
    ("chmod", "icacls"),
    ("chown", "icacls"),
    ("which", "Get-Command"),
    ("xargs", "ForEach-Object"),
    ("df", "Get-PSDrive"),
    ("du", "Get-ChildItem -Recurse | Measure-Object Length -Sum"),
    ("wc", "Measure-Object -Line/-Word/-Character"),
    ("tr", "-replace / .ToLower"),
    ("cut", ".Split()"),
    ("uniq", "Select-Object -Unique"),
    ("uname", "$env:OS"),
    ("env", "Get-ChildItem env:"),
    ("basename", "Split-Path -Leaf"),
    ("dirname", "Split-Path -Parent"),
    ("ln", "New-Item -ItemType SymbolicLink"),
    ("readlink", "(Get-Item).Target"),
    ("mktemp", "New-TemporaryFile"),
    ("stat", "Get-Item"),
    ("less", "more.com / Out-Host -Paging"),
    ("open", "Start-Process (alias on macOS only)"),
    ("make", "external: install via winget"),
];

/// Flags curl-style CLIs understand; when 'curl' is the PowerShell alias for
/// Invoke-WebRequest (PS 5.1 default) these flags are rejected outright.
fn is_curl_flag(w: &str) -> bool {
    let cs: Vec<char> = w.chars().collect();
    // ^-[a-zA-Z]+ — prefix match
    if cs.len() >= 2 && cs[0] == '-' && cs[1].is_ascii_alphabetic() {
        return true;
    }
    // ^--[a-zA-Z-]+$ — full match
    if cs.len() > 2 && cs[0] == '-' && cs[1] == '-' {
        return cs[2..].iter().all(|c| c.is_ascii_alphabetic() || *c == '-');
    }
    false
}

/// Python `repr()` of a short string, for message fidelity.
pub fn py_repr(s: &str) -> String {
    let has_sq = s.contains('\'');
    let has_dq = s.contains('"');
    let quote = if has_sq && !has_dq { '"' } else { '\'' };
    let mut out = String::new();
    out.push(quote);
    for ch in s.chars() {
        match ch {
            '\\' => out.push_str("\\\\"),
            '\n' => out.push_str("\\n"),
            '\t' => out.push_str("\\t"),
            '\r' => out.push_str("\\r"),
            c if c == quote => {
                out.push('\\');
                out.push(c);
            }
            c => out.push(c),
        }
    }
    out.push(quote);
    out
}

pub fn check_dialect(tokens: &[Token], segments: &[Segment], target: &str) -> Vec<Issue> {
    let mut issues = Vec::new();
    let ps = is_powershell(target);
    let win = is_windows(target);
    let on_bash = target == "bash" || target == "sh";

    for tok in tokens {
        if matches!(tok.kind, Kind::Squote | Kind::Dquote | Kind::Backtick) && !is_closed(tok) {
            let head: String = tok.text.chars().take(12).collect();
            issues.push(Issue::new(
                "UNCLOSED-QUOTE",
                "error",
                "syntax",
                &format!(
                    "unclosed quote starting at {} — the shell will hang or fail",
                    py_repr(&head)
                ),
            )
            .with_fix("close the quote"));
        }
    }

    for tok in tokens {
        let text = tok.text.as_str();
        match tok.kind {
            Kind::Op => {
                if text == "&&" && (target == "powershell5" || target == "cmd") {
                    issues.push(Issue::new(
                        "SEP-AND",
                        "error",
                        "syntax",
                        "operator '&&' is not supported in this shell",
                    )
                    .with_fix("separate commands, or chain with ';' if order alone matters"));
                } else if text == "||" && (target == "powershell5" || target == "cmd") {
                    issues.push(Issue::new(
                        "SEP-OR",
                        "error",
                        "syntax",
                        "operator '||' is not supported in this shell",
                    )
                    .with_fix("separate commands and check exit codes explicitly"));
                }
            }
            Kind::Word => {
                if text == "/dev/null" && win {
                    issues.push(Issue::new(
                        "REDIR-DEVNULL",
                        "error",
                        "syntax",
                        "redirection to /dev/null — path does not exist on Windows",
                    )
                    .with_fix("redirect to NUL instead:  2>NUL"));
                } else if (ps || target == "cmd") && is_dollar_var(text) {
                    issues.push(Issue::new(
                        "ENV-VAR",
                        "error",
                        "syntax",
                        &format!(
                            "bash-style variable {} — PowerShell reads it as an unset PS variable",
                            text
                        ),
                    )
                    .with_fix(&format!("use the environment: $env:{}", &text[1..])));
                } else if ps && is_brace_var(text) {
                    let name: String = text.chars().skip(2).take(text.chars().count() - 3).collect();
                    issues.push(Issue::new(
                        "BRACE-VAR",
                        "error",
                        "syntax",
                        "bash-style ${var} expansion is not PowerShell syntax",
                    )
                    .with_fix(&format!("use $env:{}", name)));
                } else if ps && SPECIAL_VARS.contains(&text) {
                    issues.push(Issue::new(
                        "SPECIAL-VAR",
                        "error",
                        "syntax",
                        &format!(
                            "{} means one thing in bash and another in PowerShell (e.g. bash $? is the numeric exit code, PS $? is a bool)",
                            text
                        ),
                    )
                    .with_fix("use $LASTEXITCODE for native exit codes"));
                } else if text == "export" && (ps || target == "cmd") {
                    issues.push(Issue::new(
                        "EXPORT",
                        "error",
                        "syntax",
                        "'export' is not available in this shell",
                    )
                    .with_fix("PowerShell: $env:NAME = \"value\"   cmd: set NAME=value"));
                } else if text == "source" && ps {
                    issues.push(Issue::new(
                        "SOURCE",
                        "error",
                        "syntax",
                        "'source' is not available in PowerShell",
                    )
                    .with_fix("dot-source it:  . ./script.ps1"));
                }
            }
            Kind::Backtick => {
                if ps || target == "cmd" {
                    issues.push(Issue::new(
                        "BACKTICK",
                        "error",
                        "syntax",
                        "backticks mean command substitution in bash but are the escape character in PowerShell — the command will do something else entirely",
                    )
                    .with_fix("use $(...) for subexpressions in PowerShell"));
                }
            }
            _ => {}
        }
    }

    for seg in segments {
        let head = match seg.head.as_deref() {
            Some(h) => h,
            None => continue,
        };
        let rest: Vec<&str> = seg.words.iter().skip(1).map(|w| w.as_str()).collect();
        if ps && head == "curl" && rest.iter().any(|w| is_curl_flag(w)) {
            issues.push(Issue::new(
                "CURL-ALIAS",
                "error",
                "tool",
                "'curl' is an alias for Invoke-WebRequest in Windows PowerShell 5.1 and rejects unix curl flags",
            )
            .with_fix("call the real binary: curl.exe -sL ..."));
        }
        const RM_FLAG_SET: &[&str] = &["-r", "-f", "-rf", "-fr", "-Rf", "-R"];
        if ps && ["rm", "del", "erase", "rd"].contains(&head)
            && rest.iter().any(|w| RM_FLAG_SET.contains(w))
        {
            issues.push(Issue::new(
                "RM-FLAGS",
                "error",
                "syntax",
                &format!(
                    "'{}' maps to Remove-Item in PowerShell, which has no -r/-f short flags",
                    head
                ),
            )
            .with_fix("Remove-Item -Recurse -Force"));
        }
        if on_bash && is_cmdlet(head) {
            issues.push(Issue::new(
                "CMDLET-IN-POSIX",
                "error",
                "tool",
                &format!("'{}' is a PowerShell cmdlet; it does not exist in {}", head, target),
            )
            .with_fix("use the POSIX equivalent for this shell"));
        }
        if win {
            if let Some((_, replacement)) = POSIX_TO_PS.iter().find(|(k, _)| *k == head) {
                issues.push(
                    Issue::new(
                        "POSIX-CMD",
                        "error",
                        "tool",
                        &format!("'{}' is not a Windows PowerShell command", head),
                    )
                    .with_fix(replacement)
                    .with_tool(head),
                );
            }
        }
    }
    issues
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn dollar_vars() {
        assert!(is_dollar_var("$PATH"));
        assert!(is_dollar_var("$HOME2"));
        assert!(!is_dollar_var("$1"));
        assert!(!is_dollar_var("PATH"));
        assert!(!is_dollar_var("${PATH}"));
    }

    #[test]
    fn brace_vars() {
        assert!(is_brace_var("${PATH}"));
        assert!(!is_brace_var("${}"));
        assert!(!is_brace_var("$PATH}"));
    }

    #[test]
    fn cmdlet_shapes() {
        assert!(is_cmdlet("Get-ChildItem"));
        assert!(is_cmdlet("Select-String"));
        assert!(!is_cmdlet("grep"));
        assert!(!is_cmdlet("Get"));
        assert!(!is_cmdlet("get-childitem"));
    }

    #[test]
    fn curl_flags() {
        assert!(is_curl_flag("-sL"));
        assert!(is_curl_flag("--force"));
        assert!(!is_curl_flag("-"));
        assert!(!is_curl_flag("-9"));
    }
}
