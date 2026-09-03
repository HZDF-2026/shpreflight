//! Shell dialect registry and target detection.

pub const SHELLS: &[(&str, &str)] = &[
    ("powershell5", "Windows PowerShell 5.1 (powershell.exe)"),
    ("pwsh7", "PowerShell 7+ (pwsh.exe)"),
    ("bash", "Bash (Git Bash / WSL / Unix)"),
    ("cmd", "Windows cmd.exe"),
    ("sh", "POSIX sh"),
];

pub const POWERSHELL: &[&str] = &["powershell5", "pwsh7"];
pub const WINDOWS: &[&str] = &["powershell5", "pwsh7", "cmd"];

pub fn is_powershell(target: &str) -> bool {
    POWERSHELL.contains(&target)
}

pub fn is_windows(target: &str) -> bool {
    WINDOWS.contains(&target)
}

pub fn default_shell() -> &'static str {
    if cfg!(windows) {
        "powershell5"
    } else {
        "bash"
    }
}

pub fn resolve_target(shell: Option<&str>) -> Result<String, String> {
    let s = shell.unwrap_or("");
    if s.is_empty() || s == "auto" {
        return Ok(default_shell().to_string());
    }
    if SHELLS.iter().any(|(k, _)| *k == s) {
        return Ok(s.to_string());
    }
    Err(format!(
        "unknown shell {}; see `shpreflight shells`",
        crate::rules::py_repr(s)
    ))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn resolves_auto() {
        assert_eq!(resolve_target(None).unwrap(), default_shell());
        assert_eq!(resolve_target(Some("auto")).unwrap(), default_shell());
    }

    #[test]
    fn resolves_known() {
        assert_eq!(resolve_target(Some("bash")).unwrap(), "bash");
        assert_eq!(resolve_target(Some("pwsh7")).unwrap(), "pwsh7");
    }

    #[test]
    fn rejects_unknown() {
        assert!(resolve_target(Some("fish")).is_err());
    }

    #[test]
    fn families() {
        assert!(is_powershell("powershell5"));
        assert!(!is_powershell("bash"));
        assert!(is_windows("cmd"));
        assert!(!is_windows("bash"));
    }
}
