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

Manage sessions at `/admin` → **New session**: give it a name (the address, or slug, is derived from it), languages and provider, then use **Start**, **Pause** and **Stop** on its card. **Links** shows the viewer, stage, overlay and capture links with a QR code for the audience. The capture link carries the session's ingest token, which the server only stores hashed: it's shown right after creating the session, or after **Make a new capture link** (which retires the old one). When the admin runs on `localhost`, the capture link stays on `localhost` too, so the browser allows the microphone.

After the PIN, `/setup` walks through the hardware check, local models, the Google key and the first session; every step after the PIN can be skipped (**Finish later** goes to `/admin`). In the admin, **Ctrl+K** (⌘K on a Mac) opens the command palette. The other admin pages:

- `/admin/glossaries`: terms per glossary. **Paste CSV** takes comma, semicolon or tab separated rows; with a header, the columns are `term`, language codes (`es`, `en`, …), `note` and `keep`; without one, the order is term, the table's languages, then note.
- `/admin/overlays`: built-in and saved overlay presets with a live preview. A saved preset's overlay link is `/overlay/<session>?lang=es&preset=<preset id>`.
- `/admin/providers`: which provider new sessions use, and the write-only Google API key.
- `/admin/settings` and `/admin/tls` (certificate details and how to trust the local CA on each OS).

Scripts can do the same over the API. Anything left out comes from the settings (target languages `[es, en]`, source language `auto`, recording on). `GET /api/languages` lists the supported languages: captions can be translated into `es`, `en`, `pt`, `fr`, `de`, `it`, `zh`, `ja` and `ko`, and the source language is `auto`, `en` or `es` (`canBeSource`). Any other language answers 400 `session.invalid_language`:

```sh
curl -H "Authorization: Bearer $LIVESUBS_ADMIN_TOKEN" -H "Content-Type: application/json" \
  -d '{"slug":"main","name":"Main stage"}' http://localhost:8080/api/sessions
```

The answer includes the session's `ingestToken` (shown only here and on rotation) and its `urls` for the viewer, stage, overlay and capture pages, built from the LAN address in `GET /api/network` (or `--public-base-url`). Open `urls.capture` + `?token=<ingestToken>` on the capture device: it picks the audio input (remembered per computer), resamples it to 16 kHz in an AudioWorklet and keeps the screen awake while sending. Browsers only allow the microphone on `https://` or `http://localhost`, so on a LAN IP over plain HTTP the page explains that instead; run it on the server computer via `localhost`. The audience opens `/s` (every session) or `urls.viewer`; `?lang=es` (or `source` for the original) preselects the caption track, and each device remembers its last track and text size. The stage screen (`urls.stage`) is for a projector: the session's stage style sets its preset, lines, second language and QR code, and the link can override each (`?preset=yellow-on-black&lines=2&dual=1&lang=en&lang2=source&qr=0`). Double-click or F toggles full screen; the cursor hides after 3 s. The overlay (`urls.overlay`) goes in OBS or vMix as a 1920 × 1080 browser source with a transparent background: `?lang=es&preset=classic` (or `outline`, `lower-third`), and any of `fontSize`, `fontWeight`, `color`, `outlineColor`, `outlineWidth`, `background` (a CSS colour or `transparent`), `position=top|bottom`, `align`, `margin` (sizes in px at 1080p), `maxLines` (1–4), `fadeAfter` (ms, 0 never fades) and `interim=0`. `PATCH` changes a session: its name, room, recording and styles any time; its languages, provider and glossary only while it isn't running. `DELETE` removes an idle session and its captions. The audience sees `/api/public/sessions`, which has no tokens or provider settings.

A running session is one pipeline: audio source → speech recognition → one translator per target language → the caption bus (`/ws/captions/{id}?lang=es&lang=source`) and, for final captions, the database. `POST /api/sessions/{id}/start` uses browser audio from `/ws/ingest/{id}?token=…` (the token comes from `POST /api/sessions/{id}/ingest-token`); `pause` stops feeding the provider without dropping the capture connection, `start` resumes, and `stop` waits for the provider to flush its last sentence.

