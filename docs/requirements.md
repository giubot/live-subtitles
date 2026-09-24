# Live Subtitles — Requirements

Open source platform that takes live audio from conference stages, produces real-time subtitles (transcription + translation), and delivers them to in-room screens, audience phones, and the live stream.

Built for the **Nerdearla Vibeathon 2026** (sponsor: Google DeepMind).

- Hackathon site: https://nerdearla26.devpost.com/
- Resources: https://nerdearla26.devpost.com/resources
- Rules: https://nerdearla26.devpost.com/rules

---

## 1. Context

### 1.1 The problem

Nerdearla runs 30+ English-language sessions in parallel. Today it uses a commercial SaaS for simultaneous transcription and translation, which doesn't scale well in cost or operations at that size. The goal is an **open source, scalable, real-time transcription and translation solution** that Nerdearla can fork and run themselves.

### 1.2 Current production setup (from the organizers, Discord #nerdearla-vibeathon)

| Topic | Today |
|---|---|
| **Audio source** | Mic output leaves the audio mixer through a **3.5 mm jack** straight into a **mini PC** per stage. The input is opened **in a browser**. |
| **Operation** | The browser runs a SaaS page that transcribes and translates the raw mic input until someone presses stop. There is no dedicated per-room operator. |
| **Audience delivery** | Screens in front of the stage show the subtitles (the same browser window that captures the audio). The audience can also **scan a QR code** and follow along on their phones. |
| **Gap** | They'd like to **plug it into vMix and burn the subtitles into the stream**, since the virtual audience currently gets no live translation. |

Design implication: the capture machine is a low-powered mini PC with a browser and a line-in. Setup per room has to be close to "open a URL, pick the input, press start."

### 1.3 Hackathon constraints

| Item | Value |
|---|---|
| Start | Thu 2026-09-24 15:00 UTC |
| **Submission deadline** | **Fri 2026-09-25 15:00 UTC (12:00 ART)** |
| Winners | Sat 2026-09-26, live at Nerdearla (Ciudad Cultural Konex, Buenos Aires) |
| License | OSI-approved open source (MIT / Apache-2.0 / GPL…). Nerdearla may use, adapt, fork and deploy it. |
| Code origin | The project must be started during the hackathon. |
| Suggested tech | Gemini audio capabilities (Gemini Live API) **or** Gemma for fully local processing |

**Submission deliverables**
1. A 1–2 minute demo video using real conference audio (YouTube @nerdearla talks work well). English subtitles are encouraged.
2. A public GitHub/GitLab repo with an OSI license.
3. A README with deployment instructions and the credentials and models required.
4. Submission before the deadline.

**Judging criteria** (unweighted)
1. **Quality**: transcription accuracy and clarity, including technical terminology.
2. **Latency**: subtitle delay acceptable for a live talk.
3. **Scalability**: multiple simultaneous sessions without major changes or prohibitive cost.
4. **Deployment & operations**: ease of production deployment and event-day operation.
5. **Innovation**: creative features that add value.

---

## 2. Goals and non-goals

### Goals
- Real-time transcription in the original language, with **automatic detection of the input language (English or Spanish)**, and real-time translation into a **configurable list of major languages (default: Spanish + English)** from a live audio input.
- **Two or more concurrent sessions** (stages), with a documented path to 5–10+ and to 30+.
- Two interchangeable AI backends: **Gemini (cloud)** and **Gemma (local, offline-capable)**.
- Runs **locally for dev/testing** and can be **self-hosted** on a LAN machine or server. It works on an event LAN with no internet when using the local provider.
- Subtitle outputs: web viewer, stage screen, phone via QR/LAN IP, **OBS/vMix overlay** for burning into the stream, and **SRT/VTT** for players and export.
- API keys are stored securely.

### Non-goals (for the hackathon)
- Multi-tenant SaaS and user accounts beyond a single operator role.
- Video processing. The platform only deals with audio in and text out; compositing happens in OBS/vMix.
- Speaker diarization (nice to have, not required).
- Mobile native apps. The viewer is a responsive web app.

---

## 3. Actors

| Actor | Description |
|---|---|
| **Operator / Admin** | Sets up the server, providers, API keys and sessions. Starts and stops sessions and monitors health. |
| **Capture station** | The mini PC at each stage. Runs a browser page that captures line-in audio and streams it to the server. |
| **Stage screen** | Full-screen, high-contrast subtitle display in the room. |
| **Audience member** | Scans a QR code and follows a session on their phone, choosing the language. |
| **Stream producer** | Adds a subtitle overlay to OBS/vMix, or pulls caption data into the production. |
| **Developer** | Runs everything locally with hot reload and test audio. |

