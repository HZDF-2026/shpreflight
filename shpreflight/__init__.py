"""shpreflight — pre-flight diagnostics for agent-generated shell commands."""

from .check import preflight
from .lex import lex, reconstruct
from .report import Issue, Report

__all__ = ["preflight", "lex", "reconstruct", "Issue", "Report"]
__version__ = "0.1.0"
