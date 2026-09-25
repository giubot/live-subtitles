# Development guide

How to run Live Subtitles from source. The [plan](plan.md) covers the architecture and the task board.

## Requirements

- Go 1.26+, Node 22+ with pnpm, Python 3, [Task](https://taskfile.dev) (`brew install go-task`)
- `ffmpeg` (file/URL/SRT sources, recording; `brew install ffmpeg`)
- For the local AI provider: whisper.cpp and Ollama (below)
- For the test clips: `yt-dlp` (`brew install yt-dlp`)

## Everyday commands

| Command | What it does |
|---|---|
| `task dev` | Go server with live reload (air) on `:8080` + Vite with HMR on `:5173`. Open http://localhost:5173. |
| `task dev PORT=18080` | The same, with the Go server on another port if 8080 is taken. |
| `task dev:web API=mock` + `task dev:mock` | Frontend against the Prism mock of `api/openapi.yaml` (`:4010`), no Go server needed for REST. |
| `task gen` | Regenerate Go/TS code from the spec, the route tree and `palette.ts`. Commit the output. |
| `task check` | Everything CI runs. |
| `task build` | Web app + single binary in `bin/livesubs`. |
| `task build:all` | Web app + binaries for macOS, Linux and Windows × amd64/arm64 in `bin/<os>_<arch>/`. |
| `task loadtest SESSIONS=10 VIEWERS=500` | Load test on the mock provider: starts a throwaway server, plays the EN fixture into every session and reports caption delivery, drops, throughput and server CPU/RSS. `ADDR=http://host:port` targets a running server instead. See [scaling.md](scaling.md#load-test). |
| `task release:snapshot` | GoReleaser dry run: the release archives (`.tar.gz`, `.zip` for Windows) and `checksums.txt` in `dist/`, nothing published. Uses `goreleaser` from `PATH`, else `go run` of the pinned version. |

Every binary reports its version with `livesubs -version`: `git describe` locally, the tag in a release. Pushing a `v*` tag runs `.github/workflows/release.yml`, which makes a **draft** GitHub release with the archives and pushes the container image to GHCR ([deployment](deployment.md)).

Environment variables are listed in [`.env.example`](../.env.example). Flags win over `LIVESUBS_*` variables, which win over defaults (`bin/livesubs -h`).

## Admin access

On first run, open `/setup` and choose the admin PIN (4 to 64 characters). It's stored as an Argon2id hash in `<data dir>/livesubs.db`. Logging in sets the `ls_admin` cookie (HttpOnly, SameSite=Strict, 7 days); after 5 wrong PINs in 15 minutes, a device has to wait.

Scripts can skip the PIN with a bearer token set in `LIVESUBS_ADMIN_TOKEN`:

```sh
curl -H "Authorization: Bearer $LIVESUBS_ADMIN_TOKEN" http://localhost:8080/api/sessions
```

To start over with a new PIN, stop the server and delete `<data dir>/livesubs.db` (this also deletes sessions and captions).

## HTTPS

The server listens on HTTP `:8080` and HTTPS `:8443` side by side, with the same app on both. Audience phones, the QR code and every printed or generated link stay on plain HTTP (TLS-3). HTTPS is for capture pages and admin access from other computers, because browsers only allow the microphone on `https://` or `http://localhost`. The startup banner prints the HTTPS address and, in `local-ca` mode, where to download the CA.

| Flag | Variable | Default | |
|---|---|---|---|
| `--tls` | `LIVESUBS_TLS` | `auto` | `auto`, `local-ca`, `provided`, `acme`, or `disabled` (`false`/`off`) for no HTTPS listener |
| `--https-addr` | `LIVESUBS_HTTPS_ADDR` | `--addr` host on `:8443` | HTTPS listen address. A loopback `--addr` keeps HTTPS on loopback too. |
| `--tls-cert`, `--tls-key` | `LIVESUBS_TLS_CERT`, `LIVESUBS_TLS_KEY` | | Your own certificate (PEM, chain included) and key |
| `--acme-email` | `LIVESUBS_ACME_EMAIL` | | Optional contact address for Let's Encrypt |

`auto` picks `provided` when `--tls-cert` is set, `acme` when `--public-base-url` is `https://` with a public domain (not an IP address, `localhost` or a LAN-only name such as `*.local`, `*.lan`, `*.internal`), and `local-ca` otherwise.

- **`local-ca`**: on first run the server creates a CA (ECDSA P-256, valid 10 years) and a server certificate signed by it (397 days), with SANs `localhost`, `127.0.0.1`, `::1`, every LAN IP, the hostname and `<hostname>.local` (plus the `--public-base-url` host, if any). They live in `<data dir>/tls/` (`ca.crt`, `server.crt`, and the `0600` keys `ca.key`, `server.key`). Every 5 minutes, and at startup, the server reissues the server certificate if the LAN IPs or hostname changed, or if it expires within 30 days. The swap needs no restart. The CA stays the same, so devices install it only once: download it from `GET /api/tls/ca.crt` (public) and compare its SHA-256 fingerprint with `caFingerprintSha256` in `GET /api/tls`. If you delete `ca.key`, a new CA is created and every device has to install it again.
- **`provided`**: the server serves your files and reloads them when they change on disk, so a certbot renewal needs no restart. If a reload fails, it keeps serving the last good pair.
- **`acme`**: certificates come from Let's Encrypt through `autocert`, cached in `<data dir>/tls/acme/`. The HTTP listener answers the HTTP-01 challenge and the HTTPS listener answers TLS-ALPN-01, so the domain must reach them on ports 80 or 443, for example with `--addr :80 --https-addr :443` or a port forward. If a reverse proxy terminates TLS in front of the server, use `--tls=disabled`.

`GET /api/tls` answers with `mode`, `httpsPort`, `sans`, `notAfter` and, for `local-ca`, `caFingerprintSha256`. Outside `local-ca`, `GET /api/tls/ca.crt` answers 404 `tls.no_local_ca`.

```sh
curl -o livesubs-ca.crt http://localhost:8080/api/tls/ca.crt
curl --cacert livesubs-ca.crt https://localhost:8443/healthz
```

## Sessions and realtime

Manage sessions at `/admin` → **New session**: give it a name (the address, or slug, is derived from it), languages and provider, then use **Start**, **Pause** and **Stop** on its card. **Links** shows the viewer, stage, overlay and capture links with a QR code for the audience. The capture link carries the session's ingest token, which the server only stores hashed: it's shown right after creating the session, or after **Make a new capture link** (which retires the old one). When the admin runs on `localhost`, the capture link stays on `localhost` too, so the browser allows the microphone. To rehearse without a microphone, **Play a file** on an idle session's card feeds a file under the server's data or testdata folder (or an http(s) link) at real-time speed, optionally from a start time and on a loop; **Stop file** stops it. Once the provider reports usage, the card also shows the audio minutes and the estimated cost in USD. The session dialog's **Audio input** picks what **Start** opens: the capture page (browser, the default) or [SRT from an encoder](#srt-ingest). The API takes the source per start and doesn't store it, so this choice is remembered by the admin's browser (`localStorage`, per session) and is locked while the session runs; SRT is offered only when the session has `urls.srtIngest` (ffmpeg with libsrt and `srt.enabled` in the settings), otherwise the dialog says which is missing. For an SRT session, **Links** shows the SRT address with the OBS steps instead of the capture link. While the server restarts a crashed provider or audio input, the card shows a warning chip with the attempt and the next try, and the provider readout counts the automatic restarts.

After the PIN, `/setup` walks through the hardware check (with **Check again** once whisper-server and Ollama are started, and an optional benchmark, which answers 422 `benchmark.runtime_unavailable` without them), local models (download progress is polled from `GET /api/models`; a stopped whisper download shows where it stopped and resumes; the Gemma models wait for Ollama), the Google key (saved and checked with Google; on a computer without an OS keychain, set `LIVESUBS_MASTER_KEY` first or the save answers 422 `secret.no_backend`) and the first session (only its name and address are sent, so the settings defaults and the default provider apply), then shows its capture link and QR code. Every step after the PIN can be skipped (**Finish later** goes to `/admin`). In the admin, **Ctrl+K** (⌘K on a Mac) opens the command palette. The other admin pages:

- `/admin/glossaries`: terms per glossary. **Paste CSV** takes comma, semicolon or tab separated rows; with a header, the columns are `term`, language codes (`es`, `en`, …), `note` and `keep`; without one, the order is term, the table's languages, then note. A new database comes with **Tech terms (EN/ES)** (id `tech-terms`, migration `0003`): common tech vocabulary with its preferred translation each way and a do-not-translate list of product names; edit it, or delete it for good. Deleting a glossary detaches it from the sessions and the settings default that use it; a running session keeps the copy it loaded until it stops.
- `/admin/overlays`: built-in and saved overlay presets with a live preview. A saved preset's overlay link is `/overlay/<session>?lang=es&preset=<preset id>`. `GET /api/overlay-presets` (public) lists the built-ins `classic`, `outline` and `lower-third` (`builtIn: true`, which answer 409 `overlay.builtin_read_only` to `PUT` and `DELETE`), then the saved presets by name. A new preset's id comes from its name (`Caja clásica` → `caja-clasica`, then `caja-clasica-2`).
- `/admin/recordings`: disk used by recordings and every recording (filterable by session), with its replay, the audio as M4A and delete. A recording still being written can't be deleted.
- `/admin/providers`: which provider new sessions use (from `GET /api/providers`; a key that couldn't be checked yet has no `defaultReason` and gets its own banner), and the write-only Google API key (saved and checked at once, with **Check key** to recheck) and OBS WebSocket password.
- `/admin/models`: the hardware check and benchmark from the setup, and the local model catalog with **Download** (progress follows `modelProgress` on `/ws/admin`, with polling while it's disconnected) and **Use for new sessions** on a ready model, which saves `providers.local.whisperModel` or `gemmaModel` (whisper-server still has to be restarted with that model).
- `/admin/settings` (`GET`/`PUT /api/settings`) and `/admin/tls` (certificate details and how to trust the local CA on each OS). Before the first save, `GET /api/settings` answers the defaults the server applies; a `PUT` fills left-out objects and blank strings with those defaults and answers 400 with per-field codes (`settings.invalid_language`, `settings.invalid_url`, `settings.out_of_range`, `settings.too_long`) in `fields`, keyed by path such as `providers.local.whisperUrl`, and `glossary.not_found` for a `defaultGlossaryId` that doesn't exist. The page also picks the default glossary and, in its own panel saved apart from the form, the write-only `srt_passphrase` secret (10–79 characters, checked by the page; `srt.passphraseSet` reports it). Saved settings apply without a restart: provider models and URLs at the next session start, recording bitrate and retention at the next recording, caption line limits at the next subtitle download, and **Network** (public URL and preferred interface) at once in `/api/network`, session links and QR codes. The saved public URL overrides `--public-base-url`, except for the HTTPS certificate (`acme` and the `local-ca` names), which reads the flag at startup.

Scripts can do the same over the API. Anything left out comes from the settings (target languages `[es, en]`, source language `auto`, recording on). `GET /api/languages` lists the supported languages: captions can be translated into `es`, `en`, `pt`, `fr`, `de`, `it`, `zh`, `ja` and `ko`, and the source language is `auto`, `en` or `es` (`canBeSource`). Any other language answers 400 `session.invalid_language`:

```sh
curl -H "Authorization: Bearer $LIVESUBS_ADMIN_TOKEN" -H "Content-Type: application/json" \
  -d '{"slug":"main","name":"Main stage"}' http://localhost:8080/api/sessions
```

The answer includes the session's `ingestToken` (shown only here and on rotation) and its `urls` for the viewer, stage, overlay and capture pages, built from the LAN address in `GET /api/network` (or `--public-base-url`). Open `urls.capture` + `?token=<ingestToken>` on the capture device: it picks the audio input (remembered per computer), resamples it to 16 kHz in an AudioWorklet and keeps the screen awake while sending. Browsers only allow the microphone on `https://` or `http://localhost`, so on a LAN IP over plain HTTP the page explains that instead; run it on the server computer via `localhost`. The audience opens `/s` (every session) or `urls.viewer`; `?lang=es` (or `source` for the original) preselects the caption track, and each device remembers its last track and text size. The stage screen (`urls.stage`) is for a projector: the session's stage style sets its preset, lines, second language and QR code, and the link can override each (`?preset=yellow-on-black&lines=2&dual=1&lang=en&lang2=source&qr=0`). Double-click or F toggles full screen; the cursor hides after 3 s. The overlay (`urls.overlay`) goes in OBS or vMix as a 1920 × 1080 browser source with a transparent background: `?lang=es&preset=classic` (or `outline`, `lower-third`), and any of `fontSize`, `fontWeight`, `color`, `outlineColor`, `outlineWidth`, `background` (a CSS colour or `transparent`), `position=top|bottom`, `align`, `margin` (sizes in px at 1080p), `maxLines` (1–4), `fadeAfter` (ms, 0 never fades) and `interim=0`. `PATCH` changes a session: its name, room, recording and styles any time; its languages, provider and glossary only while it isn't running. `DELETE` removes an idle session and its captions. The audience sees `/api/public/sessions`, which has no tokens or provider settings.

A running session is one pipeline: audio source → speech recognition → one translator per target language → the caption bus (`/ws/captions/{id}?lang=es&lang=source`) and, for final captions, the database. `POST /api/sessions/{id}/start` uses browser audio from `/ws/ingest/{id}?token=…` (the token comes from `POST /api/sessions/{id}/ingest-token`); `pause` stops feeding the provider without dropping the capture connection, `start` resumes, and `stop` waits for the provider to flush its last sentence.

- Translation (`internal/translate`) runs one queue per target language. Final captions are translated in order and never dropped; interim captions are debounced to the translator's pace (only the newest waits); a target equal to the detected source language shows the original text without a translation call. Each caption carries its segment's `sourceLang`; the session's detected language (in the status and the `/ws/captions` `state` messages) switches only after four words in the other language, in one final or in consecutive ones, so an "OK" or "sí" from the room doesn't flip the translation direction. A pinned source language overrides detection. Each request carries the last `translation.contextSentences` final sentences (default 3) as context. All text translators share one prompt template (`internal/translate/prompt.go`), which also renders the session glossary's terms and do-not-translate list (matched as whole words, ignoring case, plurals included). The glossary is loaded once when the session starts, for the ASR and the translators, so edits apply from the next start. Do-not-translate entries are enforced on the output: an entry the model re-cased gets its glossary spelling back, and a final that lost one is translated once more with the entries masked as `⟦n⟧` placeholders, keeping the first result if that fails too. With Gemini, finals stream in as interims while they're translated; the model is `providers.gemini.translationModel` (default `gemini-3.5-flash-lite`, with thinking at its minimal level) and the key is the `google_api_key` secret, both read at call time.
- A session with provider `gemini` runs on the [Gemini provider](#gemini-provider); provider `local` transcribes with whisper-server and translates with Gemma through Ollama ([Local AI provider](#local-ai-provider)).
- Provider `default` follows the [default-provider rule](#default-provider-rule): Gemini with a valid Google API key, local otherwise.
- Provider `mock` runs on the **mock provider**: it ignores the audio content and "hears" a scripted EN/ES talk at one word per 300 ms of audio, so any sound (or silence) from the capture page produces captions. It is never the default; name it on the session (development, demos, load tests).
- Caption times are seconds on the **session clock**. Each start continues the clock at least one second after the previous run, so exports never overlap. After a server restart the clock continues after the session's last caption and its last recording, whichever ends later, so a new run never falls inside an earlier recording's replay.
- `/ws/admin` streams `AdminEvent`s: the status of every session on connect, then every state change, plus each running session's status once a second.

### Default-provider rule

`provider: default` resolves when a session starts (and in each session's `effectiveProvider`), per AI-11 (`internal/provider/selector`):

| Google API key (`google_api_key`) | Default | `defaultReason` |
|---|---|---|
| Saved, and Google accepts it | `gemini` | `google_api_key_valid` |
| Not saved | `local` | `no_google_api_key` |
| Saved, Google rejects it (400/401/403) | `local` | `google_api_key_invalid` |
| Saved, Google unreachable and never checked | `local` | omitted; the gemini entry's `reasonCode` is `provider.key_unverified` |

The key is checked by listing one model (`GET /v1beta/models?pageSize=1`), which costs no tokens. A 429 counts as valid (the key works, it's over quota). A result is kept for 10 minutes per key value (only a hash of it is kept), and after that the old result is used while a background check refreshes it. If Google can't be reached, a key keeps its last result and is retried after 30 s. Saving a key checks it right away (unless the body has `"validate": false`); an invalid key is still stored and the response says `"valid": false`. `POST /api/secrets/google_api_key/validate` checks it on demand, with a repeat within 5 s returning the previous result. `SecretInfo.valid` is the last result for the current value. The OBS password has no check (422 `secret.validation_unsupported`).

When the default falls back to local because the key is rejected, removed or can't be checked, `/ws/admin` gets one `log` event at level `warn` with the code `provider.fallback_key_invalid`, `provider.fallback_key_removed` or `provider.fallback_key_unverified`. Having no key from the start is normal and doesn't warn. `GET /api/providers` gives the same decision for a lasting banner, plus whether each provider is available: gemini when the key is valid, local when whisper-server (`/health`) and Ollama (`/api/version`) answer at the URLs in the settings (`provider.whisper_unreachable`, `provider.ollama_unreachable`), and mock always. Local is still the default when its sidecars are down; the session then fails at start with `provider.unavailable`, unless [provider fallback](#provider-fallback) is on and Gemini is usable.

### Latency and cost

Every caption carries `latencyMs`: the time from when the end of its audio reached the server to when the caption was emitted, so it covers recognition on the `source` track and recognition plus translation on the others (a target equal to the source language passes through untranslated). `SessionStatus.latency` (in `GET /api/sessions/{id}/status` and on `/ws/admin`) has the p50, p95 and last value per track over the last 200 final captions of the current or last run.

`SessionStatus.usage` sums what a session used across its runs since the server started: audio seconds sent to the provider, the tokens the provider reports, and `estimatedCostUsd`. Only Gemini is priced; the local and mock providers cost 0. Speech recognition and translation run on different models, so each has its own prices. They are **estimates** from Google's published list prices (September 2026) and go stale, so set your own:

| Flag | Environment | Default (USD) |
|---|---|---|
| `--gemini-asr-audio-usd-per-min` | `LIVESUBS_GEMINI_ASR_AUDIO_USD_PER_MIN` | `0.005` per minute of audio (`gemini-3.5-transcribe-live`) |
| `--gemini-asr-output-usd-per-mtok` | `LIVESUBS_GEMINI_ASR_OUTPUT_USD_PER_MTOK` | `21.00` per million transcript tokens (`gemini-3.5-transcribe-live`) |
| `--gemini-translation-input-usd-per-mtok` | `LIVESUBS_GEMINI_TRANSLATION_INPUT_USD_PER_MTOK` | `0.30` per million input tokens (`gemini-3.5-flash-lite`) |
| `--gemini-translation-output-usd-per-mtok` | `LIVESUBS_GEMINI_TRANSLATION_OUTPUT_USD_PER_MTOK` | `2.50` per million output tokens (`gemini-3.5-flash-lite`) |

Audio is priced per minute rather than as tokens. Google puts the transcript output at about $0.004 per minute, so an hour of Gemini captions costs about $0.55 plus the translation tokens. Stats live in memory: a restart resets them.

## Subtitle files

Final captions of every track can be downloaded while a session runs or afterwards (`lang` is a target language or `source`):

| URL | What you get |
|---|---|
| `/api/public/sessions/main/subtitles?lang=es&format=vtt` | WebVTT download (`srt`, `txt` and `json` also work) |
| `…&live=true` | The same, served with `Cache-Control: no-store` for players and tools that poll it |
| `/api/public/sessions/main/captions?lang=es` | JSON pages of 200 captions; pass `nextCursor` back as `after` |

VTT and SRT cues hold at most 2 lines of 42 characters (settings `captions.maxLines` / `maxCharsPerLine`), break between sentences where they can, and stay on screen 5/6 s to 7 s. Captions an admin hid are left out.

### Live correction

An admin corrects or hides a final caption line with `PATCH /api/sessions/{id}/captions/{segmentId}?lang=es` and a body of `{"text": "…"}`, `{"hidden": true}` (or `false` to show it again), or both. Only final captions that are already stored can be patched (`404 caption.not_found` otherwise); a blank `text` or an empty body is `400 request.invalid`. The change:

- is stored with `edited: true`, so exports, `/captions` and replay show the new text and leave out hidden lines;
- goes to `/ws/captions` viewers of that track as a `caption` message with `edited: true` (and `hidden: true` when hidden), which replaces the line with the same `segmentId`; viewers who connect later get it in their `history`, without hidden lines;
- survives a late re-final from the provider for the same segment, which neither the database nor the bus lets overwrite an edited line;
- is not re-sent as a stream closed caption: YouTube and OBS captions already sent can't be taken back.

```sh
curl -X PATCH -H "Authorization: Bearer $LIVESUBS_ADMIN_TOKEN" -H 'Content-Type: application/json' \
  -d '{"text":"Hoy hablamos de observabilidad."}' \
  'http://localhost:8080/api/sessions/main/captions/s-000123?lang=es'
```

## Automatic recovery

A running session recovers on its own when part of the pipeline fails (SES-5). The state stays `live` throughout. `status.recovering` says what is restarting (`provider` or `source`), the attempt, the bound and the next retry, and `status.restarts` counts the restarts of the current run.

- **Provider stream**: if the speech-recognition stream ends while audio is still coming (a crash, or Gemini giving up on its own reconnects), the session opens a new one. The wait between attempts starts at 0.5 s and doubles up to 10 s, with 20 % jitter. Audio that arrives while no stream is open is dropped. Segment IDs of the new stream get the restart number (`r0-1-…`), so they never overwrite earlier captions.
- **Audio source**: a source whose stream ends with an error (an SRT or http(s) input dropping) is started again with the same backoff. Its new audio continues the session clock after the wall time that was lost. Local files aren't restarted, because they would play again from the beginning: the session goes to `error` with `source.failed`, as before.
- **Capture station**: when the browser's `/ws/ingest` connection drops and comes back, nothing restarts. The ingest source stays open, and the session stays live and waits for audio.
- **Gaps**: every stretch of audio that never reached the provider (a restart, or a capture reconnect) is logged as an `audio.gap` admin event with its length. The next captions carry `gapBeforeMs` on every track, so viewers and the dashboard can mark the gap. It's live only: the caption store doesn't keep it.
- **Giving up**: 5 failed attempts in a row (a restart that fails to start, or a stream or source that crashes again within 30 s of its restart) end the run in `error` with `provider.failed` or `source.failed`. A stream or source that ran longer than 30 s before failing starts counting from 1 again.

The admin log shows `provider.restarting` / `source.restarting` (warn) for each attempt and `provider.restarted` / `source.restarted` (info) when it worked. Local sidecars also retry within a stream: see [How the local provider uses whisper-server](#how-the-local-provider-uses-whisper-server) and [Gemma translation](#gemma-translation). With [provider fallback](#provider-fallback) on, a provider that keeps failing is replaced by the other one before the run gives up.

### Provider fallback

With `providers.fallback: true` in the settings (off by default: a switch can send audio meant to stay on the local box to Google, or start billing), a running session moves to the other provider when the one it runs on keeps failing (AI-8). Gemini falls back to local and local to Gemini. Mock never switches.

| Trigger | `reasonCode` |
|---|---|
| Google refuses a transcription or a translation for its quota (429, `RESOURCE_EXHAUSTED`) | `provider.quota_exhausted`, at once |
| Google rejects the key (401, 403, `API_KEY_INVALID`) | `provider.auth_failed`, at once |
| 5 provider errors (stream errors and failed translations) within a minute | `provider.errors_repeated` |
| 2 restarts of a crashed stream fail in a row (a crash loop, or whisper-server gone) | `provider.restarts_failed` |
| The provider doesn't start at all | `provider.unavailable`, before the session goes live |

The switch only happens when the other provider is usable by the [default-provider rule](#default-provider-rule): Gemini needs a key Google accepted, local needs whisper-server and Ollama to answer. Otherwise the session goes on with automatic recovery as before, and the other provider is checked again at most every 10 s.

On a switch the session stays `live`. The old speech stream is closed and a new one opens on the other provider, with segment IDs `r0-f1-…`. Translation uses the new provider from the next caption. Captions go on in the same tracks, and the audio the old stream hadn't finalized, plus any audio lost while switching, is a gap: an `audio.gap` event and `gapBeforeMs` on the next captions. `status.provider` shows the provider in use, and `status.fallback` has `from`, `to`, `at`, `reasonCode`, the `error` behind it and `switches`. `/ws/admin` gets a `provider.fallback` log event (warn, with `from`, `to` and `reason`), and the server logs `provider fallback`. Usage before the switch is priced for the old provider.

A run switches at most once every 10 minutes, so two failing providers don't flap. Stopping and starting the session again starts on its own provider.

## Gemini provider

A session with `provider: gemini` transcribes with the [Gemini Live API](https://ai.google.dev/gemini-api/docs/live). It needs a Google API key (Google AI Studio) in the `google_api_key` secret: paste it in Settings, or export `GEMINI_API_KEY` (or `GOOGLE_API_KEY`) before starting the server. The key and the model are read when a session starts, so changing them only affects the next start. Without a key the start fails with `provider.unavailable`. A valid key also makes Gemini the default provider ([default-provider rule](#default-provider-rule)).

- **Model**: settings `providers.gemini.liveModel`, default `gemini-3.5-transcribe-live`, Gemini's [streaming transcription model](https://ai.google.dev/gemini-api/docs/live-api/live-transcribe). The setup asks for text responses and input transcription in `SMART` mode, which drops filler words and false starts. The provider is built for this model; conversational Live models such as the 2.5 native-audio ones, now limited to past users, aren't supported.
- **Captions**: audio goes out as 16 kHz mono PCM in 100 ms chunks, and the server's voice activity detection splits the speech into utterances. Each utterance is one caption: the server's interim transcription replaces its text while the speaker talks, and the server's final transcription, sent when the speaker pauses, is its final. A final with several sentences becomes one caption per sentence. As a safety net, an interim becomes final 1 s after the end of a turn with no final, after 5 s without updates, or when the audio ends. A server final that arrives after such a flush is dropped rather than shown twice. Live transcription has no timestamps, so caption times are estimates on the session clock: a caption ends where the audio was when its text last changed.
- **Custom vocabulary**: the session glossary's terms and do-not-translate entries, up to 100, go to the model as `customVocabulary`, so it spells names and jargon the way the glossary does. The API takes up to 1,000, but Google reports the best results at 100 or fewer.
- **Language**: a pinned `en`/`es` is sent as the language hint (`en-US`, `es-419`; the API lists only regional codes) and is used for every caption. With `auto`, no hint is sent and the model identifies the language itself. Each caption takes the language code the API reports for the utterance (the SDK has the field; Google doesn't document it for Live, so it may be missing), else a guess from common English and Spanish words, else the previous caption's. Auto doesn't limit the model to English and Spanish: another language reported by the API is ignored and the fallback decides.
- **10-minute limit**: a Live transcription session streams for at most 10 minutes, and the model supports neither session resumption nor context window compression. After 9 minutes (or when the server sends `GoAway`), the provider opens the next connection while the current one keeps working, then switches the audio at the next pause, when no utterance is open. If there's no pause within 45 s it switches anyway, mid-utterance. Every chunk goes to exactly one connection, so no audio is lost or transcribed twice. The old connection gets `audioStreamEnd` and has 4 s to deliver its last final. The log shows `gemini live connection rotated`.
- **Drops**: a connection that drops unexpectedly is reopened with backoff (0.5 s doubling to 10 s, 8 tries). Meanwhile up to 15 s of audio is kept and sent on reconnect; anything older is logged as an audio gap (`gemini live reconnected; audio was lost`, with the session-clock range). Each drop also shows as a `provider.error` on the session. A drop during a rotation switches straight to the already-open next connection, with no error.
- **Usage**: the seconds of audio sent go to `usage` as audio, and the text tokens the API reports as output tokens. Audio isn't counted as input tokens.

`go test -tags gemini -run 'Integration|Live' -v ./internal/provider/gemini/` with `GEMINI_API_KEY` (or `GOOGLE_API_KEY`) set streams the EN and ES fixtures to the real API in real time and logs the transcript, the detected languages and the latency, translates a caption each way and checks the key validator with the real key and a made-up one (`GEMINI_LIVE_MODEL` and `GEMINI_TRANSLATION_MODEL` override the models). Without the tag or the key it's skipped, so CI never calls Google.

Measured on 2026-09-24 from Buenos Aires with the ~10 s fixtures:

- **Transcription** (`gemini-3.5-transcribe-live`, `auto`): both fixtures transcribed without errors and in the right language. The first interim came about 0.9–1.5 s after the audio started, and interims then kept pace with the audio. A final arrives about 1.3–1.5 s after the speaker pauses. A continuous 10 s utterance is one final at the end, split into sentences. The API reported no token counts, so the cost estimate comes from audio minutes only.
- **Translation** (`gemini-3.5-flash-lite`): about 0.7–0.8 s per caption, es→en and en→es.

## Recordings

A session with `recordingEnabled` (new sessions take `settings.recording.enabledByDefault`) records the same 16 kHz audio it sends to the provider. ffmpeg encodes it to AAC-LC in fragmented MP4 at `settings.recording.bitrateKbps` (32, 48 or 64) into `<data dir>/recordings/<session>/<recording id>.m4a`. A new file starts with each run of the session and every 30 minutes, and after a pause longer than 5 minutes; shorter pauses are recorded as silence. Each ~1 s fragment is written to disk as it's done, so after a crash or `kill -9` the file plays up to its last second, and on the next start the server marks such recordings `complete` (or `failed` if nothing playable was written). If ffmpeg is missing, or the disk can't keep up, the session runs on without recording (the log says why).

Each recording has `offsetSec`, where it starts on the session clock: a session caption at `start` plays at `start - offsetSec` in the file. Add `recordingId=<id>` to the captions or subtitles URLs above to get just that recording's captions, already shifted to its timeline.

| URL | What you get |
|---|---|
| `/api/recordings?sessionId=main` | The session's recordings, newest first (`status` is `recording`, `complete` or `failed`) |
| `/api/recordings/{id}/audio` | The audio (`audio/mp4`), with HTTP Range so players can seek |
| `/api/recordings/usage` (admin) | Bytes used by recordings and free on their disk |
| `DELETE /api/recordings/{id}` (admin) | Deletes the recording and its file; one still being written stops |

Recordings older than `settings.recording.retentionDays` (default 30, counted from when they ended; `0` keeps them forever) are deleted at startup and then hourly. Deleting a session keeps its recordings.

## Logs and metrics

Every HTTP request is logged once it finishes, as `http request` with `method`, `path` (never the query string, which can carry an ingest token), `route` (the matched pattern, such as `/api/sessions/{sessionId}`), `status`, `bytes`, `duration_ms`, `remote` and, on session routes, `session`. A WebSocket is logged when it closes, with `websocket=true`. API and WebSocket calls log at `info` and server errors at `warn`; `/healthz`, `/metrics` and the web app's files log at `debug` (`--log-level debug` or `task dev:server`). `--log-format json` (`LIVESUBS_LOG_FORMAT=json`) gives one JSON object per line for a log collector.

`--metrics` (`LIVESUBS_METRICS=true`) serves Prometheus metrics at `GET /metrics`. It's off by default (404) and, when on, admin-only like the admin API: send the `LIVESUBS_ADMIN_TOKEN` bearer token (or be logged in).

```sh
curl -H "Authorization: Bearer $LIVESUBS_ADMIN_TOKEN" http://localhost:8080/metrics
```

```yaml
# prometheus.yml
scrape_configs:
  - job_name: livesubs
    metrics_path: /metrics
    authorization: { credentials_file: /etc/prometheus/livesubs-token }
    static_configs: [{ targets: ['livesubs.lan:8080'] }]
```

| Metric | Type | Labels | What |
|---|---|---|---|
| `livesubs_sessions` | gauge | `state` | Sessions by runtime state (`idle`, `starting`, `live`, `paused`, `stopping`, `error`) |
| `livesubs_session_viewers` | gauge | `session` | Caption viewers of each running or watched session |
| `livesubs_ws_clients` | gauge | `endpoint` | Open WebSockets on `/ws/captions`, `/ws/ingest` and `/ws/admin` |
| `livesubs_caption_latency_seconds` | histogram | `provider`, `track` | Latency of final captions, as in [Latency and cost](#latency-and-cost) |
| `livesubs_session_errors_total` | counter | `provider`, `code` | Errors reported by running sessions: `provider.error`, `provider.unavailable`, `translation.failed`, `source.*` or a provider's own code |
| `livesubs_recordings_bytes`, `livesubs_recordings`, `livesubs_recordings_free_bytes` | gauge | | Recordings on disk: bytes, count and free space, as in `/api/recordings/usage` |
| `livesubs_http_requests_total` | counter | `route`, `method`, `code` | HTTP requests (WebSocket upgrades answer `101`) |
| `livesubs_http_request_duration_seconds` | histogram | `route` | HTTP request duration, WebSockets excluded |

Counters and histograms start at zero when the server starts. The gauges are read from the running services at each scrape.

## Stream closed captions (YouTube)

A session can send the final captions of one track to a YouTube live stream as closed captions that viewers switch on in the player (CC-1). It works the same whether the stream comes from OBS or vMix, because the captions go straight to YouTube over HTTP, not through the video.

1. In YouTube Live Control Room, open the stream's settings → Closed captions, pick **POST captions to URL** and copy the ingestion URL. Set a broadcast delay of 30 to 60 s, so captions arrive before the video they belong to.
2. Save the URL for the session: `PUT /api/sessions/main/stream-captions/youtube-url` with `{"value":"http://upload.youtube.com/closedcaption?cid=…"}`, or export `LIVESUBS_SECRET_SESSION_MAIN_YOUTUBE_URL` before starting the server. It's a secret (keychain or encrypted file): the API only ever shows its last 4 characters, and it's masked in logs. `DELETE` on the same path removes it; deleting the session removes it too.
3. In the session, set `streamCaptions`: `enabled: true`, `track` (a target language or `source`, default `en`; YouTube takes one track) and `maxCharsPerLine` (default 32).
4. `POST /api/sessions/main/stream-captions/test` (optional `{"text":"…"}`) sends a caption right away, running or not, and answers with the delivery result. It answers 422 `streamcc.no_url` when no URL is saved.

While the session runs, each final caption of the track is wrapped into lines of `maxCharsPerLine`, two lines per cue, and sent as one POST: a UTC timestamp line (`YYYY-MM-DDTHH:MM:SS.mmm`) before each cue, lines joined with `<br>`, and an increasing `seq` added to the URL. The timestamp is when the words were spoken, estimated from the caption's latency and duration, and moved onto YouTube's clock using the time YouTube sends back with each answer (`clockOffsetMs`, local minus YouTube). Corrections of a caption already sent aren't sent again.

Failed POSTs (network errors, HTTP 5xx, 408, 429) are retried with the same `seq`, with backoff from 0.5 s doubling to 10 s. Other 4xx answers (`streamcc.rejected`) aren't retried. Up to 64 captions wait in a queue per session; when it's full the oldest is dropped. A caption whose speech ended more than 60 s ago is dropped instead of being sent late, so after an outage the stream doesn't replay old lines; the dashboard gets a `streamcc.dropped` log with the count. When the session stops, what's queued gets up to 10 s to go out.

The configuration and the URL are read when the session starts; a URL saved or removed while it runs takes effect at the next caption. `streamCaptions` in the session status (and on `/ws/admin`, as `sessionStatus` and `streamCaptionStatus` events) shows `state` (`disabled`, `idle`, `ok`, `retrying`, `error`), `lastSeq`, `lastSentAt`, `clockOffsetMs` and the last `error`.

### OBS (`SendStreamCaption`)

With `target: obs_websocket` the captions go to OBS instead, through obs-websocket v5 (`SendStreamCaption`), and OBS encodes them as CEA-608 into its stream (YouTube and Twitch show them; Vimeo doesn't). Use it when the stream comes from OBS and the platform has no caption ingestion URL.

- Turn on OBS's websocket server (Tools → WebSocket Server Settings). Its address is the setting `obs.websocketUrl` (default `ws://127.0.0.1:4455`); if it asks for a password, save it as the `obs_websocket_password` secret (Settings, or `LIVESUBS_SECRET_OBS_WEBSOCKET_PASSWORD`).
- Lines are at most 32 characters (the 608 limit, whatever `maxCharsPerLine` says), two per caption. Each caption stays up for the time its words took, at least 1.5 s and at most 4 s, before the next replaces it.
- The connection opens with the first caption. If OBS closes or restarts, the caption is retried with the same backoff as YouTube and the connection is reopened; the lines of a caption already shown aren't sent again. A wrong password shows `streamcc.obs_auth_failed`, and OBS refuses captions while it isn't streaming (`streamcc.obs_rejected`).
- `POST …/stream-captions/test` sends the test caption to OBS. `lastSeq` counts the captions sent; there's no clock offset.

## Local AI provider

The local provider needs two sidecars: **whisper-server** (whisper.cpp) for speech recognition and **Ollama** running Gemma for translation.

| Service | Port | Default model |
|---|---|---|
| whisper-server | `8178` | `ggml-large-v3-turbo` (multilingual; smaller: `medium`, `small`) |
| Ollama | `11434` | `gemma3:4b` (smaller: `gemma3:1b`) |

`task models:pull` downloads the whisper model into `./models` (checked against Hugging Face's SHA-256) and pulls Gemma through Ollama. Override with `WHISPER_MODEL=small` or `GEMMA_MODEL=gemma3:1b`. English-only whisper models (`*.en`) are refused, because Spanish needs a multilingual model. The server can also download them itself (see [Model downloads](#model-downloads)).

### macOS (Apple Silicon): native, with Metal

Containers can't use the Apple GPU, and the whisper.cpp image is amd64-only, so run both natively:

```sh
brew install whisper.cpp ollama
task models:pull
whisper-server --host 127.0.0.1 --port 8178 --model models/ggml-large-v3-turbo.bin
ollama serve        # skip if the Ollama app is already running
```

### Linux / Windows: Docker

```sh
task models:pull SKIP_GEMMA=1   # whisper model into ./models
task dev:ai                     # whisper-server + ollama (CPU)
task dev:ai GPU=1               # NVIDIA GPU (needs the NVIDIA Container Toolkit)
task models:pull SKIP_WHISPER=1 # Gemma, pulled inside the ollama container
task dev:ai:down
```

`task dev:ai SERVICES=whisper` starts only one service, for example when Ollama already runs natively.

### Gemma translation

The local translator (`internal/provider/local/gemma`) talks to Ollama's `/api/chat` with streaming, using `providers.local.ollamaUrl` and `providers.local.gemmaModel` from the settings (read at call time). When a session starts it loads the model with an empty chat, and every request sends `keep_alive: 30m`, so the first caption doesn't wait for a model load and a pause doesn't unload it. It runs at most 2 requests at once per model and sends `think: false`, so thinking models such as Gemma 4 answer straight away. A request that fails before any output (Ollama unreachable, or an HTTP 5xx while it restarts or reloads the model) is retried twice, after about 250 ms and 500 ms, within the 15 s translation timeout. A caption that still fails is missing from that track (`translation.failed`), and the next ones are translated as usual. Measure it on your machine with:

```sh
GEMMA_MODEL=gemma3:4b go test -tags ollama -run TestLiveLatency -v ./internal/provider/local/gemma/
```

On an Apple M5 Pro (48 GB, Ollama 0.34, `gemma4:26b`), loading the model took about 7 s. After that, one caption took 270–440 ms (median 330 ms, first token after about 190 ms) for both es→en and en→es.

### How the local provider uses whisper-server

The server reads `providers.local.whisperUrl` and `whisperModel` from the settings when a session starts (defaults `http://127.0.0.1:8178` and `large-v3-turbo`). whisper-server loads its model at launch, so `whisperModel` has to name the model it runs. At start the provider checks `GET /health` and sends one second of silence with `language=es`. If the server doesn't answer or is still loading its model, the session fails with `provider.unavailable`. If the model is English-only, the session fails with `provider.model_english_only`. That can come from the name (`*.en`) or from the probe: an English-only model answers "english" even when asked for Spanish.

whisper-server transcribes files, not streams, so the provider (`internal/provider/local/whisper`) cuts the audio into utterances with an energy VAD. The speech threshold is -55 dBFS, or 12 dB above the tracked noise floor, whichever is higher:

- While someone speaks, the utterance so far is sent to `POST /inference` (WAV, `response_format=verbose_json`) after every 1 s of new audio, for **interim** text. When the server falls behind, interims are skipped; finals never are.
- A 600 ms pause, or 12 s without one, commits the utterance as **final** under the same segment ID. At 12 s the cut lands on the quietest moment of the last 3 s.
- If whisper-server stops answering mid-session (a restart, or 503 while it reloads its model), the stream keeps going. A final is retried with backoff, 300 ms doubling to 5 s, up to 6 times (about 15 s), and later audio waits its turn. Failed interims are skipped. If the final still fails, the interim text shown so far becomes final and the session shows a `provider.error`.
- Silence and short clicks never reach the server. Text that whisper invents on noise is dropped: `[Música]`, `[BLANK_AUDIO]`, "Thanks for watching", "Subtítulos realizados por la comunidad de Amara.org", and phrases looping three or more times.
- With source language `auto`, the provider keeps the likelier of `en` and `es` from `language_probabilities` for each utterance. If whisper picked a third language, the utterance is transcribed again in that choice. A pinned source language skips detection. Very short utterances ("OK", "sí") can be misdetected; the session keeps its language until four words in the other one (see Translation above). Glossary terms go in whisper's `prompt`.

Final captions arrive about 600 ms (the pause) plus one inference after the speaker stops. Check against a running server (the build tag keeps it out of CI):

```sh
WHISPER_URL=http://127.0.0.1:8178 WHISPER_MODEL=large-v3-turbo \
  go test -tags whisper -run Real -v ./internal/provider/local/whisper/
```

On an Apple M5 Pro (Metal), fed in real time:

| Model | ES fixture WER | EN fixture WER | Final after the audio ends | Language detected |
|---|---|---|---|---|
| `large-v3-turbo-q5_0` | 0 % | 10 % ("observ ability") | 0.5 s pinned, 0.7 s `auto` | es / en, correct |
| `tiny` | 26 % ("encovernetes", "locs", "trases") | 30 % | < 0.1 s | es / en, correct |

The fixtures are about 8 s of synthetic speech each. Measure WER on the real talks (`task audio:fetch`) before relying on these numbers for an event.

### Hardware check and benchmark

At startup the server logs a hardware check, and `GET /api/system/hardware` (admin) returns it for the setup wizard: OS, CPU, RAM, GPUs, whether whisper-server, Ollama and ffmpeg answer, and the recommended models (`internal/hwcheck`). GPUs are Metal on Apple Silicon, CUDA from `nvidia-smi` and Vulkan from `vulkaninfo --summary`. Missing tools just leave their part empty. The recommendation comes from a table in `internal/hwcheck/recommend.go`:

| Hardware | whisper | Gemma | Real time likely |
|---|---|---|---|
| GPU with 8 GB or more for models (a discrete GPU's own memory, or half the RAM on Apple Silicon and integrated GPUs) | `large-v3-turbo` | `gemma3:4b` | yes |
| GPU with 4 GB or more | `large-v3-turbo` | `gemma3:1b` | yes |
| smaller GPU, or CPU with 8+ cores and 16 GB RAM | `small` | `gemma3:1b` | yes |
| anything else | `small` | `gemma3:1b` | no |

`POST /api/system/benchmark` (admin) runs a bundled 37.6 s clip (the EN and ES fixtures, alternating) through the configured local provider: whisper-server transcribes it, then Gemma translates every final into the other language. The real-time factor is the processing time divided by the clip length. Model loading isn't counted. The audio goes in as fast as whisper takes it, so the benchmark measures finals only. A live session also transcribes interims, so the result counts as `ok` only up to 0.8. Above that, the dashboard should suggest smaller models or a Google API key. The last result is kept in `<data dir>/benchmark.json` and replaces the table's "real time likely" guess. One benchmark runs at a time (409 `benchmark.running`). If whisper-server or Ollama is down, the answer is 422 `benchmark.runtime_unavailable`.

`GET /healthz` lists `database`, `ffmpeg`, `whisper` and `ollama` under `checks`. It reports `degraded` only when the database or ffmpeg fails, since the sidecars matter only to local-provider sessions. `GET /api/system/info` sets `features.srtIngest` when `ffmpeg -protocols` lists `srt` as an input (ffmpeg built with libsrt; Homebrew's default build has no libsrt). Other code can reuse the same probe through `ffmpeg.Probe` or the caching `ffmpeg.Prober`.

On an Apple M5 Pro (48 GB, Metal) with whisper.cpp 1.9.4 (`large-v3-turbo`) and Ollama 0.34.2 (`gemma3:4b`), four runs gave a real-time factor of 0.14 (about 3.4 s of speech recognition and 1.8 s of translation for the 37.6 s clip).

### Model downloads

`GET /api/models` (admin) lists the catalog (`internal/models/catalog.go`): whisper `large-v3-turbo`, `medium` and `small` (multilingual GGML files from Hugging Face, each with its size and SHA-256), and `gemma3:4b` and `gemma3:1b` through Ollama. `recommended` follows the hardware check. `POST /api/models/{id}/download` starts the download in the background and answers 202. Progress goes out as `modelProgress` events on `/ws/admin`, at most one per percent or per second, and `GET /api/models` shows it too.

- **whisper**: the file goes to `<models dir>/ggml-<name>.bin.part` and is renamed to `ggml-<name>.bin` once its SHA-256 matches. An interrupted download resumes with an HTTP `Range` request, both on the automatic retries (3 attempts) and on the next `POST`. A checksum mismatch deletes the file (`model.checksum_mismatch`). The models directory is `./models` by default (`--models-dir` / `LIVESUBS_MODELS_DIR`). That is the directory `task models:pull` fills and `compose.dev.yaml` mounts into whisper-server. whisper-server loads its model at launch, so restart it with `--model <models dir>/ggml-<name>.bin` and set `providers.local.whisperModel` to match.
- **Gemma**: the server asks Ollama at `providers.local.ollamaUrl` to pull the tag (`POST /api/pull`). Ollama resumes partial layers and checks their digests itself. Without Ollama the `POST` answers 409 `model.ollama_unreachable`.

## Test audio

- **Committed fixtures**: `testdata/audio/fixtures/{en,es}.wav`, about 8 s each, 16 kHz mono s16le. They're synthetic (macOS text-to-speech, `scripts/make-fixtures.sh`) so CI can use them without third-party rights. whisper `tiny` transcribes both and detects the right language.
- **Real talks**: `task audio:fetch` downloads one English and one Spanish Nerdearla talk from YouTube and trims each to a 10-minute 16 kHz mono clip in `testdata/audio/{en,es}.m4a` (gitignored). Change the talks or the window with `EN_URL`, `ES_URL`, `START` and `DURATION`.

### Feeding a file to a session

`task demo:file SESSION=main FILE=testdata/audio/en.m4a` plays a file into a session in real time, as if someone were speaking (`LOOP=true` repeats it, `PORT=18080` or `URL=…` picks the server). It calls `POST /api/sessions/{id}/sources/file` with the admin bearer token, so export the same `LIVESUBS_ADMIN_TOKEN` in the server's environment and in your shell. The session must be idle; `DELETE` on the same URL stops it.

The server decodes the file with ffmpeg (`--ffmpeg` / `LIVESUBS_FFMPEG` if it isn't in `PATH`). Only files under the data directory and `./testdata`, or `http(s)` URLs, are accepted; relative paths are resolved from the server's working directory.

## SRT ingest

A session can take its audio from an SRT sender (vMix, OBS, a hardware encoder) instead of browser capture (AUD-5). Start it with `POST /api/sessions/{id}/start` and `{"source":"srt"}`: the server opens an ffmpeg SRT listener and the session goes live, waiting for a sender. The sender pushes MPEG-TS with any audio codec ffmpeg decodes (AAC, MP2, Opus, AC-3; video is ignored) to the session's `urls.srtIngest`, e.g. `srt://192.168.1.20:9000?streamid=main`. When the sender disconnects the listener reopens, so the session stays live and the next connection continues it (`audio.lastGapMs` shows the gap). Stop the session to close the listener.

- **One UDP port per session.** ffmpeg's listener takes one caller and can't route by `streamid`, so each session gets its own port, counting up from `settings.srt.port` (default 9000) in the order sessions are listed or started, at most 100. A session keeps its port until the server restarts or the session is deleted. `streamid=<session>` is in the URL but not checked. Open the UDP ports on the event LAN's firewall (9000–9009 covers ten SRT sessions).
- **Latency** is `settings.srt.latencyMs` (default 200 ms). Senders usually negotiate the larger of theirs and ours.
- **Passphrase**: store `srt_passphrase` (10–79 characters) with `PUT /api/secrets/srt_passphrase`, and set the same passphrase on the sender. Without it only unencrypted senders are accepted. A sender with the wrong passphrase is refused and logged as `srt caller rejected`; the passphrase itself is passed to ffmpeg as an option and never logged (it is visible to local users in the process list, like any command-line argument).
- **Status**: `SessionStatus.srt` has `connected` and `bitrateKbps` (the received audio stream, measured from a stream copy of it). ffmpeg doesn't expose libsrt's RTT and packet-loss counters, so those fields stay empty.
- **libsrt**: ffmpeg must be built with it. Check with `ffmpeg -hide_banner -protocols | grep -w srt`. Most Linux packages (Debian/Ubuntu `apt install ffmpeg`) have it; Homebrew's default `ffmpeg` formula doesn't, so on macOS use the `homebrew-ffmpeg/ffmpeg` tap (`brew install homebrew-ffmpeg/ffmpeg/ffmpeg --with-srt`) or Docker. Without libsrt, `srtIngest` is absent and starting with SRT answers `source.srt_unavailable`.

Try it with ffmpeg as the sender (add `&passphrase=…`, URL-encoded, if one is set):

```sh
ffmpeg -re -i testdata/audio/fixtures/en.wav -c:a aac -f mpegts 'srt://127.0.0.1:9000?streamid=main'
```

**OBS**: Settings → Stream → Service *Custom…*, Server `srt://<server-ip>:9000?streamid=main` (append `&passphrase=…` if one is set; `&latency=200000` sets the sender's latency in microseconds), Stream Key empty. OBS sends MPEG-TS over SRT by itself, and its default AAC audio works. Start the session, then Start Streaming. **vMix**: add an SRT output in *Caller* mode to the same host and port, with the same passphrase and latency.

The SRT tests (`internal/audio/ffmpeg/srt_e2e_test.go`, `internal/api/handlers/srt_test.go`) push the fixture through a real connection and skip without libsrt. To run them on a Mac without it, use Docker: `docker run --rm -v "$PWD":/src -w /src golang:1.26-trixie sh -c 'apt-get update -qq && apt-get install -y -qq ffmpeg >/dev/null && go test -run SRT ./internal/audio/ffmpeg/ ./internal/api/handlers/'`.

## Troubleshooting

- **`localhost:8080` answers with another app's 404.** Another process is listening on `127.0.0.1:8080`, and the Go server binds `*:8080` alongside it without an error. Check with `lsof -nP -iTCP:8080 -sTCP:LISTEN`, then stop it or use `task dev PORT=18080`.
