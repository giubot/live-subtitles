# Scaling and cost

How many rooms and viewers one Live Subtitles node handles, what hardware each room needs, what Gemini costs per hour, and how to grow from 2 to 30+ rooms (requirements §5.3 and §5.8). The numbers come from `task loadtest` on the machine listed under [Measured](#measured), and from the price defaults in `internal/metrics/cost.go`. Deployment steps are in [deployment.md](deployment.md).

## The model: one pipeline per room

Each session (room) is its own pipeline: an audio source, a provider stream, translation, and a caption track per language on an in-process bus. Sessions share nothing but the bus, the SQLite file and the HTTP listener, so they scale out by adding nodes, not by making one node bigger. The same binary runs every mode; only the defaults change.

| Mode | Where the AI runs | Rooms per node | Viewers reach | Good for |
|---|---|---|---|---|
| **Edge** | On the room's own box (local provider) or Gemini | 1 | the box on the venue LAN, by QR | Events with unreliable internet; one room down never affects another |
| **Cloud** | Gemini (or a GPU VM with the local provider) | many | a public HTTPS URL | Remote audiences, streams, no hardware at the venue beyond a capture laptop |
| **Hub** (stretch, P5-06, not built yet) | Edge boxes | 1 per edge | the cloud hub, which re-serves the edges' captions | Local AI in each room, plus thousands of remote viewers |

With Gemini the box only captures, serves and forwards, so any mini PC works. With the local provider the box runs whisper.cpp and Gemma next to the server, so it needs a GPU or Apple Silicon (see [Hardware per edge node](#hardware-per-edge-node)).

## Measured

`task loadtest` (below) on one machine, with the load generator and the server sharing it:

- **Machine**: Apple M5 Pro, 18 cores, 48 GB RAM, macOS 27.0; Go 1.26.5, ffmpeg 8.1.2; commit `b3dcf49` plus the load test.
- **Load**: the mock provider (EN → ES), the 7.8 s `testdata/audio/fixtures/en.wav` looping as a file source in every session, each viewer on `lang=source&lang=es`. That is about 5.6 caption events per second per session (one interim per word per track, plus the finals), roughly what a live talk produces.
- **Runs**: 60 s measured after a 3 s warm-up, all viewers connected before the audio starts.

| Sessions × viewers | Viewers | Msgs/s to viewers | Out | Fan-out skew p50 / p99 / max | Delivery lag p50 / p99 | Server CPU | Server RSS (before → peak) | Drops |
|---|---|---|---|---|---|---|---|---|
| 1 × 100 | 100 | 556 | 0.11 MB/s | 0.5 / 2.5 / 3.4 ms | 37 / 58 ms | 1.4 % of a core | 35 → 55 MiB | 0 |
| 5 × 200 | 1,000 | 5,563 | 1.1 MB/s | 2.2 / 12 / 20 ms | 37 / 67 ms | 12 % | 34 → 158 MiB | 0 |
| 30 × 100 | 3,000 | 16,673 | 3.4 MB/s | 0.5 / 3.0 / 12 ms | 37 / 61 ms | 37 % | 35 → 372 MiB | 0 |
| 10 × 500 | 5,000 | 27,785 | 5.7 MB/s | 2.2 / 11 / 38 ms | 37 / 66 ms | 61 % | 34 → 572 MiB | 0 |
| 20 × 500 | 10,000 | 55,494 | 11.4 MB/s | 2.3 / 11 / 34 ms | 39 / 64 ms | 150 % | 37 → 1,095 MiB | 0 |

Every viewer stayed connected in every run: no dial failures, no slow-consumer drops (close code 1013) and no other disconnects, and `/metrics` saw the same number of caption sockets as the load generator. The ffmpeg process of each file source added about 0.17 % of a core and 13 MiB, outside the server's numbers; browser capture needs no ffmpeg.

What the columns mean:

- **Fan-out skew**: how long after the session's first viewer each other viewer read the same caption event. It is the cost of one room's fan-out, and stays around a few milliseconds.
- **Delivery lag**: how much later than the session's fastest final a viewer read a final, both measured against the audio clock. It includes the drift of ffmpeg's real-time pacing over a looping file (tens of milliseconds a minute), which is why it is 37 ms at 100 viewers too. It doesn't grow with load, which is the point.
- **Pipeline**: the captions' own `latencyMs` (not in the table) stayed at p50 44 ms and p99 55–66 ms in every run. With the mock provider that is the bus and translation plumbing; with a real provider, add recognition and translation time.

Rules of thumb from these runs (a least-squares fit of the first four rows; the 10,000-viewer run is a bit over it because the load generator competes for the same cores):

| Cost | CPU | Memory |
|---|---|---|
| Per caption viewer (2 tracks) | ≈ 0.012 % of a core (≈ 8,000 viewers per core) | ≈ 105 KB |
| Per session, mock provider, excluding viewers | ≈ 0.1 % of a core | ≈ 1 MiB |
| Per viewer, network | ≈ 1.1 KB/s (≈ 9 kbit/s) | |

So on the server side the limit is viewers, not rooms, and it is far away: extrapolating, a 4-vCPU / 4 GB cloud VM carries on the order of 20,000 viewers (measured here up to 10,000). At a venue the Wi-Fi runs out long before the box.

The mock provider doesn't do the work a real one does. With Gemini the recognition and translation run in Google's cloud, and the node only adds a WebSocket to the Live API and the translation calls, so these numbers carry over. With the local provider the sidecars (whisper-server, Ollama) take most of the box; see below.

## Hardware per edge node

An edge node runs one room: the server, plus whisper-server and Ollama for the local provider. The setup wizard's hardware check (`internal/hwcheck/recommend.go`) picks the models from the memory the models may use, which is a discrete GPU's own memory, or half the RAM on Apple Silicon and integrated GPUs:

| Node | Model memory | whisper | Gemma | Download (catalog) | Real time likely |
|---|---|---|---|---|---|
| Apple Silicon with 16 GB+ (Mac mini M4 or later), or a PC with an 8 GB+ NVIDIA GPU | ≥ 8 GB | `large-v3-turbo` | `gemma3:4b` | 1.6 GB + 3.3 GB | yes |
| GPU with 4–8 GB (e.g. 8 GB Apple Silicon, a small discrete GPU) | ≥ 4 GB | `large-v3-turbo` | `gemma3:1b` | 1.6 GB + 0.8 GB | yes |
| Smaller GPU, or a CPU with 8+ cores and 16 GB RAM | | `small` | `gemma3:1b` | 0.5 GB + 0.8 GB | yes |
| Anything else | | `small` | `gemma3:1b` | 0.5 GB + 0.8 GB | no: use Gemini |

The table is a guess; the benchmark (Admin → hardware check, `POST /api/system/benchmark`) measures the real-time factor on the node, and anything up to 0.8 counts as keeping up ([dev.md](dev.md#hardware-check-and-benchmark)). On the Apple M5 Pro above, `large-v3-turbo` + `gemma3:4b` ran at a real-time factor of 0.14, so one such box could run a few local rooms. Benchmark before putting two rooms on one node: they share the GPU, and the factor roughly adds up.

Recommended edge box per room:

- **Local provider**: Apple Silicon Mac mini with 16 GB, or a mini PC with a recent NVIDIA GPU (8 GB+). Wired Ethernet to the venue switch.
- **Gemini**: any mini PC or laptop with 4 GB RAM and a stable uplink of about 0.5 Mbit/s per room.
- Line-in or USB audio from the room's mixer (browser capture on `http://localhost`), or SRT from vMix/OBS.

## Gemini cost per hour per session

The server estimates each session's cost as it runs (`SessionStatus.usage.estimatedCostUsd` on the dashboard), from the token counts Gemini reports and these prices (`internal/metrics/cost.go`, overridable with the `--gemini-*-usd-*` flags, [dev.md](dev.md#latency-and-cost)):

| Model | Price (USD) |
|---|---|
| Speech recognition, `gemini-3.5-transcribe-live` | 0.005 per minute of audio in, 21.00 per million transcript tokens out |
| Translation, `gemini-3.5-flash-lite` | 0.30 per million input tokens, 2.50 per million output tokens |

The estimate for one hour of an EN talk translated to ES assumes 140 words a minute, 1.3 tokens a word and 15 words a sentence:

**Speech recognition** (per session, whatever the number of languages):

- Audio: 60 min × $0.005 = **$0.300**
- Transcript: 140 × 60 × 1.3 = 10,920 tokens × $21.00 / 1M = **$0.229**
- Total: **$0.53 per hour**

**Translation** (per target language). Finals carry the last 3 sentences as context. Interims are translated too, but debounced to the translator's pace; assume one a second while speaking:

- Finals: 8,400 words / 15 = 560 a hour. Input 560 × 255 tokens (about 150 of instructions, 75 of context, 30 of caption) = 142,800 × $0.30 / 1M = $0.043. Output 560 × 25 = 14,000 × $2.50 / 1M = $0.035. Subtotal **$0.078**.
- Interims: 3,600 a hour. Input 3,600 × 205 tokens (instructions plus the "unfinished fragment" rule, no context) = 738,000 × $0.30 / 1M = $0.221. Output 3,600 × 15 = 54,000 × $2.50 / 1M = $0.135. Subtotal **$0.356**.
- Total: **$0.43 per hour per target language**

| Session | Per hour | 8-hour day |
|---|---|---|
| EN → ES (one target) | 0.53 + 0.43 = **$0.96** | $7.70 |
| EN → ES + PT (two targets) | 0.53 + 2 × 0.43 = **$1.40** | $11.20 |
| Floor: one target, finals only | 0.53 + 0.08 = $0.61 | $4.90 |

Silence still streams (the audio is billed per minute), but a paused session sends nothing. Ten rooms for an 8-hour day at one target cost about $77; thirty rooms about $230. The prices are list-price estimates from September 2026 and go stale; trust the dashboard's `estimatedCostUsd` after a real talk over this arithmetic. Each session holds one Live API connection for its whole run, so check the Live API concurrent-session quota of your Google Cloud project against the number of rooms before the event.

## From 2 to 10 to 30+ rooms

| Rooms | Recommended | Hardware | Notes |
|---|---|---|---|
| **2** | Edge: one box per room | Two Mac minis (local) or two mini PCs (Gemini) | Simplest. Each room's QR points at its own box. If the internet is unreliable, the local provider keeps working offline. |
| **10** | Edge per room, or one cloud VM with Gemini | 10 boxes; or one 4-vCPU / 8 GB VM (server load is well under a core, see [Measured](#measured)) | Edge: label boxes by room and give them fixed LAN IPs. Cloud: each room's capture page streams to the VM over WSS (256 kbit/s per room), or vMix/OBS pushes SRT. Budget about $10/h of Gemini. |
| **30+** | Cloud (Gemini), or edge + hub | One or two 4–8 vCPU VMs; split rooms across VMs by track or floor | One VM handles the viewers easily; the limits are the Live API quota, the venue uplink (30 × 256 kbit/s ≈ 8 Mbit/s of capture audio), and SRT ports (one UDP port per session, 100 at most per node). A second node halves the blast radius of a crash. With local AI, 30 edge boxes and the hub (stretch) serve one directory. |

A node only knows its own sessions, so with several nodes the public directory is per node (or the hub's). Put a reverse proxy or DNS name per node, and print each room's QR from its own node.

## Network and ports

| Port | Protocol | Who connects | Traffic |
|---|---|---|---|
| 8080 | TCP (HTTP + WebSocket) | Audience phones, stage screen, OBS overlay; capture on `localhost` | ≈ 9 kbit/s per viewer for two tracks, plus the web app once per phone |
| 8443 | TCP (HTTPS + WSS) | Remote capture and admin | 256 kbit/s per capturing room (16 kHz mono PCM) |
| 9000 and up | UDP (SRT) | vMix, OBS, encoders | The encoder's audio bitrate; one port per SRT session counting up from `settings.srt.port` (9000–9009 covers ten) |
| outbound 443 | TCP | Node → Gemini | ≈ 256 kbit/s per Gemini session, plus small translation requests |

On the venue LAN, 1,000 phones at 9 kbit/s is about 9 Mbit/s, which a switch doesn't notice, but a crowded Wi-Fi might. Put the edge box on a wire, and check that the audience Wi-Fi can reach its IP: some guest networks isolate clients from the LAN. In the cloud, terminate TLS on the node (Let's Encrypt, `--tls acme`) or on a proxy that passes WebSockets through without buffering.

## Load test

```sh
task loadtest                                   # 1 session × 100 viewers, 30 s
task loadtest SESSIONS=10 VIEWERS=500 DURATION=60s
task loadtest -- -json /tmp/run.json            # also write the report as JSON
LIVESUBS_ADMIN_TOKEN=… task loadtest ADDR=http://10.0.0.5:8080 PID=12345
```

Without `ADDR`, it builds the server into `bin/loadtest/livesubs` and starts it on a free loopback port with a throwaway data directory, TLS off, `/metrics` on and a random admin token, and deletes it all afterwards. It then:

1. creates `SESSIONS` sessions on the mock provider (EN → ES, recording off) with the admin bearer token,
2. connects `VIEWERS` `/ws/captions` viewers to each (`-langs`, default `source,es`),
3. starts a looping file source on each (`-file`, default `testdata/audio/fixtures/en.wav`), waits 3 s and measures for `DURATION`,
4. samples the server's CPU time and RSS, and its ffmpeg children's, with `ps` every second, and reads the caption socket count from `/metrics`,
5. stops and deletes the sessions, and prints the report.

With `ADDR` it targets a server you started; export the server's `LIVESUBS_ADMIN_TOKEN`, and pass `PID` for CPU and RSS. The file is then resolved by that server, under its data directory or `./testdata`. A viewer the server drops for falling behind (close 1013) is counted and reconnects. CPU sampling uses `ps`, so it works on macOS and Linux, not Windows.

One client address can open only about 16,000 connections to one server port (the ephemeral port range), so test beyond that from several machines.