---

## 4. Functional requirements

Priority: **M** = must (hackathon MVP), **S** = should, **C** = could / stretch.

### 4.1 Sessions (stages)

| ID | Requirement | P |
|---|---|---|
| SES-1 | Create, edit and delete sessions (name, room, source language, target languages, AI provider, glossary). | M |
| SES-2 | Start, stop and pause a session. Each running session has its own independent audio → AI → subtitle pipeline. | M |
| SES-3 | Run ≥2 sessions concurrently on one server. The design must scale horizontally (see §5.3). | M |
| SES-4 | Session state (idle / connecting / live / error) is visible in real time on the admin dashboard. | M |
| SES-5 | Auto-reconnect: if the capture station or AI provider drops, the pipeline resumes without operator action and the gap is logged. | S |
| SES-6 | Optional schedule/agenda per room (talk title, speaker) so transcripts are split per talk. | C |

### 4.2 Audio ingest

| ID | Requirement | P |
|---|---|---|
| AUD-1 | **Browser capture page**: pick an audio input device (`getUserMedia`), show a live level meter, and stream PCM (16 kHz mono, 16-bit) over WebSocket to the backend. This mirrors today's setup (3.5 mm line-in → mini PC → browser). | M |
| AUD-2 | Capture page is a single URL per session, can be bookmarked and starts on one click. It persists the selected device and keeps the screen awake (Wake Lock API). | M |
| AUD-3 | **File / URL source for testing**: stream a local audio or video file (or URL via ffmpeg) at real-time speed as if it were live, so the platform can be tested with YouTube @nerdearla talks. | M |
| AUD-4 | Server-side capture of a local audio device from the Go backend, for a headless setup with no browser. | C |
| AUD-5 | **SRT (Secure Reliable Transport) ingest**: the server opens an SRT listener per session (`srt://<server>:<port>?streamid=<session>`), and vMix/OBS or a hardware encoder pushes program audio (or audio+video) to it. ffmpeg (built with `libsrt`) demuxes the MPEG-TS and decodes the audio to 16 kHz mono PCM, which feeds the same pipeline as AUD-1. See §4.7. | S |
| AUD-7 | Other network stream ingest (RTMP / HLS / Icecast URL) via ffmpeg. | C |
| AUD-6 | Basic audio health: silence detection, clipping warning, input level exposed to the dashboard. | S |

### 4.3 Transcription and translation (AI providers)

| ID | Requirement | P |
|---|---|---|
| AI-1 | A **provider abstraction** in the backend: `stream audio in → stream of caption events out` (partial/interim and final segments, timestamps, language). New providers plug in without touching the rest of the pipeline. | M |
| AI-2 | **Gemini provider (cloud)**: uses the Gemini Live API (streaming audio) for low-latency transcription, plus translation into each target language. The model is configurable. | M |
| AI-3 | **Local provider (whisper.cpp + Gemma)**: fully on-device, no internet once models are downloaded. **Speech-to-text runs on whisper.cpp** (multilingual model, e.g. `large-v3-turbo`, or `medium`/`small` on weaker hardware; never an English-only `.en` model) with VAD-based chunking for streaming. **Translation runs on Gemma** via a local runtime (Ollama or llama.cpp server). Spanish input must be transcribed at the same quality as English. Validate with Spanish Nerdearla talks. | M |
| AI-4 | Provider selectable **per session**, so one room can run on Gemini and another on the local provider. | S |
| AI-11 | **Default provider rule**: new sessions use the **local provider (whisper.cpp + Gemma)** unless a **valid Google API key is configured**, in which case the default becomes **Gemini**. The key is validated on save. The operator can always override per session. If the key is later removed or becomes invalid, the default falls back to local and the dashboard shows a warning. | M |
| AI-12 | **Hardware self-check** at startup and in the setup wizard: detect CPU/GPU (Metal, CUDA, Vulkan), RAM, and whether the local runtime and models are present, then **recommend model sizes** (e.g. whisper `large-v3-turbo` vs `small`, Gemma 4B vs 1B). A short benchmark on a bundled audio clip reports the real-time factor. If the local provider can't keep up in real time, the dashboard warns the operator and suggests adding a Google API key. | S |
| AI-13 | **Model bootstrap**: a guided first-run download of the whisper.cpp model and the Gemma model (via Ollama pull or a GGUF download), with progress in the UI and a checksum check. Works offline once downloaded. | S |
| AI-5 | **Translation into a configurable list of target languages** per session, defaulting to **`[es, en]`**. The list is chosen from a curated set of major languages (at least ES, EN, PT, FR, DE, IT, ZH, JA, KO). Each target language is a separate caption track. If a target equals the detected source language, that track shows the original transcription with no translation call. | M (ES, EN) / S (others) |
| AI-10 | **Automatic input language detection** between **English and Spanish**. It runs per utterance/segment, so a talk that switches language (e.g. a Spanish Q&A after an English talk) is handled without operator action. The detected language is attached to each caption event and shown on the dashboard. The operator can optionally pin the source language for a session. Whisper: language detection restricted to `{en, es}`. Gemini: the prompt tells the model to detect between EN and ES. | M |
| AI-6 | **Interim vs. final captions**: show fast partial text that gets replaced when the segment is finalized, to keep perceived latency low. | S |
| AI-7 | **Technical glossary** per session or global (terms, product names, speaker names) injected into the prompts to improve accuracy on technical vocabulary. Also a "do not translate" list (e.g. Kubernetes, React). | S |
| AI-8 | Optional **fallback**: if the cloud provider fails or quota runs out, fall back to local (or the reverse). | C |
| AI-9 | Per-session metrics: end-to-end latency (audio timestamp → caption emitted), tokens/minutes used, estimated cost for Gemini. | S |

