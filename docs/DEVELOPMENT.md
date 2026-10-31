# Developing Flint

This guide covers building, running and testing Flint from source. For how the code is organized see [ARCHITECTURE.md](ARCHITECTURE.md); for contribution rules see [CONTRIBUTING.md](../CONTRIBUTING.md).

## Requirements

- macOS 12 or later
- Go 1.27.3+
- Node 22+ (only for type checking and running frontend tests; there is no npm build step)
- Xcode Command Line Tools (`xcode-select --install`), needed for cgo
- `wails3` v3.0.0:

  ```bash
  go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0
  ```

  The Makefile expects it at `$(go env GOPATH)/bin/wails3`; override with `make WAILS=/path/to/wails3 …`.

Optional tools, used by the features that need them:

- `ripgrep` (`brew install ripgrep`): fast search and `⌘P`; without it a slower Go fallback is used
- `gh`: pull request review
- `kubectl`, `docker`, `aws` (and `saml2aws` if you use it): the infrastructure panels
- `dlv` (`go install github.com/go-delve/delve/cmd/dlv@latest`): the debugger
- language servers (`gopls`, `rust-analyzer`, `vtsls` or `typescript-language-server`, `basedpyright` or `pyright`)
- `claude` and/or `codex`: the agent chat
- Docker: database integration tests

## First setup

```bash
git clone https://github.com/ODudek/flint.git
cd flint
cd frontend && npm ci --ignore-scripts && cd ..
make dev
```

`frontend/.npmrc` already sets `ignore-scripts=true`; pass `--ignore-scripts` anyway so the policy holds even if that file is bypassed.

## Make targets

```bash
make dev        # live reload (wails3 task dev)
make build      # build/bin/flint.app (wails3 task package, Taskfile.yml)
make server     # Flint as an HTTP server for a browser, no window (DEV ONLY, see below)
make install    # copy to /Applications and install the `flint` command in ~/.local/bin
make test       # Go (race) + frontend (tsc + node --test)
make lint       # go vet + gofmt check
make vuln       # govulncheck + npm audit
flint .         # open the current directory (after make install)
```

The Makefile sets `GOPROXY` to `https://proxy.golang.org,direct` unless you already have one set.

## Live reload: `make dev`

`make dev` runs `wails3 task dev`, which runs `wails3 dev` with `build/config.yml`:

- **Go**: when a `.go` file changes (tests, `frontend/`, `build/` and `.git` are ignored), Wails rebuilds the dev binary (`wails3 task dev:build`, which regenerates bindings first) and restarts Flint.
- **Frontend**: `wails3 task dev:frontend` runs `go run ../cmd/webbuild --watch` in `frontend/`. It rebuilds on every change, serves the result on `localhost:5173`, and reloads the page through esbuild's SSE stream (`/esbuild`). There is no HMR; a full reload is fast enough.

Use `FLINT_CONFIG_DIR` to keep a dev build's state separate from your installed Flint. A Flint with a different `FLINT_CONFIG_DIR` is also a separate single instance.

## Server mode (dev only)

```bash
make server                 # serves the current repository
wails3 task server -- /path/to/folder
```

This builds Flint with the `server` build tag and serves the UI over HTTP (`http://localhost:8080`) so you can check it in a regular browser, without a window. It is meant for UI checks during development.

**Warning: server mode exposes every Go binding on localhost with no authentication.** Anything that can reach the port can read and write files, run commands in terminals, and use your database connections. Never run it on a shared machine, and never expose the port. Server mode is out of scope for security reports.

## Bindings

The TypeScript bindings in `frontend/bindings` are generated from the Go services:

```bash
wails3 task bindings
```

Regenerate them whenever you change the exported methods or types of a service (`api_*.go`, or types they return from `internal/*`), and commit the result. `wails3 task build` and `make dev` regenerate them automatically. CI fails if `frontend/bindings` is stale.

Do not edit files in `frontend/bindings` by hand. `frontend/src/wails/` is a small compatibility layer over the bindings and the Wails runtime; application code imports from there or from the bindings directly.

## Building the app

```bash
make build
```

This runs `wails3 task package`:

