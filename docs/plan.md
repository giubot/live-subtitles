# Live Subtitles: Implementation Plan

This plan turns [`requirements.md`](requirements.md) into phased, parallelizable tasks, from scaffolding through the last requirement. It's written so that **several agents and developers (with their agents) can work at the same time** with minimal merge conflicts and minimal waiting on each other.

- **API contract**: [`api/openapi.yaml`](../api/openapi.yaml) (OpenAPI 3.0.3). This is the source of truth for REST and WebSocket message schemas.
- **Hard deadline**: Devpost submission by **Fri 2026-09-25 15:00 UTC (12:00 ART)**. Phases 0–4 are the submission scope. Phase 5 is stretch work that continues after the hackathon.

---

## 1. How to use this plan

1. **Pick a task** whose dependencies (`Deps`) are all done. Tasks within one phase are parallel unless a dependency says otherwise.
2. **Claim it**: create a GitHub issue per task (title `P1-04 Caption bus + /ws/captions`, labels `phase:1`, `lane:core`) and assign yourself or the agent. The issue is the lock; don't start a task that's already assigned.
3. **Work in an isolated git worktree**: `git worktree add ../ls-P1-04 -b task/P1-04`. Agents launched with worktree isolation get this automatically.
4. **Stay inside your lane's files** (§3.3). If you must touch another lane's files, keep it minimal and mention it in the commit body.
5. **Land on `main`**: rebase on `main`, make sure `task check` is green, then fast-forward `main` (linear history, no merge commits). Use **Conventional Commits** (`feat(server): …`, `fix(web): …`, `docs: …`, `feat(api): …`), and reference the task ID in the body (`Refs: P1-04`).
6. **Meet the Definition of Done** (§3.5) before closing the issue.

---

## 2. Target architecture and repository layout

```
live-subtitles/
├── api/
│   └── openapi.yaml               # contract (lane: api)
├── cmd/
│   └── livesubs/main.go           # single binary entrypoint
├── internal/                      # Go packages (see ownership §3.3)
│   ├── api/                       # generated only (gen.go): types + strict server, imports nothing internal
│   │   └── handlers/              # StrictServerInterface implementation, one file per tag
│   ├── app/                       # wiring: config → services → http server
│   ├── config/                    # flags/env/config file
│   ├── domain/                    # shared types + interfaces (Provider, Source, Bus, …)
│   ├── store/                     # SQLite + migrations + repositories
│   ├── auth/                      # admin PIN, cookies, ingest tokens
│   ├── session/                   # manager, per-session pipeline, state machine, clock
│   ├── bus/                       # caption pub/sub + history ring buffers
│   ├── audio/                     # ws ingest, ffmpeg sources (file/url/srt), levels, VAD
│   ├── provider/
│   │   ├── mock/                  # scripted captions (dev + tests)
│   │   ├── gemini/                # Gemini Live ASR + Gemini translation
│   │   └── local/                 # whisper.cpp ASR + Gemma (Ollama) translation
│   ├── translate/                 # translator interface, fan-out, context, glossary prompts
│   ├── subtitle/                  # segmenter + VTT/SRT/TXT/JSON writers
│   ├── recording/                 # ffmpeg AAC fMP4 recorder, retention
│   ├── streamcc/                  # stream closed captions: YouTube HTTP POST, OBS SendStreamCaption
│   ├── secrets/                   # keychain / encrypted file / env
│   ├── tlsutil/                   # local CA, leaf certs, autocert
│   ├── netinfo/                   # LAN IPs, base URL, terminal QR
│   ├── hwcheck/                   # hardware report + benchmark
│   ├── models/                    # local model catalog + downloads
│   └── metrics/                   # latency/usage stats, Prometheus
├── web/                           # React app (Vite)
│   ├── embed.go                   # package web: //go:embed dist
│   ├── src/
│   │   ├── api/                   # generated schema.d.ts + client + query hooks
│   │   ├── realtime/              # WS clients (captions, ingest, admin) + zustand stores
│   │   ├── i18n/ + locales/{es,en}/
│   │   ├── theme/                 # tokens.css (source of truth), palette.ts (generated), theme.ts (MUI), fonts
│   │   ├── components/            # shared UI (QrCode, LanguagePicker, ThemeToggle, …)
│   │   ├── features/
│   │   │   ├── setup/  admin/  capture/  viewer/  stage/  overlay/  replay/
│   │   └── routes/                # TanStack Router file routes (thin; import features)
│   └── e2e/                       # Playwright smoke tests
├── deploy/
│   ├── docker/Dockerfile
│   └── compose/{compose.yaml,compose.dev.yaml}
├── design/
│   └── preview.html               # visual reference for every surface (light + dark)
├── scripts/                       # fetch-test-audio.sh, gen-palette.py, check-contrast.py, …
├── testdata/audio/                # small committed fixtures + gitignored downloads
├── docs/                          # requirements, plan, design (locked design system), guides, runbook, scaling
├── Taskfile.yml  redocly.yaml  go.mod  LICENSE  NOTICE  .env.example
```

**Runtime components**

- **Single Go binary** (pure Go, `CGO_ENABLED=0`). It embeds the web app and serves HTTP, HTTPS and WS. It owns ffmpeg subprocesses for file/URL/SRT sources and recording.
- **Sidecars for the local provider** (optional, not linked into the binary):
  - `whisper-server` (whisper.cpp HTTP server) for ASR.
  - `ollama` running Gemma for translation.
- **External tools**: `ffmpeg` (with `libsrt`) for file/URL/SRT sources and recording.

**Pipeline per session**

```
Source (ws ingest | ffmpeg file/url | ffmpeg srt)
  → PCM 16 kHz mono frames (+ session clock)
      ├──▶ Recorder (AAC fMP4)
      ├──▶ Level meter / silence / clipping → status
      └──▶ ASR provider (Gemini Live | whisper.cpp)  → source captions (interim/final, detected lang)
                 └──▶ Translator fan-out (Gemini | Gemma) per target lang → translated captions
                          └──▶ Caption bus (session × track) → WS viewers / overlay / stage
                                                          → store (final) → VTT/SRT/TXT/JSON, replay
```

---

## 3. Parallel-work model

### 3.1 Lanes

| Lane | Scope | Suggested owner |
|---|---|---|
| **api** | `api/openapi.yaml`, codegen config | Tech lead / one agent (gatekeeper) |
| **platform** | Repo tooling, Taskfile, CI, Docker, release, dev env | Agent A |
| **core** | Go: config, store, auth, session manager, bus, handlers wiring, network | Agent B |
| **audio** | Go: ingest, ffmpeg sources, SRT, levels/VAD, recording | Agent C |
| **ai** | Go: providers (mock, Gemini, local), translation, glossary, language detection, hwcheck, models | Agents D1 (Gemini) + D2 (local) |
| **sec** | Go: secrets, TLS / local CA / autocert | Agent E |
| **web-shell** | Web: app shell, router, theme, i18n, API client, realtime lib, shared components | Agent F |
| **web-admin** | Web: setup wizard, dashboard, sessions, settings, secrets, glossary, models, TLS pages | Agent G |
| **web-audience** | Web: capture, viewer, stage, overlay, replay | Agent H |
| **docs** | README, runbook, OBS/vMix guide, scaling/cost doc, demo | Human + agent |

