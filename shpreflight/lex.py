"""Single-pass shell-command lexer.

Design invariant (machine-checked in Lean 4, see proofs/):
    concat(token text for token in lex(cmd)) == cmd
Every input character lands in exactly one token — no gaps, no overlaps.
That is what makes the downstream rule engine trustworthy: a destructive
fragment can never hide in a lexing blind spot.

Token kinds:
    WORD      bare word, may contain $ ( ) . / - etc.
    SEP       run of whitespace (preserved verbatim)
    OP        run of operator chars & | ; < >   e.g. && || >> 2>'s '>'
    SQUOTE    'single quoted span', quotes included
    DQUOTE    "double quoted span", quotes included
    BACKTICK  `command substitution span`, backticks included
"""

from __future__ import annotations

OP_CHARS = frozenset("&|;<>")
WHITESPACE = frozenset(" \t\r\n")
WORD_STOP = OP_CHARS | WHITESPACE | {"'", '"', "`"}

WORD = "WORD"
SEP = "SEP"
OP = "OP"
SQUOTE = "SQUOTE"
DQUOTE = "DQUOTE"
BACKTICK = "BACKTICK"

UNCLOSED = {"'": SQUOTE, '"': DQUOTE, "`": BACKTICK}


def _quoted_span(cmd: str, i: int) -> int:
    j = cmd.find(cmd[i], i + 1)
    return len(cmd) if j == -1 else j + 1


def lex(cmd: str) -> list[tuple[str, str]]:
    tokens: list[tuple[str, str]] = []
    i, n = 0, len(cmd)
    while i < n:
        ch = cmd[i]
        if ch in WHITESPACE:
            j = i + 1
            while j < n and cmd[j] in WHITESPACE:
                j += 1
            tokens.append((SEP, cmd[i:j]))
            i = j
        elif ch in "'\"`":
            kind = UNCLOSED[ch]
            j = _quoted_span(cmd, i)
            tokens.append((kind, cmd[i:j]))
            i = j
        elif ch in OP_CHARS:
            j = i + 1
            while j < n and cmd[j] in OP_CHARS:
                j += 1
            tokens.append((OP, cmd[i:j]))
            i = j
        else:
            j = i + 1
            while j < n and cmd[j] not in WORD_STOP:
                j += 1
            tokens.append((WORD, cmd[i:j]))
            i = j
    return tokens


def is_closed(token: tuple[str, str]) -> bool:
    kind, text = token
    if kind not in (SQUOTE, DQUOTE, BACKTICK):
        return True
    return len(text) >= 2 and text[0] == text[-1]


def reconstruct(tokens: list[tuple[str, str]]) -> str:
    return "".join(text for _, text in tokens)
