# live-subtitles

Real-time transcription and translation for live events. A speaker talks in English or Spanish; the audience reads live captions on their phones, a projector shows them on stage, and OBS or vMix burns them into the stream or sends them to YouTube as closed captions.

It is one Go binary with the web app built in. Run one per room on a mini PC next to the sound desk, or one on a cloud VM for many rooms. Speech recognition and translation run on Google Gemini, or fully offline on the machine with whisper.cpp and Gemma.

<!-- screenshot: admin dashboard with two live sessions (level meter, latency, viewers) -->
<!-- screenshot: audience viewer on a phone, Spanish track -->
<!-- screenshot: stage screen on a projector with the QR corner -->
<!-- screenshot: OBS with the transparent overlay over the program video -->

## Features

- **Live captions with translation**: English/Spanish detected per utterance (a Spanish Q&A after an English talk needs no operator action), translated into Spanish and English by default, and optionally Portuguese, French, German, Italian, Chinese, Japanese and Korean. Interim text shows while the speaker talks and is replaced by the final sentence.
- **Two AI providers, per session**: Gemini (Live API, cloud) or local (whisper.cpp for speech, Gemma through Ollama for translation). New sessions use Gemini when a valid Google API key is saved, and the local provider otherwise.
- **Audio in** from a browser capture page (a line-in or USB interface on the mini PC), from vMix/OBS/an encoder over **SRT**, or from a file or URL for rehearsals.
- **Audience viewer** on phones, opened from a QR code on the LAN, with language switching and text size controls.
- **Stage screen** for a projector: large high-contrast text, optional second language and QR code.
- **OBS / vMix overlay**: a transparent 1920 × 1080 browser source, styled by presets or link parameters.
- **YouTube closed captions** over the HTTP POST ingestion URL, or through OBS's `SendStreamCaption` (CEA-608).
- **Subtitle files**: live and exported WebVTT, SRT, plain text and JSON per language.
- **Recording and replay**: session audio recorded as AAC in fragmented MP4, with a replay page that syncs the transcript.
- **Glossaries**: preferred translations and a do-not-translate list (product names, speaker names), fed to both recognition and translation.
- **Built-in HTTPS**: a local CA for remote capture stations, your own certificate, or Let's Encrypt.
- **Secrets** (API keys, the YouTube URL) stay write-only in the OS keychain or an encrypted file, and never reach logs.
- **Spanish and English UI**, light and dark themes.

## Quick start

You need **ffmpeg** on `PATH` for SRT, file and URL sources and for recordings (built with libsrt for SRT: `ffmpeg -hide_banner -protocols | grep -w srt`). Without it, browser capture and captions still work.

### Release binary

1. Download the archive for your OS and architecture from the GitHub releases page (`livesubs_<version>_<os>_<arch>.tar.gz`, `.zip` for Windows) and check it against `checksums.txt`.
2. Run it:

   ```sh
   tar xzf livesubs_*_linux_amd64.tar.gz
   ./livesubs
   ```

3. Open http://localhost:8080/setup, choose the admin PIN and follow the wizard (hardware check, local models, Google API key, first session). Every step after the PIN can be skipped.

The startup banner prints the audience and admin URLs on the LAN, and a QR code when it runs in a terminal.

### Docker