### 4.4 Subtitle delivery and playback

| ID | Requirement | P |
|---|---|---|
| OUT-1 | **Real-time caption bus**: every caption event is published per session and per language to subscribers over WebSocket (or SSE). | M |
| OUT-2 | **Audience web viewer** (responsive, mobile-first): choose session → choose language → scrolling live transcript. Readable font size controls, dark/light mode, auto-scroll with "jump to live". | M |
| OUT-3 | **Stage screen mode**: full-screen, large high-contrast text showing the last 2–3 lines, optional dual-language (original + translation), and an optional QR corner for the audience URL. | M |
| OUT-4 | **LAN access via QR**: the server detects its LAN IP address(es) and shows a QR code (in admin and on the stage screen) linking to `http://<lan-ip>:<port>/s/<session>`. The operator can pick the interface when there are several and override the public URL (e.g. a DNS name). | M |
| OUT-5 | **OBS / vMix overlay**: a transparent-background browser-source URL per session and language (`/overlay/<session>?lang=es&lines=2&style=…`) for burning the subtitles into the stream. Styling (font, size, color, outline, background box, position, max lines, fade timeout) is set via query params or saved presets. The admin UI has a "Copy overlay URL" button with a live preview. | M |
| OUT-5b | **Step-by-step guide** (`docs/obs-vmix-guide.md`, with screenshots) for adding the overlay: **OBS** (Sources → Browser → URL, 1920×1080, no custom CSS, layer above the video source) and **vMix** (Add Input → Web Browser → URL, 1920×1080, place it as an overlay channel or layer over the program input; the page background is transparent, so no keying is needed). It also covers multi-language streams (one overlay per language or scene) and troubleshooting. | M |
| OUT-6 | **Live WebVTT**: a continuously updated `.vtt` per session and language, plus a demo player page showing a `<track>` over a video/audio element. | S |
| OUT-7 | **Live SRT (SubRip)**: a continuously updated `.srt` per session and language, for tools that poll or tail a subtitle file. | S |
| OUT-8 | **Export** after or during a session: SRT, VTT, plain text and JSON (with timestamps) per language. | S |
| OUT-9 | ~~vMix Title data source~~. Dropped: the browser input overlay (OUT-5/5b) is the vMix integration. | — |
| OUT-10 | **OBS native closed captions**: push captions into the stream as CEA-608 via obs-websocket (`SendStreamCaption`), so platforms like YouTube/Twitch show toggleable CC. | C |
| OUT-11 | Accessibility: WCAG AA contrast, screen-reader friendly live region on the viewer, no flashing. | S |

### 4.5 Administration and operations

