"""Destructive-operation detection over command segments.

Matching is structural (head + flags + targets), not regex-over-strings,
so the pattern table is data and its recall over the table itself is
machine-checked in Lean 4 (proofs/Shpreflight.lean): every pattern in the
table is matched by at least one concrete command — the detector cannot
have a dead entry, i.e. known-dangerous commands are never silently
unmatchable.
"""

from __future__ import annotations

from .report import Issue
from .segments import Segment

_SHELL_HEADS = frozenset({"sh", "bash", "zsh", "pwsh", "powershell", "dash"})
_EXEC_HEADS = frozenset({"iex", "Invoke-Expression", "eval"})
_FETCH_HEADS = frozenset({"curl", "curl.exe", "wget", "iwr", "Invoke-WebRequest"})

_DELETE = ("rm",)
_RM_DELETE_FLAGS = frozenset({"-rf", "-fr", "-Rf", "-RF", "-r", "-f", "-R"})
_ROOTISH = frozenset({"/", "/*", "~", "~/*", "*", ".", "..", "$HOME", "$HOME/",
                      "$PWD", "C:\\", "C:/", "C:\\Windows", "%SYSTEMROOT%"})

# Raw block devices a `dd of=` can overwrite. Matching is exact-string, so the
# table enumerates concrete device names per family: sd[a-p] (SATA/SCSI/USB),
# vd[a-c] (virtio, common on cloud VMs), hd[a-b] (legacy IDE), xvda/xvdb
# (Xen/AWS), nvme[01]n[12] (NVMe), mmcblk0 (SD/eMMC, e.g. Raspberry Pi),
# disk0/rdisk0 (macOS), PhysicalDrive[01] (Windows). Each is listed both bare
# (dd /dev/sda) and with the of= key (dd of=/dev/sda), the form real dd
# invocations use.
_RAW_DEVICES = (
    frozenset(f"/dev/sd{c}" for c in "abcdefghijklmnop")
    | frozenset(f"/dev/vd{c}" for c in "abc")
    | frozenset(f"/dev/hd{c}" for c in "ab")
    | frozenset({"/dev/xvda", "/dev/xvdb",
                 "/dev/nvme0n1", "/dev/nvme0n2", "/dev/nvme1n1", "/dev/nvme1n2",
                 "/dev/mmcblk0", "/dev/disk0", "/dev/rdisk0",
                 r"\\.\PhysicalDrive0", r"\\.\PhysicalDrive1"})
)
_DD_RAW_TARGETS = _RAW_DEVICES | {f"of={d}" for d in _RAW_DEVICES}

_SENSITIVE_BASES = frozenset({".env", ".env.local", ".env.production", "secrets",
                              "id_rsa", "id_ed25519", "credentials", "credentials.json",
                              ".npmrc", ".netrc", ".aws/credentials"})
_PUBLISH_GUARD = ("--dry-run",)


class DangerPattern:
    __slots__ = ("code", "severity", "heads", "flags", "targets", "message", "fix")

    def __init__(self, code, severity, heads, flags=None, targets=None,
                 message="", fix=None):
        self.code = code
        self.severity = severity
        self.heads = frozenset(heads)
        self.flags = frozenset(flags or ())
        self.targets = frozenset(targets or ())
        self.message = message
        self.fix = fix


