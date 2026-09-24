# live-subtitles

Real-time transcription and translation for live events. See [`docs/requirements.md`](docs/requirements.md) and [`docs/plan.md`](docs/plan.md).

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