| ID | Requirement | P |
|---|---|---|
| ADM-1 | Admin dashboard: session list with status, audio level, latency, provider, viewer count, errors, and start/stop controls. | M |
| ADM-2 | Settings: provider configuration (Gemini model, Gemma endpoint/model), default languages, glossary, network/public URL, overlay presets. | M |
| ADM-3 | Admin is protected by an operator password/PIN or an admin token. Viewer, stage and overlay routes are read-only and public on the LAN. The capture page requires a per-session ingest token. | M |
| ADM-4 | Live correction: the operator can edit or hide a caption line, and the change propagates to viewers and exports. | C |
| ADM-5 | Structured logs and a `/healthz` endpoint. Prometheus `/metrics` is optional. | S |

### 4.6 API key and secret management

| ID | Requirement | P |
|---|---|---|
| SEC-1 | API keys (e.g. `GEMINI_API_KEY`) are entered in the admin UI or provided via environment variables, and are **never returned to the browser** after saving (write-only fields showing a masked hint like `••••abcd`). | M |
| SEC-2 | On desktop/dev hosts, secrets are stored in the **OS keychain** (macOS Keychain, Windows Credential Manager, Linux Secret Service), e.g. via `zalando/go-keyring`. | M |
| SEC-3 | Fallback for headless servers and containers: an **encrypted secrets file** (AES-256-GCM, key derived with Argon2id from a master passphrase supplied via env or prompt), or plain env vars / Docker secrets. | M |
| SEC-4 | Secrets are never written to logs, the SQLite DB in plaintext, exports or the repo. `.env` is gitignored, and a `.env.example` is provided. | M |
| SEC-5 | Support multiple named keys (e.g. per event or per team) and let the operator test a key ("Validate" button). | C |

### 4.7 SRT streaming protocol

SRT (Secure Reliable Transport) is supported natively by vMix (SRT output/input), OBS (`srt://` in custom stream output and in Media Source) and most hardware encoders. It is a good fit here in two directions:

| ID | Requirement | P |
|---|---|---|
| SRT-1 | **Ingest (audio in)**: this is AUD-5. The server listens on a configurable UDP port range (one port per session, or one port with `streamid` routing), with optional passphrase encryption (`passphrase=`, AES) and configurable `latency`. This lets the production send the *mixed program audio* from vMix/OBS instead of (or as a backup to) the 3.5 mm line-in on the mini PC. | S |
| SRT-2 | **Egress (video out with burned-in subtitles)**: the server takes an SRT input with video+audio, burns the live captions onto the video, and republishes it as an SRT output that vMix/OBS/a CDN can pull. Implementation: ffmpeg `drawtext` fed from a live-updated text file (`reload=1`), re-encoding H.264. Trade-offs: needs CPU/GPU for video encoding per session and adds ~0.5–2 s of video latency. Main use case: a turnkey "captioned feed" when the stream isn't produced in OBS/vMix. | C |
| SRT-3 | **Egress (captions-only key feed)**: an SRT output of a captions-only video (subtitles on solid green/black or with alpha), which vMix can add as an SRT input and luma/chroma-key. This is an alternative to the browser input for setups where the vMix machine can't reach the web UI. Cheaper than SRT-2 because the video frames are trivial. | C |
| SRT-4 | The dashboard shows SRT connection stats per session (connected, RTT, packet loss, bitrate). | C |

Implementation notes:
- Needs `ffmpeg` built with `--enable-libsrt` (the default in most distro/Homebrew builds). The Docker image ships it. Startup checks for `libsrt` and disables the SRT features with a clear warning if it's missing.
- Pure-Go alternative for the transport layer: `datarhei/gosrt`. Decoding AAC/MPEG-TS audio still needs ffmpeg, so ffmpeg is the simplest path.
- Firewall: document the UDP ports that need to be open on the event LAN.

### 4.8 Audio recording and replay

| ID | Requirement | P |
|---|---|---|
| REC-1 | **Record session audio by default** (per-session toggle). The server encodes the same 16 kHz mono PCM it feeds to the AI into a compressed file as it arrives. | S |
| REC-2 | Format: **AAC-LC in fragmented MP4 (`.m4a`) at ~32–48 kbps**. It plays in every browser (including iOS Safari) and survives crashes mid-recording because it's fragmented. Written in chunks (e.g. one file per talk or per 30 minutes) via an ffmpeg subprocess. | S |
| REC-3 | Caption timestamps are relative to the recording's start (a shared session clock), so the exported VTT/SRT line up with the audio with no offset fixing. | S |
| REC-4 | **Replay page** `/replay/<session>`: an audio player with a synced transcript per language (`<track>` + clickable transcript lines that seek). Language switcher, download buttons (audio, SRT, VTT, TXT). | S |
| REC-5 | Retention: configurable auto-delete after N days, a disk usage indicator, and delete per recording from the admin UI. A notice on the viewer page says the session is recorded. | S |
| REC-6 | Re-process a recording offline with a better/slower model or updated glossary to produce improved subtitles. | C |

