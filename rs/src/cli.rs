//! CLI: check / shells.
//!
//! Exit codes are machine-readable so agents can branch on them:
//! 0 ok, 1 warn, 2 fail, 3 usage error.

use crate::check::preflight;
use crate::report::EXIT_OK;
use crate::shells::SHELLS;

const USAGE: &str = "usage: shpreflight <command> [options]

commands:
  check    diagnose a command before running it
  shells   list supported shell dialects

check options:
  --command words...    the shell command to check (quote it, or use --stdin)
  --stdin               read the command from stdin instead
  --shell SHELL         target shell: auto (default), powershell5, pwsh7, bash, cmd, sh
  --format FORMAT       output for humans (text, default) or agents (json)
  --no-path-check       skip PATH resolution of command heads";

struct CheckArgs {
    positionals: Vec<String>,
    shell: String,
    format: String,
    use_stdin: bool,
    no_path_check: bool,
}

/// Splits flags from positional command words. Only exact matches of known
/// flags are options; anything else (including "-rf",
/// "--flag-of-the-command") is part of the command, matching how the Python
/// reference accepts intermixed positionals.
fn parse_check_args(args: &[String]) -> Result<CheckArgs, String> {
    let mut out = CheckArgs {
        positionals: Vec::new(),
        shell: "auto".to_string(),
        format: "text".to_string(),
        use_stdin: false,
        no_path_check: false,
    };
    let mut i = 0;
    while i < args.len() {
        let a = args[i].as_str();
        if a == "--stdin" {
            out.use_stdin = true;
        } else if a == "--no-path-check" {
            out.no_path_check = true;
        } else if a == "--shell" || a == "--format" {
            if i + 1 >= args.len() {
                return Err(format!("{} requires a value", a));
            }
            i += 1;
            let val = args[i].clone();
            if a == "--shell" {
                out.shell = val;
            } else {
                out.format = val;
            }
        } else if let Some(v) = a.strip_prefix("--shell=") {
            out.shell = v.to_string();
        } else if let Some(v) = a.strip_prefix("--format=") {
            out.format = v.to_string();
        } else if a == "--" {
            out.positionals.extend(args[i + 1..].iter().cloned());
            break;
        } else {
            out.positionals.push(a.to_string());
        }
        i += 1;
    }
    if out.format != "text" && out.format != "json" {
        return Err(format!(
            "invalid --format choice: '{}' (choose from 'text', 'json')",
            out.format
        ));
    }
    Ok(out)
}

/// Runs the CLI against in-memory buffers and returns (exit code, stdout,
/// stderr) — used by both main() and the integration tests.
pub fn run(args: &[String], stdin: &str) -> (i32, String, String) {
    let mut stdout = String::new();
    let mut stderr = String::new();
    let code = run_with(args, stdin, &mut stdout, &mut stderr);
    (code, stdout, stderr)
}

pub fn run_with(args: &[String], stdin: &str, stdout: &mut String, stderr: &mut String) -> i32 {
    if args.is_empty() {
        stderr.push_str(USAGE);
        stderr.push('\n');
        return 3;
    }
    match args[0].as_str() {
        "check" => run_check(&args[1..], stdin, stdout, stderr),
        "shells" => {
            run_shells(stdout);
            EXIT_OK
        }
        "-h" | "--help" | "help" => {
            stdout.push_str(USAGE);
            stdout.push('\n');
            EXIT_OK
        }
        other => {
            stderr.push_str(&format!("error: unknown command {}\n", crate::rules::py_repr(other)));
            stderr.push_str(USAGE);
            stderr.push('\n');
            3
        }
    }
}

fn run_shells(stdout: &mut String) {
    stdout.push_str("{\n");
    for (i, (key, desc)) in SHELLS.iter().enumerate() {
        let comma = if i + 1 < SHELLS.len() { "," } else { "" };
        stdout.push_str(&format!("  {:?}: {:?}{}\n", key, desc, comma));
    }
    stdout.push('}');
    stdout.push('\n');
}

fn run_check(args: &[String], stdin: &str, stdout: &mut String, stderr: &mut String) -> i32 {
    let parsed = match parse_check_args(args) {
        Ok(p) => p,
        Err(e) => {
            stderr.push_str(&format!("error: {}\n", e));
            return 3;
        }
    };

    let cmd: String = if parsed.use_stdin {
        stdin.trim().to_string()
    } else if !parsed.positionals.is_empty() {
        parsed.positionals.join(" ").trim().to_string()
    } else {
        stderr.push_str("error: no command given\n");
        return 3;
    };
    if cmd.is_empty() {
        stderr.push_str("error: empty command\n");
        return 3;
    }

    let rep = match preflight(&cmd, Some(&parsed.shell), !parsed.no_path_check) {
        Ok(r) => r,
        Err(e) => {
            stderr.push_str(&format!("error: {}\n", e));
            return 3;
        }
    };
    if parsed.format == "json" {
        stdout.push_str(&rep.to_json());
        stdout.push('\n');
    } else {
        stdout.push_str(&rep.to_text());
        stdout.push('\n');
    }
    rep.exit_code()
}
