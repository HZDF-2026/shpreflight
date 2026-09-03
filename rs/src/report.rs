//! Findings model and rendering: JSON for agents, text for humans.

use crate::tools::ToolInfo;

pub const VERDICTS: [&str; 3] = ["ok", "warn", "fail"];
pub const EXIT_OK: i32 = 0;
pub const EXIT_WARN: i32 = 1;
pub const EXIT_FAIL: i32 = 2;

fn severity_rank(sev: &str) -> i32 {
    match sev {
        "error" => 2,
        "warning" => 1,
        _ => 0,
    }
}

#[derive(Clone, Debug)]
pub struct Issue {
    pub code: String,
    pub severity: String,
    pub kind: String,
    pub message: String,
    pub fix: Option<String>,
    pub tool: Option<String>,
}

impl Issue {
    pub fn new(code: &str, severity: &str, kind: &str, message: &str) -> Issue {
        Issue {
            code: code.to_string(),
            severity: severity.to_string(),
            kind: kind.to_string(),
            message: message.to_string(),
            fix: None,
            tool: None,
        }
    }

    pub fn with_fix(mut self, fix: &str) -> Issue {
        self.fix = Some(fix.to_string());
        self
    }

    pub fn with_tool(mut self, tool: &str) -> Issue {
        self.tool = Some(tool.to_string());
        self
    }
}

#[derive(Clone, Debug)]
pub struct Report {
    pub command: String,
    pub target: String,
    pub issues: Vec<Issue>,
    pub tools: Vec<ToolInfo>,
    pub elapsed_ms: f64,
}

impl Report {
    pub fn new(command: &str, target: &str, issues: Vec<Issue>, tools: Vec<ToolInfo>, elapsed_ms: f64) -> Report {
        Report {
            command: command.to_string(),
            target: target.to_string(),
            issues,
            tools,
            elapsed_ms,
        }
    }

    pub fn verdict(&self) -> &'static str {
        let mut worst = 0;
        for i in &self.issues {
            let rank = severity_rank(&i.severity);
            if rank > worst {
                worst = rank;
            }
        }
        VERDICTS[worst as usize]
    }

    pub fn exit_code(&self) -> i32 {
        match self.verdict() {
            "warn" => EXIT_WARN,
            "fail" => EXIT_FAIL,
            _ => EXIT_OK,
        }
    }

    pub fn errors(&self) -> usize {
        self.issues.iter().filter(|i| i.severity == "error").count()
    }

    pub fn warnings(&self) -> usize {
        self.issues.iter().filter(|i| i.severity == "warning").count()
    }

    /// Renders the agent-facing report. Field order, 2-space indentation and
    /// non-ASCII passthrough match the Python reference implementation
    /// (json.dumps(..., indent=2, ensure_ascii=False)).
    pub fn to_json(&self) -> String {
        let root = jobj(vec![
            ("command".into(), jstr(&self.command)),
            ("target".into(), jstr(&self.target)),
            ("verdict".into(), jstr(self.verdict())),
            ("errors".into(), JVal::Num(self.errors().to_string())),
            ("warnings".into(), JVal::Num(self.warnings().to_string())),
            (
                "issues".into(),
                JVal::Arr(
                    self.issues
                        .iter()
                        .map(|i| {
                            let mut fields = vec![
                                ("code".into(), jstr(&i.code)),
                                ("severity".into(), jstr(&i.severity)),
                                ("kind".into(), jstr(&i.kind)),
                                ("message".into(), jstr(&i.message)),
                            ];
                            if let Some(fix) = &i.fix {
                                fields.push(("fix".into(), jstr(fix)));
                            }
                            if let Some(tool) = &i.tool {
                                fields.push(("tool".into(), jstr(tool)));
                            }
                            jobj(fields)
                        })
                        .collect(),
                ),
            ),
            (
                "tools".into(),
                JVal::Arr(
                    self.tools
                        .iter()
                        .map(|t| {
                            jobj(vec![
                                ("name".into(), jstr(&t.name)),
                                ("status".into(), jstr(t.status)),
                                (
                                    "path".into(),
                                    match &t.path {
                                        Some(p) => jstr(p),
                                        None => JVal::Null,
                                    },
                                ),
                            ])
                        })
                        .collect(),
                ),
            ),
            ("elapsed_ms".into(), JVal::Num(fmt_f64(round3(self.elapsed_ms)))),
        ]);
        render(&root, 0)
    }

    pub fn to_text(&self) -> String {
        let mut lines = vec![format!(
            "shpreflight: {} ({} error(s), {} warning(s)) for {}",
            self.verdict(),
            self.errors(),
            self.warnings(),
            self.target
        )];
        for i in &self.issues {
            lines.push(format!("  {} [{}] {}: {}", i.code, i.severity, i.kind, i.message));
            if let Some(fix) = &i.fix {
                lines.push(format!("    fix: {}", fix));
            }
        }
        for t in &self.tools {
            if t.status == "missing" {
                lines.push(format!("  {}: not found on PATH", t.name));
            }
        }
        if self.issues.is_empty() {
            lines.push("  no issues found".to_string());
        }
        lines.join("\n")
    }
}

fn round3(v: f64) -> f64 {
    (v * 1000.0).round() / 1000.0
}

fn fmt_f64(v: f64) -> String {
    let s = format!("{}", v);
    if s.contains('.') || s.contains('e') || s.contains("inf") || s.contains("NaN") {
        s
    } else {
        format!("{}.0", s)
    }
}