**Cost estimate**: at 32 kbps AAC, one session uses **~14 MB per hour** (48 kbps: ~22 MB/h). A full event day (8 h) is roughly 110–175 MB per room, and **30 rooms × 2 days ≈ 7–10 GB**. CPU cost of AAC encoding at 16 kHz mono is negligible (<1% of a core). In the cloud, that's cents per month in object storage.

### 4.9 HTTPS / TLS

| ID | Requirement | P |
|---|---|---|
| TLS-1 | **Built-in HTTPS**. On first run the server generates a local CA and a leaf certificate whose SANs include `localhost`, every LAN IP, the hostname and `<hostname>.local`. It's regenerated when the LAN IPs change. HTTP and HTTPS listeners run side by side (default `:8080` / `:8443`). | M |
| TLS-2 | The capture page on the mini PC uses `http://localhost`, which is already a secure context, so it works without trusting any certificate. HTTPS is for **remote** capture stations and admin access. | M |
| TLS-3 | **Audience phones use plain HTTP** via the QR code, because a self-signed certificate would show a scary warning on every phone. The viewer needs no mic or other secure-context API. | M |
| TLS-4 | The admin UI offers a **CA download + install instructions** (macOS, Windows, iOS, Android) for machines that must use HTTPS on the LAN. | S |
| TLS-5 | Bring-your-own certificate (`--tls-cert`, `--tls-key`), and **automatic Let's Encrypt** (`autocert`) when a public domain is configured (cloud mode, §5.8). | S |

### 4.10 User interface: language and theme

| ID | Requirement | P |
|---|---|---|
| UI-1 | The **whole UI is available in Spanish and English**: admin, setup wizard, capture page, audience viewer, stage screen chrome, replay page, and error messages. The UI language is **separate from the subtitle language** (e.g. an English UI can show Spanish subtitles). | M |
| UI-2 | UI language defaults to the browser language (`navigator.languages`: `es-*` → Spanish, otherwise English), can be changed from a switcher in the app bar, and is stored per device. `?ui=es\|en` sets it via the URL, useful for QR links and kiosk screens. | M |
| UI-3 | Translations live in JSON resource files (`web/src/locales/{es,en}/*.json`) via **i18next + react-i18next**. There are no hard-coded user-facing strings, and a CI/lint check flags missing keys. Dates, numbers and durations are formatted with `Intl` in the active locale. MUI component locales (`esES`/`enUS`) switch too. | M |
| UI-4 | The backend returns **error codes plus parameters** (e.g. `{"code":"provider.key_invalid"}`) rather than English sentences, and the frontend translates them. Server-side logs stay in English. | S |
| UI-5 | **Light and dark mode** with three settings: **Light / Dark / System** (default: System, following `prefers-color-scheme` and updating live). A toggle in the app bar stores the choice per device. Built on the MUI theme (`colorSchemes` / CSS variables) so there's no flash of the wrong theme on load. | M |
| UI-6 | Both themes meet **WCAG AA contrast**, including caption text, status chips and the audio level meter. | S |
| UI-7 | Theme scope: the **audience viewer** and **admin** follow light/dark. The **stage screen** has its own high-contrast presets (default: light text on black, which suits projectors), configurable per session. The **OBS/vMix overlay** is always transparent and styled only by its own overlay settings, never by the UI theme. | M |
| UI-8 | Additional UI languages (e.g. Portuguese) can be added by dropping in a new locale folder, with no code changes. | C |

---

## 5. Non-functional requirements

### 5.1 Latency (judging: Latency)
- **Target**: interim captions ≤ **1.5 s** and final transcription ≤ **3 s** after speech; translation ≤ **4 s** after speech (Gemini, good network).
- Local Gemma targets depend on hardware and are documented with measured numbers.
- Latency is measured and shown per session (AI-9).

### 5.2 Quality (judging: Quality)
- Glossary and "do-not-translate" lists (AI-7).
- Sentence-aware segmentation: captions are split at natural boundaries with a max of ~42 chars/line and 2 lines for overlays.
- Configurable context window (prior sentences passed to translation) for coherent translation.

