# live-subtitles

Real-time transcription and translation for live events. See [`docs/requirements.md`](docs/requirements.md) and [`docs/plan.md`](docs/plan.md).

## Quick start

**Binary**: download the archive for your OS from the GitHub release, then run `./livesubs` (it needs `ffmpeg` on `PATH` for SRT, files and recordings) and open http://localhost:8080/setup.

**Docker**: set your LAN address in `deploy/compose/.env` (`LIVESUBS_PUBLIC_BASE_URL=http://192.168.1.20:8080`), then:

```sh
task docker:up                  # Gemini only (profile app)
task docker:up PROFILE=local    # + whisper-server and Ollama; GPU=1 for NVIDIA
```

Ports: 8080 (HTTP), 8443 (HTTPS), 9000–9009/udp (SRT). See [`docs/deployment.md`](docs/deployment.md).

## Development

Requirements: Go 1.26+, Node 22+ with pnpm, Python 3, [Task](https://taskfile.dev) (`brew install go-task`), ffmpeg.

```sh
task dev      # Go server (air) + Vite; open http://localhost:5173
task check    # everything CI runs
task build    # web app + single binary in bin/livesubs
task          # list every command
```

See [`docs/dev.md`](docs/dev.md) for the local AI provider (whisper.cpp + Ollama), test audio and troubleshooting.

## License

Apache-2.0. See [`LICENSE`](LICENSE) and [`NOTICE`](NOTICE).
