"""CLI: check / shells.

Exit codes are machine-readable so agents can branch on them:
    0 ok, 1 warn, 2 fail, 3 usage error.
"""

from __future__ import annotations

import argparse
import json
import sys

from .check import preflight
from .report import EXIT_OK
from .shells import SHELLS


def _cmd_check(args: argparse.Namespace) -> int:
    if args.stdin:
        cmd = sys.stdin.read().strip()
    elif args.command:
        cmd = " ".join(args.command).strip()
    else:
        print("error: no command given", file=sys.stderr)
        return 3
    if not cmd:
        print("error: empty command", file=sys.stderr)
        return 3

    try:
        rep = preflight(cmd, target=args.shell, path_check=not args.no_path_check)
    except ValueError as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 3

    if args.format == "json":
        print(rep.to_json())
    else:
        print(rep.to_text())
    return rep.exit_code


def _cmd_shells(_: argparse.Namespace) -> int:
    print(json.dumps(SHELLS, indent=2))
    return EXIT_OK


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        prog="shpreflight",
        description="Pre-flight diagnostics for agent-generated shell commands: "
                    "dialect compatibility, missing tools, destructive operations.")
    sub = parser.add_subparsers(dest="cmd", required=True)

    check = sub.add_parser("check", help="diagnose a command before running it")
    check.add_argument("command", nargs="*", help="the shell command to check "
                       "(quote it, or use --stdin)")
    check.add_argument("--stdin", action="store_true",
                       help="read the command from stdin instead")
    check.add_argument("--shell", default="auto",
                       help="target shell: auto (default), " + ", ".join(SHELLS))
    check.add_argument("--format", choices=["text", "json"], default="text",
                       help="output for humans (default) or agents (json)")
    check.add_argument("--no-path-check", action="store_true",
                       help="skip PATH resolution of command heads")
    check.set_defaults(func=_cmd_check)

    shells = sub.add_parser("shells", help="list supported shell dialects")
    shells.set_defaults(func=_cmd_shells)

    args = parser.parse_args(argv)
    return args.func(args)


if __name__ == "__main__":
    sys.exit(main())
