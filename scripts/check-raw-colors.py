#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
"""Fail on raw colour or font-family literals in web code outside web/src/theme/.

Feature code uses the design tokens (CSS variables or the MUI theme), never
hex, rgb(), hsl(), oklch()… or a hard-coded font family (docs/design.md,
plan §3.6). Put a new colour in tokens.css first.
Run: python3 scripts/check-raw-colors.py
"""
import pathlib, re, sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
SRC = ROOT / "web/src"
ALLOWED = [SRC / "theme"]
SKIP = {SRC / "api/schema.d.ts", SRC / "routeTree.gen.ts"}
EXTENSIONS = {".ts", ".tsx", ".css"}

RULES = [
    ("hex colour", re.compile(r"(?<![\w&/])#(?:[0-9a-fA-F]{8}|[0-9a-fA-F]{6}|[0-9a-fA-F]{3,4})\b")),
    ("colour function", re.compile(r"\b(?:rgba?|hsla?|hwb|oklch|oklab|lch|lab|color)\(")),
    ("font family", re.compile(r"""(?:fontFamily\s*:\s*['"`](?!var\()|font-family\s*:(?!\s*var\())""")),
]


def main():
    problems = []
    for path in sorted(SRC.rglob("*")):
        if path.suffix not in EXTENSIONS or path in SKIP or "node_modules" in path.parts:
            continue
        if any(path.is_relative_to(a) for a in ALLOWED):
            continue
        for n, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
            for what, rx in RULES:
                if rx.search(line):
                    problems.append(f"{path.relative_to(ROOT)}:{n}: raw {what}: {line.strip()}")
    for p in problems:
        print(p, file=sys.stderr)
    if problems:
        print(f"\n{len(problems)} raw literal(s); use a token from web/src/theme/tokens.css", file=sys.stderr)
        return 1
    print("no raw colour or font literals outside web/src/theme/")
    return 0


if __name__ == "__main__":
    sys.exit(main())