### 5.3 Scalability (judging: Scalability)
- One pipeline per session (a goroutine group). No shared global state beyond the caption bus.
- Documented capacity: sessions per core/RAM for local Gemma, and concurrent Live API sessions plus estimated **cost per hour per session** for Gemini.
- Scaling model: see §5.8. **One instance per room** at the event (edge), and the **same binary** runs many sessions in the cloud. Adding rooms means adding instances; nothing shared needs to scale except the optional hub.
- A doc covers going from 2 → 10 → 30+ rooms, with hardware per edge node and cost per hour per session in cloud mode.

### 5.4 Deployment and operations (judging: Deployment)
- **Single binary**: the Go backend embeds the built React app (`embed.FS`). One command starts it. Cross-compiled for macOS, Linux and Windows (amd64/arm64).
- **Docker Compose** profile: `app` plus optional `ollama` (or another local model server) with GPU passthrough when available.
- **Dev mode**: Vite dev server with HMR proxying to the Go backend (`air` or similar for Go live reload). A `make dev` / `task dev` one-liner, plus test-audio fixtures.
- Binds to `0.0.0.0` so LAN devices can connect. Prints the LAN URLs and QR in the terminal at startup.
- Zero-config defaults: SQLite in a local data dir and sensible ports. The first-run setup wizard sets the admin PIN, runs the hardware check, downloads the local models, and optionally takes a Google API key, which switches the default provider to Gemini (AI-11).
- The README covers requirements, models to download, credentials, and a step-by-step event-day runbook.

### 5.5 Reliability
- Auto-reconnect for capture WebSockets and provider streams (SES-5). Caption history is buffered server-side so late-joining viewers see recent context.
- Gemini Live sessions have duration limits. The provider transparently rotates or resumes sessions (session resumption / context window compression) for talks of 45+ minutes.

### 5.6 Security and privacy
- LAN-first. No telemetry.
- Audio **is recorded by default** (§4.8). It's toggled per session, has configurable retention, and viewers see a notice. Recordings never leave the machine unless cloud mode or hub sync is configured.
- HTTPS per §4.9. `getUserMedia` requires a secure context: `localhost` on the mini PC, or HTTPS for remote capture.

### 5.7 Licensing
- **Apache-2.0** for the repo (`LICENSE` + `NOTICE` at the root, SPDX headers in source files). All dependencies must be OSI-compatible. Model weights (Gemma) are downloaded by the user under their own terms, not redistributed.

### 5.8 Deployment modes

The same binary and config format support every mode. Only the defaults change.

| Mode | Where | Sessions | Capture | Audience access | TLS |
|---|---|---|---|---|---|
| **Dev** | Developer laptop | Any | Browser on `localhost`, file/URL test source | `localhost` + LAN QR | Optional |
| **Edge** (event default) | **One instance per mini PC / room** | 1 (can be more) | Browser on `http://localhost` (line-in), or SRT from vMix/OBS | `http://<lan-ip>` via QR on the stage screen | Local CA (§4.9) |
| **Cloud** | VM / container (Cloud Run is a poor fit for long WebSockets and UDP; use a VM/GKE or similar) | Many (N rooms) | Remote browser over **WSS**, or **SRT** push from each venue | Public HTTPS URL, one directory of all rooms | Let's Encrypt |
| **Hub** (stretch) | Edge instances + one cloud hub | Edge: 1 each | Edge (local) | Edge publishes caption events to the hub, which serves a single directory and remote viewers | Let's Encrypt on the hub |

