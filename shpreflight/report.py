"""Findings model and rendering: JSON for agents, text for humans."""

from __future__ import annotations

import json

VERDICTS = ("ok", "warn", "fail")
EXIT_OK, EXIT_WARN, EXIT_FAIL = 0, 1, 2

SEVERITY_RANK = {"error": 2, "warning": 1, "info": 0}


class Issue:
    __slots__ = ("code", "severity", "kind", "message", "fix", "tool")

    def __init__(self, code: str, severity: str, kind: str, message: str,
                 fix: str | None = None, tool: str | None = None):
        self.code = code
        self.severity = severity
        self.kind = kind
        self.message = message
        self.fix = fix
        self.tool = tool

    def to_dict(self) -> dict:
        d = {"code": self.code, "severity": self.severity, "kind": self.kind,
             "message": self.message}
        if self.fix:
            d["fix"] = self.fix
        if self.tool:
            d["tool"] = self.tool
        return d


class Report:
    __slots__ = ("command", "target", "issues", "tools", "elapsed_ms")

    def __init__(self, command: str, target: str, issues: list[Issue],
                 tools: list[dict], elapsed_ms: float):
        self.command = command
        self.target = target
        self.issues = issues
        self.tools = tools
        self.elapsed_ms = elapsed_ms

    @property
    def verdict(self) -> str:
        worst = 0
        for issue in self.issues:
            rank = SEVERITY_RANK[issue.severity]
            if rank > worst:
                worst = rank
        return ("ok", "warn", "fail")[worst]

    @property
    def exit_code(self) -> int:
        return {"ok": EXIT_OK, "warn": EXIT_WARN, "fail": EXIT_FAIL}[self.verdict]

    def to_dict(self) -> dict:
        errors = sum(1 for i in self.issues if i.severity == "error")
        warnings = sum(1 for i in self.issues if i.severity == "warning")
        return {
            "command": self.command,
            "target": self.target,
            "verdict": self.verdict,
            "errors": errors,
            "warnings": warnings,
            "issues": [i.to_dict() for i in self.issues],
            "tools": self.tools,
            "elapsed_ms": round(self.elapsed_ms, 3),
        }

    def to_json(self) -> str:
        return json.dumps(self.to_dict(), indent=2, ensure_ascii=False)

    def to_text(self) -> str:
        errors = sum(1 for i in self.issues if i.severity == "error")
        warnings = sum(1 for i in self.issues if i.severity == "warning")
        head = f"shpreflight: {self.verdict} ({errors} error(s), {warnings} warning(s)) for {self.target}"
        lines = [head]
        for i in self.issues:
            lines.append(f"  {i.code} [{i.severity}] {i.kind}: {i.message}")
            if i.fix:
                lines.append(f"    fix: {i.fix}")
        for t in self.tools:
            if t["status"] == "missing":
                lines.append(f"  {t['name']}: not found on PATH")
        if not self.issues:
            lines.append("  no issues found")
        return "\n".join(lines)