- Translation (`internal/translate`) runs one queue per target language. Final captions are translated in order and never dropped; interim captions are debounced to the translator's pace (only the newest waits); a target equal to the detected source language shows the original text without a translation call. Each request carries the last `translation.contextSentences` final sentences (default 3) as context. All text translators share one prompt template (`internal/translate/prompt.go`), which also renders the session glossary's terms and do-not-translate list. With Gemini, finals stream in as interims while they're translated; the model is `providers.gemini.translationModel` (default `gemini-3.5-flash-lite`, with thinking at its minimal level) and the key is the `google_api_key` secret, both read at call time.
- A session with provider `gemini` runs on the [Gemini provider](#gemini-provider); provider `local` transcribes with whisper-server and translates with Gemma through Ollama ([Local AI provider](#local-ai-provider)).
- Provider `mock`, and `default` until the default-provider rule (P2-07), runs on the **mock provider**: it ignores the audio content and "hears" a scripted EN/ES talk at one word per 300 ms of audio, so any sound (or silence) from the capture page produces captions.
- Caption times are seconds on the **session clock**. Each start continues the clock at least one second after the previous run, so exports never overlap.
- `/ws/admin` streams `AdminEvent`s: the status of every session on connect, then every state change, plus each running session's status once a second.

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

## Gemini provider

A session with `provider: gemini` transcribes with the [Gemini Live API](https://ai.google.dev/gemini-api/docs/live). It needs a Google API key (Google AI Studio) in the `google_api_key` secret: paste it in Settings, or export `GEMINI_API_KEY` (or `GOOGLE_API_KEY`) before starting the server. The key and the model are read when a session starts, so changing them only affects the next start. Without a key the start fails with `provider.unavailable`.

- **Model**: settings `providers.gemini.liveModel`, default `gemini-3.5-transcribe-live`, Gemini's [streaming transcription model](https://ai.google.dev/gemini-api/docs/live-api/live-transcribe). The setup asks for text responses and input transcription in `SMART` mode, which drops filler words and false starts. The provider is built for this model; conversational Live models such as the 2.5 native-audio ones, now limited to past users, aren't supported.
- **Captions**: audio goes out as 16 kHz mono PCM in 100 ms chunks, and the server's voice activity detection splits the speech into utterances. Each utterance is one caption: the server's interim transcription replaces its text while the speaker talks, and the server's final transcription, sent when the speaker pauses, is its final. A final with several sentences becomes one caption per sentence. As a safety net, an interim becomes final 1 s after the end of a turn with no final, after 5 s without updates, or when the audio ends. A server final that arrives after such a flush is dropped rather than shown twice. Live transcription has no timestamps, so caption times are estimates on the session clock: a caption ends where the audio was when its text last changed.
- **Custom vocabulary**: the session glossary's terms and do-not-translate entries, up to 100, go to the model as `customVocabulary`, so it spells names and jargon the way the glossary does. The API takes up to 1,000, but Google reports the best results at 100 or fewer.
- **Language**: a pinned `en`/`es` is sent as the language hint (`en-US`, `es-419`; the API lists only regional codes) and is used for every caption. With `auto`, no hint is sent and the model identifies the language itself. Each caption takes the language code the API reports for the utterance (the SDK has the field; Google doesn't document it for Live, so it may be missing), else a guess from common English and Spanish words, else the previous caption's. Auto doesn't limit the model to English and Spanish: another language reported by the API is ignored and the fallback decides.
- **10-minute limit**: a Live transcription session streams for at most 10 minutes, and the model supports neither session resumption nor context window compression. After 9 minutes (or when the server sends `GoAway`), the provider opens the next connection while the current one keeps working, then switches the audio at the next pause, when no utterance is open. If there's no pause within 45 s it switches anyway, mid-utterance. Every chunk goes to exactly one connection, so no audio is lost or transcribed twice. The old connection gets `audioStreamEnd` and has 4 s to deliver its last final. The log shows `gemini live connection rotated`.
- **Drops**: a connection that drops unexpectedly is reopened with backoff (0.5 s doubling to 10 s, 8 tries). Meanwhile up to 15 s of audio is kept and sent on reconnect; anything older is logged as an audio gap (`gemini live reconnected; audio was lost`, with the session-clock range). Each drop also shows as a `provider.error` on the session. A drop during a rotation switches straight to the already-open next connection, with no error.
- **Usage**: the seconds of audio sent go to `usage` as audio, and the text tokens the API reports as output tokens. Audio isn't counted as input tokens.

`go test -tags gemini -run 'Integration|Live' -v ./internal/provider/gemini/` with `GEMINI_API_KEY` (or `GOOGLE_API_KEY`) set streams the EN and ES fixtures to the real API in real time and logs the transcript, the detected languages and the latency, then translates a caption each way (`GEMINI_LIVE_MODEL` and `GEMINI_TRANSLATION_MODEL` override the models). Without the tag or the key it's skipped, so CI never calls Google.

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

## Local AI provider

The local provider needs two sidecars: **whisper-server** (whisper.cpp) for speech recognition and **Ollama** running Gemma for translation.

| Service | Port | Default model |
|---|---|---|
| whisper-server | `8178` | `ggml-large-v3-turbo` (multilingual; smaller: `medium`, `small`) |
| Ollama | `11434` | `gemma3:4b` (smaller: `gemma3:1b`) |

`task models:pull` downloads the whisper model into `./models` (checked against Hugging Face's SHA-256) and pulls Gemma through Ollama. Override with `WHISPER_MODEL=small` or `GEMMA_MODEL=gemma3:1b`. English-only whisper models (`*.en`) are refused, because Spanish needs a multilingual model.

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

The local translator (`internal/provider/local/gemma`) talks to Ollama's `/api/chat` with streaming, using `providers.local.ollamaUrl` and `providers.local.gemmaModel` from the settings (read at call time). When a session starts it loads the model with an empty chat, and every request sends `keep_alive: 30m`, so the first caption doesn't wait for a model load and a pause doesn't unload it. It runs at most 2 requests at once per model and sends `think: false`, so thinking models such as Gemma 4 answer straight away. Measure it on your machine with:

```sh
GEMMA_MODEL=gemma3:4b go test -tags ollama -run TestLiveLatency -v ./internal/provider/local/gemma/
```

On an Apple M5 Pro (48 GB, Ollama 0.34, `gemma4:26b`), loading the model took about 7 s. After that, one caption took 270–440 ms (median 330 ms, first token after about 190 ms) for both es→en and en→es.

### How the local provider uses whisper-server

The server reads `providers.local.whisperUrl` and `whisperModel` from the settings when a session starts (defaults `http://127.0.0.1:8178` and `large-v3-turbo`). whisper-server loads its model at launch, so `whisperModel` has to name the model it runs. At start the provider checks `GET /health` and sends one second of silence with `language=es`. If the server doesn't answer or is still loading its model, the session fails with `provider.unavailable`. If the model is English-only, the session fails with `provider.model_english_only`. That can come from the name (`*.en`) or from the probe: an English-only model answers "english" even when asked for Spanish.

whisper-server transcribes files, not streams, so the provider (`internal/provider/local/whisper`) cuts the audio into utterances with an energy VAD. The speech threshold is -55 dBFS, or 12 dB above the tracked noise floor, whichever is higher:

- While someone speaks, the utterance so far is sent to `POST /inference` (WAV, `response_format=verbose_json`) after every 1 s of new audio, for **interim** text. When the server falls behind, interims are skipped; finals never are.
- A 600 ms pause, or 12 s without one, commits the utterance as **final** under the same segment ID. At 12 s the cut lands on the quietest moment of the last 3 s.
- Silence and short clicks never reach the server. Text that whisper invents on noise is dropped: `[Música]`, `[BLANK_AUDIO]`, "Thanks for watching", "Subtítulos realizados por la comunidad de Amara.org", and phrases looping three or more times.
- With source language `auto`, the provider keeps the likelier of `en` and `es` from `language_probabilities` for each utterance. If whisper picked a third language, the utterance is transcribed again in that choice. A pinned source language skips detection. Very short utterances ("OK", "sí") can be misdetected, and smoothing across segments is P2-05. Glossary terms go in whisper's `prompt`.

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

## Test audio

- **Committed fixtures**: `testdata/audio/fixtures/{en,es}.wav`, about 8 s each, 16 kHz mono s16le. They're synthetic (macOS text-to-speech, `scripts/make-fixtures.sh`) so CI can use them without third-party rights. whisper `tiny` transcribes both and detects the right language.
- **Real talks**: `task audio:fetch` downloads one English and one Spanish Nerdearla talk from YouTube and trims each to a 10-minute 16 kHz mono clip in `testdata/audio/{en,es}.m4a` (gitignored). Change the talks or the window with `EN_URL`, `ES_URL`, `START` and `DURATION`.

### Feeding a file to a session

`task demo:file SESSION=main FILE=testdata/audio/en.m4a` plays a file into a session in real time, as if someone were speaking (`LOOP=true` repeats it, `PORT=18080` or `URL=…` picks the server). It calls `POST /api/sessions/{id}/sources/file` with the admin bearer token, so export the same `LIVESUBS_ADMIN_TOKEN` in the server's environment and in your shell. The session must be idle; `DELETE` on the same URL stops it.

The server decodes the file with ffmpeg (`--ffmpeg` / `LIVESUBS_FFMPEG` if it isn't in `PATH`). Only files under the data directory and `./testdata`, or `http(s)` URLs, are accepted; relative paths are resolved from the server's working directory.

## Troubleshooting

- **`localhost:8080` answers with another app's 404.** Another process is listening on `127.0.0.1:8080`, and the Go server binds `*:8080` alongside it without an error. Check with `lsof -nP -iTCP:8080 -sTCP:LISTEN`, then stop it or use `task dev PORT=18080`.
