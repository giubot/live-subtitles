# Design: Live Subtitles

This is the locked design system for every screen in `web/`. Pages and components use it; they don't invent their own. Change it on purpose: edit this file and `web/src/theme/tokens.css` together.

- Tokens (source of truth): [`web/src/theme/tokens.css`](web/src/theme/tokens.css)
- MUI palette (generated hex mirror, since MUI can't parse `oklch()`): [`web/src/theme/palette.ts`](web/src/theme/palette.ts), produced by `python3 scripts/gen-palette.py`
- Visual reference for every surface, in light and dark: [`design/preview.html`](design/preview.html). Open it in a browser.

## System

- **Genre**: modern-minimal. This is an operator tool for live events. It should read like an instrument panel, not a marketing site.
- **Theme**: Cobalt, adapted for an app. It's cool engineered paper, hairline structure, and **one** cobalt signal accent. A full dark scheme is derived with the same hue anchor.
- **Axes**: paper light, cool, hue 250–258 / display grotesk-sans / accent electric cobalt (hue 258).
- **Shape**: Workbench. The admin is a side-rail app shell with a working ⌘K command palette. Audience surfaces have no nav at all.
- **Tone**: technical, calm, specific. Name the room, the port, the number.

## Surfaces

Each surface has different viewing conditions, so each gets its own treatment:

| Surface | Route | Theme | Key rules |
|---|---|---|---|
| Admin, setup wizard | `/admin/*`, `/setup` | UI theme (light/dark/system) | Side rail (N3) + ⌘K palette. One filled cobalt button per view. Status always shown as chip + label. |
| Capture station | `/capture/$id` | UI theme | Answers one question: is audio reaching the server? Big level meter, one big action, plain-language warnings. |
| Audience viewer | `/s`, `/s/$id` | UI theme, follows the phone | Caption-first. Newest line at the bottom. Interim text in `--color-muted` with a cobalt caret. Timecodes in mono. `aria-live="polite"`. |
| Stage screen | `/stage/$id` | **Own presets**, never the UI theme | `white-on-black` (default), `yellow-on-black`, `black-on-white`. 2–3 lines at `--text-caption-stage`, optional second language underneath in `--stage-fg-secondary`, QR code in a corner. |
| OBS/vMix overlay | `/overlay/$id` | **Transparent**, never themed | Styled only by `OverlayStyle` presets: "Classic box" (`--overlay-box`), "Outline only", "Lower third". No MUI chrome, no scrollbars. |
| Replay | `/replay/$id` | UI theme | Audio player + clickable transcript that highlights the current cue. |

## Colour

All colours are OKLCH tokens. Never use raw hex, `rgb()` or pure `#000`/`#fff` in components; use a token, or add a token first.

| Role | Light | Dark | Use |
|---|---|---|---|
| `paper` / `paper-2` / `paper-3` | 98.5 / 96.6 / 93.8 % L, hue 250–254 | 16.5 / 19.5 / 23.5 % L | Page / rail and wells / hover fill. In dark mode, raised surfaces get lighter. |
| `rule` / `rule-2` / `control` | 90 / 80 / 62 % L | 29 / 37 / 55 % L | Hairlines / panel borders / input and outlined-button borders (≥ 3:1) |
| `ink` / `ink-2` / `neutral` / `muted` | 23 / 33 / 42 / 50 % L | 95 / 87 / 79 / 70 % L | Headings and final captions / body / secondary / meta and interim captions |
| `accent` (+ `-hover`, `-ink`, `-text`, `-soft`) | `oklch(55% 0.20 258)` | `oklch(70% 0.15 256)` | Primary button, active nav, links, focus ring, selection. **≤ 5 % of any view.** |
| `live` | `oklch(56% 0.21 25)` | `oklch(66% 0.20 25)` | On-air tally only: the LIVE chip and dot |
| `ok` / `warn` / `danger` (+ `-soft`) | green 150 / amber 70 / red 27 | lighter in dark | Health, attention, errors. **Never colour alone**: always with an icon or label. |
| `graphite` | `oklch(22% 0.016 260)` | `oklch(12.5% 0.012 260)` | The one dark band in light mode: URL/token cards, stage and overlay previews |

**Contrast** was checked numerically (WCAG 2.1) for both schemes. Every text pair is ≥ 4.5:1 (body ≥ 11:1), and UI boundaries (`control`, `accent`, focus ring) are ≥ 3:1. The stage presets are ≥ 15:1. Re-run the check whenever a colour changes.

## Typography

Three families, all self-hosted with `@fontsource-variable/*` (edge nodes may be offline, so no font CDN):

| Token | Family | Role |
|---|---|---|
| `--font-display` | **Space Grotesk** 600 | Headings, session names, wordmark. Tracking −0.02em. Never italic. |
| `--font-body` | **Atkinson Hyperlegible Next** 400 (350 in dark) / 500 captions / 700 labels and buttons | Everything people read, **captions above all**. Chosen for legibility: I/l/1 and O/0 stay distinct at projector distance and for low-vision readers. |
| `--font-mono` | **Atkinson Hyperlegible Mono** 400/500 | One role only, **machine readout**: timecodes, URLs, IPs, ports, tokens, kbd hints, uppercase status labels (`0.06em` tracking). Never prose. |

Scale is the 1.25 major third from 16 px (`--text-xs` … `--text-2xl`), plus `--text-caption` (phone) and `--text-caption-stage` (projector, capped at 5.5 rem). Use at most five sizes per screen. Numbers use `tabular-nums`. Measure is `65ch`.

## Space, shape, depth

- 4-pt spacing scale `--space-3xs` (2 px) … `--space-3xl` (96 px). Use `gap` for siblings. No raw pixel spacing.
- Radii: `--radius-input` **6 px** for buttons and inputs (drawn with a ruler, **never pills**), `--radius-card` 10 px for panels and dialogs, `--radius-chip` 4 px.
- Depth comes from **hairline borders**, not shadows. The only shadow is `--shadow-lift` on menus and dialogs, and it becomes a hairline in dark mode (no glow). No card-in-card.
- Controls are 40 px tall, or 48 px under `(pointer: coarse)`. Touch targets are ≥ 44 px.
- Z-index uses named layers only: `--z-dropdown` … `--z-tooltip`.

## Components (MUI mapping)

Build on MUI but override its defaults so it doesn't look like stock Material:

- **Theme**: `createTheme({ cssVariables: { colorSchemeSelector: '[data-theme="%s"]' }, colorSchemes: { light, dark } })` with `palette.ts` values: `primary` = accent, `error` = danger, `success` = ok, `warning` = warn, `background.default` = paper, `background.paper` = paper, `divider` = rule. Typography families come from the CSS tokens. `shape.borderRadius: 6`. Keep `data-theme` on `<html>` in sync with the stored preference (`light` / `dark` / `system`).
- **Button**: `disableElevation`, `textTransform: 'none'`, weight 700. `contained` + primary is the **one** main action per view. Secondary actions use `outlined` with `control` border and ink text. Destructive actions (Stop, Delete) use `outlined` with `danger` text; they are never red-filled. `text` is for inline links like "Copy overlay URL".
- **Status chip**: a custom `StatusChip` (not MUI Chip defaults) with a mono uppercase label, 4 px radius and a leading dot. LIVE is filled with `live`; the others are outlined in their status colour.
- **Card / Paper**: `variant="outlined"`, `elevation={0}`, `rule-2` border, 10 px radius.
- **AppBar**: flat, `paper` background, bottom hairline. **Drawer** (side rail): `paper-2` with active item on `accent-soft` + a 4 px accent tick.
- **Inputs**: label above (never placeholder-as-label), helper text below with reserved height so errors don't shift layout, and error text replacing the helper with `aria-invalid`. Secrets are write-only fields showing the masked hint (`••••3f9a`).
- **Tooltip**: `enterDelay={800}` on hover and `0` on focus.
- **Icons**: one set only, `@mui/icons-material` **Outlined**, 20 px, `aria-hidden` when next to text.
- **Level meter**: a 24-segment bar that is `ok` → `warn-fill` near the top → `danger` at clipping, with `role="meter"`.
- **Snackbar**: only for failures and effects the user can't see. Success is silent where the result is already visible.

## States

Every interactive control ships all eight states: default, hover (only under `@media (hover: hover)`), `:focus-visible` (2 px `--color-focus` ring with 2 px offset, **appears instantly**), active (1 px press), disabled (50 % opacity + a reason nearby), loading (inline spinner replaces the icon and the label stays readable), error (danger border, icon and message), success (ok tint, check icon).

## Motion

Motion is minimal, and the UI is composed rather than animated. Use `--ease-out` / `--ease-in` / `--ease-in-out` with `--dur-micro` (120 ms), `--dur-short` (220 ms) and `--dur-long` (420 ms). Animate `transform` and `opacity` only (plus colour on hover). Never bounce, never `transition: all`. Captions appear without animation, since latency matters more than flourish. Under `prefers-reduced-motion`, durations collapse to ≤ 150 ms.

## Copy

Use specific verbs ("Start session", "Send test caption", "Copy overlay URL"; never "Submit" or "OK"). Errors state what broke, why, and what to do. Example: "The speech model stopped responding at 127.0.0.1:8178. Check that whisper-server is running, then retry." Never write "Oops" or "Something went wrong", and never add exclamation marks. Use curly quotes, the `…` character and en/em dashes. Every string goes through i18n (ES + EN); allow ~30 % extra width for translations.

## Accessibility floor

WCAG AA contrast in both themes. `:focus-visible` on everything. `aria-live="polite"` on caption logs. `lang` attributes on caption text (`es`, `en`) so screen readers switch voice. No hover-only affordances. No horizontal scroll at 320–1920 px. Clickable text never wraps to two lines. Logical CSS properties (`margin-inline-start`, not `margin-left`).

## Exports

`web/src/theme/tokens.css` is the source of truth. `palette.ts` is generated from it for MUI. For Tailwind v4 `@theme`, DTCG `tokens.json` or other formats, ask for them and they'll be appended here.
