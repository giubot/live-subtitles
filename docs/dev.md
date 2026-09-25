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

## Sessions and realtime

Manage sessions at `/admin` → **New session**: give it a name (the address, or slug, is derived from it), languages and provider, then use **Start**, **Pause** and **Stop** on its card. **Links** shows the viewer, stage, overlay and capture links with a QR code for the audience. The capture link carries the session's ingest token, which the server only stores hashed: it's shown right after creating the session, or after **Make a new capture link** (which retires the old one). When the admin runs on `localhost`, the capture link stays on `localhost` too, so the browser allows the microphone.

Scripts can do the same over the API. Anything left out comes from the settings (target languages `[es, en]`, source language `auto`, recording on):

```sh
curl -H "Authorization: Bearer $LIVESUBS_ADMIN_TOKEN" -H "Content-Type: application/json" \
  -d '{"slug":"main","name":"Main stage"}' http://localhost:8080/api/sessions
```

The answer includes the session's `ingestToken` (shown only here and on rotation) and its `urls` for the viewer, stage, overlay and capture pages, built from the LAN address in `GET /api/network` (or `--public-base-url`). Open `urls.capture` + `?token=<ingestToken>` on the capture device: it picks the audio input (remembered per computer), resamples it to 16 kHz in an AudioWorklet and keeps the screen awake while sending. Browsers only allow the microphone on `https://` or `http://localhost`, so on a LAN IP over plain HTTP the page explains that instead; run it on the server computer via `localhost`. The audience opens `/s` (every session) or `urls.viewer`; `?lang=es` (or `source` for the original) preselects the caption track, and each device remembers its last track and text size. The stage screen (`urls.stage`) is for a projector: the session's stage style sets its preset, lines, second language and QR code, and the link can override each (`?preset=yellow-on-black&lines=2&dual=1&lang=en&lang2=source&qr=0`). Double-click or F toggles full screen; the cursor hides after 3 s. `PATCH` changes a session: its name, room, recording and styles any time; its languages, provider and glossary only while it isn't running. `DELETE` removes an idle session and its captions. The audience sees `/api/public/sessions`, which has no tokens or provider settings.

A running session is one pipeline: audio source → speech recognition → one translator per target language → the caption bus (`/ws/captions/{id}?lang=es&lang=source`) and, for final captions, the database. `POST /api/sessions/{id}/start` uses browser audio from `/ws/ingest/{id}?token=…` (the token comes from `POST /api/sessions/{id}/ingest-token`); `pause` stops feeding the provider without dropping the capture connection, `start` resumes, and `stop` waits for the provider to flush its last sentence.

- Until the Gemini and local providers land, sessions run on the **mock provider**: it ignores the audio content and "hears" a scripted EN/ES talk at one word per 300 ms of audio, so any sound (or silence) from the capture page produces captions.
- Caption times are seconds on the **session clock**. Each start continues the clock at least one second after the previous run, so exports never overlap.
- `/ws/admin` streams `AdminEvent`s: the status of every session on connect, then every state change, plus each running session's status once a second.

## Subtitle files

Final captions of every track can be downloaded while a session runs or afterwards (`lang` is a target language or `source`):

| URL | What you get |
|---|---|
| `/api/public/sessions/main/subtitles?lang=es&format=vtt` | WebVTT download (`srt`, `txt` and `json` also work) |
| `…&live=true` | The same, served with `Cache-Control: no-store` for players and tools that poll it |
| `/api/public/sessions/main/captions?lang=es` | JSON pages of 200 captions; pass `nextCursor` back as `after` |

VTT and SRT cues hold at most 2 lines of 42 characters (settings `captions.maxLines` / `maxCharsPerLine`), break between sentences where they can, and stay on screen 5/6 s to 7 s. Captions an admin hid are left out.

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

## Test audio

- **Committed fixtures**: `testdata/audio/fixtures/{en,es}.wav`, about 8 s each, 16 kHz mono s16le. They're synthetic (macOS text-to-speech, `scripts/make-fixtures.sh`) so CI can use them without third-party rights. whisper `tiny` transcribes both and detects the right language.
- **Real talks**: `task audio:fetch` downloads one English and one Spanish Nerdearla talk from YouTube and trims each to a 10-minute 16 kHz mono clip in `testdata/audio/{en,es}.m4a` (gitignored). Change the talks or the window with `EN_URL`, `ES_URL`, `START` and `DURATION`.

### Feeding a file to a session

`task demo:file SESSION=main FILE=testdata/audio/en.m4a` plays a file into a session in real time, as if someone were speaking (`LOOP=true` repeats it, `PORT=18080` or `URL=…` picks the server). It calls `POST /api/sessions/{id}/sources/file` with the admin bearer token, so export the same `LIVESUBS_ADMIN_TOKEN` in the server's environment and in your shell. The session must be idle; `DELETE` on the same URL stops it.

The server decodes the file with ffmpeg (`--ffmpeg` / `LIVESUBS_FFMPEG` if it isn't in `PATH`). Only files under the data directory and `./testdata`, or `http(s)` URLs, are accepted; relative paths are resolved from the server's working directory.

## Troubleshooting

- **`localhost:8080` answers with another app's 404.** Another process is listening on `127.0.0.1:8080`, and the Go server binds `*:8080` alongside it without an error. Check with `lsof -nP -iTCP:8080 -sTCP:LISTEN`, then stop it or use `task dev PORT=18080`.
