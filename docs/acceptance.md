# Acceptance run (P4-06)

This is the [requirements §8](requirements.md#8-acceptance-criteria-demo-checklist) demo checklist, walked against the real binary. It covers the backend half: everything reachable over HTTP, WebSockets and ffmpeg. The pages (viewer, stage, overlay, capture, admin) are checked in the frontend half with the Playwright smoke suite.

- **When**: 2026-09-24, commit `b3dcf49` plus the fixes on `task/p4-06-acceptance`
- **Machine**: Apple M5 Pro, macOS, Homebrew ffmpeg (no libsrt), whisper.cpp `whisper-server` with `ggml-tiny.bin`, Ollama 0.34.2 with `gemma3:4b`. No Google API key.
- **Status values**: `pass` (checked end to end), `pass-backend-only` (backend checked; the page still needs the frontend check), `needs real service` (needs something this machine didn't have; the code path is covered by tests), `needs frontend` (browser only), `fail`.

## Summary

| # | §8 item | Status |
|---|---|---|
| 1 | Two sessions at once from two audio sources | pass-backend-only |
| 2 | Source + ES + EN live in the viewer, < 3–4 s | pass-backend-only |
| 3 | EN and ES talks detected with no setting change | pass-backend-only |
| 4 | Local provider (whisper.cpp + Gemma) on a Spanish talk | pass-backend-only (tiny model; quality needs `large-v3-turbo`) |
| 5 | Phone on the Wi-Fi scans the QR, switches language | needs frontend |
| 6 | Stage screen with large subtitles and a QR | needs frontend |
| 7 | OBS browser source, transparent overlay | pass-backend-only (presets); needs frontend + OBS |
| 8 | YouTube CC via HTTP POST, overlay burns the other language | pass-backend-only (fake ingestion server); needs a real YouTube stream |
| 9 | Same overlay as a vMix Web Browser input | needs frontend + vMix |
| 10 | SRT from OBS/vMix produces subtitles | pass (Docker, ffmpeg with libsrt); needs OBS/vMix for the sender side |
| 11 | `.vtt` / `.srt` download and play | pass-backend-only (ffprobe + parser); a player check is frontend |
| 12 | Switch a session between Gemini and local | pass-backend-only for local; Gemini needs a real key |
| 13 | No key → local; valid key → Gemini | pass for "no key" and "invalid key"; "valid key" needs a real key |
| 14 | Key saved, survives restart, not in DB/logs/API, in keychain | pass (encrypted file); keychain not exercised |
| 15 | Replay plays the audio with synced subtitles | pass-backend-only |
| 16 | Second machine captures over HTTPS after installing the CA | pass-backend-only (same machine, `wss://` with the CA) |
| 17 | Every screen in ES/EN, browser language default | needs frontend |
| 18 | Light / Dark / System; overlay transparent | needs frontend |
| 19 | Fresh clone runs from the README | not run here (docs lane) |

One backend bug was found and fixed (below). Nothing is failing in the backend.

## Setup

```sh
task build
export LIVESUBS_ADMIN_TOKEN=acceptance-admin-token-0123   # at least 16 characters
export LIVESUBS_MASTER_KEY=acceptance-master-passphrase  # encrypted secrets file
DATA=$(mktemp -d)
bin/livesubs --addr 127.0.0.1:18090 --https-addr 127.0.0.1:18453 --data-dir "$DATA" \
  --no-keychain --metrics --models-dir "$DATA/models"
```

Free ports matter: another app on `:8080` makes `localhost:8080` answer for it ([dev.md troubleshooting](dev.md#troubleshooting)). `--no-keychain` keeps the run from touching a real install's keychain entry, so secrets go to the encrypted file, which needs `LIVESUBS_MASTER_KEY` (without it, saving a key answers 422 `secret.no_backend`).

For the local provider, whisper-server ran on its own port, and the settings pointed at it:

```sh
whisper-server --host 127.0.0.1 --port 18178 --model models/ggml-tiny.bin
# PUT /api/settings with providers.local.whisperUrl=http://127.0.0.1:18178, whisperModel=tiny
```

In the commands below, `A='-H "Authorization: Bearer $LIVESUBS_ADMIN_TOKEN" -H "Content-Type: application/json"'` stands for the admin headers, and `B=http://127.0.0.1:18090`.

## Automated

`TestAcceptanceTwoSessionsExportsReplayRestart` (`internal/app/acceptance_test.go`) repeats items 1, 2, 11, 15 and the restart checks with the mock provider over real HTTP and WebSockets, in about 3 s. It runs in `task check` (skipped without ffmpeg):

```sh
go test -race -run TestAcceptance -v ./internal/app/
```

## Items

### Setup, login and admin access

**pass**. `GET /api/setup` → `adminPinSet:false`; `POST /api/setup {"pin":"4321"}` → 204 with `ls_admin` (HttpOnly, SameSite=Strict, 7 days); a second setup → 409; a wrong PIN on `/api/auth/login` → 401; `/api/sessions` → 401 anonymous, 200 with the cookie or the bearer token. The cookie still worked after a restart.

### 1. Two sessions at once from two audio sources

**pass-backend-only**. `room-a` played `testdata/audio/fixtures/en.wav` on a loop (`POST /api/sessions/room-a/sources/file`), while `room-b` got `es.wav` as browser audio over `/ws/ingest/room-b?token=…` (hello, `ready`, 100 ms PCM chunks in real time, `level` messages back). Both were live together on the mock provider, each with its own recording.

```sh
curl $A -d '{"slug":"room-a","name":"Room A","provider":"mock"}' $B/api/sessions
curl $A -d '{"uri":"testdata/audio/fixtures/en.wav","loop":true}' $B/api/sessions/room-a/sources/file
curl $A -X POST $B/api/sessions/room-b/start   # then stream PCM to /ws/ingest/room-b
```

### 2. Original + ES + EN live, < 3–4 s

**pass-backend-only**. A viewer on `/ws/captions/<id>?lang=source&lang=es&lang=en` got `history`, `viewers`, `state` and interim and final captions on all three tracks, with the same `segmentId` across tracks. Server-side `latencyMs` of finals: mock p50 47 ms, max 106 ms (also over `wss://`); local provider (whisper tiny + gemma3:4b) 93–175 ms on `source` and 581–584 ms on the translated track. The first final arrives when the speaker finishes the sentence plus that latency, well under 3 s on these fixtures. The end-to-end browser delay is for the frontend check.

Seen along the way (not bugs): a caption the mock flushes at `stop` long after the capture disconnected reports its real latency (34 s in one run), which lifts that run's p95 in the status; and a flushed one-word partial on the mock can get the mock translator's `[es] …` placeholder.

### 3. EN and ES detected without changing a setting

**pass-backend-only**. With `sourceLanguage: auto`, the mock's alternating EN/ES script produced `sourceLang` per caption and `state` messages switching `detectedLanguage` en ↔ es, with the translated track following (an ES segment's `es` track is the original, its `en` track the translation). On the local provider, `es.wav` came out as `detectedLanguage: es` and `en.wav` as `en` in the same session, with no change in between.

### 4. Local provider on a Spanish talk

**pass-backend-only** with the `tiny` model: session `room-c` with `provider: default` resolved to `local` and transcribed `es.wav`: "Bienvenidos a todos, hoy vamos a hablar de observabilidad de encovernetes, y de cómo se combina las métricas, los locs y las trases." → EN "Welcome everyone, today we're going to talk about observability of encovernetes and how metrics, logs, and traces are combined." The tiny model's errors match the WER table in [dev.md](dev.md#how-the-local-provider-uses-whisper-server); "comparable to English" needs `large-v3-turbo` and the real talks (`task audio:fetch`). The benchmark (`POST /api/system/benchmark`) gave a real-time factor of 0.06 (`ok: true`). Also run: `go test -tags whisper -run Real ./internal/provider/local/whisper/` (tiny) and `go test -tags ollama -run TestLiveLatency ./internal/provider/local/gemma/` (gemma3:4b), both pass.

### 5–6. Phone via QR; stage screen

**needs frontend**. Backend side: session `urls` carry viewer, stage, overlay, capture and replay links; `GET /api/network` gives the viewer base URL (`localhost` while bound to loopback, as designed). The SPA routes `/s/<id>`, `/stage/<id>` and `/overlay/<id>` answer 200. A LAN-bound instance and a real phone weren't part of this run.

### 7. OBS overlay

**pass-backend-only**. `GET /api/overlay-presets` (public) lists `classic`, `outline`, `lower-third`; `DELETE` of a built-in → 409 `overlay.builtin_read_only`; `POST` "Caja clásica" twice → ids `caja-clasica`, `caja-clasica-2`; `/overlay/room-a?lang=es&preset=caja-clasica` → 200. Transparency and OBS itself are frontend checks.

### 8. YouTube closed captions over HTTP POST

**pass-backend-only**. With no URL, `POST …/stream-captions/test` → 422 `streamcc.no_url`. A fake ingestion server (a Python `http.server` that answers each POST with a UTC timestamp) stood in for YouTube:

```sh
curl $A -X PUT -d '{"value":"http://127.0.0.1:18999/closedcaption?cid=…"}' $B/api/sessions/room-a/stream-captions/youtube-url
curl $A -X PATCH -d '{"streamCaptions":{"enabled":true,"track":"en","maxCharsPerLine":32}}' $B/api/sessions/room-a
curl $A -X POST -d '{"text":"Prueba de subtítulos"}' $B/api/sessions/room-a/stream-captions/test
```

The test caption and then each EN final of a live run arrived as `text/plain` POSTs with `&seq=1,2,3…`, a timestamp line and the cue wrapped at 32 characters with `<br>`. The API only showed `••••-777`; the `cid` never appeared in the logs or the data directory. With the server down, the test answered `state: error`, `streamcc.unreachable`, without the URL. A real YouTube stream is still to be checked.

### 9. vMix Web Browser input

**needs frontend** (and vMix). Same overlay page as item 7.

### 10. SRT ingest

**pass** in Docker; **needs** OBS/vMix for a real sender. Homebrew's ffmpeg has no libsrt, so on the Mac `srtIngest` is absent from `/api/system/info` and `start {"source":"srt"}` answers 422 `source.srt_unavailable`, as documented. In `golang:1.26-trixie` (Debian ffmpeg with libsrt), the linux/arm64 binary opened the listener, and

```sh
ffmpeg -re -i testdata/audio/fixtures/en.wav -c:a aac -f mpegts 'srt://127.0.0.1:9000?streamid=srt-room'
```

produced captions and a recording (`srt sender connected` / `disconnected` in the log; the ES SRT export had the expected cues). The SRT tests pass there too:

```sh
docker run --rm -v "$PWD":/src -w /src golang:1.26-trixie sh -c \
  'apt-get update -qq && apt-get install -y -qq ffmpeg >/dev/null && go test -run SRT ./internal/audio/ffmpeg/ ./internal/api/handlers/'
```

### 11. `.vtt` / `.srt` files

**pass-backend-only**. `GET /api/public/sessions/room-a/subtitles?lang={source,es,en}&format={vtt,srt}` → 200, `text/vtt` / `application/x-subrip`, `Content-Disposition: attachment; filename="room-a-es.vtt"`. `ffprobe` reads them as `webvtt` / `srt` (15 cues each), and `ffmpeg -i room-a-es.vtt -f srt -` converts cleanly. Cues are two lines of at most 42 characters, ordered and non-overlapping across runs. The committed test parses every track in both formats. An unknown `format` → 400 with `fields.format`. Playing in VLC or an HTML `<track>` is the frontend check.

### 12. Switch between Gemini and local

**pass-backend-only** for local, **needs real service** for Gemini. `PATCH {"provider":"gemini"}` on an idle session → `effectiveProvider: gemini`; its start reached the real Live API and failed with 422 `provider.unavailable` ("API key not valid") because the key was fake. `PATCH {"provider":"local"}` → start → live. `PATCH` while live → 409 `session.state_conflict`. Pause, resume and stop worked. With a real key: `go test -tags gemini -run 'Integration|Live' ./internal/provider/gemini/`.

### 13. Default provider

**pass** for no key and an invalid key; **needs real service** for a valid key. No key: `GET /api/providers` → `defaultProvider: local`, `defaultReason: no_google_api_key`, and a session with `provider: default` had `effectiveProvider: local`. After saving a made-up key: `google_api_key_invalid`, `valid: false`, `provider.key_invalid`, and one `provider.fallback_key_invalid` warning in the log. The valid-key case is in `internal/provider/selector` tests (`TestRule`, `TestResolve`) and `keycheck_live_test.go` (`-tags gemini`).

### 14. API key storage

**pass** with the encrypted file; the keychain isn't exercised here. `PUT /api/secrets/google_api_key` → `source: encrypted_file`, `hint: ••••9zzQ`. After a graceful restart it was still `set: true`. `grep -a` for the key over the data directory (`livesubs.db`, `-wal`, `-shm`, `secrets.enc`) and the server log found nothing, and no API response carried it. Ingest tokens, the admin token and the master key weren't in the database or logs either (only hashed ingest tokens are stored; the log never prints query strings). The keychain backend is covered by `go test -tags keychain ./internal/secrets/` on a desktop session, which this run skipped so it wouldn't touch a real install's entry.

### 15. Replay

**pass-backend-only**. Every run left a recording (`GET /api/recordings?sessionId=room-a`, newest first, `status: complete`, `offsetSec` on the session clock: 0, 54, 66, 76, 107, 111…). `GET /api/recordings/<id>/audio` → `audio/mp4` with `Accept-Ranges`; `Range: bytes=0-99` → 206 `bytes 0-99/227046`; an open-ended range → 206; past the end → 416; `HEAD` works; `ffprobe` over HTTP: AAC, 16 kHz, mono, 53.0 s for 52.94 s of audio. A recording still being written serves with `Cache-Control: no-store` and plays up to its last fragment; `DELETE` on it stops it and the session keeps running. `…/subtitles?recordingId=<id>` gives cues shifted to the recording (starting at 00:00:00), and another session's recording id → 404 `recording.not_found`. After `kill -9` mid-recording, the next start marked it `complete` with a playable 4.3 s file, and no ffmpeg processes were left behind.

### 16. Capture over HTTPS

**pass-backend-only** (same machine). `local-ca` created the CA and a server certificate for `localhost`, `127.0.0.1`, the LAN IPs and the hostname. `GET /api/tls/ca.crt` → `application/x-x509-ca-cert`, whose SHA-256 matches `caFingerprintSha256` in `GET /api/tls`. With it, `curl --cacert ca.crt https://localhost:18453/healthz` and `/capture/room-b` answer 200; without it, the certificate is refused. A client trusting only that CA streamed `en.wav` to `wss://localhost:18453/ws/ingest/room-b` and watched captions over `wss://`. A rotated-out ingest token → 401. A second machine is still to be checked.

### 17–18. UI language and theme

**needs frontend**.

### 19. Fresh clone from the README

Not run in this lane (the docs lane is rewriting the README).

### Other backend checks

- **`/healthz`**: `database`, `ffmpeg`, `whisper`, `ollama` under `checks`; `whisper: unreachable` with `status: ok` until whisper-server ran, as documented.
- **`/metrics`** (`--metrics`): 401 without credentials; with the bearer token, `livesubs_sessions{state=…}`, `livesubs_ws_clients`, `livesubs_caption_latency_seconds` per provider and track, `livesubs_recordings`.
- **Graceful shutdown**: SIGTERM with a live session logged `shutting down`, stopped the session, finished its recording and exited 0; the recording was `complete` after the restart.
- **Restart persistence**: sessions, captions, settings, overlay presets, stream-caption settings and URL, secrets and recordings all survived; the new run's clock continued after the old one.
- **Hardware check and models**: logged at startup (`recommended_whisper=large-v3-turbo`, `recommended_gemma=gemma3:4b`).

## Bugs found

| Bug | Fix |
|---|---|
| After a server restart the session clock resumed one second after the last caption, ignoring recordings. Audio often runs past the last caption, so a new run could start inside an earlier recording's window (seen after `kill -9`: recording 107 s + 4.34 s, next run at 111 s), and that recording's replay would pick up the new run's captions. | `fix(session): continue the clock past recordings after a restart`: the clock origin also takes the end of the session's latest recording. Test: `TestClockOriginAfterRestartSkipsRecordings`. |

Minor, not fixed:

- The log line `Google API key checked` prints `secret=[REDACTED]`: it logs the secret's name under the attribute key `secret`, which the redacting handler masks. Renaming the attribute to `name` in `internal/provider/selector` would keep the name readable. That package belongs to the provider-fallback lane.
- The `latencyMs` of a caption flushed at `stop` counts the time since its audio arrived, which can be much longer than the rest when the capture disconnected first (see item 2).
- `/ws/captions` accepts a `lang` the session doesn't have and sends nothing on it; `subtitles` for such a language answers an empty file. The spec allows both.