Notes:
- **Edge** isolates failures (one room down doesn't affect others) and keeps working if the venue internet drops (local provider) or is flaky (Gemini).
- **Moving to the cloud** is a config change: the mini PC's capture page points at the cloud URL (or vMix pushes SRT to it) instead of `localhost`. Session, caption and recording logic is identical.
- **Hub mode** combines both: AI runs at the edge, and fan-out to thousands of remote viewers or the stream runs in the cloud. Transport: each edge keeps an outbound WebSocket to the hub, so no inbound ports are needed at the venue.
- **Hardware caveat**: with **Gemini**, any mini PC works because the AI runs in the cloud and the box only captures, encodes and serves. With the **local provider** (whisper.cpp + Gemma), real time needs roughly an Apple Silicon Mac mini or a mini PC with a recent iGPU/NPU or discrete GPU. On weaker boxes, use smaller models (whisper `small`/`medium`, Gemma 1B–4B) and document the measured latency.

---

## 6. Proposed architecture

```
 Stage mini PC (browser)                Server (Go, single binary)                         Consumers
┌──────────────────────┐   WS PCM   ┌──────────────────────────────────────────┐
│ /capture/:session    │──────────▶ │ Ingest ─▶ Session pipeline (per room)    │   WS/SSE   ┌─────────────────────┐
│ getUserMedia + meter │            │            │                             │──────────▶ │ Viewer (phone, QR)  │
└──────────────────────┘            │            ▼                             │            │ Stage screen        │
 File/URL/SRT source ──(ffmpeg)───▶ │   Provider interface                     │            │ OBS/vMix overlay    │
                                    │   ├─ Gemini Live API (cloud)             │            └─────────────────────┘
                                    │   └─ Local: whisper.cpp ASR + Gemma MT   │   HTTP     ┌─────────────────────┐
                                    │            │                             │──────────▶ │ live .vtt / .srt    │
                                    │            ▼                             │            │ exports, vMix data  │
                                    │   Caption bus (per session × language)   │            └─────────────────────┘
                                    │   SQLite (sessions, captions, settings)  │
                                    │   Secret store (OS keychain / enc. file) │
                                    └──────────────────────────────────────────┘
```

### 6.1 Tech stack

**Frontend** (`/web`)
- React + TypeScript + **Vite** (no Next.js)
- **TanStack Router** (type-safe routes: `/admin/*`, `/capture/:id`, `/s/:id`, `/stage/:id`, `/overlay/:id`)
- **TanStack Query** for REST data (sessions, settings)
- **Zustand** for client state (live caption buffers, audio device, UI prefs)
- **MUI** components and theming (the overlay route uses minimal/no MUI chrome for a transparent background and performance)
- **i18next + react-i18next** for UI translations (ES/EN), plus MUI locale packs
- MUI theme with `colorSchemes` for light/dark/system
- AudioWorklet for PCM capture and resampling to 16 kHz; `qrcode` lib for QR rendering

**Backend** (`/server`)
- **Go** (1.23+), `net/http` stdlib routing (or `chi`)
- WebSockets: `coder/websocket` (nhooyr)
- Gemini: official `google.golang.org/genai` SDK (Live API)
- Local ASR: **whisper.cpp** (`whisper-server` HTTP, or CGO bindings), multilingual `ggml-large-v3-turbo` (Metal on macOS, CUDA on Linux), with Silero VAD for chunking
- Local translation: **Gemma** (e.g. `gemma3` 4B/12B, sized to hardware) via the Ollama or llama.cpp server HTTP API
- ffmpeg with `libsrt` for file, URL and SRT ingest (and SRT egress, a stretch goal)
- Storage: SQLite via `modernc.org/sqlite` (pure Go, no CGO, so cross-compiling stays easy)
- Secrets: `zalando/go-keyring` plus an encrypted-file fallback
- ffmpeg (external binary, optional) for file/URL sources

### 6.2 Key API surface (draft)

| Method | Path | Purpose |
|---|---|---|
| `GET/POST/PUT/DELETE` | `/api/sessions[/:id]` | Session CRUD |
| `POST` | `/api/sessions/:id/start` · `/stop` | Control |
| `WS` | `/ws/ingest/:id?token=` | Audio in (binary PCM frames and control JSON) |
| `WS` | `/ws/captions/:id?lang=` | Caption events out |
| `GET` | `/api/sessions/:id/captions.{vtt,srt,txt,json}?lang=&live=1` | Live and export subtitle files |
| `GET` | `/api/network` | LAN IPs, public URL (for the QR) |
| `GET` | `/api/recordings[/:id]`, `/api/recordings/:id/audio.m4a` | List, stream (HTTP range) and delete recordings |
| `GET` | `/api/tls/ca.crt` | Download the local CA for trusting HTTPS on the LAN |
| `PUT` | `/api/secrets/:name` | Write-only secret set. `GET` returns only a masked hint. |
| `GET` | `/healthz`, `/metrics` | Ops |

Caption event (draft):
```json
{ "sessionId": "main-stage", "lang": "es", "segmentId": "s-000123", "final": true,
  "text": "Hoy vamos a hablar de Kubernetes…", "start": 1234.56, "end": 1237.80,
  "sourceLang": "en", "latencyMs": 1850 }
```

---

## 7. MVP scope (hackathon, ~24 h)

In order of delivery:

1. Go server + embedded React shell (i18n ES/EN + light/dark theme from day one), session CRUD in SQLite, admin PIN.
2. Browser capture page → WebSocket PCM ingest. File-source test harness (ffmpeg) for YouTube talks.
3. Gemini Live provider: EN transcription + ES translation, interim and final.
4. Caption bus → audience viewer + stage screen + LAN QR.
5. OBS/vMix overlay route (transparent) + setup guide + live VTT/SRT endpoints + export.
6. Local provider: whisper.cpp (EN/ES auto-detect) + Gemma translation.
7. Secret storage (keychain + encrypted file + env) + default-provider rule (local unless a Google API key is set).
8. Two concurrent sessions demo, latency metrics on the dashboard.
9. Audio recording + replay page with synced subtitles.
10. Local CA / HTTPS listener.
11. SRT ingest (vMix/OBS program audio → server).
12. README, scalability/cost doc, LICENSE, 1–2 min demo video.

Stretch: glossary UI, extra target languages beyond ES/EN, SRT egress (burned-in or key feed), OBS CEA-608 captions, live correction, fallback provider, cloud hub mode, re-processing recordings.

---

## 8. Acceptance criteria (demo checklist)

- [ ] Two sessions run simultaneously from two different audio sources (e.g. mic + a YouTube talk file).
- [ ] Each shows the original transcription and translations (default ES + EN) live in the viewer with < ~3–4 s delay.
- [ ] An English talk and a Spanish talk are both detected correctly without changing any setting. The Spanish talk shows a Spanish transcription and an English translation.
- [ ] A session with the local provider (whisper.cpp + Gemma) transcribes a Spanish talk with quality comparable to English.
- [ ] A phone on the same Wi-Fi scans the QR, opens the session and switches language.
- [ ] The stage screen displays large subtitles with a QR.
- [ ] The OBS browser source shows transparent overlay subtitles on top of a video source and is recorded/streamed.
- [ ] Following the guide, the same overlay works as a vMix Web Browser input.
- [ ] Audio pushed from OBS/vMix via SRT to the server produces subtitles.
- [ ] The `.vtt` / `.srt` files download and play correctly in a player (e.g. VLC or an HTML `<track>`).
- [ ] A session switches between the Gemini and local (whisper.cpp + Gemma) providers.
- [ ] With no API key, new sessions default to the local provider. After saving a valid Google API key, new sessions default to Gemini.
- [ ] The Gemini API key is saved via the UI, survives a restart, doesn't appear in the DB/logs/API responses, and lives in the OS keychain.
- [ ] After a session, the replay page plays the recorded audio with synced subtitles in each language.
- [ ] A second machine on the LAN opens the capture page over HTTPS (after installing the local CA) and streams audio.
- [ ] Every screen can be switched between Spanish and English, and defaults to the browser language.
- [ ] Every screen supports Light / Dark / System. The overlay stays transparent regardless of the theme.
- [ ] A fresh clone runs following only the README (`make dev` and single-binary/Docker).

---

## 9. Decisions log

| # | Topic | Decision |
|---|---|---|
| D1 | Local AI path | Hybrid: **whisper.cpp** for speech-to-text (multilingual model, must handle Spanish well) + **Gemma** for translation. |
| D2 | SRT | Means the **SRT streaming protocol** (in addition to `.srt` SubRip files). Ingest is planned (should). Egress with burned-in or key-feed video is a stretch goal. See §4.7. |
| D3 | vMix | Use the **overlay page as a vMix Web Browser input**, plus a setup guide. No Title data-source integration. |
| D4 | Languages | **Auto-detect input between English and Spanish.** Output in a configurable list of major languages, **default `[es, en]`**. |
| D5 | Capture host and TLS | Capture runs in a browser **on the mini PC itself** (`localhost`). Built-in HTTPS with an auto-generated local CA is still provided for remote capture and admin (§4.9). |
| D6 | Topology | **One instance per mini PC (edge)** for the event. The same binary runs **multi-session in the cloud**, and a hub mode that combines both is a stretch goal (§5.8). |
| D7 | License | **Apache-2.0**. |
| D8 | Audio recording | **Record by default** as AAC `.m4a` (~14–22 MB/h per room), with a replay page that shows synced subtitles (§4.8). |
| D9 | Default provider | Mini PC specs are unknown. The **default is local (whisper.cpp + Gemma)** unless a Google API key is provided, in which case the default is Gemini (AI-11). A hardware self-check recommends model sizes and warns if the box can't keep up in real time (AI-12). |
| D10 | UI language and theme | UI in **Spanish and English** (independent of the subtitle language) with **Light / Dark / System** themes (§4.10). |

## 10. Open questions

None at the moment. New questions will be added here as implementation progresses.
