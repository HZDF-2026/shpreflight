"""Dialect-compatibility rules: will this command actually run on the target?

Each rule is triggered by concrete token evidence and fires only for the
shells where it genuinely breaks. Severity is "error" when the command
will fail (or silently do the wrong thing) and "warning" when it merely
smells wrong.
"""

from __future__ import annotations

import re

from .lex import BACKTICK, DQUOTE, OP, SQUOTE, WORD, is_closed
from .report import Issue
from .shells import POWERSHELL, WINDOWS

_VAR_RE = re.compile(r"\$[A-Za-z_][A-Za-z0-9_]*$").fullmatch
_BRACE_VAR_RE = re.compile(r"\$\{[A-Za-z_][A-Za-z0-9_]*\}$").fullmatch
_SPECIAL_VARS = frozenset({"$?", "$!", "$$", "$#", "$@", "$*", "$0", "$1", "$2",
                           "$3", "$4", "$5", "$6", "$7", "$8", "$9"})
_CMLET_RE = re.compile(r"[A-Z][A-Za-z]+-[A-Z][A-Za-z]+$").fullmatch

# POSIX commands with no Windows PowerShell 5.1 equivalent alias, plus the
# native replacement agents should switch to.
POSIX_TO_PS = {
    "grep": "Select-String (sls)",
    "sed": "-replace operator",
    "awk": "ForEach-Object + split",
    "find": "Get-ChildItem -Recurse",
    "head": "Select-Object -First N",
    "tail": "Get-Content -Tail N",
    "touch": "New-Item -ItemType File",
    "chmod": "icacls",
    "chown": "icacls",
    "which": "Get-Command",
    "xargs": "ForEach-Object",
    "df": "Get-PSDrive",
    "du": "Get-ChildItem -Recurse | Measure-Object Length -Sum",
    "wc": "Measure-Object -Line/-Word/-Character",
    "tr": "-replace / .ToLower",
    "cut": ".Split()",
    "uniq": "Select-Object -Unique",
    "uname": "$env:OS",
    "env": "Get-ChildItem env:",
    "basename": "Split-Path -Leaf",
    "dirname": "Split-Path -Parent",
    "ln": "New-Item -ItemType SymbolicLink",
    "readlink": "(Get-Item).Target",
    "mktemp": "New-TemporaryFile",
    "stat": "Get-Item",
    "less": "more.com / Out-Host -Paging",
    "open": "Start-Process (alias on macOS only)",
    "make": "external: install via winget",
}

# Flags curl-style CLIs understand; when 'curl' is the PowerShell alias for
# Invoke-WebRequest (PS 5.1 default) these flags are rejected outright.
_CURL_FLAGS = re.compile(r"^-[a-zA-Z]+|^--[a-zA-Z-]+$").match


def check_dialect(tokens, segments, target: str) -> list[Issue]:
    issues: list[Issue] = []
    ps = target in POWERSHELL
    win = target in WINDOWS
    on_bash = target in ("bash", "sh")

    for kind, text in tokens:
        if kind in (SQUOTE, DQUOTE, BACKTICK) and not is_closed((kind, text)):
            issues.append(Issue(
                "UNCLOSED-QUOTE", "error", "syntax",
                f"unclosed quote starting at {text[:12]!r} — the shell will hang or fail",
                "close the quote"))

    for kind, text in tokens:
        if kind == OP:
            if text == "&&" and target in ("powershell5", "cmd"):
                fix = "separate commands, or chain with ';' if order alone matters"
                issues.append(Issue(
                    "SEP-AND", "error", "syntax",
                    "operator '&&' is not supported in this shell",
                    fix))
            elif text == "||" and target in ("powershell5", "cmd"):
                issues.append(Issue(
                    "SEP-OR", "error", "syntax",
                    "operator '||' is not supported in this shell",
                    "separate commands and check exit codes explicitly"))
        elif kind == WORD:
            if text == "/dev/null" and win:
                issues.append(Issue(
                    "REDIR-DEVNULL", "error", "syntax",
                    "redirection to /dev/null — path does not exist on Windows",
                    "redirect to NUL instead:  2>NUL"))
            elif (ps or target == "cmd") and _VAR_RE(text):
                issues.append(Issue(
                    "ENV-VAR", "error", "syntax",
                    f"bash-style variable {text} — PowerShell reads it as an unset PS variable",
                    f"use the environment: $env:{text[1:]}"))
            elif ps and _BRACE_VAR_RE(text):
                name = text[2:-1]
                issues.append(Issue(
                    "BRACE-VAR", "error", "syntax",
                    f"bash-style ${{var}} expansion is not PowerShell syntax",
                    f"use $env:{name}"))
            elif ps and text in _SPECIAL_VARS:
                issues.append(Issue(
                    "SPECIAL-VAR", "error", "syntax",
                    f"{text} means one thing in bash and another in PowerShell "
                    f"(e.g. bash $? is the numeric exit code, PS $? is a bool)",
                    "use $LASTEXITCODE for native exit codes"))
            elif kind == WORD and text == "export" and (ps or target == "cmd"):
                issues.append(Issue(
                    "EXPORT", "error", "syntax",
                    "'export' is not available in this shell",
                    "PowerShell: $env:NAME = \"value\"   cmd: set NAME=value"))
            elif text == "source" and ps:
                issues.append(Issue(
                    "SOURCE", "error", "syntax",
                    "'source' is not available in PowerShell",
                    "dot-source it:  . ./script.ps1"))
        elif kind == BACKTICK and (ps or target == "cmd"):
            issues.append(Issue(
                "BACKTICK", "error", "syntax",
                "backticks mean command substitution in bash but are the escape "
                "character in PowerShell — the command will do something else entirely",
                "use $(...) for subexpressions in PowerShell"))

    for seg in segments:
        head = seg.head
        if not head:
            continue
        if ps and head == "curl" and any(_CURL_FLAGS(w) for w in seg.words[1:]):
            issues.append(Issue(
                "CURL-ALIAS", "error", "tool",
                "'curl' is an alias for Invoke-WebRequest in Windows PowerShell 5.1 "
                "and rejects unix curl flags",
                "call the real binary: curl.exe -sL ..."))
        if ps and head in ("rm", "del", "erase", "rd") and any(
                w in ("-r", "-f", "-rf", "-fr", "-fr", "-Rf", "-R") for w in seg.words[1:]):
            issues.append(Issue(
                "RM-FLAGS", "error", "syntax",
                f"'{head}' maps to Remove-Item in PowerShell, which has no -r/-f short flags",
                "Remove-Item -Recurse -Force"))
        if on_bash and _CMLET_RE(head):
            issues.append(Issue(
                "CMDLET-IN-POSIX", "error", "tool",
                f"'{head}' is a PowerShell cmdlet; it does not exist in {target}",
                "use the POSIX equivalent for this shell"))
        if win and head in POSIX_TO_PS:
            issues.append(Issue(
                "POSIX-CMD", "error", "tool",
                f"'{head}' is not a Windows PowerShell command",
                POSIX_TO_PS[head], tool=head))
    return issues
