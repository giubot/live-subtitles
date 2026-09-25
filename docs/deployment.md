# Deployment

How to run Live Subtitles outside development: a release binary, or Docker, in one of the [deployment modes](#deployment-modes) (dev, edge, cloud). For running from source, see [dev.md](dev.md); for the event day, the [runbook](runbook.md); for sizing, [scaling.md](scaling.md).

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
  -v livesubs-data:/data -e LIVESUBS_PUBLIC_BASE_URL=http://192.168.1.20:8080 \
  -e LIVESUBS_MASTER_KEY="$LIVESUBS_MASTER_KEY" livesubs:latest
```

A `v*` tag also publishes `ghcr.io/<owner>/<repo>:<version>` for linux/amd64 and linux/arm64.

Inside a container the server only sees its own IP address, so set `LIVESUBS_PUBLIC_BASE_URL` to the host's LAN address. Otherwise the QR codes and links point at the container. It also adds that host to the local-CA certificate. There's no OS keychain in the container, so secrets saved in the UI (the Google API key, YouTube URLs) go to the encrypted file `/data/secrets.enc`, which needs `LIVESUBS_MASTER_KEY`: without it, saving a key answers `secret.no_backend`, and only keys from environment variables (`GEMINI_API_KEY`, `LIVESUBS_SECRET_<NAME>`) work. Keep the master key with the volume: another key can't open the file. Back up the volume to keep sessions, recordings and the CA.

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
LIVESUBS_MASTER_KEY=…                   # encrypts the secrets saved in the UI
GEMINI_API_KEY=…                        # or paste the key in Settings
LIVESUBS_METRICS=true                   # optional: /metrics (dev.md#logs-and-metrics)
```

The compose file also reads `LIVESUBS_HTTP_PORT` and `LIVESUBS_HTTPS_PORT` (the host ports, 8080 and 8443 by default), `LIVESUBS_MODELS_DIR` (the whisper models, `./models` at the repository root by default, filled by `task models:pull`), `WHISPER_MODEL`, `GEMMA_MODEL` and `LIVESUBS_LOG_FORMAT` (`json` by default).

With the `local` profile, open Admin → Settings → Local provider once and set the whisper URL to `http://whisper:8178` and the Ollama URL to `http://ollama:11434`. The defaults point at `127.0.0.1`, which in a container is the server itself. Only the server's ports are published; the sidecars are reachable only on the compose network. The whisper.cpp CPU image is amd64-only, so on Apple Silicon it runs emulated and slowly. There, run whisper-server and Ollama natively with Metal ([dev.md](dev.md#macos-apple-silicon-native-with-metal)) and use the `app` profile.

Data lives in the `livesubs_data` volume and the Ollama models in `livesubs_ollama`. `docker compose … down -v` deletes both.

## Deployment modes

Every mode runs the same binary with the same flags and settings; only the configuration changes (requirements §5.8). There is no mode switch: a mode is a set of values for `--public-base-url`, the listen addresses and TLS.

| Mode | Where | Sessions | Capture | Audience | TLS |
|---|---|---|---|---|---|
| [Dev](#dev) | Developer laptop | Any | Browser on `localhost`, file or URL | `localhost` and the LAN | Local CA, optional |
| [Edge](#edge-one-instance-per-room) (event default) | One mini PC per room | 1 (or a few) | Browser on `http://localhost` (line-in), or SRT from vMix/OBS | `http://<lan-ip>:8080` via the QR code | Local CA |
| [Cloud](#cloud) | A VM | Many rooms | Remote browser over **WSS**, or **SRT** from each venue | `https://<domain>`, one directory of all rooms | Let's Encrypt |
| [Hub](#hub-not-implemented) (stretch) | Edges plus one cloud hub | 1 per edge | At the edge | Through the hub | Let's Encrypt on the hub |

`GET /api/system/info` reports `mode: dev` for an unversioned build and `edge` for a release build; it doesn't detect the cloud setup.

### Dev

A developer laptop, from source: `task dev` runs the Go server on `:8080` and Vite on `:5173` ([dev.md](dev.md)). The capture page works on `localhost`, test audio comes from `task demo:file`, and the `mock` provider needs no AI at all. Phones on the same Wi-Fi reach `http://<laptop-ip>:8080`.

```sh
task dev                                      # http://localhost:5173
LIVESUBS_ADMIN_TOKEN=$(openssl rand -hex 24) task dev   # also allow scripts and task demo:file
```

### Edge (one instance per room)

The event default: each room has its own mini PC with its own server, sessions, recordings and certificate. One room going down doesn't affect the others, and with the local provider a room keeps working when the venue's internet goes away (with Gemini it needs the uplink). Adding a room means adding a mini PC; nothing is shared. Setup, autostart, kiosk and the pre-show checklist are in the [runbook](runbook.md).

- **Capture** runs on the same mini PC at `http://localhost:8080/capture/<session>?token=…`: `localhost` is a secure context, so the microphone works without HTTPS. Or vMix/OBS push the program audio over [SRT](obs-vmix-guide.md#srt-audio-from-obs-or-vmix).
- **Audience** phones use plain HTTP on the LAN (`http://<lan-ip>:8080/s/<session>`, TLS-3), so they never see a certificate warning. Give the mini PC a fixed IP, or set a local DNS name as the public URL.
- **HTTPS** (`:8443`, local CA) is for remote capture stations and admin laptops, which install the CA once from Admin → TLS.

Example (`/etc/livesubs.env` for the systemd unit in the [runbook](runbook.md#server-autostart)):

```sh
LIVESUBS_DATA_DIR=/var/lib/livesubs
LIVESUBS_MODELS_DIR=/var/lib/livesubs/models
LIVESUBS_PUBLIC_BASE_URL=http://192.168.1.20:8080   # optional: detected from the LAN otherwise
LIVESUBS_ADMIN_TOKEN=…                              # optional: scripts and /metrics
LIVESUBS_MASTER_KEY=…                               # when there's no keychain (headless Linux)
GEMINI_API_KEY=…                                    # or save it in Admin → Providers
```

With several interfaces (Wi-Fi and Ethernet), pick the audience one in Admin → Settings → Network. For the local provider, run whisper-server and Ollama on the same machine ([dev.md § Local AI provider](dev.md#local-ai-provider)), or use the compose `local` profile.

### Cloud

One server on a VM serves many rooms: each room is a session, the venues send audio over the internet, and the audience (in the room or remote) opens a public HTTPS URL. Moving a room from edge to cloud is a configuration change: the capture page points at the cloud URL instead of `localhost`, or vMix pushes SRT to the VM. Sessions, captions and recordings work the same.

- **Where**: a VM (or Kubernetes with a UDP-capable load balancer). Serverless platforms such as Cloud Run are a poor fit: they cut long WebSockets and don't take UDP for SRT.
- **Provider**: Gemini. Running the local provider in the cloud needs GPU VMs; see [scaling.md](scaling.md) for sizes and cost.
- **DNS**: an A/AAAA record for the domain pointing at the VM.
- **Ports**: TCP 80 and 443 (the certificate challenge and the app), and UDP 9000 and up if venues send SRT.

#### Let's Encrypt (autocert)

With `--public-base-url https://<public domain>`, `--tls=auto` picks `acme`: the server gets and renews a Let's Encrypt certificate for that domain by itself (cached in `<data dir>/tls/acme/`). Let's Encrypt must reach the server on **port 80** (HTTP-01, answered by the HTTP listener) or **443** (TLS-ALPN-01, answered by the HTTPS listener), so listen on those ports or forward them. HTTP on port 80 keeps serving the app too; every generated link and QR code uses the `https://` URL.

Binary with systemd (binding ports below 1024 needs the capability):

```ini
# /etc/systemd/system/livesubs.service (as in the runbook, plus:)
[Service]
ExecStart=/opt/livesubs/livesubs --data-dir /var/lib/livesubs --addr :80 --https-addr :443
AmbientCapabilities=CAP_NET_BIND_SERVICE
EnvironmentFile=/etc/livesubs.env
```

```sh
# /etc/livesubs.env
LIVESUBS_PUBLIC_BASE_URL=https://subs.example.com
LIVESUBS_ACME_EMAIL=ops@example.com
LIVESUBS_ADMIN_TOKEN=…
LIVESUBS_MASTER_KEY=…
LIVESUBS_NO_KEYCHAIN=true
GEMINI_API_KEY=…
LIVESUBS_LOG_FORMAT=json
LIVESUBS_METRICS=true
```

Docker (the container runs unprivileged, so map the host's 80 and 443 onto 8080 and 8443):

```sh
docker run -d --name livesubs --restart unless-stopped \
  -p 80:8080 -p 443:8443 -p 9000-9009:9000-9009/udp -v livesubs-data:/data \
  -e LIVESUBS_PUBLIC_BASE_URL=https://subs.example.com -e LIVESUBS_ACME_EMAIL=ops@example.com \
  -e LIVESUBS_ADMIN_TOKEN -e LIVESUBS_MASTER_KEY -e GEMINI_API_KEY \
  ghcr.io/giubot/live-subtitles:<version>
```

Check it with `curl https://subs.example.com/healthz` and `curl https://subs.example.com/api/tls` (`"mode": "acme"`). The first HTTPS request triggers the certificate order, which takes a few seconds.

**Your own certificate** instead: `--tls-cert fullchain.pem --tls-key privkey.pem` (mode `provided`); the files are reloaded when they change, so a certbot renewal needs no restart. **Behind a reverse proxy** that terminates TLS: `--tls=disabled`, `--public-base-url https://…`, and a proxy that forwards WebSockets (`/ws/…`). The server ignores `X-Forwarded-*` headers, so the logs and the PIN lockout (5 wrong PINs in 15 minutes per client) see the proxy's address for everyone, and the admin cookie isn't marked `Secure`; the built-in `acme` mode avoids both.

#### Capture over WSS or SRT

- **Browser (WSS)**: at the venue, open the session's capture link from the admin, `https://subs.example.com/capture/<session>?token=…`, on the computer with the audio input. The page uses `wss://` automatically; the public certificate needs no CA install. It needs about 256 kbit/s upstream per room, and after a network drop it reconnects and sends the last few seconds it kept. A capture browser behind a strict proxy needs WebSockets allowed.
- **SRT**: vMix, OBS or a hardware encoder at the venue calls `srt://subs.example.com:<port>?streamid=<session>` (`urls.srtIngest` of the session). Each SRT session uses its own UDP port from 9000, so open as many as rooms; the Docker image publishes 9000–9009. Over the internet, set the `srt_passphrase` secret and a higher latency in Admin → Settings → SRT (for example 500–1000 ms) for lossy uplinks.

The audience directory is `https://subs.example.com/s`, with every room's session.

### Hub (not implemented)

A stretch goal from the requirements, **not implemented**: edge instances would run the AI in each room and keep an outbound WebSocket to one cloud hub, which would serve a single directory and the remote viewers, with no inbound ports at the venue. Today, pick edge or cloud per room. To give remote viewers access to an edge room, run that room in cloud mode instead, or expose the edge server through a tunnel or reverse proxy of your own.
