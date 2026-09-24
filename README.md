# live-subtitles

Real-time transcription and translation for live events. See [`docs/requirements.md`](docs/requirements.md) and [`docs/plan.md`](docs/plan.md).

## Development

Requirements: Go 1.26+, Node 22+ (for `npx`), Python 3, [Task](https://taskfile.dev) (`brew install go-task`).

```sh
task          # list commands
task gen      # regenerate Go/TS code from api/openapi.yaml and palette.ts
task check    # SPDX, spec lint, codegen drift, gofmt, go vet, tests
task build    # bin/livesubs
```

## License

Apache-2.0. See [`LICENSE`](LICENSE) and [`NOTICE`](NOTICE).