PATTERNS = (
    DangerPattern(
        "RM-ROOT", "error", _DELETE, flags=_RM_DELETE_FLAGS, targets=_ROOTISH,
        message="recursive force-delete aimed at a root/home/glob path",
        fix="restrict the target path; verify with --dry-run or -WhatIf"),
    DangerPattern(
        "RM-RECURSIVE", "warning", _DELETE,
        flags=frozenset({"-r", "-R", "-rf", "-fr", "-Rf", "-RF"}),
        message="recursive delete — irreversible without a trash can",
        fix="list the target first, or use a git-tracked directory"),
    DangerPattern(
        "REMOVE-ITEM-RECURSE-FORCE", "warning",
        ("Remove-Item", "ri", "del", "erase", "rd"),
        flags=frozenset({"-Recurse", "-Force", "/s", "/q", "/f"}),
        message="recursive/forced removal (Remove-Item -Recurse -Force or del /s /q)",
        fix="add -WhatIf to preview what would be removed"),
    DangerPattern(
        "GIT-RESET-HARD", "warning", ("git",),
        flags=frozenset({"--hard"}),
        message="git reset --hard discards uncommitted work",
        fix="git stash first, or reset a specific path"),
    DangerPattern(
        "GIT-PUSH-FORCE", "warning", ("git",),
        flags=frozenset({"--force", "-f", "--force-with-lease"}),
        message="force push can overwrite upstream history",
        fix="prefer --force-with-lease and confirm the branch"),
    DangerPattern(
        "GIT-CLEAN-ND", "warning", ("git",),
        flags=frozenset({"clean", "-fd", "-fdx", "-f", "-d", "-x", "-X"}),
        message="git clean removes untracked files permanently",
        fix="run 'git clean -n' first to preview"),
    DangerPattern(
        "SHUTDOWN", "error", ("shutdown", "reboot", "halt", "poweroff", "restart"),
        message="machine power state change",
        fix="confirm intent with the user before running"),
    DangerPattern(
        "FORMAT", "error", ("format", "mkfs", "mkfs.ext4", "mkfs.vfat"),
        message="filesystem format destroys all data on the volume",
        fix="never run this from an agent without explicit user confirmation"),
    DangerPattern(
        "DD-RAW", "error", ("dd",), targets=_DD_RAW_TARGETS,
        message="dd targeting a raw device overwrites the disk",
        fix="verify of= points at a file, not a device"),
    DangerPattern(
        "CHMOD-777-ROOT", "error", ("chmod",),
        targets=frozenset({"/", "/*", "~", "~/*", "$HOME", "*"}),
        message="chmod applied to a root/home/glob path",
        fix="scope permissions to the actual project directory"),
    DangerPattern(
        "SHRED", "error", ("shred", "srm"),
        message="secure delete is irreversible",
        fix="confirm intent with the user"),
    DangerPattern(
        "TASKKILL-FORCE", "warning", ("taskkill",), flags=frozenset({"/f", "-f"}),
        message="taskkill /f terminates processes without cleanup",
        fix="drop /f if graceful termination is acceptable"),
    DangerPattern(
        "SET-EXECUTIONPOLICY", "warning", ("Set-ExecutionPolicy",),
        flags=frozenset({"Bypass", "-Scope", "Process", "-Force"}),
        message="loosens the PowerShell execution policy",
        fix="scope it to -Scope Process if unavoidable"),
    DangerPattern(
        "TRUNCATE", "warning", ("truncate",),
        message="truncate empties files in place",
        fix="confirm the target file is disposable"),
)


def match_pattern(words: list[str], pattern: DangerPattern) -> bool:
    """Structural match: head, then any-of flags, then any-of targets."""
    if not words:
        return False
    head = words[0]
    if head not in pattern.heads:
        return False
    rest = words[1:]
    if pattern.flags and not pattern.flags.intersection(rest):
        return False
    if pattern.targets and not pattern.targets.intersection(rest):
        return False
    return True


def _basename(path: str) -> str:
    for sep in ("/", "\\"):
        if sep in path:
            path = path.rsplit(sep, 1)[1]
    return path


def check_danger(segments: list[Segment]) -> list[Issue]:
    issues: list[Issue] = []
    pipe_receiver = False
    for seg in segments:
        head = seg.head
        if head:
            if pipe_receiver and (head in _SHELL_HEADS or head in _EXEC_HEADS):
                issues.append(Issue(
                    "PIPE-EXEC", "error", "danger",
                    f"pipe feeds untrusted input into '{head}' — remote-code-execution pattern "
                    f"(curl ... | sh)",
                    "download first, inspect, then run"))
            if pipe_receiver and head == "npm" and "publish" in seg.wordset():
                issues.append(Issue(
                    "NPM-PUBLISH", "warning", "danger",
                    "publishing to the npm registry makes the package public",
                    "use npm publish --dry-run until content is final"))
        hit_codes: set[str] = set()
        for pattern in PATTERNS:
            if match_pattern(seg.words, pattern):
                # RM-ROOT already says everything about this segment's rm
                if pattern.code == "RM-RECURSIVE" and "RM-ROOT" in hit_codes:
                    continue
                hit_codes.add(pattern.code)
                issues.append(Issue(
                    pattern.code, pattern.severity, "danger",
                    f"{seg.words[0]}: {pattern.message}",
                    pattern.fix))
        for redir in seg.redirects:
            if _basename(redir) in _SENSITIVE_BASES or redir.endswith(".key") or redir.endswith(".pem"):
                issues.append(Issue(
                    "REDIR-SENSITIVE", "warning", "danger",
                    f"overwriting sensitive file '{redir}' via redirection",
                    "write to a temp file and diff first"))
        pipe_receiver = seg.pipes_out
    return issues
