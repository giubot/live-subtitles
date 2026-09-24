#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
"""WCAG 2.1 contrast check of the design tokens, light and dark (UI-6).

Pairs and thresholds follow docs/design.md § Colour: text ≥ 4.5:1 (body
≥ 11:1), UI boundaries ≥ 3:1, stage presets ≥ 15:1. Colours are read from
web/src/theme/tokens.css, so a failing colour change fails `task check`.
Run: python3 scripts/check-contrast.py [-v]
"""
import sys
from tokens import oklch_to_srgb, schemes

TEXT, BODY, UI, STAGE = 4.5, 11.0, 3.0, 15.0

# (foreground, background, minimum, what)
PAIRS = [
    *[(fg, bg, TEXT, "text") for fg in ("ink", "ink-2", "neutral", "muted")
      for bg in ("paper", "paper-2", "paper-3")],
    ("ink-2", "paper", BODY, "body"),
    ("ink-2", "paper-2", BODY, "body"),
    *[("accent-text", bg, TEXT, "link / accent text") for bg in ("paper", "paper-2", "accent-soft")],
    ("accent-ink", "accent", TEXT, "primary button"),
    ("accent-ink", "accent-hover", TEXT, "primary button, hover"),
    ("live-ink", "live", TEXT, "LIVE chip"),
    *[(s, bg, TEXT, "status text") for s in ("ok", "warn", "danger") for bg in ("paper", f"{s}-soft")],
    ("graphite-ink", "graphite", TEXT, "URL / token card"),
    *[(fg, bg, UI, "UI boundary") for fg in ("control", "accent", "focus") for bg in ("paper", "paper-2")],
    *[(s, "paper", UI, "status icon / dot") for s in ("live", "ok", "danger")],
]
STAGE_PAIRS = [
    ("stage-fg-white", "stage-bg-dark", STAGE, "stage white-on-black"),
    ("stage-fg-yellow", "stage-bg-dark", STAGE, "stage yellow-on-black"),
    ("stage-fg-black", "stage-bg-light", STAGE, "stage black-on-white"),
    ("stage-fg-secondary", "stage-bg-dark", TEXT, "stage second language"),
]


def luminance(rgb):
    lin = [c/12.92 if c <= 0.04045 else ((c + 0.055)/1.055)**2.4 for c in rgb]
    return 0.2126*lin[0] + 0.7152*lin[1] + 0.0722*lin[2]


def ratio(a, b):
    la, lb = sorted((luminance(a), luminance(b)), reverse=True)
    return (la + 0.05) / (lb + 0.05)


def main():
    verbose = "-v" in sys.argv
    light, dark = schemes()
    failures = 0
    checks = [("light", light, PAIRS + STAGE_PAIRS), ("dark", dark, PAIRS)]  # stage presets don't follow the theme
    for scheme, tokens, pairs in checks:
        rgb = lambda name: oklch_to_srgb(*tokens[name][:3]) if name.startswith("stage-") \
            else oklch_to_srgb(*tokens["color-" + name][:3])
        for fg, bg, minimum, what in pairs:
            r = ratio(rgb(fg), rgb(bg))
            ok = r >= minimum
            failures += not ok
            if verbose or not ok:
                print(f"{'ok  ' if ok else 'FAIL'} {scheme:5} {fg:>18} on {bg:<14} {r:5.2f}:1 (≥ {minimum}) {what}")
    n = sum(len(p) for _, _, p in checks)
    if failures:
        print(f"\n{failures} of {n} contrast pairs below the minimum", file=sys.stderr)
        return 1
    print(f"contrast ok: {n} pairs, light + dark")
    return 0


if __name__ == "__main__":
    sys.exit(main())
