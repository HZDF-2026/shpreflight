"""Shell dialect registry and target detection."""

from __future__ import annotations

import sys

SHELLS = {
    "powershell5": "Windows PowerShell 5.1 (powershell.exe)",
    "pwsh7": "PowerShell 7+ (pwsh.exe)",
    "bash": "Bash (Git Bash / WSL / Unix)",
    "cmd": "Windows cmd.exe",
    "sh": "POSIX sh",
}

DEFAULT_BY_PLATFORM = {"win32": "powershell5"}

POWERSHELL = frozenset({"powershell5", "pwsh7"})
WINDOWS = frozenset({"powershell5", "pwsh7", "cmd"})


def default_shell() -> str:
    return DEFAULT_BY_PLATFORM.get(sys.platform, "bash")


def resolve_target(shell: str | None) -> str:
    if not shell or shell == "auto":
        return default_shell()
    if shell not in SHELLS:
        raise ValueError(f"unknown shell {shell!r}; see `shpreflight shells`")
    return shell
