# Developing Flint

This guide covers building, running and testing Flint from source. For how the code is organized see [ARCHITECTURE.md](ARCHITECTURE.md); for contribution rules see [CONTRIBUTING.md](../CONTRIBUTING.md).

## Requirements

- macOS 12 or later, or Linux (x86_64 or arm64) with GTK 4 and WebKitGTK 6.0 (see [Building on Linux](#building-on-linux))
- Go 1.27.3+
- Node 22+ (only for type checking and running frontend tests; there is no npm build step)
- macOS: Xcode Command Line Tools (`xcode-select --install`), needed for cgo
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

To try **Import from VS Code** without your own editor's settings, point it at the test fixtures: `FLINT_IMPORT_HOME=$PWD/internal/vscodeimport/testdata/home` (server builds only).

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

## Building on Linux

Flint builds against GTK 4 and WebKitGTK 6.0 (Wails v3's default on Linux), so it needs a distribution that ships them: Ubuntu 24.04+, Debian 13+, Fedora 39+ or Arch. On Debian and Ubuntu:

```bash
sudo apt install build-essential pkg-config libgtk-4-dev libwebkitgtk-6.0-dev libx11-dev
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0
cd frontend && npm ci --ignore-scripts && cd ..
wails3 task build            # build/bin/flint
wails3 task package:linux    # build/bin/flint-dev-linux-<arch>.tar.gz
build/bin/flint .
```

`package:linux` packs a prefix layout: `bin/flint`, `share/applications/io.github.odudek.flint.desktop` and the icon in `share/icons/hicolor/512x512/apps`. `tar -xzf build/bin/flint-dev-linux-amd64.tar.gz -C ~/.local` installs it for the current user (the launcher appears in the desktop's app list; `~/.local/bin` must be on `PATH`). The binary is the `flint` command: a second `flint <folder>` hands the folder to the running Flint, as on macOS.

`make dev`, `make server` and `make test` work as on macOS; `make build` and `make install` build the macOS app bundle, so use the tasks above.

### From macOS, in Docker

`build/linux/Dockerfile` is a Linux environment with Go, Node 22, wails3, the GTK and WebKitGTK headers, and Xvfb. `node_modules` from a macOS checkout holds macOS binaries, so the container gets its own in a volume:

```bash
docker build -t flint-linux build/linux
docker run --rm -v "$PWD:/src" -v flint-linux-node-modules:/src/frontend/node_modules -w /src flint-linux sh -c '
  (cd frontend && npm ci --ignore-scripts) &&
  go vet ./... && go test ./... &&
  wails3 task package:linux'
```

To see the window, start it under Xvfb in the same container and take a screenshot of the virtual display:

```bash
Xvfb :99 -screen 0 1440x920x24 & export DISPLAY=:99
FLINT_CONFIG_DIR=/tmp/flint dbus-run-session -- build/bin/flint /src &
sleep 20; xwd -root -silent | convert xwd:- png:/src/build/bin/screen.png
```

Server mode (`go run -tags server .`) needs no display at all. Remove the image and volumes when you are done (`docker rmi flint-linux`, `docker volume rm flint-linux-node-modules`).

### Platform code

What differs per system is listed in [ARCHITECTURE.md](ARCHITECTURE.md#platforms). In short: a `_darwin.go` / `_linux.go` / `_other.go` file per system for anything that needs cgo or a system service, `runtime.GOOS` in small pure functions otherwise, and `lib/platform.ts` in the frontend. Shortcuts are written with `Mod` (⌘ on macOS, Ctrl elsewhere) and shown with `formatKeys`; never put a ⌘ glyph in UI text. Go tests of Linux code run in the Linux CI job (or the Docker recipe above); frontend tests switch platforms with `setMac(false)`.

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

## Benchmark

`go run ./cmd/bench -flint build/bin/flint.app` measures time to the first window and memory after 30 s idle for Flint, VS Code, Zed and Cursor (those installed), each with a fresh profile, on the Go source tree. It opens editor windows, so quit those editors first and leave the machine idle. The method, the latest results and the caveats are in [BENCHMARK.md](BENCHMARK.md); update the README table from it.

## Lint

```bash
make lint       # go vet ./... and gofmt -l (fails on unformatted files)
make vuln       # govulncheck (pinned version, via go run) and npm audit of runtime dependencies
```

CI (`.github/workflows/ci.yml`) runs on every pull request and on pushes to `main`. The macOS job runs the bindings staleness check, frontend tests and typecheck, gofmt, go vet, `go test -race ./...`, a build with the `production` and `server` tags, govulncheck, and `npm audit --omit=dev --audit-level=high`. The Linux jobs (Ubuntu 24.04, x86_64 and arm64) run the frontend tests, go vet, `go test -race ./...`, the `server` build and `wails3 task package:linux`. CodeQL (`.github/workflows/codeql.yml`) analyzes the Go and TypeScript code.

## npm policy

- Keep runtime dependencies minimal. The frontend depends on React, CodeMirror, xterm.js and the Wails runtime; TypeScript and React types are dev dependencies for type checking. Do not add a new npm dependency without discussing it in an issue first.
- `@replit/codemirror-vim` (MIT) provides Vim mode; writing a Vim emulation ourselves is not a reasonable alternative. It is pinned at 6.3.0, the last version without runtime dependencies (6.4.0 adds `@replit/codemirror-vim-core`), has no install scripts, and only peer-depends on `@codemirror/*`. `lib/editorSetup.ts` imports it lazily, so it is a separate chunk (about 120 KB minified) that is fetched only when Vim mode is switched on.
- Install with `npm ci --ignore-scripts`. `frontend/.npmrc` disables lifecycle scripts and enforces exact versions.
- No `npx`, at build time or at runtime. The bundler and test runner are Go (`cmd/webbuild`).

## Release process

1. Make sure `main` is green.
2. Tag the release: `git tag vX.Y.Z && git push origin vX.Y.Z`.
3. The release workflow (`.github/workflows/release.yml`) runs the tests, builds a universal (Apple silicon + Intel) `Flint.app` with `wails3 task package:universal VERSION=vX.Y.Z`, packs it as `.zip` and `.dmg` with SHA-256 checksums and build provenance, and drafts a GitHub release.
4. Review the draft and publish it.
5. Publishing runs the workflow's `homebrew` job (on macOS): it downloads the release's `.zip`, checks it against `SHA256SUMS.txt`, checks that the app is notarized (a valid signature and a stapled ticket: `codesign --verify` and `xcrun stapler validate`), renders `packaging/homebrew/flint.rb` with the version and SHA-256, and pushes it to the tap as `Casks/flint.rb`. Prereleases, tags that aren't `vX.Y.Z` and releases that aren't notarized are skipped with a notice: an ad-hoc signed app installed by Homebrew would be blocked by Gatekeeper. The tap token is passed to the push step only.

### Homebrew tap: one-time setup

The `homebrew` job does nothing (it logs a notice) until this is done. It also needs notarized releases, so the Apple signing secrets listed above must be set first.

1. Create a public repository for the tap. Homebrew requires the name to start with `homebrew-`; the default is `ODudek/homebrew-tap`, which users install from with `brew install --cask odudek/tap/flint`.
2. Create a fine-grained personal access token with access to that repository only and the **Contents: Read and write** permission.
3. In this repository's **Settings ▸ Secrets and variables ▸ Actions**, add the token as the secret `HOMEBREW_TAP_TOKEN`.
4. If the tap has another name, add the repository variable `HOMEBREW_TAP` (`owner/homebrew-name`) and update the `brew install` command in `site/installation.md`.
5. Publish a release (or re-run the `homebrew` job of the last release's workflow run) and check that `Casks/flint.rb` appears in the tap, then remove the "not published yet" note from `site/installation.md`.

To check the cask locally after a release: `brew install --cask odudek/tap/flint`, `brew audit --cask --online odudek/tap/flint` and `brew uninstall --zap --cask flint`.

The version is embedded with `-ldflags -X main.version=…` and written into `Info.plist`; local builds report `dev`. To build a universal app locally, run `wails3 task package:universal`.

Signing and notarization are optional. They run only when the Apple secrets are configured in the repository (`MACOS_CERTIFICATE`, `MACOS_CERTIFICATE_PASSWORD`, `MACOS_SIGNING_IDENTITY`, `APPLE_ID`, `APPLE_TEAM_ID`, `APPLE_APP_PASSWORD`). Without them the app is ad-hoc signed, and users have to right-click it and choose Open on first launch.

## Logs while developing

Flint writes its log to `~/Library/Logs/Flint/flint.log` on macOS and `~/.local/state/flint/flint.log` on Linux (Help ▸ Show Logs in Finder / File Manager). Frontend errors and Go panics are logged there too. Use `internal/redact` for anything that might contain secrets or personal paths before it reaches the log.

## User documentation site

The user guide lives in `site/` as Markdown: one file per page, the sidebar in `site/_nav.md` (`## Section` and `- [Title](page.md)` lines). `cmd/docsite` renders it with goldmark (no other dependencies) and fails on pages missing from the nav and on broken links or anchors.

Screenshots and the README's tour GIF live in `site/images/` (copied to the site as they are; a missing image fails the build). Take them from a clean checkout in server mode (`WAILS_SERVER_PORT=… go run -tags server .`) with a throwaway `FLINT_CONFIG_DIR`, a `KUBECONFIG` that points only at a local cluster and empty AWS config files, so no real account, cluster or path ends up in a picture.

```bash
make docs                                   # build into _site/ and preview at http://localhost:8090
go run ./cmd/docsite -src site -out _site   # build only
```

`.github/workflows/pages.yml` builds the site on pull requests that touch it and publishes it to GitHub Pages from `main` (repository Settings ▸ Pages ▸ Source: GitHub Actions). While the repository is private it only builds it: Pages needs a public repository on the free plan.
