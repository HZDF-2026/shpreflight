//! End-to-end CLI tests: exit codes, output formats, flag handling.

use shpreflight::cli::run;

fn a(args: &[&str]) -> Vec<String> {
    args.iter().map(|s| s.to_string()).collect()
}

fn codes_from(cmd: &str, shell: &str) -> Vec<String> {
    let (_, out, _) = run(&a(&["check", cmd, "--shell", shell, "--no-path-check"]), "");
    // text format: lines like "  CODE [severity] kind: ..."
    out.lines()
        .filter_map(|l| l.trim().split(' ').next())
        .filter(|t| t.contains('-') && t.chars().next().map_or(false, |c| c.is_ascii_uppercase()))
        .map(|s| s.to_string())
        .collect()
}

#[test]
fn ok_exit_zero() {
    let (code, out, _) = run(&a(&["check", "echo", "hi"]), "");
    assert_eq!(code, 0);
    assert!(out.contains("shpreflight: ok"));
}

#[test]
fn fail_exit_two() {
    let (code, out, _) = run(&a(&["check", "rm", "-rf", "/"]), "");
    assert_eq!(code, 2);
    assert!(out.contains("RM-ROOT"));
}

#[test]
fn warn_exit_one() {
    let (code, out, _) = run(&a(&["check", "git", "reset", "--hard", "--no-path-check"]), "");
    assert_eq!(code, 1);
    assert!(out.contains("GIT-RESET-HARD"));
}

#[test]
fn json_format() {
    let (code, out, _) = run(&a(&["check", "rm", "-rf", "/", "--format", "json"]), "");
    assert_eq!(code, 2);
    assert!(out.starts_with("{\n  \"command\": \"rm -rf /\","));
    assert!(out.contains("\"verdict\": \"fail\""));
    assert!(out.contains("\"elapsed_ms\":"));
}

#[test]
fn stdin_input() {
    let (code, out, _) = run(&a(&["check", "--stdin"]), "rm -rf /\n");
    assert_eq!(code, 2);
    assert!(out.contains("RM-ROOT"));
}

#[test]
fn no_command_is_usage_error() {
    let (code, _, err) = run(&a(&["check"]), "");
    assert_eq!(code, 3);
    assert!(err.contains("no command given"));
}

#[test]
fn empty_command_is_usage_error() {
    let (code, _, err) = run(&a(&["check", ""]), "");
    assert_eq!(code, 3);
    assert!(err.contains("empty command"));
}

#[test]
fn unknown_shell_is_usage_error() {
    let (code, _, err) = run(&a(&["check", "ls", "--shell", "fish"]), "");
    assert_eq!(code, 3);
    assert!(err.contains("unknown shell"));
}

#[test]
fn bad_format_is_usage_error() {
    let (code, _, err) = run(&a(&["check", "ls", "--format", "yaml"]), "");
    assert_eq!(code, 3);
    assert!(err.contains("invalid --format choice"));
}

#[test]
fn no_args_shows_usage() {
    let (code, _, err) = run(&a(&[]), "");
    assert_eq!(code, 3);
    assert!(err.contains("usage: shpreflight"));
}

#[test]
fn unknown_command_shows_usage() {
    let (code, _, err) = run(&a(&["frobnicate"]), "");
    assert_eq!(code, 3);
    assert!(err.contains("unknown command"));
}

#[test]
fn help_shows_usage() {
    for h in ["-h", "--help", "help"] {
        let (code, out, _) = run(&a(&[h]), "");
        assert_eq!(code, 0);
        assert!(out.contains("usage: shpreflight"));
    }
}

#[test]
fn shells_lists_all() {
    let (code, out, _) = run(&a(&["shells"]), "");
    assert_eq!(code, 0);
    for k in ["powershell5", "pwsh7", "bash", "cmd", "sh"] {
        assert!(out.contains(&format!("\"{}\"", k)));
    }
}

#[test]
fn double_dash_stops_flag_parsing() {
    let (code, out, _) = run(&a(&["check", "--", "rm", "-rf", "/"]), "");
    assert_eq!(code, 2);
    assert!(out.contains("RM-ROOT"));
}

#[test]
fn equals_form_of_flags() {
    let (code, out, _) = run(&a(&["check", "rm", "-rf", "/", "--format=json"]), "");
    assert_eq!(code, 2);
    assert!(out.starts_with('{'));
}

#[test]
fn command_flags_are_not_ours() {
    // "-rf" and "--flag" belong to the command, not to shpreflight
    let (code, out, err) = run(&a(&["check", "rm", "-rf", "/", "--no-path-check"]), "");
    assert_eq!(code, 2);
    assert!(out.contains("RM-ROOT"));
    assert!(!err.contains("requires a value"));
}

#[test]
fn dialect_rules_fire_per_shell() {
    assert!(codes_from("a && b", "powershell5").contains(&"SEP-AND".to_string()));
    assert!(codes_from("a && b", "cmd").contains(&"SEP-AND".to_string()));
    assert!(!codes_from("a && b", "pwsh7").contains(&"SEP-AND".to_string()));
    assert!(!codes_from("a && b", "bash").contains(&"SEP-AND".to_string()));
    assert!(codes_from("grep foo", "powershell5").contains(&"POSIX-CMD".to_string()));
    assert!(codes_from("echo x > /dev/null", "cmd").contains(&"REDIR-DEVNULL".to_string()));
    assert!(codes_from("echo $HOME", "powershell5").contains(&"ENV-VAR".to_string()));
}

#[test]
fn tool_not_found_in_json() {
    let (_, out, _) = run(&a(&["check", "zz_no_such_tool_abc", "--format", "json"]), "");
    assert!(out.contains("\"TOOL-NOT-FOUND\""));
    assert!(out.contains("\"status\": \"missing\""));
}
