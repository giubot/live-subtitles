# OBS / vMix / YouTube guide

How to put Live Subtitles into a produced stream:

1. **Burn captions into the picture** with the transparent overlay, in [OBS](#obs-browser-source) or [vMix](#vmix-web-browser-input).
2. **Send the program audio** from OBS or vMix to the server [over SRT](#srt-audio-from-obs-or-vmix), instead of (or as a backup to) the line-in on the mini PC.
3. **Send closed captions to YouTube** that viewers switch on in the player, [over HTTP POST](#youtube-closed-captions-http-post) or [through OBS](#obs-sendstreamcaption-optional).

A good default is one language burned in and the other as closed captions, for example Spanish in the picture and English as CC.

In the examples the server is at `192.168.1.20:8080` and the session is `main-stage`. The admin's **Links** dialog on each session card has the exact overlay link to copy.

## The overlay

`/overlay/<session>` is a page with a transparent background that shows the last lines of one caption track. It never follows the UI theme and needs no login. It is laid out for 1920 × 1080 and scales with the height of the browser source, so a 1280 × 720 source looks the same, only smaller. It reconnects on its own if the server restarts.

```text
http://192.168.1.20:8080/overlay/main-stage?lang=es&preset=classic
```

<!-- screenshot: Admin → Overlays, preset list with the live preview and the copy button -->

### Query parameters

| Parameter | Values | Default | What |
|---|---|---|---|
| `lang` | a target language (`es`, `en`, …) or `source` | the session's first language | Which caption track to show |
| `preset` | `classic`, `outline`, `lower-third`, or a saved preset's id | `classic` | The base look; the other parameters override it |
| `fontSize` | 12–200 | 44 (38 in `lower-third`) | Text size in px at 1080p |
| `fontWeight` | 100–900 | 600 | |
| `color` | a CSS colour (`%23FFFFFF`, `yellow`, `rgb(255,220,0)`) | white | Text colour |
| `outlineColor` | a CSS colour | black | |
| `outlineWidth` | 0–12 | 1 (3 in `outline`) | Outline in px at 1080p |
| `background` | a CSS colour or `transparent` | a translucent dark box (`transparent` in `outline`) | The box behind the text |
| `position` | `bottom`, `top` | `bottom` | |
| `align` | `left`, `center`, `right` | `center` (`left` in `lower-third`) | |
| `margin` | 0–400 | 60 (80 in `lower-third`) | Distance from the edge in px at 1080p |
| `maxLines` | 1–4 | 2 | Lines on screen |
| `fadeAfter` | 0–600000 | 6000 | Hide the text after this many ms without a new caption; `0` keeps it up |
| `interim` | `0`, `1` | `1` | `0` shows only final sentences (no text rewriting itself on screen) |

Invalid values are ignored and the preset's value is used. Colours in a URL need `#` written as `%23`.

**Presets**: `classic` (white text in a translucent box, bottom centre), `outline` (outlined text with no box) and `lower-third` (left-aligned, smaller, higher margin). Admin → Overlays lists them with a live preview, and saves your own presets: a saved preset's link is `/overlay/<session>?lang=es&preset=<preset id>`, and editing the preset changes every overlay that uses it the next time it loads. Built-in presets can't be edited; duplicate one instead.

## OBS: Browser source

1. In the scene that has the program video, **Sources → + → Browser**, and name it after the language ("Subtitles ES").
2. Set:
   - **URL**: the overlay link.
   - **Width** 1920, **Height** 1080 (the canvas size; the overlay scales with the height).
   - **Custom CSS**: empty, or OBS's default, which only makes the background transparent.
   - Leave *Shutdown source when not visible* and *Refresh browser when scene becomes active* off, so the captions keep their connection across scene changes.
3. Put the source **above** the video source in the list, so it's drawn on top. Don't add a chroma key: the page is already transparent.
4. Speak into the room (or play a file into the session, see [dev.md](dev.md#feeding-a-file-to-a-session)) and check that the captions show and fade after the pause.

If the overlay stops updating after a network change, right-click the source → **Refresh**.

<!-- screenshot: OBS Browser source properties with the overlay URL, 1920 × 1080 -->
<!-- screenshot: OBS preview with the overlay over the camera -->

## vMix: Web Browser input

1. **Add Input → Web Browser**, paste the overlay link, and set the size to 1920 × 1080.
2. Use it as an **overlay channel**: click one of the overlay buttons (1–4) on the input to show it over whatever is in Program. Or add it as a **layer** on the program input (the input's **Layers** / MultiView settings), so it goes wherever that input goes.
3. The page background is transparent, so vMix shows only the text: no keying is needed.

vMix keeps the page loaded while the input exists. After a network change, right-click the input → **Reload**, or use the input's refresh button.

<!-- screenshot: vMix Add Input → Web Browser with the overlay URL -->
<!-- screenshot: vMix with the overlay on overlay channel 1 over the program -->

## Multi-language scenes

Each overlay shows one track, so add one browser source (or vMix input) per language and choose per scene which one is visible:

| Setup | Overlays |
|---|---|
| One language burned in, the other as YouTube CC | `?lang=es&preset=classic`, and the CC track `en` (below) |
| Switch languages by scene | Scene "ES": `?lang=es`; scene "EN": `?lang=en` |
| Both on screen | `?lang=es&position=bottom&maxLines=2` and `?lang=en&position=top&maxLines=2&preset=outline` |
| The original speech, whatever the language | `?lang=source` |

For a bilingual event, a scene per talk language keeps the burned-in text in the language most of the audience doesn't hear. In vMix, put each language on its own overlay channel and switch them with the overlay buttons or a shortcut.

## SRT audio from OBS or vMix

Instead of the mini PC's line-in, the production can send the mixed program audio to the server over SRT (Secure Reliable Transport). The server listens, OBS or vMix calls it.

### On the server

- ffmpeg must be built with libsrt: `ffmpeg -hide_banner -protocols | grep -w srt`. The Docker image and most Linux packages have it; Homebrew's default ffmpeg doesn't (see [dev.md § SRT ingest](dev.md#srt-ingest)).
- SRT is on by default (Admin → Settings → SRT: first UDP port, default 9000, and latency, default 200 ms).
- Each session that uses SRT gets **its own UDP port**, counting up from the first port (9000, 9001, …). Open those UDP ports in the firewall; 9000–9009 covers ten sessions.
- In the session dialog (Admin → Sessions → edit), set **Audio input** to *SRT from an encoder (OBS, vMix…)*. The session's links then show **SRT input for the encoder**, the address to paste into OBS or vMix (for example `srt://192.168.1.20:9000?streamid=main-stage`). Start the session: it goes live and waits for the sender. The choice is kept per session in that browser.
- The dashboard card then shows **SRT input**: *Connected* or *Disconnected*, and the received bitrate.
- From a script, the address is `urls.srtIngest` in the session, and the start takes the source in its body:

  ```sh
  curl -s -H "Authorization: Bearer $LIVESUBS_ADMIN_TOKEN" \
    http://192.168.1.20:8080/api/sessions/main-stage | jq -r .urls.srtIngest
  # srt://192.168.1.20:9000?streamid=main-stage
  curl -X POST -H "Authorization: Bearer $LIVESUBS_ADMIN_TOKEN" -H "Content-Type: application/json" \
    -d '{"source":"srt"}' http://192.168.1.20:8080/api/sessions/main-stage/start
  ```

- **Optional encryption**: set a passphrase of 10–79 characters in Admin → Settings → SRT passphrase (or `PUT /api/secrets/srt_passphrase` with `{"value":"…"}`, or `LIVESUBS_SECRET_SRT_PASSPHRASE`) and set the same one on the sender. Without it, only unencrypted senders are accepted.

The listener takes one sender at a time. When the sender disconnects, the listener reopens and the session stays live; the gap is marked. The `streamid` in the URL is sent but not checked yet.

### vMix

vMix sends SRT from its outputs, separately from streaming to YouTube:

1. **Settings → Outputs / NDI / SRT**, pick an output (e.g. Output 1, source *Program*), open its settings and enable **SRT**.
2. **Type** *Caller*, **Hostname** `192.168.1.20`, **Port** the session's port (`9000`), **Latency** 200 ms (or more on Wi-Fi), **Passphrase** the same as the server's (or empty), **Stream ID** `main-stage`.
3. Any codec vMix offers works; the server only decodes the audio (AAC, MP2, Opus or AC-3 in MPEG-TS) and ignores the video. Pick the lowest video quality to save bandwidth.

<!-- screenshot: vMix SRT output settings in Caller mode -->

### OBS

OBS streams to one service at a time, so how you send SRT depends on whether the same OBS also streams to YouTube:

- **OBS only feeds the server** (a separate OBS instance, or a rehearsal): **Settings → Stream → Service** *Custom…*, **Server** `srt://192.168.1.20:9000?streamid=main-stage` (add `&passphrase=…` if one is set, and `&latency=200000` for the sender's latency in microseconds), **Stream Key** empty, then **Start Streaming**. OBS sends MPEG-TS over SRT with its default AAC audio.
- **OBS also streams to YouTube**: send SRT as a second output. With **Settings → Output → Output Mode: Advanced → Recording**, set **Type** *Custom Output (FFmpeg)*, **FFmpeg Output Type** *Output to URL*, **File path or URL** the SRT URL above, **Container Format** `mpegts`, and an AAC audio encoder; then **Start Recording** to start sending. A multistream plugin that supports SRT works too.

<!-- screenshot: OBS Stream settings with the srt:// server -->

Test the path from any machine with ffmpeg:

```sh
ffmpeg -re -i testdata/audio/fixtures/en.wav -c:a aac -f mpegts 'srt://192.168.1.20:9000?streamid=main-stage'
```

## YouTube closed captions (HTTP POST)

The server posts the final captions of **one track** straight to YouTube's caption ingestion URL. It works the same whether the video comes from OBS, vMix or a hardware encoder, because it doesn't go through the video. Viewers turn the captions on with the CC button in the player.

YouTube accepts **one caption track per stream**, and only final captions are sent: live captions can't revise text already shown.

### 1. Enable "POST captions to URL" in YouTube

1. In **YouTube Studio → Go live** (Live Control Room), open the stream's settings.
2. Under **Closed captions**, turn captions on and pick **POST captions to URL**.
3. Copy the **caption ingestion URL** (it starts with `http://upload.youtube.com/closedcaption?cid=…`). It is a secret: anyone who has it can post captions to your stream.
4. Pick the latency: **Ultra low-latency** doesn't support captions; use normal or low latency.

<!-- screenshot: YouTube Live Control Room, Closed captions set to POST captions to URL, with the ingestion URL -->

### 2. Set the broadcast delay

Captions are placed by the time the words were spoken, so they must reach YouTube before the video they belong to. Add a **30 to 60 second delay** to the video: in OBS, **Settings → Advanced → Stream Delay** (e.g. 30 s); in vMix or a hardware encoder, use its delay setting if it has one. YouTube viewers are already several seconds behind, so they barely notice.

### 3. Paste the ingestion URL in the admin

1. In `/admin`, edit the session (captions in the live stream are set on an existing session).
2. Under **Captions in the live stream**: turn on **Send captions to the stream**, target **YouTube (HTTP ingestion URL)**, the **Caption track** (for example `en`, or `source` for the original speech), and paste the **YouTube caption ingestion URL**.
3. **Send test caption**. With the stream live in YouTube, the test line shows in the player (with CC on) after the delay. The card shows **Stream captions** with the state, the last sequence number and the last send.

The URL is stored like an API key (keychain or encrypted file), shown only as its last 4 characters, and masked in logs. Each YouTube stream (and so each session) has its own URL; a new YouTube event gives a new URL, so paste it again. From a script: `PUT /api/sessions/main-stage/stream-captions/youtube-url` with `{"value":"…"}`, then `POST /api/sessions/main-stage/stream-captions/test`.

<!-- screenshot: the session dialog, Captions in the live stream, with the test button -->

### 4. Choose the burned-in and the CC language

- The **overlay** decides what is burned in (`?lang=es`); the session's **caption track** decides what goes to CC (`en`). Everyone sees the burned-in language; CC is for those who turn it on.
- Burn in the language most remote viewers read, and send the other as CC. For a Spanish talk with an English audience online, `lang=source` on the overlay and `en` as CC also works.
- Line length defaults to 32 characters per line, two lines per cue.

The configuration is read when the session starts, so turn it on **before** Start. A URL saved or removed while the session runs applies from the next caption.

## OBS SendStreamCaption (optional)

With OBS, captions can also go into the stream itself: the server sends them to OBS over obs-websocket v5, and OBS embeds them as **CEA-608** in the video it streams. YouTube and Twitch show them; Vimeo doesn't. Use it when the platform has no caption ingestion URL, or you'd rather not handle one.

1. In OBS, **Tools → WebSocket Server Settings**: check **Enable WebSocket server**, keep port **4455**, and (recommended) **Enable Authentication**. **Show Connect Info** has the password.
2. In Live Subtitles, **Admin → Settings → OBS**: the **OBS WebSocket address**, `ws://127.0.0.1:4455` when OBS runs on the same machine, or `ws://<obs-machine-ip>:4455` (open TCP 4455 on that machine). Then **Admin → Providers**: save the **OBS WebSocket password** (or set `LIVESUBS_SECRET_OBS_WEBSOCKET_PASSWORD`).
3. Edit the session: **Captions in the live stream** → target **OBS (WebSocket, CEA-608)**, and the caption track.
4. On YouTube, set the stream's **Closed captions** to **Embedded 608/708** instead of POST.
5. Start streaming in OBS, then **Send test caption**. OBS only accepts captions while it is streaming.

Lines are at most 32 characters (the CEA-608 limit), two per caption; accents and ñ, ¿, ¡ come through. Each caption stays up for as long as its words took, between 1.5 and 4 seconds.

## Troubleshooting

| Symptom / code | Cause | Fix |
|---|---|---|
| The overlay is blank | No captions yet, the wrong `lang`, or it faded after `fadeAfter` ms of silence | Speak, or check the track in `/s/<session>?lang=…`; try `fadeAfter=0` while testing |
| The overlay has a black or white background | Custom CSS in OBS, or a `background=` colour | Clear the Custom CSS; use `background=transparent` or the `outline` preset |
| Captions are too small or cut off | The source isn't 1920 × 1080, or it's cropped | Set the source to the canvas size; the text scales with the height |
| Text rewrites itself on screen | Interim captions | Add `interim=0` to show only final sentences |
| `overlay.not_found` / the look is the classic one | The `preset` id doesn't exist | Check the id in Admin → Overlays |
| SRT: the card says *Disconnected* | The sender can't reach the port, or the session wasn't started with `{"source":"srt"}` | Check the IP, the port from `urls.srtIngest`, and that UDP is open in the firewall; start the session with the SRT source |
| `source.srt_unavailable`, no `srtIngest` URL | The server's ffmpeg has no libsrt, or SRT is off | Install an ffmpeg with SRT; turn SRT on in Settings |
| `source.srt_port_busy` | Another program (or another server) uses the port | Stop it, or change the first SRT port |
| Log `srt caller rejected` | The sender's passphrase doesn't match | Set the same passphrase (10–79 characters) on both sides, or none on either |
| `streamcc.no_url` | No ingestion URL saved for the session | Paste it in the session's stream captions |
| `streamcc.url_invalid` | The pasted text isn't a full URL | Copy the whole URL from Live Control Room again |
| `streamcc.rejected` | YouTube refused the caption (4xx): the stream isn't live, the URL belongs to another event, or POST captions is off | Go live, check the Closed captions setting, paste the current URL, send a test caption |
| `streamcc.server_error`, `streamcc.unreachable` | YouTube is unreachable or answering 5xx | Nothing: captions are retried with backoff. Check the server's internet uplink |
| `streamcc.dropped` | Captions waited more than 60 s and were skipped | Recovers by itself once YouTube answers; check the uplink |
| CC appear too early or late | The broadcast delay is missing or too short | Set 30–60 s of stream delay; `clockOffsetMs` in the session status shows the server's clock against YouTube's |
| `streamcc.obs_unreachable` | OBS is closed, its websocket server is off, or the address is wrong | Open OBS, enable the websocket server, check the address in Settings |
| `streamcc.obs_auth_failed` | The OBS password is missing or wrong | Copy it from **Show Connect Info** and save it in Admin → Providers |
| `streamcc.obs_rejected` | OBS isn't streaming | Start streaming, then send a test caption |
| CEA-608 captions don't show on YouTube | The stream expects POST captions | Set Closed captions to **Embedded 608/708** |
