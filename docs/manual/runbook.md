# 🎛️ Event-day runbook

[← User manual](README.md) · [Broadcast guide](obs-vmix-guide.md) · [Live Subtitles](../../README.md)

**Help everyone follow the talk, from the first microphone check to the last caption.**

This guide is for the person running captions in a room. It assumes one computer beside the sound desk, with an optional projector. Install Live Subtitles using the [quick start](../../README.md#-get-started) before you begin. For a central server serving several rooms, see [deployment](../deployment.md#deployment-modes).

| When | Go to |
|---|---|
| Setting up a room for the first time | [Prepare the computer](#mini-pc-setup), [connect audio](#audio-input-35-mm-line-in) and [check the network](#network) |
| Rehearsing with the sound team | [Set the input level](#input-gain) and [test the audience screens](#audience-screens) |
| About to open the doors | [Pre-show checklist](#pre-show-checklist) |
| A talk is running | [During the show](#during-the-show) |
| Something stopped working | [Troubleshooting](#troubleshooting) |
| The event has finished | [Wrap up](#wrap-up) |
| Preparing a permanent installation | [Automatic startup and kiosk mode](#server-autostart) |

<a id="the-room-at-a-glance"></a>

## 🧭 The room at a glance

```text
Speaker microphones → Mixer → USB audio interface or line-in → Room computer
                                                                  │
                                 ┌────────────────────────────────┼─────────────┐
                                 ▼                                ▼             ▼
                           Audience phones                    Projector     OBS / vMix
                           Viewer link / QR                   Stage link    Overlay link
```

The room computer runs **Live Subtitles** and the **capture page** that sends audio. The operator uses the **admin dashboard** to start sessions and monitor captions. Audience members only need the viewer link.

If OBS or vMix sends your audio over SRT, follow the [SRT setup](obs-vmix-guide.md#srt-audio-from-obs-or-vmix) instead of the browser-capture steps below.

<!-- screenshot: room wiring from mixer to USB interface and computer, with projector and audience phones labelled -->

<a id="mini-pc-setup"></a>

## 💻 1. Prepare the computer

Do this before event day, with time for a full rehearsal.

### What to have ready

- [ ] A computer connected to mains power, with Live Subtitles installed.
- [ ] A feed from the sound mixer and a suitable audio cable or USB audio interface.
- [ ] A browser for the capture page; Chrome or Edge is a practical choice.
- [ ] A network that audience phones can use to reach the computer.
- [ ] Your admin PIN and access to the provider you plan to use.
- [ ] ffmpeg installed if you want recordings, file playback or SRT input.

<a id="hardware-and-ai-provider"></a>

### Choose and test your AI provider

| Provider | Prepare before the event | Check it works |
|---|---|---|
| ☁️ **Gemini** | Save a Google API key in **Admin → Providers**. Arrange a reliable internet connection. | Confirm the key is valid, then rehearse with live speech. Audio is processed in the cloud. |
| 🏠 **Local: Whisper + Gemma** | Install and start whisper-server and Ollama, then download the models. | Run the hardware check and **Run benchmark** in setup or the models page. Try smaller models if the computer cannot keep up. |

For local AI, open **How to install whisper-server and Ollama** on the models page for instructions for your computer. A benchmark real-time factor of **0.8 or less** is considered OK. Once the models are downloaded and both programs are running, the local provider can work without internet. The [technical setup guide](../dev.md#local-ai-provider) has more detail.

### Create the room's session

1. Open **Admin → New session** and give it a name the audience will recognize.
2. Choose the provider and caption languages for the room.
3. Add a glossary with speaker names, product names and preferred translations if needed.
4. Decide whether to record the talk. Check free space in **Admin → Recordings**.
5. Open the session's **Links** dialog. You will use its capture, audience and stage links in the next steps.

> 💡 Keep one session for the room if you want its audience link and QR code to stay the same throughout the day.

<!-- screenshot: session dialog showing room name, provider, languages and recording choice -->

<a id="audio-input-35-mm-line-in"></a>

## 🎙️ 2. Connect the room audio

1. Ask the sound engineer for a **line-level** feed from the mixer: an aux send, matrix or record output with all speaker microphones.
2. Connect it to a USB audio interface or a genuine line input on the room computer.
3. Open the session's **capture link** on that computer. When using the same computer as the server, open Admin through `http://localhost:8080` so the capture link also uses `localhost`.
4. Allow microphone access, select the interface or line input, and choose **Start sending**.
5. Speak into a room microphone and check that the level meter moves.

> ⚠️ A headset microphone socket is not a line input. Feeding the mixer's output into it can cause distortion even at low volume. Use an interface with a line input if you are unsure.

Mono audio is enough. Ask the sound engineer for the right adapter for balanced XLR/TRS outputs, keep unbalanced cables short, and leave music out of the caption feed when possible.

### Check the operating system's input settings

The capture page disables browser echo cancellation, noise suppression and automatic gain. Also check the computer's own settings:

| Computer | Where to check |
|---|---|
| **macOS** | **System Settings → Sound → Input**, or **Audio MIDI Setup** for the USB interface. |
| **Windows** | **Settings → System → Sound → input device → Properties**. Turn off **Audio enhancements**. |
| **Linux** | **Input Devices** in `pavucontrol`, or capture controls in `alsamixer`. Turn off automatic gain if offered. |

✅ **Ready when:** the capture page says **Sending**, and the meter responds to the speaker's microphone. Captions begin when you start the session in Admin.

<!-- screenshot: capture page with selected USB interface, Sending status and a healthy level meter -->

<a id="input-gain"></a>

## 🔊 3. Set the input level

Have someone speak at the podium at their normal presentation volume.

1. Start with the computer's input volume around **50%** and the mixer's send at its normal level.
2. Watch the capture meter. Aim for normal speech around **−20 to −12 dBFS**, with loud peaks below **−3 dBFS**. On this scale, numbers closer to zero are louder.
3. If the meter shows **clipping**, lower the mixer's send first, then the computer's input volume.
4. If it reports **no sound**, check the mixer send, cable and selected input before raising the gain.

Clipping means the input is too loud and distorted; it makes speech harder to recognize. The silence warning appears after roughly three seconds below −50 dBFS. Both warnings also appear on the admin dashboard.

✅ **Ready when:** normal and louder speech move the meter without a clipping warning.

<a id="network"></a>

## 🌐 4. Check the network

1. Give the room computer a stable network address. Ask the venue's network team for a **DHCP reservation** so it does not change during the event.
2. In **Admin → Settings → Network**, select the correct interface or set the public URL that audience devices should use.
3. Open the audience link on a phone connected to the **actual audience Wi-Fi**.
4. If the phone cannot connect, check whether the guest network blocks access to other devices (*client isolation*). The venue may need to change the network setup, or you may need [cloud deployment](../deployment.md#deployment-modes).

Give the network team this table if firewall changes are needed:

| Connection | Default port | Used by |
|---|---|---|
| HTTP | **TCP 8080** | Audience phones, stage screen and overlays |
| HTTPS | **TCP 8443** | Remote capture and admin access |
| SRT, if used | **One UDP port per session**, starting at 9000 | Audio from an encoder; ports 9000–9009 cover ten sessions |

For a capture page on a different computer, use **HTTPS**. If you use Live Subtitles' local certificate authority, open **Admin → TLS** and follow the certificate installation steps on that computer.

> 💡 `localhost` means “this device.” It works for capture on the server computer, but audience phones and other computers need the server's network address.

<a id="audience-screens"></a>

## 📱 5. Test the audience screens

1. **Start** the session in Admin and speak a few sentences into the room microphone.
2. Open the **audience link** from **Links**. Check the original speech and each translated language you intend to offer.
3. Scan the **QR code** with a phone on the audience Wi-Fi. Check that the captions are readable and the language controls work.
4. If you use a projector, open the **stage link** on the connected computer. Move the browser to the projector display and press **F** or double-click to enter full screen.
5. If you stream the talk, complete the [OBS / vMix / YouTube guide](obs-vmix-guide.md) and check the actual broadcast output.

✅ **Ready when:** a test sentence appears on the phone, projector and broadcast surfaces you plan to use.

<!-- screenshot: audience viewer on a phone beside the stage screen with its QR code -->

<a id="keep-it-awake"></a>

### Keep the computer awake

The capture page requests a screen wake lock while it is visible and sending. Still disable sleep at the operating-system level: hiding the page or closing a laptop lid can interrupt capture.

| Computer | What to do for the event |
|---|---|
| **macOS** | Enable **Prevent automatic sleeping when the display is off** in Energy or Battery settings. |
| **Windows** | Set sleep to **Never** while plugged in. Disable USB selective suspend so the audio interface stays powered. |
| **Linux** | Turn off automatic suspend and screen blanking in the desktop's power settings. |

Keep the computer on mains power, postpone automatic updates and silence notifications. Restore your usual power settings after the event.

<a id="pre-show-checklist"></a>

## ✅ Pre-show checklist

### The day before

- [ ] Run the same application version you rehearsed with.
- [ ] Confirm each room has the right session name, languages, provider and glossary.
- [ ] Test Gemini with the event's internet connection, or run the local-provider benchmark.
- [ ] Check recording settings and disk space. At 32 kbit/s, allow about **14 MB per hour per session**.
- [ ] Print or display the correct audience QR code and test it on the audience Wi-Fi.
- [ ] If you configured automatic startup, restart the computer and check that the server and browser windows return.

### An hour before doors open

- [ ] Connect mains power, disable sleep and check that the computer's clock is correct.
- [ ] For browser capture: confirm **Sending**, a moving meter and no clipping warning while someone speaks.
- [ ] For SRT: confirm **SRT input: Connected** and a received bitrate.
- [ ] **Start** the session and check a test sentence on every audience surface.
- [ ] Say a sentence in the other spoken language and check detection and translation.
- [ ] If broadcasting, check the overlay and use **Send test caption** for closed captions.
- [ ] Keep the admin PIN and admin URL where the operator can find them, away from the projector.

<a id="during-the-show"></a>

## ▶️ During the show

<a id="dashboard"></a>

### Watch the dashboard

Keep Admin open on the operator's display and leave the capture page running.

| What you see | What to do |
|---|---|
| **Live**, with a moving audio meter | Continue monitoring. |
| **Clipping** | Lower the mixer send or computer input level. |
| **Silence** while someone is speaking | Check the microphone, mixer send and selected input. |
| **No capture page connected** | Reopen the capture page and confirm it says **Sending**. |
| Caption latency keeps increasing | Check the internet connection for Gemini, or whether the local computer is keeping up. “p95” is the delay that 95% of measured captions fall within. |
| Provider or input **restarting** | Give automatic recovery time to work; check the connection if it repeats. |
| **Error** after repeated retries | Fix the reported cause, then choose **Start**. |
| Viewer count changes | This is the number of devices currently following the captions. |

A brief provider or input outage can leave the session **Live** while it retries. Lost audio is marked as a gap. After five failed restarts in a row, the session enters **Error** and needs operator attention.

<!-- screenshot: live session card with audio level, latency, viewer count and a recovery notice -->

### Between talks

- Choose **Pause** to pause transcription while keeping the capture page connected. Choose **Start** to resume.
- Keep the same session if you want the audience to keep using the same link and QR code.
- If you need to change languages or the audio input, **Stop** the session, edit it, then **Start** again.

Recordings may appear as several files: a new file starts with each run, every 30 minutes, and after pauses longer than five minutes. Shorter pauses are recorded as silence.

<a id="wrap-up"></a>

## 🏁 Wrap up

1. **Stop** the session when the event finishes. On the capture page, choose **Stop sending**.
2. Open **Admin → Recordings** and confirm that the expected audio is available if recording was enabled.
3. Open the replay page to review the audio and transcript. Download the captions or transcript in the formats you need.
4. Save the exports and recordings you want to keep. Check the recording retention setting; the default removes recordings after **30 days**.
5. Close the capture and stage windows, then restore the computer's normal power settings.

<!-- screenshot: completed recording with replay and transcript download options -->

<a id="troubleshooting"></a>

## 🆘 Troubleshooting

### If captions stop, check in this order

1. **Audio:** is the speaker reaching the capture meter or SRT input?
2. **Session:** is the session **Live**, or does its card show a specific error?
3. **Provider:** is Gemini reachable, or are whisper-server and Ollama running?
4. **Audience link:** do captions appear in the viewer on the server computer? If they do, check the affected phone, projector or overlay connection.
5. **Restart only what needs it:** reopen browser capture if disconnected. If the session does not recover, choose **Stop**, then **Start**.

Restart the server only as a last resort. Saved sessions, captions, settings and recordings are kept, but sessions return **idle** and need **Start** again. For SRT, copy the current address from **Links** after restarting; ports may change.

### Find the symptom

The dashboard explains errors in the selected interface language. Use the codes below to match a message to a fix.

#### 🎤 Audio and capture

| What you see | What to try |
|---|---|
| Capture page: **This page can't use the microphone** (`capture.insecure`) | Open the capture link on the mini PC via `localhost`, or use HTTPS on 8443 with the local CA installed |
| The browser or the OS blocked the microphone (`capture.permission_denied`) | Allow it in the address bar, and in the OS privacy settings (macOS: Privacy & Security → Microphone) |
| No input, it was unplugged, or another program holds it (`capture.no_device`, `capture.device_lost`, `capture.device_busy`) | Plug the interface back in, close other audio programs, pick the input again |
| Clipping warning | Lower the mixer's send, then the OS input volume |
| Silence warning, or **No capture page connected** | Check the send, the cable, the selected input, and that the capture page is open and says **Sending** |
| The capture link's token is old (`auth.invalid_token`) | Admin → session → **Links** → **Make a new capture link**, and open the new one |
| A second capture page took over (`ingest.replaced`) | Close the other one; only one page sends per session |
| The session was stopped or deleted (`ingest.closed`) | Start the session again; the page reconnects by itself |

#### 🧠 Recognition and translation

| What you see | What to try |
|---|---|
| The session's provider isn't set up (`provider.unavailable`) | Gemini: save a key. Local: start whisper-server and Ollama. Or switch the session's provider |
| Google rejected the key; new sessions use local (`provider.key_invalid`, `provider.fallback_key_invalid`) | Check the key in Google AI Studio and save it again in Admin → Providers |
| Google is unreachable, so the key can't be checked (`provider.key_unverified`, `provider.fallback_key_unverified`) | Check the internet uplink; the server checks again by itself |
| A local sidecar is down or at the wrong URL (`provider.whisper_unreachable`, `provider.ollama_unreachable`) | Start it, or fix the URL in Admin → Settings → Local provider |
| whisper-server runs a `.en` model (`provider.model_english_only`) | Restart whisper-server with a multilingual model (`large-v3-turbo`) |
| The provider failed mid-talk; the session retries (`provider.error`, `provider.restarting`) | Usually nothing. If it repeats, check the uplink (Gemini) or the sidecars (local) |
| 5 restarts failed in a row; the session stopped (`provider.failed`, `source.failed`) | Fix the cause above, then **Start**. Switch provider if the cloud is down |
| Some seconds of audio never reached the provider (`audio.gap`) | Nothing if rare. If frequent, check the capture station's network |
| One line is missing from one language (`translation.failed`) | Nothing: the next lines translate. If it repeats, check the provider |

#### 📡 SRT and broadcast captions

| What you see | What to try |
|---|---|
| ffmpeg has no libsrt (`source.srt_unavailable`) | Install an ffmpeg with SRT (most Linux packages, the Docker image) or use browser capture |
| The session's UDP port is taken (`source.srt_port_busy`) | Close the other program, or change the first SRT port in Admin → Settings |
| The passphrase is too short or long, or the sender's doesn't match (`source.srt_passphrase_invalid`, log `srt caller rejected`) | Use 10–79 characters, the same on both sides |
| SRT is off in the settings (`source.srt_disabled`) | Turn it on in Admin → Settings → SRT |
| YouTube or OBS closed captions failed (`streamcc.*`) | See the [guide's troubleshooting](obs-vmix-guide.md#troubleshooting) |

#### 🔐 Access, settings and network

| What you see | What to try |
|---|---|
| No keychain and no master key (`secret.no_backend`) | Set `LIVESUBS_MASTER_KEY` and restart, or pass the key as `GEMINI_API_KEY` |
| The key comes from an environment variable (`secret.read_only_env`) | Change it where the server is started, then restart |
| The action doesn't fit the state (e.g. editing languages while live) (`session.state_conflict`) | Stop the session first, or wait for the state to settle |
| Too many wrong PINs from this device (`auth.rate_limited`) | Wait, then try again |
| Phones can't open the QR link | Open the link from another laptop; check the IP in Admin → Settings → Network and the firewall on 8080 |
| Another program holds 127.0.0.1:8080 (`localhost:8080` shows another app) | `lsof -nP -iTCP:8080 -sTCP:LISTEN`; stop it or start with `--addr :18080` |
| HTTPS is off (`HTTPS is off: its port is taken` in the log) | Free it, or set `--https-addr :9443` |

<a id="logs"></a>

### When you need more detail

The server's console or service log records connection failures and restarts. Useful messages include `srt caller rejected` (check the SRT passphrase) and `gemini live reconnected; audio was lost` (there was an audio gap). API keys and other saved secrets are masked in logs.

<a id="metrics"></a>

For health checks, log collection and Prometheus monitoring, see the [technical guide](../dev.md#logs-and-metrics). These tools are optional for day-to-day operation.

<a id="server-autostart"></a>

## ⚙️ Optional: automatic startup

For a permanent room installation, arrange for the server to start automatically, then open the capture and stage pages. Test the entire sequence with a restart before event day.

The examples below are for the person maintaining the computer. A manual launch is enough for a rehearsal or temporary setup.

<details>
<summary>Show server startup instructions for Linux, macOS and Windows</summary>

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
sudo systemctl daemon-reload
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


</details>

<a id="browser-kiosk"></a>

### 🖥️ Optional: browser kiosk mode

Use kiosk mode when you want the projector browser to open full screen automatically. For a one-off event, opening the stage link and pressing **F** is enough.

<details>
<summary>Show browser launch commands and login startup options</summary>

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


</details>

[↑ Back to the user manual](README.md) · [Set up a broadcast →](obs-vmix-guide.md)
