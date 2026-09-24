# AGENTS.md

Live Subtitles: real-time EN/ES transcription and translation for live events. One pure-Go binary (`CGO_ENABLED=0`) embeds the React app in `web/dist` and serves HTTP + WebSockets. The local AI provider uses sidecars (whisper-server, Ollama) that are not linked into the binary.

## Read before working

- **Picking up a task** (`P1-04`, "the caption bus", any phase work): [`docs/plan.md`](docs/plan.md). It has the task board, lanes, file ownership (§3.3), contract-first rules (§3.2) and the Definition of Done (§3.5).
- **Any UI change**: [`docs/design.md`](docs/design.md), with [`design/preview.html`](design/preview.html) as the visual reference.
- **Requirement IDs** (`AUD-1`, `UI-4`, `SEC-2`…): [`docs/requirements.md`](docs/requirements.md).
- **Running locally, sidecars, test audio, port clashes**: [`docs/dev.md`](docs/dev.md).
- **Commands**: `task --list`. `task check` is exactly what CI runs.

## Contract first

- `api/openapi.yaml` is the source of truth for REST and WebSocket message schemas. Change it in its own `feat(api):` commit containing only the spec plus `task gen` output, additive changes only, and land it before the code that uses it.
- If a handler needs a status code the spec doesn't list, add it to the spec first, then return the generated response type. `WriteError` is only for errors raised outside the strict handlers.
- Generated files are listed under `GENERATED` in `Taskfile.yml`. Regenerate them with `task gen` and commit the output; `check:gen` fails on drift.
- `internal/api` holds only generated code, so `internal/domain` can alias its types without an import cycle. Handlers live in `internal/api/handlers/<tag>.go`, one file per OpenAPI tag, as methods on `*Server`.
- Who may call an operation comes from its `security` in the spec: `handlers/security.go` reads it from the embedded spec and answers 401 `auth.required` to admin operations without the `ls_admin` cookie or the bearer token. Don't re-check it in handlers; read the caller with `auth.RequestFrom(ctx)`.
- An operation with no implementation yet returns `api.ErrNotImplemented`, which answers 501. Services are optional fields on `Server` (nil means 501), set in `internal/app/app.go`. That file is the wiring point the plan calls `wire.go`.
- `internal/domain` interfaces are the contract between backend packages: small, additive changes, landed first.

## Backend (Go)

- Errors carry a translatable dotted `code` (`secret.read_only_env`, `session.not_found`, `request.invalid`) plus an English `message`. The web app translates the code, so every new code needs `errors.<code>.{title,why,fix}` in both locales' `common.json`.
- Secrets never reach logs. `cmd/livesubs` wraps the logger in `secrets.RedactingHandler` (`internal/secrets/redact.go`); a secret your package holds outside the secrets store goes to the `*secrets.Redactor` with `Add`. Log secret names, never values.
- Tests are table-driven and pass under `go test -race`. The real OS keychain test only runs with `-tags keychain`.
- Use the committed 16 kHz mono fixtures in `testdata/audio/fixtures/` for audio tests. `testdata/audio/*.m4a` are gitignored downloads that CI doesn't have.
- For dependencies, prefer pure Go with no transitive bloat, and give the reason in the commit body. `air` runs through `go run` on purpose: adding it as a `go tool` pulls Hugo into `go.mod`.

## Web (`web/`)

- API calls go through `web/src/api/client.ts` (`api.useQuery('get', '/api/…')`), typed by the generated `schema.d.ts`. `task dev:web API=mock` runs the app against the Prism mock before the Go handler exists.
- Realtime goes through `web/src/realtime/`: `useCaptions(sessionId, langs)` for viewers, stage and overlay, `useAdminEvents()` for the dashboard, `createIngestSocket()` for capture. They reconnect with backoff and keep state in Zustand stores; tests use `src/test/fakeWebSocket.ts`.
- Routes in `web/src/routes/` stay thin and import from `features/`. Feature lanes edit only their own route file.
- Every user-facing string comes from i18n with both `es` and `en` keys in the feature's own namespace (`web/src/locales/<lang>/<namespace>.json`). `common.json` belongs to the shared shell. `check:i18n` enforces key parity.
- Style with the CSS variables from `web/src/theme/tokens.css` (`var(--color-ok)`, `var(--space-xs)`) or the MUI theme. `check:design` fails on raw colours or fonts outside `web/src/theme/`. A design change is its own `feat(design):` commit that updates `docs/design.md` and `tokens.css` together and regenerates `palette.ts`.
- Build new UI from the shared primitives in `web/src/components/`. If you need a variant, add it to the shared component instead of restyling locally. Check each page in light and dark.
- The stage screen (`/stage`) and the overlay (`/overlay`) never follow the UI theme.

## Every file and commit

- New source files start with `SPDX-License-Identifier: Apache-2.0` in a comment within the first 10 lines (`check:spdx`).
- Conventional Commits, scoped by area (`feat(bus):`, `fix(web):`, `feat(api):`, `ci:`, `docs:`). The body explains what changed and why, including library choices, and ends with `Refs: <task-id>`.
- History is linear: rebase on `main`, get `task check` green, then fast-forward.
- Update `docs/` in the same change when behaviour, config or commands change.
