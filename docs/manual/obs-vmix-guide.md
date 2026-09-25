# 📺 OBS, vMix and YouTube guide

[← User manual](README.md) · [Event-day runbook](runbook.md) · [Live Subtitles](../../README.md)

**Bring live captions into your broadcast.** Start with the option you need; you do not have to configure every part of this guide.

| I want to… | Follow this path |
|---|---|
| Show captions over the video in OBS | [Copy an overlay link](#the-overlay) → [add it to OBS](#obs-browser-source) |
| Show captions over the video in vMix | [Copy an overlay link](#the-overlay) → [add it to vMix](#vmix-web-browser-input) |
| Let YouTube viewers turn captions on and off | [Send captions directly to YouTube](#youtube-closed-captions-http-post) |
| Send closed captions through OBS | [Set up OBS embedded captions](#obs-sendstreamcaption-optional) |
| Send audio from the production computer to Live Subtitles | [Set up SRT audio](#srt-audio-from-obs-or-vmix) |
| Offer more than one caption language | [Choose a language layout](#multi-language-scenes) |
| Fix a missing overlay or caption feed | [Troubleshooting](#troubleshooting) |

## 🧭 Understand the three connections

| Connection | What it carries | What the audience sees |
|---|---|---|
| **Overlay** | Captions from Live Subtitles into an OBS browser source or vMix browser input | Text permanently visible in the picture. Viewers cannot turn it off. |
| **Closed captions (CC)** | Captions sent to YouTube directly, or embedded in the stream through OBS | Text viewers can turn on or off with the player's **CC** control. |
| **SRT audio** | Program audio from OBS or vMix into Live Subtitles | No visible change by itself; this supplies the speech to transcribe. |

You can use an overlay and CC together—for example, Spanish in the picture and English as optional CC. **Adding an overlay does not send audio to Live Subtitles.** Keep browser capture running, or configure SRT separately.

## 🎒 Before you begin

- [ ] Start Live Subtitles and create a session with the languages you need.
- [ ] Test your audio and provider using the [event-day runbook](runbook.md). If you will use SRT for audio, configure [that connection](#srt-audio-from-obs-or-vmix) first.
- [ ] Check that captions appear in the session's audience viewer.
- [ ] Confirm the OBS or vMix computer can open that viewer link.
- [ ] For YouTube CC, have access to the correct event in YouTube Studio.

Examples use **`192.168.1.20`** for the Live Subtitles computer and **`main-stage`** for the session. Replace both with your own values. Copy links from the session's **Links** dialog whenever possible. Use `localhost` only when the application opening the link is on the same computer as Live Subtitles.

<a id="the-overlay"></a>

## 🔗 Copy and style your overlay link

1. In **Admin**, open the session's **Links** dialog and copy its overlay link.
2. Select the caption language you want to display. In a link, `lang=es` selects Spanish, `lang=en` selects English, and `lang=source` follows the original speech.
3. Open **Admin → Overlays** to preview the available styles. Start with **Classic box** if you are unsure.
4. Open the link in a browser and speak into the session. Check the text before adding it to the broadcast.

```text
http://192.168.1.20:8080/overlay/main-stage?lang=es&preset=classic
```

| Preset | Appearance |
|---|---|
| `classic` | White text in a translucent dark box, at the bottom centre. |
| `outline` | Outlined text without a box. |
| `lower-third` | Smaller, left-aligned text with more space from the edge. |

Duplicate a built-in preset to customize it. Saved presets have their own IDs; editing one updates overlays using it **the next time they load**, so refresh the browser source after saving.

The overlay has a transparent background, needs no login, and keeps its appearance regardless of the app's light or dark theme. It reconnects if the server restarts. The standard layout is **1920 × 1080**; text scales with the source height.

> 💡 The overlay may be blank while nobody is speaking. By default, text disappears after six seconds without a new caption. Add `&fadeAfter=0` to the link if you want text to stay visible while setting up.

<!-- screenshot: Admin → Overlays with the preset list, live preview and copy button -->

<a id="obs-browser-source"></a>

## 🎬 Add captions to OBS

### 1. Create the browser source

Open the scene with your program video. Choose **Sources → + → Browser** and give it a clear name, such as **Subtitles ES**.

### 2. Enter the source settings

| Setting | Value |
|---|---|
| **URL** | Your session's overlay link |
| **Width / Height** | **1920 / 1080** for a 1080p canvas; match your canvas if different |
| **Custom CSS** | Leave empty, or keep OBS's default transparent-background CSS |
| **Shutdown source when not visible** | Off |
| **Refresh browser when scene becomes active** | Off |

Keeping the last two options off helps the caption connection stay open through scene changes. See the [OBS browser-source reference](https://obsproject.com/kb/browser-source) for these controls.

### 3. Place it above the video

Move the caption source **above the camera or program video** in the Sources list. Fit it to the canvas without cropping. The page is already transparent, so no chroma key is needed.

### 4. Test a sentence

Speak into the session and check the OBS preview. Switch scenes and back, then confirm new captions still arrive. Use the source's refresh control if it stops updating after a network change.

✅ **Ready when:** captions appear over the picture at a readable size, without covering important graphics.

<!-- screenshot: OBS Browser source properties with overlay URL, size and the two unchecked options -->
<!-- screenshot: OBS preview with captions over the camera and the source above the video in the Sources list -->

<a id="vmix-web-browser-input"></a>

## 🎥 Add captions to vMix

1. Choose **Add Input → Web Browser**.
2. Paste the overlay link and set **Width / Height** to **1920 / 1080** for a 1080p production.
3. Choose how the captions should appear using the table below.
4. Speak into the session and check the **Program** output.

| Use… | When… | How |
|---|---|---|
| **An overlay channel** | Captions should stay over whichever input is in Program | Click an overlay button **1–4** on the caption input. |
| **A layer on an input** | Captions should appear only with a particular program input | Add the caption input in that input's **Layers / MultiView** settings. |

The [vMix Web Browser input](https://www.vmix.com/help28/WebBrowser.html) supports transparency, so no keying is needed. Keep the input loaded; use its refresh/reload control after a network change if necessary.

✅ **Ready when:** captions appear in Program and remain visible across the input changes where you want them.

<!-- screenshot: vMix Add Input → Web Browser with the overlay URL and size -->
<!-- screenshot: vMix Program showing captions on overlay channel 1 -->

<a id="multi-language-scenes"></a>

## 🌍 Choose your language layout

Each overlay displays **one caption track**. Add a separate source or input for each language you want on screen.

| Goal | Setup |
|---|---|
| Spanish on screen, English as optional CC | Overlay with `?lang=es&preset=classic`; choose **English** as the stream caption track. |
| Switch languages between scenes | One scene with `?lang=es`; another with `?lang=en`. |
| Show both languages at once | Spanish: `?lang=es&position=bottom&maxLines=2`; English: `?lang=en&position=top&maxLines=2&preset=outline`. |
| Show the original speech | Use `?lang=source`. |

In vMix, you can assign each language to a separate overlay channel and switch with the overlay buttons. Rehearse transitions so two sources do not unexpectedly cover each other or the speaker's name graphic.

<a id="srt-audio-from-obs-or-vmix"></a>

## 📡 Optional: send program audio over SRT

Use this when OBS or vMix already has the mixed audio and you want to send it to Live Subtitles over the network. **Live Subtitles listens; the encoder connects as a caller.** This replaces browser capture as the session's input.

<a id="on-the-server"></a>

### 1. Prepare Live Subtitles

1. Confirm the server has **ffmpeg with SRT support**. If SRT is unavailable in the session dialog, see [SRT installation details](../dev.md#srt-ingest).
2. In **Admin → Settings → SRT**, enable SRT and check the first port and latency. Defaults are **UDP 9000** and **200 ms**.
3. Stop the session if it is running, then edit it. Set **Audio input** to **SRT from an encoder (OBS, vMix…)**.
4. Open **Links → SRT input for the encoder** and copy the address. Each session gets its own port, so always use the address shown for that session.
5. Allow that UDP port through the server's firewall. If using encryption, set the SRT passphrase in Settings and use the same **10–79 character** passphrase on the sender.
6. **Start** the session. It waits for the encoder to connect.

```text
srt://192.168.1.20:9000?streamid=main-stage
```

The input choice is remembered in the admin browser you used. Check it again if you switch to a different browser or computer. Only one SRT sender can connect to a session at a time.

<a id="vmix"></a>

### 2A. Send from vMix

1. Open **Settings → Outputs / NDI / SRT** and choose the output carrying your program audio.
2. Open its settings and enable **SRT**.
3. Enter the connection settings below and apply them.

| Setting | Value |
|---|---|
| **Type** | Caller |
| **Hostname** | Live Subtitles computer's address, such as `192.168.1.20` |
| **Port** | The port from the session's SRT link |
| **Latency** | Start with **200 ms**; increase if the connection needs more buffering |
| **Passphrase** | Match the server, or leave empty on both ends |
| **Stream ID** | The value in the link, such as `main-stage` |

Use AAC audio. Live Subtitles ignores the video, so a low video quality reduces unnecessary bandwidth. vMix can send SRT from an output separately from its YouTube stream. See the [vMix SRT reference](https://www.vmix.com/help28/SRT.html).

<!-- screenshot: vMix SRT output settings in Caller mode with host, port, latency and audio selection -->

<a id="obs"></a>

### 2B. Send from OBS

Choose the setup that matches your production:

**If OBS only sends to Live Subtitles**, such as during rehearsal:

1. Open **Settings → Stream** and choose **Service → Custom…**.
2. Paste the session's SRT link into **Server**. Leave **Stream Key** empty.
3. Add `&latency=200000` for 200 ms of sender latency. If using encryption, also add `&passphrase=…` with your URL-encoded passphrase.
4. Choose **Start Streaming**.

OBS accepts SRT links in its custom streaming settings. Its URL latency value is in **microseconds**, while the Live Subtitles and vMix settings use **milliseconds**. See the [OBS SRT guide](https://obsproject.com/kb/srt-protocol-streaming-guide).

**If OBS also streams to YouTube**, keep the YouTube streaming destination and configure SRT as a second output:

1. Open **Settings → Output**, choose **Output Mode: Advanced**, then open **Recording**.
2. Set **Type → Custom Output (FFmpeg)** and **FFmpeg Output Type → Output to URL**.
3. Paste the SRT link into **File path or URL**, set **Container Format** to `mpegts`, and choose an **AAC audio encoder**.
4. Choose **Start Recording** to send the SRT output, while **Start Streaming** sends to YouTube.

> 💡 In this setup, OBS's Recording output sends to the SRT URL instead of saving a local recording. Plan a separate recording method if you need one.

<!-- screenshot: OBS custom stream settings for SRT -->
<!-- screenshot: OBS Advanced Recording settings configured as a second SRT output -->

### 3. Confirm audio is arriving

Speak into the program microphones. The session card should show **SRT input: Connected**, a received bitrate and then captions in the audience viewer.

If the sender disconnects, Live Subtitles waits for it to reconnect and marks any missing audio as a gap. After a server restart, recheck the link in **Links** because the session's port may change.

<a id="youtube-closed-captions-http-post"></a>

## 💬 Send closed captions directly to YouTube

This sends one track of final captions from Live Subtitles to YouTube. It works whether your video comes from OBS, vMix or a hardware encoder. Viewers enable the text using the player's **CC** button.

Use **one caption delivery method** for the YouTube event: the direct URL method here, or the [OBS embedded method](#obs-sendstreamcaption-optional) below. YouTube accepts one caption feed per stream. See [YouTube's live-caption requirements](https://support.google.com/youtube/answer/3068031?hl=en).

<a id="1-enable-post-captions-to-url-in-youtube"></a>

### 1. Enable captions in YouTube

1. In **YouTube Studio → Go live**, open the settings for the correct event.
2. Turn **Closed captions** on and choose **POST captions to URL**.
3. Copy the **caption ingestion URL**.
4. Start with **Normal latency** for rehearsal, then test your intended production setting. Review the tradeoffs in [YouTube's latency settings](https://support.google.com/youtube/answer/7444635?hl=en).

> 🔐 Treat the caption ingestion URL like a password: it allows captions to be posted to your stream. Keep it out of screenshots and shared documents.

<!-- screenshot: YouTube Closed captions set to POST captions to URL, with the ingestion URL fully hidden -->

<a id="2-set-the-broadcast-delay"></a>

### 2. Allow time for captions to arrive

Add **30–60 seconds of broadcast delay** so captions can reach YouTube before the corresponding video. In OBS, use **Settings → Advanced → Stream Delay**. For another encoder, use its supported delay controls and rehearse the result in the YouTube player.

The delay also affects live audience interaction, so account for it when taking questions from the stream. Check that the Live Subtitles computer's clock is correct.

<a id="3-paste-the-ingestion-url-in-the-admin"></a>

### 3. Configure the session before starting it

1. In Live Subtitles, stop the session if necessary, then edit it.
2. Under **Captions in the live stream**, enable **Send captions to the stream**.
3. Choose **YouTube (HTTP ingestion URL)** as the target.
4. Select the **Caption track**, such as English or the original speech.
5. Paste the **YouTube caption ingestion URL** and save.
6. **Start** the session, then make sure your video stream is live in YouTube.

Set the target and language before starting the session. A saved or removed YouTube URL takes effect from the next caption; other stream-caption settings are read when the session starts. When you create a new YouTube event, copy its new URL into the session.

### 4. Test from the viewer's side

1. Choose **Send test caption** in Live Subtitles.
2. Open the YouTube player and turn **CC** on.
3. Wait for the broadcast delay, then look for the test line.
4. Speak a sentence and check both the language and timing.

✅ **Ready when:** the test and spoken captions appear in the YouTube player with CC enabled. The session card's **Stream captions** status helps identify sending errors.

<a id="4-choose-the-burned-in-and-the-cc-language"></a>

Only final captions are sent; text already delivered cannot be revised. The default is two lines of up to 32 characters each. The overlay language is chosen separately, so you can keep Spanish in the picture and English in CC.

<!-- screenshot: session stream-caption settings with caption track, status and Send test caption; hide the URL -->
<!-- screenshot: YouTube player with CC enabled and a test caption visible -->

<a id="obs-sendstreamcaption-optional"></a>

## 🔌 Alternative: send closed captions through OBS

Live Subtitles can send captions to OBS through its WebSocket connection. OBS embeds them as **CEA-608** captions in the stream. Use this with a streaming destination that accepts embedded captions.

### 1. Connect Live Subtitles to OBS

1. In OBS, open **Tools → WebSocket Server Settings**.
2. Enable the server, keep port **4455**, and enable authentication. Copy the password from **Show Connect Info**.
3. In Live Subtitles, open **Admin → Settings → OBS**. Set the address to `ws://127.0.0.1:4455` if OBS runs on the same computer, or `ws://<obs-computer-address>:4455` otherwise.
4. If they run on different computers, allow TCP 4455 through the OBS computer's firewall.
5. Save the OBS password in **Admin → Providers**.

### 2. Choose the caption target

1. Stop the Live Subtitles session if it is running, then edit it.
2. Under **Captions in the live stream**, enable sending, choose **OBS (WebSocket, CEA-608)** and select the caption track. Save the changes.
3. For YouTube, choose **Embedded 608/708** in the stream's Closed captions settings.
4. Start the session and start streaming in OBS.
5. Choose **Send test caption**, then check the destination's player with **CC** enabled.

OBS accepts stream captions only while it is streaming. Lines are limited to 32 characters, two per caption; the app keeps each caption visible for 1.5–4 seconds according to its length.

✅ **Ready when:** the test caption reaches the destination player, not just the OBS connection status.

<!-- screenshot: OBS WebSocket settings with authentication enabled and password hidden -->

## ✅ Before going live

- [ ] A spoken sentence reaches the Live Subtitles audience viewer.
- [ ] The overlay is readable in OBS Program or vMix Program, with the intended language.
- [ ] Captions do not overlap names, slides or other graphics.
- [ ] Scene or input changes keep captions visible where intended.
- [ ] If using SRT, the session shows **Connected** and receives the correct audio mix.
- [ ] If using CC, **Send test caption** succeeds and the test appears in the destination player.
- [ ] Closed-caption timing matches the spoken video after the configured delay.
- [ ] Screenshot captures and shared notes do not expose passwords or the YouTube ingestion URL.

<a id="troubleshooting"></a>

## 🆘 Troubleshooting

**First check the audience viewer.** If it has no captions either, start with the [runbook's audio and provider checks](runbook.md#troubleshooting). If the viewer works, use the relevant table below.

### 🖼️ Overlay appearance

| What you see | What to try |
|---|---|
| The overlay is blank | Speak, or check the track in `/s/<session>?lang=…`; try `fadeAfter=0` while testing |
| The overlay has a black or white background | Clear the Custom CSS; use `background=transparent` or the `outline` preset |
| Captions are too small or cut off | Set the source to the canvas size; the text scales with the height |
| Text rewrites itself on screen | Add `interim=0` to show only final sentences |
| The `preset` id doesn't exist (`overlay.not_found` / the look is the classic one) | Check the id in Admin → Overlays |

### 📡 SRT audio

| What you see | What to try |
|---|---|
| SRT: the card says *Disconnected* | Check the IP, the port in **Links → SRT input for the encoder**, and that UDP is open in the firewall; start the session with the SRT source |
| The server's ffmpeg has no libsrt, or SRT is off (`source.srt_unavailable`, no SRT link) | Install an ffmpeg with SRT; turn SRT on in Settings |
| Another program (or another server) uses the port (`source.srt_port_busy`) | Stop it, or change the first SRT port |
| Log `srt caller rejected` | Set the same passphrase (10–79 characters) on both sides, or none on either |

### 💬 YouTube captions

| What you see | What to try |
|---|---|
| No ingestion URL saved for the session (`streamcc.no_url`) | Paste it in the session's stream captions |
| The pasted text isn't a full URL (`streamcc.url_invalid`) | Copy the whole URL from Live Control Room again |
| YouTube refused the caption (4xx): the stream isn't live, the URL belongs to another event, or POST captions is off (`streamcc.rejected`) | Go live, check the Closed captions setting, paste the current URL, send a test caption |
| YouTube is unreachable or answering 5xx (`streamcc.server_error`, `streamcc.unreachable`) | Nothing: captions are retried with backoff. Check the server's internet uplink |
| Captions waited more than 60 s and were skipped (`streamcc.dropped`) | Recovers by itself once YouTube answers; check the uplink |
| CC appear too early or late | Set 30–60 s of stream delay; check that the server clock is correct |

### 🔌 OBS embedded captions

| What you see | What to try |
|---|---|
| OBS is closed, its websocket server is off, or the address is wrong (`streamcc.obs_unreachable`) | Open OBS, enable the websocket server, check the address in Settings |
| The OBS password is missing or wrong (`streamcc.obs_auth_failed`) | Copy it from **Show Connect Info** and save it in Admin → Providers |
| OBS isn't streaming (`streamcc.obs_rejected`) | Start streaming, then send a test caption |
| CEA-608 captions don't show on YouTube | Set Closed captions to **Embedded 608/708** |

<a id="query-parameters"></a>

## 🎨 Advanced: customize the overlay link

Start with a preset in **Admin → Overlays**. For a one-off change, add parameters to the link: the first begins with `?`, and each additional parameter begins with `&`.

For example, this shows Spanish final captions without interim rewrites and keeps them visible during silence:

```text
http://192.168.1.20:8080/overlay/main-stage?lang=es&preset=classic&interim=0&fadeAfter=0
```

<details>
<summary>Show all overlay link parameters</summary>

| Parameter | Values | Default | What |
|---|---|---|---|
| `lang` | a target language (`es`, `en`, …) or `source` | the session's first language | Which caption track to show |
| `preset` | `classic`, `outline`, `lower-third`, or a saved preset's id | `classic` | The base look; the other parameters override it |
| `fontSize` | 12–200 | 44 (38 in `lower-third`) | Text size in px at 1080p |
| `fontWeight` | 100–900 | 600 | Text weight; higher values are bolder |
| `color` | a CSS colour (`%23FFFFFF`, `yellow`, `rgb(255,220,0)`) | white | Text colour |
| `outlineColor` | a CSS colour | black | Colour of the text outline |
| `outlineWidth` | 0–12 | 1 (3 in `outline`) | Outline in px at 1080p |
| `background` | a CSS colour or `transparent` | a translucent dark box (`transparent` in `outline`) | The box behind the text |
| `position` | `bottom`, `top` | `bottom` | Which edge of the picture to use |
| `align` | `left`, `center`, `right` | `center` (`left` in `lower-third`) | Text alignment |
| `margin` | 0–400 | 60 (80 in `lower-third`) | Distance from the edge in px at 1080p |
| `maxLines` | 1–4 | 2 | Lines on screen |
| `fadeAfter` | 0–600000 | 6000 | Hide the text after this many ms without a new caption; `0` keeps it up |
| `interim` | `0`, `1` | `1` | `0` shows only final sentences (no text rewriting itself on screen) |

Invalid values are ignored and the preset's value is used. Colours in a URL need `#` written as `%23`.


</details>

For API automation and command-line SRT tests, see the [development guide](../dev.md#srt-ingest) and the [API reference](../../api/openapi.yaml).

[↑ Back to the user manual](README.md) · [Event-day checklist →](runbook.md#pre-show-checklist)