Docker Engine with Compose v2, plus [Task](https://taskfile.dev) (or run the `docker compose` commands in [`deploy/compose/compose.yaml`](deploy/compose/compose.yaml) by hand).

Put this machine's LAN address in the links and QR codes, and a passphrase for the keys you save in the UI (a container has no OS keychain):

```sh
cat > deploy/compose/.env <<EOF
LIVESUBS_PUBLIC_BASE_URL=http://192.168.1.20:8080
LIVESUBS_MASTER_KEY=$(openssl rand -hex 24)
EOF
task docker:up                  # Gemini only (profile app)
task docker:up PROFILE=local    # + whisper-server and Ollama; GPU=1 for NVIDIA
```

Run it from a clone of this repository: the first `docker:up` builds the image. Keep the `.env` file: without the same master key, the saved keys can't be decrypted. Then open http://localhost:8080/setup. See [`docs/deployment.md`](docs/deployment.md) for the image, the profiles and the ports.

### From source

Requirements: Go 1.26+, Node 22+ with pnpm (`corepack enable` picks the pinned version from `web/package.json`), Python 3, [Task](https://taskfile.dev) (`brew install go-task`, or see its install page), and ffmpeg.

```sh
git clone https://github.com/giubot/live-subtitles.git
cd live-subtitles
corepack enable
task dev      # installs the web dependencies, then the Go server (:8080) + Vite (:5173)
```

Open http://localhost:5173/setup. To try captions without any AI set up, create a session with the **mock** provider: it produces a scripted talk from any sound, or silence.

```sh
task build    # web app + single binary in bin/livesubs
task check    # everything CI runs
task          # list every command
```

[`docs/dev.md`](docs/dev.md) covers development in depth: the local AI provider, test audio, SRT testing and troubleshooting.

## Credentials and models

| What | Needed for | How to set it |
|---|---|---|
| Admin PIN | The admin pages | Chosen in `/setup` on first run |
| `LIVESUBS_ADMIN_TOKEN` (optional, 16+ characters) | Scripts and Prometheus: `Authorization: Bearer …` on the admin API and `/metrics` | Environment only |
| Google API key ([Google AI Studio](https://aistudio.google.com/apikey)) | The Gemini provider | Admin → Providers, or `GEMINI_API_KEY` / `GOOGLE_API_KEY` in the environment |
| `LIVESUBS_MASTER_KEY` | Saving keys in the UI where there is no OS keychain (Docker, headless Linux) | Environment only |
| YouTube caption ingestion URL | YouTube closed captions | Per session, in the session's stream captions |
| OBS websocket password | OBS closed captions (`SendStreamCaption`) | Admin → Providers |
| SRT passphrase (optional, 10–79 characters) | Encrypted SRT ingest | `PUT /api/secrets/srt_passphrase`, or `LIVESUBS_SECRET_SRT_PASSPHRASE` |

Secrets saved in the UI go to the OS keychain (macOS Keychain, Windows Credential Manager, Linux Secret Service) or, without one, to `<data dir>/secrets.enc`, encrypted with `LIVESUBS_MASTER_KEY`. A secret set by an environment variable can't be changed from the UI. Every secret also has a generic variable, `LIVESUBS_SECRET_<NAME>` (for example `LIVESUBS_SECRET_OBS_WEBSOCKET_PASSWORD`, or `LIVESUBS_SECRET_SESSION_MAIN_YOUTUBE_URL` for session `main`).

**Local models** (only for the local provider): whisper.cpp's `whisper-server` on port 8178 with a multilingual model (default `large-v3-turbo`; `medium` or `small` on weaker hardware; never an English-only `.en` model), and Ollama on port 11434 with Gemma (default `gemma3:4b`, or `gemma3:1b`). Download them from the setup wizard, or with `task models:pull` from a source checkout. The hardware check recommends sizes and a benchmark measures whether your machine keeps up in real time. Gemma's weights are downloaded under Google's terms and are not part of this repository. Setup per OS: [`docs/dev.md` § Local AI provider](docs/dev.md#local-ai-provider).

## Configuration

Process settings come from flags or `LIVESUBS_*` variables (a flag wins over its variable, which wins over the default; `livesubs -h` lists them). Everything else (languages, providers, models and their URLs, SRT, recording, overlay presets, public URL) is in Admin → Settings and applies without a restart.

| Flag | Variable | Default | What |
|---|---|---|---|
| `--addr` | `LIVESUBS_ADDR` | `0.0.0.0:8080` | HTTP listen address |
| `--https-addr` | `LIVESUBS_HTTPS_ADDR` | the `--addr` host on `:8443` | HTTPS listen address |
| `--tls` | `LIVESUBS_TLS` | `auto` | `auto`, `local-ca`, `provided`, `acme` or `disabled` ([HTTPS](docs/dev.md#https)) |
| `--tls-cert`, `--tls-key` | `LIVESUBS_TLS_CERT`, `LIVESUBS_TLS_KEY` | | Your own certificate (PEM with chain) and key |
| `--acme-email` | `LIVESUBS_ACME_EMAIL` | | Contact address for Let's Encrypt |
| `--public-base-url` | `LIVESUBS_PUBLIC_BASE_URL` | LAN address | Base of generated links and QR codes, e.g. `https://subs.example.com`; an `https://` public domain turns on Let's Encrypt in `auto` |
| `--data-dir` | `LIVESUBS_DATA_DIR` | `./data` | SQLite database, recordings, certificates, encrypted secrets |
| `--models-dir` | `LIVESUBS_MODELS_DIR` | `./models` | whisper models, shared with whisper-server |
| `--ffmpeg` | `LIVESUBS_FFMPEG` | `ffmpeg` | ffmpeg executable |
| `--no-keychain` | `LIVESUBS_NO_KEYCHAIN` | `false` | Never use the OS keychain; secrets go to the encrypted file only |
| `--metrics` | `LIVESUBS_METRICS` | `false` | Serve Prometheus metrics at `/metrics` (admin only) |
| `--log-format` | `LIVESUBS_LOG_FORMAT` | `text` | `text` or `json` |
| `--log-level` | `LIVESUBS_LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error` |
| `--gemini-asr-audio-usd-per-min` | `LIVESUBS_GEMINI_ASR_AUDIO_USD_PER_MIN` | `0.005` | Price estimate for the cost shown per session ([Latency and cost](docs/dev.md#latency-and-cost)) |
| `--gemini-asr-output-usd-per-mtok` | `LIVESUBS_GEMINI_ASR_OUTPUT_USD_PER_MTOK` | `21` | 〃 |
| `--gemini-translation-input-usd-per-mtok` | `LIVESUBS_GEMINI_TRANSLATION_INPUT_USD_PER_MTOK` | `0.3` | 〃 |
| `--gemini-translation-output-usd-per-mtok` | `LIVESUBS_GEMINI_TRANSLATION_OUTPUT_USD_PER_MTOK` | `2.5` | 〃 |
| `--version` | | | Print the version and exit |

Environment only: `LIVESUBS_ADMIN_TOKEN`, `LIVESUBS_MASTER_KEY`, `GEMINI_API_KEY` / `GOOGLE_API_KEY` and `LIVESUBS_SECRET_<NAME>` (see [Credentials](#credentials-and-models)).

Ports: **8080/tcp** (HTTP: audience, stage, overlay, API, WebSockets), **8443/tcp** (HTTPS: remote capture and admin), and **9000 and up/udp** (SRT, one port per session). The audience only needs 8080.

## Surfaces

`<session>` is the session's address (slug), such as `main-stage`. The admin's **Links** dialog has every link for a session, with a QR code.

| URL | Who | What |
|---|---|---|
| `/setup` | Operator, first run | Admin PIN, hardware check, models, Google key, first session |
| `/admin` | Operator (PIN) | Dashboard: sessions, level, latency, viewers, errors; also `/admin/settings`, `/admin/providers`, `/admin/glossaries`, `/admin/overlays`, `/admin/recordings`, `/admin/tls` |
| `/capture/<session>?token=…` | The computer with the audio input | Sends audio to the session. Needs `http://localhost` or HTTPS for the microphone |
| `/s` and `/s/<session>` | Audience (QR code) | All sessions, and one session's live captions (`?lang=es`, or `source` for the original) |
| `/stage/<session>` | Projector | Big captions, never themed (`?preset=yellow-on-black&lines=2&dual=1&lang=en&lang2=source&qr=0`) |
| `/overlay/<session>` | OBS / vMix | Transparent overlay (`?lang=es&preset=classic`, see the [OBS / vMix guide](docs/obs-vmix-guide.md)) |
| `/replay/<session>` | Anyone, after the talk | Recorded audio with the synced transcript and downloads |

Any page takes `?ui=es` or `?ui=en` to set the interface language. Operations endpoints: `/healthz` (public), `/metrics` (admin, with `--metrics`) and the REST API under `/api` ([`api/openapi.yaml`](api/openapi.yaml)).

## Documentation

- [Event-day runbook](docs/runbook.md): mini PC setup, pre-show checklist, monitoring and troubleshooting.
- [OBS / vMix / YouTube guide](docs/obs-vmix-guide.md): overlay, SRT audio, closed captions.
- [Deployment](docs/deployment.md): Docker, and the dev, edge and cloud modes.
- [Scaling and cost](docs/scaling.md): from 2 to 30+ rooms.
- [Development](docs/dev.md), [requirements](docs/requirements.md), [plan](docs/plan.md), [design](docs/design.md).

## License

Apache-2.0. See [`LICENSE`](LICENSE) and [`NOTICE`](NOTICE).

## Resumen en español

**live-subtitles** transcribe y traduce en tiempo real charlas en inglés y español. El público lee los subtítulos en el celular (escaneando un QR en la red local), un proyector los muestra en el escenario, y OBS o vMix los incrustan en la transmisión o los envían a YouTube como subtítulos opcionales (closed captions).

Es un único binario de Go con la aplicación web incluida. Se usa uno por sala en una mini PC junto a la consola de sonido, o uno en una VM en la nube para muchas salas. El reconocimiento y la traducción corren en Google Gemini o, sin internet, en la propia máquina con whisper.cpp y Gemma.

Para empezar:

1. Instalá ffmpeg (con libsrt si vas a recibir audio por SRT).
2. Descargá el binario de la página de releases y ejecutá `./livesubs`, o usá Docker con `task docker:up` (en ese caso definí `LIVESUBS_PUBLIC_BASE_URL` y `LIVESUBS_MASTER_KEY` en `deploy/compose/.env`).
3. Abrí http://localhost:8080/setup, elegí el PIN de administración y seguí el asistente.
4. Para Gemini, pegá la clave de Google en Administración → Proveedores. Sin clave, las sesiones usan el proveedor local (whisper-server y Ollama).

Pantallas: `/admin` (operador), `/capture/<sesión>` (captura de audio), `/s/<sesión>` (público), `/stage/<sesión>` (proyector), `/overlay/<sesión>` (OBS/vMix) y `/replay/<sesión>` (grabación). Toda la interfaz está en español e inglés (`?ui=es`). La guía del día del evento está en [`docs/runbook.md`](docs/runbook.md) (en inglés).
