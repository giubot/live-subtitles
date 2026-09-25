<p align="center">
  <img src="web/public/favicon.svg" width="80" height="80" alt="Live Subtitles CC badge" />
</p>

# Live Subtitles 🎙️

**Let everyone follow the conversation.**

Live captions and translation for talks, conferences and live streams. Speakers talk in English or Spanish; your audience follows along on their phones, on a projector or in the broadcast.

Run it on a computer beside the sound desk, or host it centrally for several rooms. Choose cloud AI with Google Gemini or keep speech recognition and translation on your own machine.

**[Get started](#-get-started)** · **[User manual](docs/manual/README.md)** · **[Downloads](https://github.com/giubot/live-subtitles/releases)** · **[Español](#-en-español)**

<!-- screenshot: admin dashboard with live sessions, audio levels and viewer counts -->

## ✨ One talk, more ways to follow

| For… | Live Subtitles gives you… |
|---|---|
| 📱 **The audience** | A QR code to open live captions on a phone, with language and text size controls. |
| 🖥️ **The room** | Large, high-contrast captions on a projector, with an optional second language and QR code. |
| 📺 **The live stream** | Transparent overlays for OBS and vMix, plus optional YouTube closed captions. |
| 🎛️ **The operator** | A dashboard for sessions, audio levels, caption latency, viewers and errors. |
| 🎧 **After the talk** | Recorded audio with a synced transcript, plus subtitle and transcript downloads. |

<!-- screenshot: audience viewer on a phone, Spanish track -->
<!-- screenshot: stage screen on a projector with the QR corner -->
<!-- screenshot: OBS with the transparent overlay over the program video -->

### Captions that fit your event

- **English and Spanish speech**, detected automatically—even when a Q&A switches languages.
- **Translation into English and Spanish**, with Portuguese, French, German, Italian, Chinese, Japanese and Korean also available.
- **Your event vocabulary**: glossaries for preferred translations, speaker names and words that should stay unchanged.
- **Flexible audio**: a browser microphone or USB audio interface, SRT from a broadcast encoder, or a file for rehearsals.
- **Useful exports**: WebVTT, SRT, plain text and JSON for each language.
- **A familiar interface**: English, Spanish and Brazilian Portuguese, with light and dark themes.

## 🚀 Get started

### 1. Download and open

[Download a release](https://github.com/giubot/live-subtitles/releases) for **macOS, Windows or Linux**, choose your computer's architecture, and extract the archive. Check the download against the release's `checksums.txt`.

Run `./livesubs` from a terminal on macOS or Linux, or `.\livesubs.exe` from PowerShell on Windows. The web app is included.

> 💡 Browser capture and live captions work without ffmpeg. Install **ffmpeg** if you want recordings or file, URL or SRT audio sources; SRT needs an ffmpeg build with libsrt.

Prefer containers? Follow the [Docker deployment guide](docs/deployment.md#docker).

### 2. Follow the setup wizard

Open **[localhost:8080/setup](http://localhost:8080/setup)** on the computer running Live Subtitles. Choose your admin PIN, then follow the hardware check, provider setup and first-session steps. You can skip steps after the PIN and return to the settings later.

| Choose… | What you need |
|---|---|
| ☁️ **Google Gemini** | A Google API key saved in **Admin → Providers**, and a reliable internet connection. Audio is processed in the cloud. |
| 🏠 **Local AI** | whisper-server and Ollama running locally, downloaded Whisper and Gemma models, and hardware fast enough to keep up. After setup, captions work without internet. |

New sessions use Gemini when a valid Google API key is saved, and the local provider otherwise. For local AI, complete the [local provider setup](docs/dev.md#local-ai-provider) and run the hardware benchmark before your event.

### 3. Connect the sound and start a session

1. Open **Admin**, create or edit a session, and choose its languages and provider.
2. Open the session's **Links** dialog and follow the capture link on the computer connected to your microphone or mixer.
3. Allow microphone access, select the correct input and check that the level meter moves when someone speaks.
4. **Start** the session and open its audience link to check the captions.

For browser capture on another computer, use HTTPS. For mixer wiring, input levels and network setup, follow the [event-day runbook](docs/manual/runbook.md).

### 4. Share the captions

Use the session's **Links** dialog to copy the right link:

| Destination | What to do |
|---|---|
| **Audience phones** | Share the viewer link or display its QR code. Phones must be able to reach the server over the event network or your public address. |
| **Projector** | Open the stage link on the computer connected to the screen. |
| **OBS / vMix** | Add the overlay link as a browser source; follow the [broadcast guide](docs/manual/obs-vmix-guide.md). |
| **After the event** | Share the replay link if recording was enabled, or download the captions. |

## 📚 Plan the show

| Guide | When to use it |
|---|---|
| [🎛️ Event-day runbook](docs/manual/runbook.md) | Set up a room, rehearse, run the pre-show checklist and troubleshoot during a talk. |
| [📺 OBS, vMix and YouTube](docs/manual/obs-vmix-guide.md) | Add overlays, send audio over SRT and configure closed captions. |
| [📦 Deployment](docs/deployment.md) | Install with Docker, run one computer per room or host centrally. |
| [🏢 Multiple rooms](docs/scaling.md) | Plan hardware, capacity and operating costs. |

Before doors open, test with the actual microphones, a phone on the audience Wi-Fi and your broadcast setup. The [pre-show checklist](docs/manual/runbook.md#pre-show-checklist) walks you through it.

## 🛠️ Contributing

For source setup, build and test commands, API details and configuration reference, see the **[development guide](docs/dev.md)**.

## 📄 License

Apache-2.0. See [LICENSE](LICENSE) and [NOTICE](NOTICE).

## 🌎 En español

**Live Subtitles** transcribe y traduce charlas en inglés y español en tiempo real. El público sigue los subtítulos desde el celular con un QR, en una pantalla del escenario o en una transmisión con OBS o vMix.

1. [Descargá la aplicación](https://github.com/giubot/live-subtitles/releases), extraé el archivo y ejecutá `livesubs`.
2. Abrí [localhost:8080/setup](http://localhost:8080/setup), elegí el PIN y configurá Gemini o los modelos locales.
3. Creá una sesión, conectá el audio e iniciá los subtítulos.
4. Compartí el enlace o QR del público desde **Links**.

La interfaz está disponible en español, inglés y portugués de Brasil. Podés usar Gemini con internet o los modelos locales sin conexión, una vez instalados. Consultá el [manual de uso](docs/manual/README.md) y la [guía del día del evento](docs/manual/runbook.md) (en inglés).
