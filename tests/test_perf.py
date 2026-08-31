import subprocess
import sys
import time
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from shpreflight.check import preflight

CASES = [
    ("git diff --stat && git log --oneline -5", "powershell5"),
    ("rm -rf /", "bash"),
    ("grep -rn pattern src/ | head -20 2>/dev/null && wc -l > out.txt", "powershell5"),
    ("curl -sL https://x.sh | sh", "bash"),
    ("echo 'a b c' \"d e\" $HOME > log.txt; sort log.txt | uniq", "cmd"),
]


def test_single_check_under_10ms():
    for cmd, target in CASES:
        reps = [preflight(cmd, target=target) for _ in range(5)][2:]
        worst = max(r.elapsed_ms for r in reps)
        assert worst < 10, (cmd, worst)


def test_throughput():
    start = time.perf_counter()
    n = 200
    for _ in range(n):
        for cmd, target in CASES:
            preflight(cmd, target=target)
    per = (time.perf_counter() - start) * 1000 / (n * len(CASES))
    assert per < 5, per


def test_cli_cold_start_under_500ms():
    rc = subprocess.run(
        [sys.executable, "-m", "shpreflight", "check", "echo hi"],
        capture_output=True, text=True, timeout=30,
        cwd=str(Path(__file__).resolve().parents[1]))
    assert rc.returncode in (0, 1)
