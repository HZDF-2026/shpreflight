"""PATH resolution for the heads of every segment.

Resolves what the shell itself would resolve — is the first word of each
command actually executable here? One scandir pass over the PATH builds a
name cache; every lookup after that is a set membership test, so checking
a five-segment pipeline costs the same as checking one command.
"""

from __future__ import annotations

import os

# Heads that are shell builtins or Windows internal commands: no PATH entry
# to resolve, on either family of shells.
BUILTINS = frozenset({
    "if", "then", "else", "elif", "fi", "for", "while", "do", "done",
    "case", "esac", "function", "in", "export", "source", "set", "unset",
    "alias", "exit", "return", "cd", "chdir", "pushd", "popd", "echo",
    "read", "shift", "true", "false", "eval", "exec", "trap", "local",
    "declare", "readonly",
    # Windows cmd.exe / PowerShell internal commands
    "mkdir", "rmdir", "rd", "del", "erase", "copy", "xcopy", "move", "ren",
    "type", "cls", "dir", "ver", "vol", "call", "start", "title", "color",
    "prompt", "setlocal", "endlocal", "break", "md",
})

_cache: set[str] | None = None

if os.name == "nt":
    _EXTS = (".exe", ".cmd", ".bat", ".com")
else:
    _EXTS = ()


def _path_names() -> set[str]:
    global _cache
    if _cache is None:
        names: set[str] = set()
        for d in os.get_exec_path():
            try:
                with os.scandir(d) as it:
                    for entry in it:
                        n = entry.name
                        names.add(n)
                        if _EXTS:
                            for ext in _EXTS:
                                if n.endswith(ext):
                                    names.add(n[: -len(ext)])
                                    break
            except OSError:
                continue
        _cache = names
    return _cache


def check_tools(segments, path_check: bool = True) -> list[dict]:
    if not path_check:
        return []
    names = _path_names()
    results: dict[str, dict] = {}
    for seg in segments:
        head = seg.head
        if not head or head in BUILTINS or head in results:
            continue
        results[head] = {
            "name": head,
            "status": "found" if head in names else "missing",
            "path": head if head in names else None,
        }
    return list(results.values())
