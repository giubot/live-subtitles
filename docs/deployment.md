# Deployment

How to run Live Subtitles outside development: a release binary, or Docker. The deployment modes guide (edge, cloud, hub) comes with P4-08. For running from source, see [dev.md](dev.md).

## Ports

| Port | Protocol | What |
|---|---|---|
| 8080 | TCP | HTTP: audience, stage, overlay, API, WebSockets (`--addr`) |
| 8443 | TCP | HTTPS: remote capture and admin (`--https-addr`, see [HTTPS](dev.md#https)) |
| 9000–9009 | UDP | SRT ingest from vMix, OBS or an encoder (`settings.srt.port`, AUD-5) |

Open them on the event LAN's firewall. The audience only needs 8080.

## Release binary

Download the archive for your OS and architecture from the GitHub release (`livesubs_<version>_<os>_<arch>.tar.gz`, or `.zip` for Windows), check it against `checksums.txt`, and run it:

```sh
tar xzf livesubs_*_linux_amd64.tar.gz
./livesubs -version
LIVESUBS_ADMIN_TOKEN=$(openssl rand -hex 24) ./livesubs --data-dir /var/lib/livesubs
```

The binary includes the web app. It needs `ffmpeg` on `PATH` (or `--ffmpeg`) for file, URL and SRT sources and for recordings, built with libsrt for SRT. Every flag has a `LIVESUBS_*` variable (`./livesubs -h`). To build the binaries yourself, run `task build:all` (into `bin/<os>_<arch>/`) or `task release:snapshot` (the release archives in `dist/`).

## Docker

The image (`deploy/docker/Dockerfile`) has the static server binary and Alpine's ffmpeg, which has SRT (the build fails if it doesn't). It runs as the unprivileged `livesubs` user (uid 10001), keeps everything in the `/data` volume, and has a healthcheck on `/healthz`. It's about 60 MB compressed, 210 MB unpacked, mostly ffmpeg.

```sh
task docker:build                       # livesubs:latest (TAG=… for another name)
docker run -d -p 8080:8080 -p 8443:8443 -p 9000-9009:9000-9009/udp \
  -v livesubs-data:/data -e LIVESUBS_PUBLIC_BASE_URL=http://192.168.1.20:8080 livesubs:latest
```

A `v*` tag also publishes `ghcr.io/<owner>/<repo>:<version>` for linux/amd64 and linux/arm64.

Inside a container the server only sees its own IP address, so set `LIVESUBS_PUBLIC_BASE_URL` to the host's LAN address. Otherwise the QR codes and links point at the container. It also adds that host to the local-CA certificate. There's no OS keychain in the container, so secrets (the Google API key) go to the encrypted file in `/data`. Back up the volume to keep sessions, recordings and the CA.

### Compose

`deploy/compose/compose.yaml` has two profiles:

| Profile | Services |
|---|---|
| `app` | The server only, with Gemini as the AI provider |
| `local` | The server, plus `whisper` (whisper.cpp server, ASR), `ollama` (Gemma translation) and `ollama-pull`, which pulls the Gemma model once |

```sh
task docker:up                          # --profile app, builds the image
task docker:up PROFILE=local            # + whisper-server and Ollama
task docker:up PROFILE=local GPU=1      # + NVIDIA GPU (compose.gpu.yaml; needs the NVIDIA Container Toolkit)
task docker:down
```

Put the configuration in `deploy/compose/.env` (gitignored, optional). The server gets every variable in it:

```sh
LIVESUBS_PUBLIC_BASE_URL=http://192.168.1.20:8080
LIVESUBS_ADMIN_TOKEN=…                  # at least 16 characters
GEMINI_API_KEY=…                        # or paste the key in Settings
LIVESUBS_METRICS=true                   # optional: /metrics (dev.md#logs-and-metrics)
```

The compose file also reads `LIVESUBS_HTTP_PORT` and `LIVESUBS_HTTPS_PORT` (the host ports, 8080 and 8443 by default), `LIVESUBS_MODELS_DIR` (the whisper models, `./models` at the repository root by default, filled by `task models:pull`), `WHISPER_MODEL`, `GEMMA_MODEL` and `LIVESUBS_LOG_FORMAT` (`json` by default).

With the `local` profile, open Admin → Settings → Local provider once and set the whisper URL to `http://whisper:8178` and the Ollama URL to `http://ollama:11434`. The defaults point at `127.0.0.1`, which in a container is the server itself. Only the server's ports are published; the sidecars are reachable only on the compose network. The whisper.cpp CPU image is amd64-only, so on Apple Silicon it runs emulated and slowly. There, run whisper-server and Ollama natively with Metal ([dev.md](dev.md#macos-apple-silicon-native-with-metal)) and use the `app` profile.

Data lives in the `livesubs_data` volume and the Ollama models in `livesubs_ollama`. `docker compose … down -v` deletes both.
