# SPDX-License-Identifier: Apache-2.0
"""Read the OKLCH design tokens in web/src/theme/tokens.css and convert them to sRGB.

Shared by gen-palette.py and check-contrast.py.
"""
import math, pathlib, re

ROOT = pathlib.Path(__file__).resolve().parent.parent
TOKENS = ROOT / "web/src/theme/tokens.css"

_TOKEN = re.compile(
    r"--([a-z0-9-]+):\s*oklch\(([\d.]+)%\s+([\d.]+)\s+([\d.]+)(?:\s*/\s*([\d.]+))?\)")


def oklch_to_srgb(L, C, H):
    """OKLCH (L in %) to gamma-encoded sRGB channels in 0..1, clipped to gamut."""
    L /= 100; a = C * math.cos(math.radians(H)); b = C * math.sin(math.radians(H))
    l_ = L + 0.3963377774*a + 0.2158037573*b
    m_ = L - 0.1055613458*a - 0.0638541728*b
    s_ = L - 0.0894841775*a - 1.2914855480*b
    l, m, s = l_**3, m_**3, s_**3
    lin = (4.0767416621*l - 3.3077115913*m + 0.2309699292*s,
           -1.2684380046*l + 2.6097574011*m - 0.3413193965*s,
           -0.0041960863*l - 0.7034186147*m + 1.7076147010*s)
    def enc(x):
        x = min(1, max(0, x))
        return 12.92*x if x <= 0.0031308 else 1.055*x**(1/2.4) - 0.055
    return tuple(enc(x) for x in lin)


def oklch_to_hex(L, C, H, A=None):
    h = "#" + "".join(f"{round(x*255):02x}" for x in oklch_to_srgb(L, C, H))
    if A is not None:
        h += f"{round(A*255):02x}"
    return h


def block(css, selector):
    """Tokens declared in the first rule block starting with selector: name -> (L, C, H, A|None)."""
    start = css.index(selector)
    end = css.index("}", start)
    return {m[0]: (float(m[1]), float(m[2]), float(m[3]), float(m[4]) if m[4] else None)
            for m in _TOKEN.findall(css[start:end])}


def schemes():
    """(light, dark) token maps; dark inherits every token it doesn't override."""
    css = TOKENS.read_text()
    light = block(css, ":root {")
    dark = {**light, **block(css, ':root[data-theme="dark"] {')}
    return light, dark