With fewer people, merge lanes (e.g. `core`+`sec`, `web-admin`+`web-audience`). The task graph stays valid.

### 3.2 Contract-first rules (how lanes stay unblocked)

1. **The OpenAPI spec is the contract.** Backend handlers implement the generated `StrictServerInterface`. The frontend uses only generated types and hooks.
2. **Codegen**, via `task gen`:
   - Go: `oapi-codegen` → `internal/api/gen.go` (`types`, `std-http-server`, `strict-server`). Go 1.22+ routing, no router library needed.
   - TS: `openapi-typescript` → `web/src/api/schema.d.ts`, plus `openapi-fetch` (client) and `openapi-react-query` (TanStack Query hooks).
   - WebSocket message types (`IngestHello`, `CaptionsServerMessage`, `AdminEvent`…) come from the same `components.schemas`.
3. **Mock server**: `task dev:mock` runs Prism on the spec (port 4010), so the web lanes can build pages before the backend handlers exist. The **mock provider** (`provider/mock`) plus the file source give the web lanes real WebSocket caption traffic as soon as P1-05 lands.
4. **Changing the contract**: open a `feat(api):` commit that changes **only** `api/openapi.yaml` + regenerated code. It must pass `redocly lint` and be backward compatible within the hackathon (additive fields and endpoints). Land it first, then implement. Breaking changes need an api-lane owner OK.
5. **Internal Go contract**: `internal/domain` (task P0-08) defines the interfaces between backend lanes. Changes there follow the same rule as the spec (small, additive, landed first).

### 3.3 File ownership (merge-conflict avoidance)

| Path | Owner lane | Shared-edit rule |
|---|---|---|
| `api/**`, `redocly.yaml` | api | Contract changes only via §3.2.4 |
| `internal/domain/**` | core (P0-08) | Additive changes only; announce |
| `internal/api/gen.go` | generated | Never hand-edit. `internal/api` holds only generated code so `domain` can alias its types without an import cycle |
| `internal/api/handlers/<tag>.go` | the lane owning that tag | One file per OpenAPI tag |
| `internal/app/wire.go` | core | Other lanes add **one line** to register their service |
| `docs/design.md`, `web/src/theme/**`, `design/**`, `scripts/gen-palette.py`, `scripts/check-contrast.py` | web-shell | Design changes follow §3.6 |
| `web/src/routes/**` | web-shell creates stubs in P0 | Feature lanes only edit their own route file |
| `web/src/locales/{es,en}/<namespace>.json` | one namespace per feature (`admin`, `viewer`, `capture`, …) | Never edit another feature's namespace; `common.json` belongs to web-shell |
| `Taskfile.yml`, CI, Docker | platform | Others propose changes in the commit body |

### 3.4 Milestones and critical path

| Milestone | Contents | Target (UTC) |
|---|---|---|
| **M0 Skeleton** | Phase 0 done: builds, dev env, contract, codegen, mock server, domain interfaces, design system in the app (theme, fonts, light/dark) | Thu 24 · 19:00 |
| **M1 Walking skeleton** | Browser capture → mock provider → bus → viewer/stage/overlay end to end, sessions CRUD, admin auth | Thu 24 · 23:59 |
| **M2 Real AI** | Gemini + local providers with EN/ES detection and ES/EN translation, VTT/SRT live + export, secrets | Fri 25 · 05:00 |
| **M3 Operable** | Dashboard, setup wizard, default-provider rule, recording + replay, TLS, SRT ingest | Fri 25 · 10:00 |
| **M4 Submitted** | Single binary + Docker, docs, acceptance checklist, demo video, Devpost submission | **Fri 25 · 14:00** (1 h buffer) |

**Critical path**: P0-01 → P0-02 → P0-08 → P1-05 (session manager) → P2-01 / P2-03 (providers) → P2-02 (translation) → P4-06 (acceptance) → P4-07 (demo video).

Everything else hangs off this path in parallel. If time runs short, cut from the bottom of each phase's priority (tasks marked **S** or **C**) and never from the critical path.

### 3.5 Definition of Done (every task)

- [ ] Code + tests (Go: table tests; Web: Vitest for logic, Playwright smoke for pages). `task check` is green (lint, typecheck, test, spec lint, codegen drift).
- [ ] Every user-facing string goes through i18n with **both `es` and `en`** keys. Pages are checked in **light and dark**.
- [ ] UI follows [`design.md`](design.md): tokens only (no raw colours/fonts/spacing), 8 interactive states, one primary button per view; compare against `design/preview.html`.
- [ ] Errors use translatable `code`s (UI-4). No secrets in logs.
- [ ] Apache-2.0 SPDX header on new source files.
- [ ] Docs touched if behavior or config changed (README / runbook / guide).
- [ ] Conventional Commit landed on `main`, with `Refs: <task-id>`.

### 3.6 Design system rules

