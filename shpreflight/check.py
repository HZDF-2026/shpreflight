"""Preflight orchestration: lex -> segment -> dialect + danger + tools."""

from __future__ import annotations

import time

from .danger import check_danger
from .lex import lex
from .report import Issue, Report
from .rules import check_dialect
from .segments import split_segments
from .shells import resolve_target
from .tools import BUILTINS, check_tools

# POSIX names whose Windows PATH namesakes do something different, so a
# PATH hit is not evidence the command will do what the agent meant.
NO_DOWNGRADE = frozenset({"find"})


def preflight(cmd: str, target: str | None = None,
              path_check: bool = True) -> Report:
    start = time.perf_counter()
    shell = resolve_target(target)
    tokens = lex(cmd)
    segments = split_segments(tokens)

    issues: list[Issue] = []
    issues.extend(check_dialect(tokens, segments, shell))
    issues.extend(check_danger(segments))
    tools = check_tools(segments, path_check)

    # A tool reported by rules as POSIX-only but actually present on PATH
    # (Git Bash in PATH etc.) downgrades from hard failure to a note:
    # the command will run, just not natively.
    if tools:
        found = {t["name"] for t in tools if t["status"] == "found"}
        for issue in issues:
            if issue.code == "POSIX-CMD" and issue.tool in found \
                    and issue.tool not in NO_DOWNGRADE:
                issue.severity = "info"
                issue.message += " (present on PATH, non-native)"

    # Anything reported missing on PATH gets an explicit issue, since
    # 'command not found' is the single largest failure mode for agents.
    if path_check:
        for t in tools:
            if t["status"] == "missing" and t["name"] not in BUILTINS:
                sev = "error"
                msg = f"'{t['name']}' was not found on PATH — this is a 'command not found' failure"
                if any(i.code == "POSIX-CMD" and i.tool == t["name"] for i in issues):
                    continue
                issues.append(Issue("TOOL-NOT-FOUND", sev, "tool", msg,
                                    "install it, or use the native equivalent", t["name"]))

    elapsed = (time.perf_counter() - start) * 1000
    return Report(cmd, shell, issues, tools, elapsed)