1. `bindings`: regenerate the TypeScript bindings;
2. `frontend`: `go run ../cmd/webbuild --typecheck` (`tsc --noEmit`, then the esbuild bundle into `frontend/dist`);
3. `build`: `go build -tags production -trimpath` into `build/bin/flint`;
4. `icons`: `build/darwin/icons.icns` from `build/appicon.png`;
5. assemble `build/bin/flint.app`, clear extended attributes and sign it ad hoc (`codesign --sign -`).

The build targets macOS 12 (`MACOSX_DEPLOYMENT_TARGET=12.0`) with cgo enabled.

`make install` copies the app to `/Applications` and installs `scripts/flint` as `~/.local/bin/flint`. `flint [folder]` opens a folder (default: the current one); a running Flint takes it over and comes to the front.

## Tests

```bash
make test                                  # Go (race) + frontend (tsc + node --test)
make test-db-up && make test-integration   # Postgres + ClickHouse (TCP and HTTP) in Docker
make test-db-down
```

- `make test-go`: `go test ./internal/... -race -cover`.
- `make test-web`: `npm run typecheck` (`tsc --noEmit`) and `npm test` (`go run ../cmd/webbuild --test`, which compiles `src/**/*.test.ts(x)` to a temporary directory and runs `node --test`). Tests use `node:test` and `node:assert`; there is no jest or vitest.
- `make test-integration`: Go tests with the `integration` build tag against throwaway Postgres 16 and ClickHouse 24.8 containers started by `make test-db-up`. Ports default to 55432 (Postgres), 59000 (ClickHouse native) and 58123 (ClickHouse HTTP) and can be changed with `PG_PORT`, `CH_PORT` and `CH_HTTP_PORT`.
- GitHub tests use a fake `gh` on `PATH` (answers from `internal/github/testdata`), so they never contact GitHub.

## Lint

```bash
make lint       # go vet ./... and gofmt -l (fails on unformatted files)
make vuln       # govulncheck (pinned version, via go run) and npm audit of runtime dependencies
```

CI (`.github/workflows/ci.yml`, macOS) runs on every pull request and on pushes to `main`: the bindings staleness check, frontend tests and typecheck, gofmt, go vet, `go test -race ./...`, a build with the `production` and `server` tags, govulncheck, and `npm audit --omit=dev --audit-level=high`. CodeQL (`.github/workflows/codeql.yml`) analyzes the Go and TypeScript code.

## npm policy

- Keep runtime dependencies minimal. The frontend depends on React, CodeMirror, xterm.js and the Wails runtime; TypeScript and React types are dev dependencies for type checking. Do not add a new npm dependency without discussing it in an issue first.
- Install with `npm ci --ignore-scripts`. `frontend/.npmrc` disables lifecycle scripts and enforces exact versions.
- No `npx`, at build time or at runtime. The bundler and test runner are Go (`cmd/webbuild`).

## Release process

1. Make sure `main` is green.
2. Tag the release: `git tag vX.Y.Z && git push origin vX.Y.Z`.
3. The release workflow (`.github/workflows/release.yml`) runs the tests, builds a universal (Apple silicon + Intel) `Flint.app` with `wails3 task package:universal VERSION=vX.Y.Z`, packs it as `.zip` and `.dmg` with SHA-256 checksums and build provenance, and drafts a GitHub release.
4. Review the draft and publish it.

The version is embedded with `-ldflags -X main.version=…` and written into `Info.plist`; local builds report `dev`. To build a universal app locally, run `wails3 task package:universal`.

Signing and notarization are optional. They run only when the Apple secrets are configured in the repository (`MACOS_CERTIFICATE`, `MACOS_CERTIFICATE_PASSWORD`, `MACOS_SIGNING_IDENTITY`, `APPLE_ID`, `APPLE_TEAM_ID`, `APPLE_APP_PASSWORD`). Without them the app is ad-hoc signed, and users have to right-click it and choose Open on first launch.

## Logs while developing

Flint writes its log to `~/Library/Logs/Flint/flint.log` (Help ▸ Show Logs in Finder). Frontend errors and Go panics are logged there too. Use `internal/redact` for anything that might contain secrets or personal paths before it reaches the log.
