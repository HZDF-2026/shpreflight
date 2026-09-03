"""Token stream -> command segments.

A pipeline like  `rg pattern src/ | head -5 2>/dev/null && echo done`
splits at control operators (| || ; && &). Inside a segment, words after
a redirection operator (>, >>, <, 2>&1, ...) belong to the redirect, not
to the command, so they are never mistaken for the program to resolve.
"""

from __future__ import annotations

from .lex import BACKTICK, DQUOTE, OP, SEP, SQUOTE, WORD

CONTROL_OPS = frozenset({"|", "||", ";", "&&", "&"})


class Segment:
    __slots__ = ("head", "words", "redirects", "raw", "terminator")

    def __init__(self, words: list[str], redirects: list[str], raw: str,
                 terminator: str | None = None):
        self.head = words[0] if words else None
        self.words = words
        self.redirects = redirects
        self.raw = raw
        # control operator that ended this segment: | || ; && & or None
        self.terminator = terminator

    @property
    def pipes_out(self) -> bool:
        return self.terminator == "|"

    def wordset(self) -> set[str]:
        return set(self.words)


def _strip_quotes(word: str) -> str:
    if len(word) >= 2 and word[0] == word[-1] and word[0] in "'\"`":
        return word[1:-1]
    return word


def split_segments(tokens: list[tuple[str, str]]) -> list[Segment]:
    segments: list[Segment] = []
    words: list[str] = []
    redirects: list[str] = []
    raw_parts: list[str] = []
    in_redirect = False

    def flush(terminator: str | None = None) -> None:
        nonlocal in_redirect
        if words or redirects or raw_parts:
            segments.append(Segment(list(words), list(redirects),
                                    "".join(raw_parts).strip(), terminator))
        words.clear()
        redirects.clear()
        raw_parts.clear()
        # redirect state is per-segment: without this reset, every word after
        # "x > f && ..." would be swallowed as a redirect target and the head
        # of the next segment (e.g. an "rm -rf /") would vanish from analysis
        in_redirect = False

    for idx, (kind, text) in enumerate(tokens):
        if kind == SEP:
            raw_parts.append(text)
        elif kind == OP:
            raw_parts.append(text)
            if text in CONTROL_OPS:
                flush(text)
            else:
                in_redirect = True
        else:
            raw_parts.append(text)
            bare = _strip_quotes(text)
            nxt = tokens[idx + 1] if idx + 1 < len(tokens) else None
            if kind == WORD and bare.isdigit() and nxt and nxt[0] == OP \
                    and (">" in nxt[1] or "<" in nxt[1]):
                # fd prefix of the next redirect: "out 2> err", "log 2>&1"
                continue
            if in_redirect:
                redirects.append(bare)
            else:
                words.append(bare)
    flush()
    return segments
