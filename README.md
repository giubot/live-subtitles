# live-subtitles

Real-time transcription and translation for live events. See [`docs/requirements.md`](docs/requirements.md) and [`docs/plan.md`](docs/plan.md).

## Development

Requirements: Go 1.26+, Python 3, [Task](https://taskfile.dev) (`brew install go-task`).

```sh
task          # list commands
task check    # SPDX headers, gofmt, go vet, tests
task build    # bin/livesubs
```

## License

Apache-2.0. See [`LICENSE`](LICENSE) and [`NOTICE`](NOTICE).
