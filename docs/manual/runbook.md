# 🎛️ Event-day runbook

[← User manual](README.md) · [Live Subtitles](../../README.md)

How to set up a room and keep it running during an event. It assumes the **edge** layout ([deployment modes](../deployment.md#deployment-modes)): one mini PC per room, next to the sound desk, running Live Subtitles, the capture page and, if there is one, the stage screen on a projector. For OBS, vMix and YouTube, see the [OBS / vMix / YouTube guide](obs-vmix-guide.md).

<!-- screenshot: a mini PC wired to the mixer's aux out, with the projector and the stage screen -->

## The room at a glance

```text
 mixer aux/matrix out ──3.5 mm / USB──▶ mini PC ──HDMI──▶ projector (/stage/<session>)
                                          │
                                          ├── browser: /capture/<session>  (http://localhost:8080)
                                          ├── livesubs (:8080 HTTP, :8443 HTTPS, :9000+/udp SRT)
                                          └── Wi-Fi / LAN ──▶ audience phones (/s/<session> via QR)
                                                          └─▶ OBS / vMix (overlay, SRT audio)
```

## Mini PC setup

Do this once per machine, ideally the week before.

### Hardware and AI provider

- With **Gemini**, any mini PC works: the box captures, encodes and serves, and the AI runs in the cloud. It needs a reliable internet uplink: the audio goes to Google as 16 kHz PCM, about 256 kbit/s per session. The audience traffic stays on the LAN.
- With the **local provider** (whisper.cpp + Gemma), real time needs roughly an Apple Silicon Mac mini, or a mini PC with a recent GPU. Run the hardware check and the benchmark in `/setup` (or `POST /api/system/benchmark`); a real-time factor up to 0.8 is `ok`. On slower machines use smaller models (whisper `small`, `gemma3:1b`) or a Google API key. Sidecar setup per OS is in [dev.md § Local AI provider](../dev.md#local-ai-provider).
- Download the models before the event, on a good connection: the setup wizard (`/setup`, the models step), `POST /api/models/{id}/download`, or `task models:pull` from a source checkout. Once downloaded, the local provider needs no internet.

### Audio input (3.5 mm line-in)

Take a **line-level** feed from the mixer: an aux send, a matrix or a record out, post-fader, with the program mix (all speaker mics, no music bed if you can avoid it).

- **Check the jack.** Many mini PCs only have a 4-pole headset jack, which is a **mic-level** input: a line-level signal will clip even at the lowest gain. Use a real line-in, a USB audio interface, or a USB adapter with a line input. A USB interface is the most predictable option.
- **Mono is fine.** The capture page mixes to mono at 16 kHz. Balanced XLR/TRS outs need an adapter or a DI; a long unbalanced cable next to power picks up hum.
- **No processing.** The capture page turns off the browser's echo cancellation, noise suppression and automatic gain, so what the mixer sends is what the AI hears. Also check the OS input settings:
  - **macOS**: set the input volume in System Settings → Sound → Input (or Audio MIDI Setup for a USB interface).
  - **Windows**: Settings → System → Sound → the input → Properties: set the volume and turn off *Audio enhancements*.
  - **Linux**: `pavucontrol` (Input Devices) or `alsamixer` (F4 for capture). Turn off any auto-gain in the input profile.

### Input gain

Set the gain with a speaker (or a recording of one) at the podium, watching the meter on the capture page, which shows the level in dBFS:

1. Start with the OS input volume around 50 % and the mixer's send at unity.
2. Normal speech should sit around **-20 to -12 dBFS** on the meter, with loud peaks below -3 dBFS.
3. If the page says **the input is clipping**, lower the mixer's send first, then the OS input volume. Clipped audio hurts recognition more than a low level does.
4. If it says **no sound is reaching the server**, the level has been below -50 dBFS for 3 seconds: check that the send is up, the cable is in the input (not the headphone jack) and the right input is picked on the page.

The dashboard card shows the same silence and clipping warnings, so the operator can see them from the admin page too.

### Server autostart

Run the server as a service with an absolute data directory (the default `./data` depends on the working directory). The examples assume `/opt/livesubs/livesubs` (or `C:\livesubs\livesubs.exe`) and ffmpeg on `PATH`.

Keys saved in the UI go to the OS keychain. A service that starts before anyone logs in (systemd system unit, Windows service) may not see a keychain: set `LIVESUBS_MASTER_KEY` so they go to the encrypted file, or pass `GEMINI_API_KEY` in the environment. Keep the file that holds them readable by the service's user only.

**Linux (systemd)**, `/etc/systemd/system/livesubs.service`:

```ini
[Unit]
Description=Live Subtitles
After=network-online.target
Wants=network-online.target

[Service]
User=livesubs
ExecStart=/opt/livesubs/livesubs --data-dir /var/lib/livesubs --models-dir /var/lib/livesubs/models
# LIVESUBS_MASTER_KEY=…, LIVESUBS_ADMIN_TOKEN=…, GEMINI_API_KEY=… (chmod 600)
EnvironmentFile=-/etc/livesubs.env
Restart=on-failure
RestartSec=2

[Install]
WantedBy=multi-user.target
```

```sh
sudo useradd --system --home /var/lib/livesubs --create-home livesubs
sudo systemctl enable --now livesubs
journalctl -u livesubs -f
```

**macOS (launchd)**, as the logged-in user so the Keychain works: `~/Library/LaunchAgents/org.livesubs.plist`, then `launchctl load -w ~/Library/LaunchAgents/org.livesubs.plist`.

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>org.livesubs</string>
  <key>ProgramArguments</key>
  <array>
    <string>/opt/livesubs/livesubs</string>
    <string>--data-dir</string><string>/Users/Shared/livesubs</string>
    <string>--ffmpeg</string><string>/opt/homebrew/bin/ffmpeg</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardErrorPath</key><string>/Users/Shared/livesubs/livesubs.log</string>
</dict>
</plist>
```

launchd doesn't read your shell's `PATH`, so give ffmpeg's full path (`which ffmpeg`). For the local provider, whisper-server and `ollama serve` need their own agents, or start them from a login item.

**Windows**: Task Scheduler → Create Task: *Run only when user is logged on* (so Credential Manager works), trigger *At log on*, action `C:\livesubs\livesubs.exe` with arguments `--data-dir C:\livesubs\data`, and on the Settings tab *If the task fails, restart every 1 minute*. Allow `livesubs.exe` through Windows Defender Firewall on private networks when it asks, for TCP 8080 and 8443 and, for SRT, UDP 9000 and up.

### Browser kiosk

Use Chrome or Edge (Chromium): the capture page needs the microphone and the Screen Wake Lock API. Open the capture page on `http://localhost:8080`, which browsers treat as a secure context, so it needs no certificate. Allow the microphone once; the browser remembers it for `localhost`.

```sh
# Stage screen, full screen on the projector (second display at x=1920)
google-chrome --kiosk --window-position=1920,0 --user-data-dir="$HOME/.livesubs-stage" \
  "http://localhost:8080/stage/main-stage?ui=es"

# Capture page as an app window on the operator's display
google-chrome --app="http://localhost:8080/capture/main-stage?token=…" \
  --user-data-dir="$HOME/.livesubs-capture"
```

On macOS the binary is `"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"`, on Windows `"C:\Program Files\Google\Chrome\Application\chrome.exe"`. A separate `--user-data-dir` per window keeps each on its own display and settings. On the stage screen, double-click or `F` toggles full screen and the cursor hides after 3 s.

Autostart the browser after the server: a `.desktop` file in `~/.config/autostart/` (Linux), a login item or a second launchd agent running `open -a "Google Chrome" --args …` (macOS), or a shortcut in `shell:startup` (Windows). The pages retry until the server answers.

### Keep it awake

The capture page holds a **screen wake lock** while it sends, but only while it is visible, and a wake lock doesn't stop the machine from sleeping when the window is hidden or the lid is closed. Turn sleep off at the OS level for the event:

- **macOS**: System Settings → Energy (or Battery → Options) → *Prevent automatic sleeping when the display is off*; or run `caffeinate -dis` in a terminal for the day.
- **Windows**: `powercfg /change standby-timeout-ac 0` and `powercfg /change monitor-timeout-ac 0`; turn off USB selective suspend in the power plan, so the USB audio interface isn't powered down.
- **Linux**: turn off automatic suspend and screen blanking in the desktop's power settings, or wrap the session in `systemd-inhibit --what=sleep:idle`.

Also postpone OS updates and silence notifications for the day.

### Network

- Put the mini PC on the event LAN with a **fixed IP** (a DHCP reservation), so the QR codes and the OBS/vMix URLs don't change. If there are several interfaces, pick one in Admin → Settings → Network, or set a public URL (such as a DNS name).
- Audience phones reach `http://<lan-ip>:8080`. Phones on a guest Wi-Fi with *client isolation* can't reach the mini PC; ask for a network where they can, or use the cloud mode.
- Open **TCP 8080** (audience, overlay), **TCP 8443** (remote capture and admin over HTTPS) and **UDP 9000+** (one per SRT session) in the host firewall.
- Remote capture stations and admin laptops use HTTPS on 8443 and need the local CA installed once: Admin → TLS has the download and the steps per OS.

## Pre-show checklist

The day before:

- [ ] `livesubs -version` is the build you tested; the server starts on boot and the browser windows come up by themselves.
- [ ] `/healthz` answers `ok` (`database` and `ffmpeg` pass; `whisper` and `ollama` too if you use the local provider).
- [ ] Each room has a session with the right name, languages and provider, and a glossary with the event's speaker and product names.
- [ ] With Gemini: Admin → Providers shows the key as valid and Gemini as the default. With local: the benchmark is `ok` on this machine.
- [ ] Recording is on (or off) as the event wants, and the disk has room: about 14 MB per hour at 32 kbit/s (Admin → Recordings shows the usage).
- [ ] The QR code is printed or shown on the stage screen, and a phone on the audience Wi-Fi opens it.

An hour before doors:

- [ ] The mini PC is on mains power, sleep is off, and the clock is right (captions and YouTube timestamps use it).
- [ ] The capture page says **Sending**, the meter moves with the room's mics, and there's no clipping or silence warning.
- [ ] **Start** the session and speak at the podium: captions appear in the viewer within a few seconds, in both languages, and on the stage screen.
- [ ] Say a sentence in the other language: the detected language on the dashboard follows.
- [ ] The overlay shows in OBS/vMix, and YouTube closed captions pass **Send test caption** ([guide](obs-vmix-guide.md)).
- [ ] With SRT audio, the dashboard card shows **SRT input: Connected** and a bitrate.
- [ ] Note the admin PIN and the admin URL somewhere the operator can find them, not on the projector.

Between talks, **Pause** stops the transcription without dropping the capture page; **Start** resumes. Keep the same session all day; each run continues the session clock, and recordings split per run and every 30 minutes.

## During the show

### Dashboard

Keep `/admin` open on the operator's screen. Each session card shows its state, input (browser, SRT, file), audio level with silence and clipping warnings, latency p95, viewers, SRT and stream-caption status, and recent errors. The admin connection (`/ws/admin`) updates it every second.

What to watch:

- **State stays `live`** during short outages: the provider or the source restarts on its own with backoff (`provider.restarting`, `source.restarting` in the log), and the gap is marked on the captions. After 5 failed attempts in a row it goes to `error` (`provider.failed`, `source.failed`) and needs a **Start**.
- **Latency p95** is from the end of the speech to the caption. With Gemini, expect a few seconds; if it keeps climbing, the uplink or the provider is struggling.
- **No capture page connected** means the capture browser is closed, asleep or offline.
- **Viewers** is how many devices follow the captions.

### Metrics

With `--metrics` (`LIVESUBS_METRICS=true`), `/metrics` serves Prometheus metrics to an admin (`Authorization: Bearer $LIVESUBS_ADMIN_TOKEN`). For a quick look without Prometheus:

```sh
curl -s -H "Authorization: Bearer $LIVESUBS_ADMIN_TOKEN" http://localhost:8080/metrics \
  | grep -E '^livesubs_(sessions|session_viewers|session_errors_total|ws_clients)'
```

`livesubs_session_errors_total` by `code` shows what went wrong, `livesubs_caption_latency_seconds` the latency per provider and track, and `livesubs_recordings_free_bytes` the disk left. The full list is in [dev.md § Logs and metrics](../dev.md#logs-and-metrics).

### Logs

The server logs to stderr: `journalctl -u livesubs -f` (Linux), the `StandardErrorPath` file (macOS), or the console window (Windows). `--log-format json` is easier for a log collector. Secrets are masked in the logs.

Useful lines: `listening` (the ports it got), `hardware check`, `srt listener open`, `srt caller rejected` (wrong SRT passphrase), `gemini live connection rotated` (normal, every ~9 minutes), `gemini live reconnected; audio was lost`, and `http request` with `status` ≥ 500.

## Troubleshooting

Error codes appear on the dashboard and in the API; the web app explains each one in the operator's language. Codes and what to do:

| Code / symptom | What it means | What to do |
|---|---|---|
| Capture page: **This page can't use the microphone** (`capture.insecure`) | The page isn't on `https://` or `http://localhost` | Open the capture link on the mini PC via `localhost`, or use HTTPS on 8443 with the local CA installed |
| `capture.permission_denied` | The browser or the OS blocked the microphone | Allow it in the address bar, and in the OS privacy settings (macOS: Privacy & Security → Microphone) |
| `capture.no_device`, `capture.device_lost`, `capture.device_busy` | No input, it was unplugged, or another program holds it | Plug the interface back in, close other audio programs, pick the input again |
| Clipping warning | The input is too hot | Lower the mixer's send, then the OS input volume |
| Silence warning, or **No capture page connected** | Nothing reaches the server | Check the send, the cable, the selected input, and that the capture page is open and says **Sending** |
| `auth.invalid_token` | The capture link's token is old | Admin → session → **Links** → **Make a new capture link**, and open the new one |
| `ingest.replaced` | A second capture page took over | Close the other one; only one page sends per session |
| `ingest.closed` | The session was stopped or deleted | Start the session again; the page reconnects by itself |
| `provider.unavailable` | The session's provider isn't set up | Gemini: save a key. Local: start whisper-server and Ollama. Or switch the session's provider |
| `provider.key_invalid`, `provider.fallback_key_invalid` | Google rejected the key; new sessions use local | Check the key in Google AI Studio and save it again in Admin → Providers |
| `provider.key_unverified`, `provider.fallback_key_unverified` | Google is unreachable, so the key can't be checked | Check the internet uplink; the server checks again by itself |
| `provider.whisper_unreachable`, `provider.ollama_unreachable` | A local sidecar is down or at the wrong URL | Start it, or fix the URL in Admin → Settings → Local provider |
| `provider.model_english_only` | whisper-server runs a `.en` model | Restart whisper-server with a multilingual model (`large-v3-turbo`) |
| `provider.error`, `provider.restarting` | The provider failed mid-talk; the session retries | Usually nothing. If it repeats, check the uplink (Gemini) or the sidecars (local) |
| `provider.failed`, `source.failed` | 5 restarts failed in a row; the session stopped | Fix the cause above, then **Start**. Switch provider if the cloud is down |
| `audio.gap` | Some seconds of audio never reached the provider | Nothing if rare. If frequent, check the capture station's network |
| `translation.failed` | One line is missing from one language | Nothing: the next lines translate. If it repeats, check the provider |
| `source.srt_unavailable` | ffmpeg has no libsrt | Install an ffmpeg with SRT (most Linux packages, the Docker image) or use browser capture |
| `source.srt_port_busy` | The session's UDP port is taken | Close the other program, or change the first SRT port in Admin → Settings |
| `source.srt_passphrase_invalid`, log `srt caller rejected` | The passphrase is too short or long, or the sender's doesn't match | Use 10–79 characters, the same on both sides |
| `source.srt_disabled` | SRT is off in the settings | Turn it on in Admin → Settings → SRT |
| `streamcc.*` | YouTube or OBS closed captions failed | See the [guide's troubleshooting](obs-vmix-guide.md#troubleshooting) |
| `secret.no_backend` | No keychain and no master key | Set `LIVESUBS_MASTER_KEY` and restart, or pass the key as `GEMINI_API_KEY` |
| `secret.read_only_env` | The key comes from an environment variable | Change it where the server is started, then restart |
| `session.state_conflict` | The action doesn't fit the state (e.g. editing languages while live) | Stop the session first, or wait for the state to settle |
| `auth.rate_limited` | Too many wrong PINs from this device | Wait, then try again |
| Phones can't open the QR link | Client isolation on the Wi-Fi, a wrong interface, or a firewall | Open the link from another laptop; check the IP in Admin → Settings → Network and the firewall on 8080 |
| `localhost:8080` shows another app | Another program holds 127.0.0.1:8080 | `lsof -nP -iTCP:8080 -sTCP:LISTEN`; stop it or start with `--addr :18080` |
| HTTPS is off (`HTTPS is off: its port is taken` in the log) | Something else listens on 8443 | Free it, or set `--https-addr :9443` |

If captions stop and the cause isn't clear: check `/healthz`, restart the capture page, then **Stop** and **Start** the session. As a last resort, restart the service: sessions, captions, settings and recordings are kept, but running sessions come back idle, so **Start** them again (SRT ports may change after a restart; check `urls.srtIngest`).