// --- minimal JSON value renderer matching json.dumps(indent=2) ---

enum JVal {
    Str(String),
    Num(String),
    Null,
    Arr(Vec<JVal>),
    Obj(Vec<(String, JVal)>),
}

fn jstr(s: &str) -> JVal {
    JVal::Str(s.to_string())
}

fn jobj(fields: Vec<(String, JVal)>) -> JVal {
    JVal::Obj(fields)
}

fn indent_of(level: usize) -> String {
    " ".repeat(level * 2)
}

fn render(v: &JVal, level: usize) -> String {
    match v {
        JVal::Str(s) => json_escape(s),
        JVal::Num(n) => n.clone(),
        JVal::Null => "null".to_string(),
        JVal::Arr(items) => {
            if items.is_empty() {
                return "[]".to_string();
            }
            let inner: Vec<String> = items
                .iter()
                .map(|it| format!("{}{}", indent_of(level + 1), render(it, level + 1)))
                .collect();
            format!("[\n{}\n{}]", inner.join(",\n"), indent_of(level))
        }
        JVal::Obj(fields) => {
            let inner: Vec<String> = fields
                .iter()
                .map(|(k, val)| {
                    format!("{}{}: {}", indent_of(level + 1), json_escape(k), render(val, level + 1))
                })
                .collect();
            format!("{{\n{}\n{}}}", inner.join(",\n"), indent_of(level))
        }
    }
}

fn json_escape(s: &str) -> String {
    let mut out = String::with_capacity(s.len() + 2);
    out.push('"');
    for ch in s.chars() {
        match ch {
            '"' => out.push_str("\\\""),
            '\\' => out.push_str("\\\\"),
            '\n' => out.push_str("\\n"),
            '\r' => out.push_str("\\r"),
            '\t' => out.push_str("\\t"),
            '\u{8}' => out.push_str("\\b"),
            '\u{c}' => out.push_str("\\f"),
            c if (c as u32) < 0x20 => out.push_str(&format!("\\u{:04x}", c as u32)),
            c => out.push(c),
        }
    }
    out.push('"');
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    fn sample() -> Report {
        Report::new(
            "rm -rf /",
            "bash",
            vec![Issue::new(
                "RM-ROOT",
                "error",
                "danger",
                "rm: recursive force-delete aimed at a root/home/glob path",
            )
            .with_fix("restrict the target path")],
            vec![],
            0.1234,
        )
    }

    #[test]
    fn verdict_and_exit_code() {
        let rep = sample();
        assert_eq!(rep.verdict(), "fail");
        assert_eq!(rep.exit_code(), EXIT_FAIL);
        let ok = Report::new("ls", "bash", vec![], vec![], 0.1);
        assert_eq!(ok.verdict(), "ok");
        assert_eq!(ok.exit_code(), EXIT_OK);
        let warn = Report::new(
            "ls",
            "bash",
            vec![Issue::new("W", "warning", "danger", "msg")],
            vec![],
            0.1,
        );
        assert_eq!(warn.verdict(), "warn");
        assert_eq!(warn.exit_code(), EXIT_WARN);
    }

    #[test]
    fn json_shape() {
        let j = sample().to_json();
        assert_eq!(
            j,
            "{\n  \"command\": \"rm -rf /\",\n  \"target\": \"bash\",\n  \"verdict\": \"fail\",\n  \
             \"errors\": 1,\n  \"warnings\": 0,\n  \"issues\": [\n    {\n      \"code\": \"RM-ROOT\",\n      \
             \"severity\": \"error\",\n      \"kind\": \"danger\",\n      \"message\": \"rm: recursive \
             force-delete aimed at a root/home/glob path\",\n      \"fix\": \"restrict the target path\"\n    \
             }\n  ],\n  \"tools\": [],\n  \"elapsed_ms\": 0.123\n}"
        );
    }

    #[test]
    fn json_empty_issues_and_tools() {
        let j = Report::new("ls", "bash", vec![], vec![], 0.0).to_json();
        assert!(j.contains("\"issues\": [],"));
        assert!(j.contains("\"tools\": [],"));
        assert!(j.contains("\"elapsed_ms\": 0.0"));
        assert!(j.ends_with("\n}"));
    }

    #[test]
    fn json_with_tool_and_null_path() {
        let tools = vec![ToolInfo { name: "zz".into(), status: "missing", path: None }];
        let j = Report::new("zz", "bash", vec![], tools, 1.0).to_json();
        assert!(j.contains("\"tools\": [\n    {\n      \"name\": \"zz\",\n      \"status\": \"missing\",\n      \"path\": null\n    }\n  ]"));
    }

    #[test]
    fn text_render() {
        let t = sample().to_text();
        assert!(t.starts_with("shpreflight: fail (1 error(s), 0 warning(s)) for bash"));
        assert!(t.contains("  RM-ROOT [error] danger: rm:"));
        assert!(t.contains("    fix: restrict the target path"));
        let ok = Report::new("ls", "bash", vec![], vec![], 0.1).to_text();
        assert!(ok.contains("  no issues found"));
    }

    #[test]
    fn escaping() {
        let j = Report::new("echo \"x\"", "bash", vec![], vec![], 0.0).to_json();
        assert!(j.contains("\"command\": \"echo \\\"x\\\"\""));
    }
}