The UI design is locked in [`docs/design.md`](design.md), with [`design/preview.html`](../design/preview.html) as the visual reference (also published as a private web page: https://claude.ai/artifact/6GoH35X8qEw6fWCX6zj2N2).

1. **Tokens only.** Components use CSS variables from `web/src/theme/tokens.css` or the MUI theme built from them (P0-09). No raw hex, `rgb()`, `oklch()`, `font-family` or pixel spacing in feature code. `task check` fails on raw colour literals outside `web/src/theme/`.
2. **Changing the design** is its own `feat(design):` commit that updates `docs/design.md` and `tokens.css` together, regenerates `palette.ts` (`task gen`), passes the contrast check, and updates `design/preview.html`. Land it before the feature that needs it. The published preview page is republished from the same file.
3. **Shared UI primitives** (status chip, level meter, copy field, panel, stat, ⌘K palette, theme and language switches) live in `web/src/components/` and belong to web-shell. Feature lanes compose them; they don't restyle them locally. If a feature needs a variant, add it to the shared component.
4. **Per-surface rules** in `docs/design.md` § Surfaces are acceptance criteria: the stage screen and overlay never follow the UI theme, the admin has one filled primary button per view, and status is always a chip plus a label.

---

## 4. Phases and tasks

Legend: **P** = priority from requirements (M must, S should, C could). **Deps** = tasks that must be done first. **Reqs** = requirement IDs covered.

### Phase 0: Scaffolding and dev environment (M0)

Goal: anyone (human or agent) can clone, run `task dev`, and see the app with hot reload. Contract and interfaces are frozen enough for Phase 1 to fan out.

P0-01 comes first. After that, P0-02..P0-07 run in parallel, and P0-08 needs P0-02.

| ID | Task | Lane | Deps | Reqs | Deliverables / acceptance |
|---|---|---|---|---|---|
| **P0-01** | **Repo skeleton** | platform | — | 5.4, 5.7 | Layout from §2; `go.mod` (`github.com/iencodev/live-subtitles`, Go 1.26); `LICENSE` (Apache-2.0), `NOTICE`; `.editorconfig`, `.env.example` (`.gitignore` already exists; extend it as tooling lands); `Taskfile.yml` with `dev`, `gen`, `check`, `build`, `test` stubs; SPDX header check script. |
| **P0-02** | **Contract + codegen pipeline** | api | P0-01 | AI-1, OUT-1 | `api/openapi.yaml` (initial version already drafted) + `redocly.yaml`; `task gen` generates Go (`internal/api/gen.go`) and TS (`web/src/api/schema.d.ts`, client, query hooks); `task check` fails on codegen drift; example values for the main schemas. |
| **P0-03** | **Go server skeleton** | core | P0-01 | 5.4 | `cmd/livesubs`: config (flags + env + `LIVESUBS_*`), `slog` JSON/text logging, HTTP server on `0.0.0.0:8080`, `/healthz`, graceful shutdown, serving the embedded `web/dist` with SPA fallback, `air` live reload config. Strict server wired with a `NotImplemented` default for all operations. |
| **P0-04** | **Web skeleton** | web-shell | P0-01 | UI-1, UI-2, UI-3, UI-5, UI-7 | Vite + React + TS; TanStack Router (file routes) with **stub routes for every page**: `/setup`, `/admin/*`, `/capture/$id`, `/s`, `/s/$id`, `/stage/$id`, `/overlay/$id`, `/replay/$id`; TanStack Query provider; Zustand; MUI installed with a `ThemeProvider` slot that P0-09 fills; **i18next ES/EN** with browser detection, `?ui=` override, switcher, MUI locale switch, per-feature namespaces; bare overlay layout (transparent, no theme); ESLint + Prettier + Vitest; missing-i18n-key check in `task check`. |
| **P0-05** | **Mock API + dev proxy** | platform | P0-02, P0-04 | — | `task dev:mock` runs Prism on `:4010`. Vite proxy switch `VITE_API=mock\|server`. |
| **P0-06** | **Dev environment** | platform | P0-01 | 5.4, AI-3 | `task dev` runs Vite (HMR) + `air` together, with Vite proxying `/api` and `/ws` to Go. `deploy/compose/compose.dev.yaml` runs `whisper-server` (whisper.cpp, multilingual model volume) + `ollama` (Gemma), with GPU passthrough where available and native-install notes for macOS/Metal. `task models:pull` pulls `ggml-large-v3-turbo` + `gemma3:4b` (smaller fallbacks documented). `scripts/fetch-test-audio.sh` uses yt-dlp + ffmpeg to fetch **one English and one Spanish Nerdearla talk** into gitignored `testdata/audio/`, trimmed to 16 kHz mono clips. A tiny self-recorded EN and ES clip is committed under `testdata/audio/fixtures/` for CI. |
| **P0-07** | **CI** | platform | P0-01 | 5.4 | GitHub Actions: Go (`vet`, `golangci-lint`, `test -race`), web (lint, typecheck, vitest, build), `redocly lint`, codegen drift, i18n key check, SPDX check. Runs on push to `main`. |
| **P0-08** | **Domain types + internal interfaces + mock provider** | core | P0-02 | AI-1, SES-2 | `internal/domain`: `Session`, `CaptionEvent` (alias of the generated `Caption`), `AudioFrame{PCM []int16, T time.Duration}`, `Clock`; interfaces `AudioSource`, `ASRProvider` (`Start(ctx, cfg) (chan<- AudioFrame, <-chan ASREvent, error)`), `Translator` (`Translate(ctx, TranslateRequest) (TranslateResult, error)`, streaming optional), `CaptionBus`, `SessionStore`, `CaptionStore`, `SecretStore`, `Recorder`. `provider/mock`: emits scripted EN/ES captions with interim→final and fake latency. `audio/fake`: generates silence or a sine wave. This is what unblocks all backend lanes. |
| **P0-09** | **Design system implementation** | web-shell | P0-04 | UI-5, UI-6, UI-7 | Implements [`docs/design.md`](design.md) in the app: global import of `web/src/theme/tokens.css`; self-hosted fonts via `@fontsource-variable/{space-grotesk,atkinson-hyperlegible-next,atkinson-hyperlegible-mono}` (no CDN, edge nodes may be offline); `web/src/theme/theme.ts` with `createTheme({ cssVariables: { colorSchemeSelector: '[data-theme="%s"]' }, colorSchemes })` fed by the generated `palette.ts`, typography from the font tokens, `shape.borderRadius: 6`, and component overrides from design.md § Components (Button: no elevation, no uppercase, 700; outlined secondary/danger; Card/Paper outlined 10 px; flat AppBar with hairline; Drawer on `paper-2`; OutlinedInput with `control` border and reserved helper height; Tooltip `enterDelay` 800 hover / 0 focus; instant `:focus-visible` ring); **Light/Dark/System** switch that keeps `data-theme` on `<html>` in sync, persisted per device, with an inline pre-mount script so there's no flash of the wrong theme; `scripts/gen-palette.py` wired into `task gen` with a drift check; `scripts/check-contrast.py` (WCAG pairs from design.md, both schemes) and a raw-colour-literal check in `task check` and CI; a dev-only `/dev/design` route that renders the theme and shared primitives in all 8 states, light and dark, matching `design/preview.html`. |

### Phase 1: Foundations and the walking skeleton (M1)

Goal: the whole flow works end to end with the mock provider: capture page → server → viewer, stage screen and overlay. Admin can create sessions.

Most tasks here are independent. Backend and frontend lanes run in parallel against the contract and the Prism mock.

**Backend**

| ID | Task | Lane | Deps | Reqs | Deliverables / acceptance |
|---|---|---|---|---|---|
| **P1-01** | **SQLite store + migrations** | core | P0-08 | SES-1 | `modernc.org/sqlite`, embedded SQL migrations; tables: `sessions`, `settings` (JSON), `glossaries`, `overlay_presets`, `captions` (session, track, segment, times, text, flags), `recordings`, `admin` (PIN hash). Repositories behind the domain interfaces; tests on a temp DB. |
| **P1-02** | **Admin auth + setup** | core | P1-01 | ADM-3 | `GET/POST /api/setup`, `/api/auth/{login,logout,me}`; PIN hashed with Argon2id; HttpOnly SameSite cookie `ls_admin`; bearer admin token via env; login rate limit; middleware applies the spec's `security`; per-session ingest tokens (random, hashed at rest, `rotateIngestToken`). |
| **P1-03** | **Sessions API** | core | P1-01, P1-02 | SES-1, SES-4 | Handlers for `sessions` tag + `/api/public/sessions*`; slug validation; defaults from settings (`[es, en]`, `auto`, recording on); `urls` built from netinfo. |
| **P1-04** | **Caption bus + `/ws/captions`** | core | P0-08 | OUT-1, 5.5 | In-process pub/sub keyed by (session, track); per-track ring buffer (history on connect); interim replacement by `segmentId`; slow-consumer drop policy; viewer counting; `CaptionsServerMessage` framing; tested with 500 simulated clients. |
| **P1-05** | **Session manager + pipeline orchestrator** | core | P0-08, P1-04 | SES-2, SES-3, SES-4, AI-1 | One goroutine group per running session: source → (recorder hook) → ASR → translator fan-out → bus + store. State machine `idle/starting/live/paused/stopping/error`; start/pause/stop handlers; session clock origin; `/ws/admin` event stream with `sessionStatus`. Works end to end with `provider/mock` and `audio/fake`. **Critical path.** |
| **P1-06** | **Audio ingest WS** | audio | P0-08 | AUD-1, AUD-6 | `/ws/ingest/{id}`: token auth, `IngestHello` handshake, binary s16le 16 kHz frames → `AudioSource`; single active connection (replace old one); RMS/peak dBFS, silence and clipping detection → `level` messages + status; jitter buffer; frame-gap detection. |
| **P1-07** | **File/URL test source** | audio | P0-08 | AUD-3 | ffmpeg subprocess `-re` → s16le 16 kHz mono pipe → `AudioSource`; `POST/DELETE /api/sessions/{id}/sources/file`; loop and start offset; `task demo:file SESSION=main FILE=testdata/audio/en.m4a` helper. |
| **P1-08** | **Network info + terminal QR** | core | P0-03 | OUT-4 | Enumerate interfaces (skip loopback, virtual, docker), pick the preferred IPv4, `publicBaseUrl` override, `GET /api/network`; print LAN URLs + a QR (half-block characters) at startup. |
| **P1-09** | **Subtitle formatting library** | core | P0-08 | OUT-6, OUT-7, OUT-8, 5.2 | `internal/subtitle`: sentence-aware segmenter (≤42 chars/line, ≤2 lines, min/max cue duration), writers for VTT, SRT, TXT, JSON; `GET /api/public/sessions/{id}/subtitles` (live = `no-store`, export = attachment) and `/captions` pagination. Golden-file tests; output validated in VLC and a browser `<track>`. |
| **P1-10** | **Secrets store** | sec | P0-08 | SEC-1, SEC-2, SEC-3, SEC-4 | `internal/secrets`: resolution order **env → OS keychain (`zalando/go-keyring`) → encrypted file** (AES-256-GCM, Argon2id KDF, passphrase from `LIVESUBS_MASTER_KEY` or a TTY prompt); `secrets` tag handlers (write-only, masked hint, source); redacting `slog` handler; tests for each backend (keychain behind a build tag / skipped in CI). |

**Frontend**

| ID | Task | Lane | Deps | Reqs | Deliverables / acceptance |
|---|---|---|---|---|---|
| **P1-11** | **API client + realtime lib** | web-shell | P0-02, P0-04 | OUT-1, SES-5 | `src/api`: `openapi-fetch` client with cookie credentials and an error → i18n code mapper; `openapi-react-query` hooks. `src/realtime`: `useCaptions(sessionId, langs[])` (WS with backoff reconnect, history merge, interim replacement, Zustand store per session/track), `useAdminEvents()`, `createIngestSocket()`. Unit tests with a mock WS. |
| **P1-12** | **Shared UI primitives** | web-shell | P0-04, P0-09 | OUT-4, UI-2, UI-5 | Per docs/design.md § Components, each shown on `/dev/design` in all 8 states: `StatusChip` (live / ok / warn / error / idle / starting: mono uppercase label + dot, LIVE filled with `live`), `LevelMeter` (24 segments ok → warn → danger, `role="meter"`), `Panel` (hairline, 10 px), `Stat` (mono label + tabular value), `CopyField` (graphite URL card + copy with clipboard fallback), `KbdHint`, `QrCode`, `LanguagePicker` (native names), `ThemeToggle` and `UiLanguageSwitcher` (segmented), `EmptyState` (what's empty, why, one action), `ErrorAlert` (translates `code`: what broke, why, what to do). Icons: `@mui/icons-material` Outlined only, `aria-hidden` next to text. |
| **P1-13** | **Capture page** | web-audience | P1-11, P1-12 | AUD-1, AUD-2 | `/capture/$id?token=`: device picker (labels after permission), **AudioWorklet** downmix + resample to 16 kHz s16le in 20 ms frames, level meter, start/stop, auto-reconnect with local buffer, device choice persisted, Screen **Wake Lock**, clear warnings when not a secure context (TLS-2). |
| **P1-14** | **Audience viewer** | web-audience | P1-11, P1-12 | OUT-2, OUT-11, UI-1, UI-7 | `/s` (session list) and `/s/$id`: language picker (tracks + `source`), auto-scrolling transcript with interim styling, "jump to live", font size control, `aria-live="polite"` region, recorded-session notice, mobile-first. Caption type: Atkinson Hyperlegible Next 500 at `--text-caption`, interim line in `--color-muted` with a cobalt caret, mono timecodes, `lang` attribute per caption track. |
| **P1-15** | **Stage screen** | web-audience | P1-11, P1-12 | OUT-3, UI-7 | `/stage/$id`: full screen, last N lines, large high-contrast presets (independent of the UI theme), optional dual language, QR corner linking to the viewer, auto-hide cursor, reconnect indicator. Uses the `--stage-*` presets (`white-on-black` default, `yellow-on-black`, `black-on-white`) and `--text-caption-stage`; captions appear without animation. |
| **P1-16** | **OBS/vMix overlay page** | web-audience | P1-11 | OUT-5, UI-7 | `/overlay/$id?lang=&preset=&…`: transparent background, no MUI chrome, style from preset + query params (`OverlayStyle`), max lines, fade after silence, text outline, 1920×1080-safe layout, no scrollbars; verified in the OBS Browser source. Built-in presets from design.md: **Classic box** (`--overlay-box`), **Outline only**, **Lower third**; Atkinson Hyperlegible Next 600. |
| **P1-17** | **Admin shell + sessions CRUD UI** | web-admin | P1-11, P1-12 | SES-1, ADM-3 | `/setup` (PIN step only for now), login, admin layout/nav, sessions list + create/edit dialog (name, slug, room, source language, target languages, provider, glossary, recording), start/pause/stop buttons, "copy URL" for viewer/stage/overlay/capture (with token), QR display. Layout per design.md: side rail (N3) on `paper-2` with accent tick on the active item, flat top bar with the ⌘K trigger (palette itself in P3-19), **one filled primary button per view**. |

**M1 exit check**: open `/capture/main` on the laptop mic and `/s/main` on a phone via the QR code, and see mock captions flowing; `/stage/main` and `/overlay/main` render; every page switches light/dark and ES/EN and matches the design preview's look; `task check` passes.

### Phase 2: Real AI and languages (M2)

Goal: real transcription and translation with both providers, EN/ES auto-detection, configurable target languages, and live and exported subtitle files.

| ID | Task | Lane | Deps | Reqs | Deliverables / acceptance |
|---|---|---|---|---|---|
| **P2-01** | **Gemini Live ASR provider** | ai (D1) | P0-08, P1-10 | AI-2, AI-6, AI-10, 5.1, 5.5 | `provider/gemini` using `google.golang.org/genai` Live API: stream PCM, input transcription → interim/final `ASREvent`s with timestamps; the system prompt constrains the source to EN/ES and emits the detected language per segment; **session resumption + context window compression** for talks over the Live session limit; backoff reconnect with gap logging; usage accounting. Measured latency with the EN and ES fixtures. **Critical path.** |
| **P2-02** | **Translation fan-out + Gemini translator** | ai (D1) | P0-08, P1-05 | AI-5, AI-6, 5.2 | `internal/translate`: per-session fan-out to N target tracks; **passthrough when target == detected source**; rolling context (last K sentences); prompt template with glossary and do-not-translate hooks (filled in P2-06); translate interim text (debounced) and final text (authoritative); Gemini text model implementation (streaming). Tests with a fake translator. **Critical path.** |
| **P2-03** | **Local ASR: whisper.cpp** | ai (D2) | P0-06, P0-08 | AI-3, AI-10, 5.1 | `provider/local/whisper`: client for `whisper-server`; **VAD chunking** (Silero or energy-based) with a sliding window for interim text + commit on pause for final text; `language=auto` restricted to `{en, es}` (pick the higher probability of the two) per segment; multilingual model only (refuse `.en` models). Spanish accuracy check on the ES fixture (WER noted in the docs). **Critical path.** |
| **P2-04** | **Local translator: Gemma via Ollama** | ai (D2) | P2-02 | AI-3, AI-5 | `provider/local/gemma`: Ollama `/api/chat` streaming, the same prompt template as P2-02, warm-up on session start, `keep_alive`, concurrency limit per model; latency measured for es↔en. |
| **P2-05** | **Language detection plumbing** | ai | P2-01 or P2-03 | AI-10 | Per-segment `sourceLang` through the pipeline; session `sourceLanguage` pin (`en`/`es`) overrides detection; hysteresis to avoid flapping on short utterances; `detectedLanguage` in status + `/ws/captions` `state` messages. Test: an EN clip followed by an ES clip in one session. |
| **P2-06** | **Glossary** | ai + web-admin | P1-01, P2-02 | AI-7, 5.2 | Backend `glossaries` handlers; injection into ASR prompts (Gemini) / whisper `prompt` (initial prompt with terms) and translation prompts; do-not-translate enforcement; admin UI page to edit terms (table, CSV paste). A seed glossary of common tech terms is shipped. |
| **P2-07** | **Default-provider rule + key validation** | ai + sec | P1-10, P2-01, P2-03 | AI-4, AI-11, SEC-5 | `GET /api/providers` (resolved default + reason); `POST /api/secrets/{name}/validate` (cheap Gemini call); `provider: default` resolves at session start; falls back to local with an admin warning if the key is invalid or removed. |
| **P2-08** | **Latency + usage metrics** | core | P1-05 | AI-9, 5.1 | Per track: latency = caption emit time − audio end timestamp (p50/p95/last); Gemini tokens + audio seconds → estimated USD with configurable prices; exposed in `SessionStatus` and `/ws/admin`. |
| **P2-09** | **Supported languages** | ai | P0-08 | AI-5 | `GET /api/languages` catalog (ES, EN, PT, FR, DE, IT, ZH, JA, KO; `canBeSource` only EN/ES); validation of `targetLanguages` in the sessions API. |

**M2 exit check**: an English fixture and a Spanish fixture run in two concurrent sessions, one on Gemini and one on local. Both show correct detected languages and ES + EN tracks within about 3–4 s; `.vtt`/`.srt` play in VLC.

### Phase 3: Operations features (M3)

Goal: event-day operability: dashboard, setup wizard, recording and replay, TLS, SRT ingest, resilience.

| ID | Task | Lane | Deps | Reqs | Deliverables / acceptance |
|---|---|---|---|---|---|
| **P3-01** | **Admin dashboard (live)** | web-admin | P1-17, P2-08 | ADM-1, SES-4, AUD-6 | Card per session: state, provider, detected language, level meter, silence/clipping flags, latency per track, viewers, SRT stats, recent errors (translated), start/pause/stop, quick links + QR. Driven by `/ws/admin`. |
| **P3-02** | **Settings UI** | web-admin | P1-17 | ADM-2, OUT-4 | Default languages, provider settings (Gemini models, whisper/Ollama URLs + models), caption formatting, network (preferred interface, public URL), recording (default, bitrate, retention), SRT (port, latency, passphrase). |
| **P3-03** | **Secrets UI** | web-admin | P1-10, P2-07 | SEC-1, SEC-5, AI-11 | Write-only API key field showing the masked hint, storage source, "Validate" button, and the resulting default-provider banner ("Using Gemini" or "Using local: add a Google API key to use Gemini"). |
| **P3-04** | **Hardware self-check + benchmark** | ai (D2) | P2-03, P2-04 | AI-12 | `internal/hwcheck`: OS/arch/CPU/RAM, GPU backend detection (Metal on Apple Silicon, `nvidia-smi` for CUDA, Vulkan probe), runtime reachability (whisper-server, ollama, ffmpeg + libsrt); model recommendation table; `POST /api/system/benchmark` runs a 30 s committed clip through the local pipeline → real-time factor. Warns if RTF > 0.8. |
| **P3-05** | **Model bootstrap** | ai (D2) | P3-04 | AI-13 | `internal/models`: catalog (whisper `large-v3-turbo`/`medium`/`small` GGUF from Hugging Face with SHA256; Gemma via `ollama pull`); resumable downloads with progress → `/api/models` + `modelProgress` events; the models dir is shared with the whisper-server sidecar. |
| **P3-06** | **Setup wizard (full)** | web-admin | P1-17, P3-03, P3-04, P3-05 | 5.4, AI-11, AI-12, AI-13 | Steps: UI language + theme → admin PIN → hardware check + benchmark → download recommended models → optional Google API key (shows which default provider will be used) → create the first session → show the capture URL + QR. |
| **P3-07** | **Recording** | audio | P1-05 | REC-1, REC-2, REC-3, REC-5 | `internal/recording`: tee PCM → ffmpeg → AAC-LC fragmented MP4 (32/48/64 kbps), chunked per talk or per 30 min, the session clock as origin (captions carry the same timeline); `recordings` handlers + Range streaming; retention job + disk usage; per-session toggle. Survives kill -9 (fMP4 is playable up to the last fragment). |
| **P3-08** | **Replay page** | web-audience | P1-11, P3-07 | REC-4, OUT-8 | `/replay/$id`: recording picker, `<audio>` + `<track>` per language, a clickable synced transcript (highlights the current cue, click to seek), language switcher, downloads (m4a, SRT, VTT, TXT). |
| **P3-09** | **TLS: local CA + dual listeners** | sec | P0-03, P1-08 | TLS-1, TLS-2, TLS-3, TLS-5 | `internal/tlsutil`: generate a local CA (10 y) + leaf (≤ 397 d) with SANs `localhost`, `127.0.0.1`, every LAN IP, hostname, `hostname.local`; regenerate the leaf when the IP set changes; HTTPS `:8443` next to HTTP `:8080`; `--tls-cert/--tls-key` (bring your own); `autocert` when `publicBaseUrl` is https with a real domain; `GET /api/tls`, `/api/tls/ca.crt`. QR codes stay HTTP (TLS-3). |
| **P3-10** | **TLS UI + install guide** | web-admin + docs | P3-09 | TLS-4 | Admin page: cert info, fingerprint, CA download, per-OS install instructions (macOS, Windows, iOS, Android, Linux) in ES/EN. |
| **P3-11** | **SRT ingest** | audio | P1-07 | AUD-5, SRT-1, SRT-4 | ffmpeg `srt://0.0.0.0:<port>?mode=listener&streamid=…` per session (or one port per session if streamid routing isn't reliable), optional passphrase, configurable latency; demux any MPEG-TS audio (AAC, MP2, Opus) → PCM; SRT stats (parsed from ffmpeg/`-stats` or srt-live-transmit) → `SessionStatus.srt`; libsrt capability check → `SystemInfo.features.srtIngest`. Tested with OBS (`srt://` output) and `ffmpeg -re … -f mpegts srt://`. |
| **P3-12** | **Resilience** | core + ai | P1-05, P2-01, P2-03 | SES-5, 5.5 | Automatic restart of a crashed provider or source with capped backoff, and a gap marker in captions and status; ingest reconnect keeps the session live; Gemini resumption verified with a 60 min test file at 4× speed (where possible) or a forced reconnect. |
| **P3-13** | **Observability** | platform | P2-08 | ADM-5 | Structured request logs with session IDs; `/metrics` Prometheus (sessions live, latency histograms, viewers, WS clients, provider errors, bytes recorded); `/healthz` with dependency checks. |
| **P3-14** | **Overlay presets + preview** | web-admin + core | P1-16 | OUT-5 | `overlay-presets` handlers; admin editor with a **live preview** over a sample video frame; "Copy overlay URL" per language; built-in presets (Classic box, Outline only, Lower-third). |
| **P3-15** | **i18n, a11y and design audit** | web-shell | most UI tasks | UI-1, UI-3, UI-4, UI-6, OUT-11 | Every screen in ES/EN with no hard-coded strings (lint rule); every backend error code has ES/EN translations; axe checks in Playwright for light and dark; contrast fixes. Design audit: Playwright screenshots of every route in light and dark at 375 and 1440 px compared by eye against `design/preview.html`; no horizontal scroll at 320 / 375 / 414 / 768 px; no two-line button or nav labels; contrast check green. |
| **P3-16** | **YouTube closed captions (Route A: HTTP POST)** | core | P1-05, P1-10 | CC-1, CC-2, CC-3, OUT-10 | `internal/streamcc`: per-session sink subscribed to the chosen track's **final** captions → line-wrapped cues → POST to the YouTube caption ingestion URL (UTC timestamp line + text, increasing `seq`); clock offset from YouTube's response timestamp; bounded queue, retry with backoff, no replay of stale cues after long outages; URL stored as a per-session secret (`PUT/DELETE …/stream-captions/youtube-url`), never logged; `POST …/stream-captions/test`; `StreamCaptionStatus` in session status and `/ws/admin`. Tested against a local fake ingestion server plus one real YouTube test stream (unlisted). Works regardless of vMix or OBS. |
| **P3-17** | **Stream captions UI** | web-admin | P1-17, P3-16 | CC-3 | Session editor section: enable, target (YouTube HTTP / OBS), track (default `en`), write-only ingestion URL field (masked hint), "Send test caption" button; dashboard card shows state, last `seq`, last sent, errors; hint recommending "burn one language with the overlay, send the other as CC". |
| **P3-18** | **OBS `SendStreamCaption` (Route B, quick option)** | core | P3-16 | CC-4 | Second `streamcc` target: obs-websocket v5 client (e.g. `andreykaipov/goobs`), `websocketUrl` setting + `obs_websocket_password` secret; final captions split to ≤32-char lines (608 limit) and paced; reconnect when OBS restarts. Verified on a YouTube or Twitch test stream from OBS. Priority C: do it if P3-16 is done and time allows. |
| **P3-19** | **⌘K command palette** | web-admin | P1-17, P1-12 | ADM-1, UI-1 | The admin's signature interaction from docs/design.md: opens with ⌘K / Ctrl+K or the top-bar trigger; `role="dialog"` + `aria-modal`, focus trapped and restored, type-to-filter, ↑/↓ + Enter, Esc closes; commands: start / pause / stop a session, open viewer / stage / overlay / capture, copy each URL, go to any admin page, switch theme and UI language. Translated (ES/EN), reduced-motion safe, no layout shift. |

**M3 exit check**: fresh data dir → the wizard completes → a session runs with the local provider by default → after entering a Google key, a new session defaults to Gemini. A replay page plays audio with synced ES/EN subtitles. A second laptop captures over HTTPS after installing the CA. OBS pushes audio via SRT and captions appear. A YouTube test stream shows the English track as closed captions via the ingestion URL while the overlay burns in Spanish.

### Phase 4: Packaging, docs and submission (M4)

| ID | Task | Lane | Deps | Reqs | Deliverables / acceptance |
|---|---|---|---|---|---|
| **P4-01** | **Release binaries** | platform | P0-03, P0-04 | 5.4 | `goreleaser` (or a `task build:all` matrix) building macOS/Linux/Windows × amd64/arm64 with the web app embedded; version info via ldflags. |
| **P4-02** | **Docker + compose** | platform | P4-01 | 5.4, 5.8 | Multi-stage Dockerfile (ffmpeg with libsrt); `compose.yaml` profiles: `app` (Gemini-only), `local` (+ whisper-server + ollama, GPU optional); volumes for data/models; healthchecks; UDP port for SRT. |
| **P4-03** | **README + event-day runbook** | docs | P3-* | 5.4, Submission | README: what it is, screenshots, quick start (binary, Docker, dev), required credentials and models, configuration reference, license. `docs/runbook.md`: mini PC setup (3.5 mm line-in, input gain, browser kiosk, wake lock, autostart), pre-show checklist, troubleshooting. Both in English, and the README also has a short Spanish summary. |
| **P4-04** | **OBS / vMix / YouTube guide** | docs | P1-16, P3-14, P3-16 | OUT-5b, CC-6 | `docs/obs-vmix-guide.md`: step by step with screenshots for OBS (Browser source) and vMix (Web Browser input as overlay), multi-language scenes, SRT audio out from OBS/vMix to the server; **YouTube closed captions**: enable "POST captions to URL" in Live Control Room, paste the ingestion URL in the admin, set the 30–60 s broadcast delay, choose the burned-in vs CC language; optional OBS `SendStreamCaption` setup; troubleshooting. |
| **P4-05** | **Scalability + cost doc and load test** | core + docs | P1-04, P2-08 | 5.3, 5.8 | `task loadtest`: N sessions (mock provider + file source) × M WS viewers; report CPU/RAM per session and fan-out capacity. `docs/scaling.md`: edge vs cloud vs hub, 2 → 10 → 30+ rooms, hardware per edge node for local, Gemini cost per hour per session (from P2-08), network/ports. |
| **P4-06** | **Acceptance run** | all | P1–P3 | §8 checklist | Walk through every item of requirements §8 on a clean machine; Playwright smoke suite (setup, create session, viewer receives mock captions, overlay transparent, theme and language switch). Fix blockers. **Critical path.** |
| **P4-07** | **Demo video + submission** | docs | P4-06 | Submission | 1–2 min video: capture from a real Nerdearla talk (EN) + an ES talk, phone via QR, stage screen, OBS overlay burned in, provider switch, replay. English subtitles burned in using our own output. Devpost entry with the repo link and description. Deadline 15:00 UTC. |
| **P4-08** | **Deployment modes docs** | docs | P3-09, P4-02 | 5.8, D6 | `docs/deployment.md`: dev, edge (one instance per mini PC), cloud (VM + Let's Encrypt, capture over WSS or SRT), with example configs. |

### Phase 5: Stretch and post-hackathon

These are independent of each other, so pick any in any order.

| ID | Task | Lane | Reqs | Notes |
|---|---|---|---|---|
| P5-01 | Live caption correction | core + web-admin | ADM-4 | `PATCH /captions/{segmentId}` (already in the contract); inline edit/hide in the dashboard; propagate to viewers, exports and replay. |
| P5-02 | Provider fallback | ai | AI-8 | On repeated Gemini errors or quota loss, switch to local mid-session (and the reverse); status + log event. |
| P5-03 | CEA-608/708 caption injection relay (Route C) | audio | CC-5 | Relay that receives the vMix/OBS RTMP/SRT stream, injects 608 caption data into the H.264 SEI without re-encoding (libcaption-style, as in its `flv+srt` example), and forwards it to the platform. Only if captions must be embedded in the stream itself. |
| P5-04 | SRT egress: captions-only key feed | audio | SRT-3 | ffmpeg `lavfi color` + `drawtext` (`textfile` + `reload=1`) → H.264 MPEG-TS → SRT listener; vMix luma/chroma key. |
| P5-05 | SRT egress: burned-in captioned feed | audio | SRT-2 | SRT A/V in → `drawtext`/`subtitles` overlay → re-encode → SRT out; hardware encoder when available. |
| P5-06 | Hub mode | core | 5.8 | Edge → hub outbound WS for caption events and session metadata; hub serves a room directory and remote viewers; auth with a hub token. |
| P5-07 | Agenda / per-talk split | core + web-admin | SES-6 | Room schedule (import CSV/JSON); auto-split recordings and transcripts per talk; titles in the viewer. |
| P5-08 | Server-side device capture | audio | AUD-4 | ffmpeg `avfoundation`/`dshow`/`alsa`/`pulse` input selection from the admin UI; headless mode. |
| P5-09 | RTMP/HLS/Icecast ingest | audio | AUD-7 | Generalize the ffmpeg source to any URL. |
| P5-10 | Re-process recordings | ai | REC-6 | Offline job: run a recording through a better model or an updated glossary → new caption version. |
| P5-11 | More UI locales (PT) | web-shell | UI-8 | Add `locales/pt`; verify no code changes are needed. |
| P5-12 | Multi-key secrets | sec | SEC-5 | Named keys per event or team; choose the key per session. |

`OUT-9` (vMix Title data source) was dropped by decision D3, so it has no task.

---

## 5. Requirements traceability

Every requirement ID maps to at least one task.

| Req | Task(s) | | Req | Task(s) |
|---|---|---|---|---|
| SES-1 | P1-01, P1-03, P1-17 | | OUT-1 | P0-02, P1-04, P1-11 |
| SES-2 | P0-08, P1-05 | | OUT-2 | P1-14 |
| SES-3 | P1-05, P4-05 | | OUT-3 | P1-15 |
| SES-4 | P1-03, P1-05, P3-01 | | OUT-4 | P1-08, P1-12, P3-02 |
| SES-5 | P1-11, P3-12 | | OUT-5 | P1-16, P3-14 |
| SES-6 | P5-07 | | OUT-5b | P4-04 |
| AUD-1 | P1-06, P1-13 | | OUT-6 | P1-09 |
| AUD-2 | P1-13 | | OUT-7 | P1-09 |
| AUD-3 | P1-07 | | OUT-8 | P1-09, P3-08 |
| AUD-4 | P5-08 | | OUT-9 | dropped (D3) |
| AUD-5 | P3-11 | | OUT-10 | P3-16, P3-18 |
| AUD-6 | P1-06, P3-01 | | OUT-11 | P1-14, P3-15 |
| AUD-7 | P5-09 | | ADM-1 | P3-01, P3-19 |
| AI-1 | P0-02, P0-08, P1-05 | | ADM-2 | P3-02 |
| AI-2 | P2-01 | | ADM-3 | P1-02, P1-17 |
| AI-3 | P0-06, P2-03, P2-04 | | ADM-4 | P5-01 |
| AI-4 | P2-07 | | ADM-5 | P3-13 |
| AI-5 | P2-02, P2-04, P2-09 | | SEC-1 | P1-10, P3-03 |
| AI-6 | P2-01, P2-02 | | SEC-2 | P1-10 |
| AI-7 | P2-06 | | SEC-3 | P1-10 |
| AI-8 | P5-02 | | SEC-4 | P1-10 |
| AI-9 | P2-08 | | SEC-5 | P2-07, P3-03, P5-12 |
| AI-10 | P2-01, P2-03, P2-05 | | SRT-1 | P3-11 |
| AI-11 | P2-07, P3-03, P3-06 | | SRT-2 | P5-05 |
| AI-12 | P3-04, P3-06 | | SRT-3 | P5-04 |
| AI-13 | P3-05, P3-06 | | SRT-4 | P3-11 |
| REC-1 | P3-07 | | TLS-1 | P3-09 |
| REC-2 | P3-07 | | TLS-2 | P1-13, P3-09 |
| REC-3 | P3-07 | | TLS-3 | P3-09 |
| REC-4 | P3-08 | | TLS-4 | P3-10 |
| REC-5 | P3-07 | | TLS-5 | P3-09 |
| REC-6 | P5-10 | | UI-1 | P0-04, P1-14, P3-15 |
| UI-2 | P0-04, P1-12 | | UI-3 | P0-04, P3-15 |
| UI-4 | P1-11, P3-15 | | UI-5 | P0-04, P0-09, P1-12 |
| UI-6 | P0-09, P3-15 | | UI-7 | P0-04, P0-09, P1-14, P1-15, P1-16 |
| UI-8 | P5-11 | | CC-1 | P3-16 |
| CC-2 | P3-16 | | CC-3 | P3-16, P3-17 |
| CC-4 | P3-18 | | CC-5 | P5-03 |
| CC-6 | P4-04 | | | |

| Non-functional | Task(s) |
|---|---|
| 5.1 Latency | P2-01, P2-03, P2-08 |
| 5.2 Quality | P1-09, P2-02, P2-06 |
| 5.3 Scalability | P1-05, P4-05 |
| 5.4 Deployment & ops | P0-01, P0-03, P0-06, P0-07, P3-06, P4-01, P4-02, P4-03 |
| 5.5 Reliability | P1-04, P2-01, P3-12 |
| 5.6 Security & privacy | P1-02, P1-10, P3-07, P3-09 |
| 5.7 Licensing | P0-01, P0-07 |
| 5.8 Deployment modes | P4-02, P4-08, P5-06 |
| Submission deliverables | P4-03, P4-06, P4-07 |

---

## 6. Suggested staffing timeline

With about 6 parallel agents, this is one way the lanes fill up (times in UTC). With fewer agents, keep the same order and collapse the columns.

| Window | platform / api | core | audio | ai | sec | web-shell | web-admin | web-audience |
|---|---|---|---|---|---|---|---|---|
| Thu 15–19 (M0) | P0-01, P0-02, P0-05, P0-06, P0-07 | P0-03, P0-08 | — | P0-06 (models) | — | P0-04, P0-09 | — | — |
| Thu 19–24 (M1) | P3-13 prep | P1-01..05, P1-08, P1-09 | P1-06, P1-07 | P2-01 (Gemini) and P2-03 (whisper) start early against `domain` | P1-10 | P1-11, P1-12 | P1-17 | P1-13..16 |
| Fri 00–05 (M2) | P4-01 | P2-08, P1-09 finish | P3-07 | P2-02, P2-04, P2-05, P2-07, P2-09 | P3-09 | P3-15 (continuous) | P2-06 UI, P3-02 | P3-08 |
| Fri 05–10 (M3) | P4-02, P3-13 | P3-12, P3-16, P3-18, P4-05 | P3-11 | P3-04, P3-05, P2-06 | P3-10 | — | P3-01, P3-03, P3-06, P3-14, P3-17, P3-19 | polish |
| Fri 10–14 (M4) | release | P4-06 | P4-06 | P4-06 | P4-06 | P4-06 | P4-04 | P4-03, P4-07, P4-08 |

---

## 7. Risks and mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| Gemini Live API limits (session duration, concurrency, quota) | Talks cut off, sessions refused | Session resumption and context compression (P2-01), reconnect (P3-12), local fallback (P5-02), and a quota check before the event |
| Local provider too slow on the mini PC | High latency or backlog | Hardware check and benchmark (P3-04) recommends smaller models; default switches to Gemini once a key is added (AI-11); document hardware in `scaling.md` |
| whisper.cpp hallucinations on silence or music | Garbage captions | VAD gate (P2-03), silence detection (P1-06), minimum-energy threshold, drop repeated n-grams |
| Code-switching EN↔ES within a sentence | Wrong language detected | Per-segment detection with hysteresis (P2-05); operator can pin the source language |
| Browser mic permissions and secure context | Capture fails on remote machines | `localhost` on the mini PC (D5), local CA over HTTPS (P3-09), SRT ingest alternative (P3-11) |
| ffmpeg without libsrt on some hosts | SRT unavailable | Capability detection + clear UI message; Docker image ships libsrt |
| YouTube caption ingestion quirks (clock skew, one track only, broadcast delay required) | CC missing or out of sync on the stream | Clock offset from YouTube's response, test caption button, runbook step to set the delay; overlay remains the primary path |
| UI drifts from the design across parallel lanes | Inconsistent screens, rework at the end | Tokens-only rule enforced in `task check`, shared primitives owned by web-shell (§3.6), `/dev/design` route, design audit in P3-15 |
| Contract churn between lanes | Rework and merge conflicts | Contract-first rules (§3.2), additive-only changes, api lane gatekeeper |
| Time (24 h hackathon) | Unfinished scope | Critical path first; S/C tasks are cut before M tasks; the M1 walking skeleton is always demo-able |

---

## 8. Commands reference (to be implemented in P0)

| Command | What it does |
|---|---|
| `task dev` | Vite (HMR) + Go (air) with proxy; opens the LAN URL/QR in the terminal |
| `task dev:mock` | Prism mock API from `api/openapi.yaml` on `:4010` |
| `task dev:ai` | `compose.dev.yaml` up: whisper-server + ollama |
| `task models:pull` | Download the default whisper + Gemma models |
| `task audio:fetch` | Download and trim the EN/ES Nerdearla test clips |
| `task demo:file SESSION=… FILE=…` | Feed a file to a session in real time |
| `task gen` | Regenerate Go + TS code from the OpenAPI spec, and `palette.ts` from `tokens.css` |
| `task check` | Lint, typecheck, tests, spec lint, codegen drift, i18n keys, SPDX, contrast, no raw colour literals |
| `task design:preview` | Open `design/preview.html` in the browser |
| `task build` | Build the web app + a single binary for the host platform |
| `task build:all` | Cross-compile release binaries |
| `task loadtest` | N sessions × M viewers load test |
