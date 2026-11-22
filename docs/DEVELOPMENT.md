# Developing Flint

This guide covers building, running and testing Flint from source. For how the code is organized see [ARCHITECTURE.md](ARCHITECTURE.md); for contribution rules see [CONTRIBUTING.md](../CONTRIBUTING.md).

## Requirements

- macOS 12 or later, or Linux (x86_64 or arm64) with GTK 4 and WebKitGTK 6.0 (see [Building on Linux](#building-on-linux))
- Go 1.27.4+
- Node 22+ (only for type checking and running frontend tests; there is no npm build step)
- macOS: Xcode Command Line Tools (`xcode-select --install`), needed for cgo
- `wails3` v3.0.0:

  ```bash
  go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0
  ```

  The Makefile expects it at `$(go env GOPATH)/bin/wails3`; override with `make WAILS=/path/to/wails3 …`.

Optional tools, used by the features that need them:

- `ripgrep` (`brew install ripgrep`): fast search and `⌘P`; without it a slower Go fallback is used
- `dlv` (`go install github.com/go-delve/delve/cmd/dlv@latest`): the debugger
- language servers (`gopls`, `rust-analyzer`, `vtsls` or `typescript-language-server`, `basedpyright` or `pyright`)
- `claude` and/or `codex`: the agent chat
- Docker: database integration tests

## First setup

```bash
git clone https://github.com/flint-ide/flint.git
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

To try the GitHub features without GitHub, run the same fake: `go run ./cmd/fakegithub -addr 127.0.0.1:34230`, start the server build with `FLINT_DEV_GITHUB=http://127.0.0.1:34230` (and a throwaway `FLINT_CONFIG_DIR`), and open a project with a remote `http://127.0.0.1:34230/acme/widgets.git`. Sign in from Settings ▸ Accounts; opening the page it shows (`/login/device`) approves the code. `FLINT_DEV_GITHUB` exists only in `server` builds, accepts only loopback addresses, keeps tokens in memory instead of the keyring and ignores `GH_TOKEN` and the GitHub CLI, so no real token can reach it.

To try **Import from VS Code** without your own editor's settings, point it at the test fixtures: `FLINT_IMPORT_HOME=$PWD/internal/vscodeimport/testdata/home` (server builds only).

**Warning: server mode exposes every Go binding on localhost with no authentication.** Anything that can reach the port can read and write files, run commands in terminals, and use your database connections. Never run it on a shared machine, and never expose the port. Server mode is out of scope for security reports.

## Bindings

The TypeScript bindings in `frontend/bindings` are generated from the Go services:

```bash
wails3 task bindings
```

Regenerate them whenever you change the exported methods or types of a service (`api_*.go`, a feature module's service in `internal/features/*`, or types they return from `internal/*`), and commit the result. `wails3 task build` and `make dev` regenerate them automatically. CI fails if `frontend/bindings` is stale.

Do not edit files in `frontend/bindings` by hand. `frontend/src/wails/` is a small compatibility layer over the bindings and the Wails runtime; application code imports from there or from the bindings directly.

## Add a feature

A feature with a backend, settings, a tool, a panel or a Settings page is a feature module ([design](design/extensions.md)). Copy the closest existing one: `todo` (a service, a settings key holding a list, a panel, a Settings page) or `update` (a boolean key, a `state.json` key, a Start hook, a status bar item, a command). A feature that only needs commands, panels, settings, terminals or agent tools can be an extension instead, like Docker, Kubernetes and AWS Profiles in [flint-ide/extensions](https://github.com/flint-ide/extensions). The steps below use a made-up `gcp` feature.

**Go: `internal/features/gcp/`**

1. `api.go`: the service, a struct holding a `feature.Host`. Its exported methods become frontend calls. Reach the app only through the host: `h.Context()`, `h.Settings()`, `h.Roots()`, `h.Emit(...)`.
2. `settings.go` (if it has settings): the value type with JSON tags and `var Section = &settings.Section[Settings]{Key: "gcp", Default: …, Normalize: …, Check: …}`. Read it with `Section.Get(h.Settings())`. The key must be new; a test refuses built-in keys.
3. `module.go`: `var Module = feature.Module{ID: "gcp", Settings: …, Tools: …, New: func(h feature.Host) feature.Instance { … }}`. Write `application.NewService(&API{…})` out in `New` (the bindings generator looks for that call). Start background work in `Start`, never in `New`, and stop it in `Close`.
4. Tests against `featuretest.New(t.Context())`, a fake host.
5. One line in `features.go`: `gcp.Module,`.

**Frontend: `frontend/src/features/gcp/`**

1. `settings.ts` (if it has settings): the type, its default and a `coerce` for untrusted JSON; `declare module '../../lib/settingsSections' { interface FeatureSettings { gcp: GcpSettings } }`; and `export const gcpSettings = defineSection('gcp', DEFAULT_GCP, coerceGcp)`. `useSettings((s) => s.settings.gcp)` is then typed.
2. Components and a store, as in any feature. Import the bindings from `../../../bindings/flint/internal/features/gcp/api`.
3. `index.ts`: `export default defineFeature({ id: 'gcp', settings, panels, commands, settingsPages, statusItems, tabKinds, fileMenu, activate })`. Panels, Settings pages and tab views use `lazy(...)` so their code stays out of startup. `order` places a panel on the activity bar and a page in Settings (Flint's pages use 10, 20 … 160).
4. One line in `frontend/src/features/index.ts`.

**Docs and checks**

1. For each settings key, add `site/reference/_settings/<key>.md`. Its first line is the key's row in the Keys table, and the rest is its section. Links are relative to `site/reference/settings.md`.
2. Regenerate the bindings (`wails3 task bindings`), then run `go test ./...` and the frontend tests. `features_test.go` fails when the Go and frontend keys differ or a key has no reference file. `installFeatures` throws at startup on a duplicate id.

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

`make install` copies the app to `/Applications` and installs `scripts/flint` as `~/.local/bin/flint`. `flint [folder]` opens a folder (default: the current one); a running Flint takes it over and comes to the front. `flint --install-extension <folder>` asks Flint to install an extension from a folder (same confirmation as in the app).

### The headless host (remote machines)

`cmd/flint-host` is Flint without a window, Wails or cgo: the background session host and its ssh relay, for machines you open terminals on over ssh ([Remote terminals](FEATURES.md#remote-terminals)). It builds anywhere for linux and darwin on amd64 and arm64:

```bash
wails3 task host:package                                  # this machine's OS and architecture
wails3 task host:package HOST_OS=linux HOST_ARCH=arm64    # any of linux/darwin x amd64/arm64
wails3 task host:package:all VERSION=vX.Y.Z               # all four
```

Each produces `build/bin/flint-host-<os>-<arch>.tar.gz` (one file, the binary, named `flint`) and a `.sha256` file next to it. `flint --version` prints `flint <version> host-protocol <n>`, which the machine Check in Settings reads. The binary hosts terminals only: agent calls fail with "agents are not available on this host" (the agent drivers live in package main). On macOS the no-cgo build cannot read a shell's live working directory, so the remote tab's folder follows only what the shell last reported. `go test ./cmd/flint-host` cross-compiles the four targets with `CGO_ENABLED=0`, checks that the import graph has no Wails and runs the built binary as a relay.

No release publishes these archives yet; the install instructions in Settings ▸ Machines say so. See "Release process" below for what a release would add.

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

`package:linux` packs a prefix layout: `bin/flint`, `share/applications/io.github.flint_ide.flint.desktop` and the icon in `share/icons/hicolor/512x512/apps`. `tar -xzf build/bin/flint-dev-linux-amd64.tar.gz -C ~/.local` installs it for the current user (the launcher appears in the desktop's app list; `~/.local/bin` must be on `PATH`). The binary is the `flint` command: a second `flint <folder>` hands the folder to the running Flint, as on macOS.

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
make test-db-up && make test-integration   # Postgres + ClickHouse in Docker
make test-db-down
```

- `make test-go`: `go test ./internal/... -race -cover`.
- `make test-web`: `npm run typecheck` (`tsc --noEmit`) and `npm test` (`go run ../cmd/webbuild --test`, which compiles `src/**/*.test.ts(x)` to a temporary directory and runs `node --test`). Tests use `node:test` and `node:assert`; there is no jest or vitest.
- `make test-integration`: Go tests with the `integration` build tag against throwaway Postgres 16 and ClickHouse 24.8 containers started by `make test-db-up`. Ports default to 55432 (Postgres) and 58123 (ClickHouse HTTP, the only ClickHouse port Flint uses) and can be changed with `PG_PORT` and `CH_HTTP_PORT`. The ClickHouse tests read `FLINT_TEST_CH_HTTP`. They cover exact 64-bit and Decimal values, per-console sessions, cancellation (checked in `system.processes`) and errors in the middle of a stream.
- GitHub tests use `internal/github/githubtest`, a fake GitHub (REST, GraphQL, device flow and a separate host for redirected job logs) on a loopback `httptest` server, so they never contact GitHub. Account tests use a fake keyring, never the real Keychain.

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
- `@wunderui/react` (WunderUI Free, MIT) provides the UI components (buttons, tabs, toasts, badges, alerts, inputs and more), built on Base UI, with `lucide-react` (ISC) for icons. Code uses them through `@flint-ide/ui` (`ui/`, not an npm dependency). See [UI components](#ui-components).
- WunderUI's npm release (0.3.0) lags its repository, so `frontend/` and `storybook/` install a build of the repository from `third_party/wunderui/wunderui-react-<version>-git.<commit>.tgz` (`file:` dependency). To update it: clone `github.com/wunder-ui/wunderui` at the chosen commit, run `npm ci --ignore-scripts && node package/build.mjs` there, delete `dist/*.map` from `package/out`, set `version` in `package/out/package.json` to `<version>-git.<short commit>`, `npm pack` it into `third_party/wunderui/` (remove the old tarball), point both `package.json` files at it, `npm install --ignore-scripts`, and regenerate the CSS (below). The build's final check flags paid component names that appear only in its metadata files (`design.json`, `figma-map.json`); the code is unaffected. Switch back to the npm release once it catches up.
- Install with `npm ci --ignore-scripts`. `frontend/.npmrc` disables lifecycle scripts and enforces exact versions.
- No `npx`, at build time or at runtime. The bundler and test runner are Go (`cmd/webbuild`).
- `storybook/` is a separate npm package with its own lock file (Storybook, Vite): developer tooling for the component catalog only. The app build and `frontend/` never depend on it.

## UI components

New UI uses **`@flint-ide/ui`**, the UI kit in `ui/`: every WunderUI component (Base UI, styled with Tailwind and compiled ahead of time) on Flint's palette, plus Flint's own compositions (`CommandPalette`, `EditorTabs`, `StatusBar`, `JobList`, `AgentRow`, `DiffStat`, `Keys`, `ContextMenuProvider`/`useContextMenu`, and the layout parts `PanelHeader`, `TreeRow`, `TreeSection`, `ViewToolbar`). Import components from `@flint-ide/ui` and icons (Lucide) from `@flint-ide/ui/icons`. The **Storybook** in `storybook/` shows every component and variant in Flint's dark and light themes, with the app's own CSS: `cd storybook && npm ci --ignore-scripts && npm run storybook` (http://localhost:6006). The Docs site workflow publishes it under `/storybook/` on GitHub Pages. Add a story there when you add or change a component.

- **The package** is source only: `ui/src` (TypeScript), `ui/css` (styles), no `node_modules` of its own. The app maps `@flint-ide/ui` to it in `frontend/tsconfig.json`; `cmd/webbuild` resolves its imports from `frontend/node_modules`, and `npm test` also runs `ui/src/**/*.test.ts`. The Storybook maps it in `.storybook/main.ts`. Type check it alone with `frontend/node_modules/.bin/tsc -p ui`.
- **Styles:** `ui/css/index.css` (imported once, in `main.tsx`) is the generated `wunderui.css`, `theme.css` and `components.css`. WunderUI is written in Tailwind CSS v4, but no build here runs Tailwind: `ui/css/wunderui.css` is WunderUI's tokens plus the utilities its components use, compiled once and committed. After changing the `@wunderui/react` version, regenerate it from the repository root with `go run ./cmd/wuicss` (or `wails3 task wunderui:css`). It downloads the pinned standalone Tailwind CLI into `.task/bin`, checks its SHA-256, compiles `ui/css/wunderui.input.css` against `frontend/node_modules`, and renames WunderUI's `--accent` and `--border` to `--wui-accent` and `--wui-border` so they don't clash with Flint's theme.
- **Colors:** `ui/css/theme.css` (hand-written) maps WunderUI's tokens onto Flint's palette: `--primary` and the `--brand-*` scale are Flint's amber accent, surfaces, borders and text come from `--bg-*`, `--border` and `--fg*`. Because they point at Flint's variables, the components follow the dark and light themes and custom themes. Change colors there, never in the generated file. The compositions' styles are plain CSS on the same variables in `ui/css/components.css` (classes `fui-*`).
- **Context menus:** WunderUI has none. `ContextMenuProvider` holds one menu for a list or tree; inside it, `useContextMenu()` gives `onContextMenu(entries)` for a row (right-click, or Shift+F10 and the menu key on a focused row) and `onOpenUnder(entries)` for a row's "more" button. Entries are `{ label, onSelect, hint?, danger?, disabled?, icon? }` or `'separator'`; falsy entries drop out, so they can be conditional. The menu closes when anything scrolls or the window loses focus, like a native one.
- **Dense mode:** the class `fui-dense` makes menu and list rows 24 px instead of 32, with 12 px text, and puts `CollapsibleContent` 4 px under its trigger instead of 16. Put it on `<body>` in a sidebar webview (menus render in portals under `<body>`, so it reaches them), or on one popup (`DropdownMenuContent`, `ComboboxContent`, `ContextMenuProvider`'s `className`).
- **Badge colors:** `blue`, `green`, `red`, `yellow` and `orange` are Flint's `--info`, `--ok`, `--danger`, `--warn` and `--accent` (`theme.css`), so status badges follow the theme. The other names keep WunderUI's colors.
- **The app is on the kit:** toasts, confirmations, dialogs (`layout/Modal.tsx` is the kit's `Dialog`: it traps focus and gives it back on close, Escape goes to the topmost dialog and only when nothing inside used it, only its own backdrop closes it by mouse, `lib/modalDismiss.ts`), menus (`Dropdown`, `Select` and context menus on the kit's `DropdownMenu`, `components/ui/AppMenu.tsx`; long or `searchable` menus keep Flint's filter list, styled like the kit's), and the screens: buttons, fields, switches, segments, badges, banners, the editor tabs (`EditorTabs`) and the status bar (`StatusBar`). `<html data-density="ide">` (frontend/index.html, rules in `ui/css/components.css`) gives the kit Flint's density: controls sm 24 · md 28 · lg 32 px, 12 px text; use `size="sm"` in toolbars, panel headers and rows, the default in forms and dialogs. A few spots stay on the app's own controls on purpose: the results grid cells, the virtualised file tree and search results, the chat composer, the keymap chord recorder and the grouped theme select. The base `button`/`input`/`.select`/`.badge`/`.modal` rules in `style.css` still draw those in the kit's look; new code uses the kit's components.
- **Layouts come from the kit too:** panel headers, tree and list rows, collapsible groups, editor-view toolbars, forms, empty states and simple tables are kit components, not view CSS. [Layout patterns](#layout-patterns-stylecss--kit) below maps each `style.css` pattern to its component; when you touch a view, move it over and delete the CSS that no longer has a user.
- **No Tailwind classes** in Flint's code or the kit's compositions: only the WunderUI package is scanned, so a utility used only here would be missing from the CSS. Lay out with plain CSS and leave the look to the components.
- **Coexistence:** WunderUI elements carry `data-slot`; the base `button`, `input`, `select` and `kbd` rules in `style.css` skip them and what is inside them, except the app's own controls inside a `Modal` (`.modal`). The `dark` class on `<html>` follows Flint's theme (`boot.ts`, `store/settings.ts`) and selects WunderUI's dark tokens.
- **Extension webviews** can use the kit too. Write the page in TypeScript/React (`import { Button, flint, syncFlintTheme } from '@flint-ide/ui'`, call `syncFlintTheme()` once) and build it from a Flint checkout, after `npm ci --ignore-scripts` in `frontend/`, with `go run ./cmd/flint-webview -o <extension>/ui/panel.html <extension>/webview/panel.tsx`. The result is the one self-contained HTML file the contract asks for: React, the components and icons the page uses, the kit's styles and the base webview styles (`ui/css/webview.css`), all inline. A small page is about 650 KB (React, the components it uses and the stylesheet); WunderUI's rich text editor (Tiptap, about 400 KB) is included only when a file next to the entry mentions `RichTextEditor`. The limit is 32 MiB. `-check` type checks the page's sources first (every `.ts`/`.tsx` file next to the entry and below) against the kit and `frontend/node_modules`, with Node.js and no tsconfig in the extension; without `-o` it only checks. `flint()` is `window.flint` with types, `useFlintMessages()` collects what the process posts.

### Layout patterns: style.css → kit

Find the class in the view, swap in the component, then delete the CSS rules (and their descendant rules, e.g. `.tree-row:hover .row-actions`) once `grep -rn '<class>' frontend/src` finds no other user. Stories: `Flint/Panel Header`, `Flint/Tree Row`, `Flint/View Toolbar`. All kit controls inside these parts are `size="sm"`.

| Pattern in `style.css` (classes to look for) | Kit component and props | Notes |
|---|---|---|
| `.panel-header` + `.panel-title` + trailing `IconButton`s | `<PanelHeader title="Databases" tooltip? actions={<>…ghost sm IconButtons…</>}>` | A node as `title` (the Commit panel's `RepoSwitcher`) keeps its own case. A line under the header (`.git-branch-line`) becomes `children`. Drop `<span className="spacer" />` before trailing controls: the title already grows. Keep the `<div className="panel">` wrapper. |
| `.tree-row` (virtualised, style with `transform`/`top`) | `<TreeRow virtual style={{ transform: … }} depth={…} …>` | `FileTree.tsx`, `SearchPanel.tsx`. Pass only the position in `style`; `depth` gives the padding (8 + 14 × depth, the same formula `FileTree` uses inline today). |
| `.tree-row.static` | `<TreeRow …>` (static is the default) | Depth 0 = the old 8 px padding. |
| `.indent-1` … `.indent-5`, `.change-row`, `.repo-changes .change-row` | `depth={1…}`, or `indent={px}` for an off-step value | `indent-1` = depth 1 (22 px), `indent-2` = depth 2 (36 px). `indent-3`/`4`/`5` were 54/70/86 px: use `depth={3}` (50 px) unless the column must stay exactly where it is, then `indent={54}`. |
| `.twisty` with `▾`/`▸` | `twisty="open" \| "closed"`; `twisty="none"` for a leaf that lines up with folders | When the twisty does something else than the row click (CommitPanel `RepoSection`), pass `onTwistyClick` and it becomes a button with `aria-expanded`; give `twistyLabel` if the default "Expand"/"Collapse" is vague. |
| `FileIcon` / icon right after the twisty | `icon={<FileIcon …/>}` | |
| `.file-name`, `.dir-name`, `.conn-name`, `.scratch-name`, `.table-name`, `.schema-name` | `label={name}`; add `file` for file names (`--fg-name`) | Ellipsis is built in. While renaming, pass the inline editor as `label` and set `editing`. |
| `.muted.path-hint` (and `<b>` highlights in it) | `hint={dir}` | Hidden on hover when the row has `actions` (the old global `.tree-row:hover > .path-hint` rule). |
| `Badge size="sm" className="count-badge"` | `badge={n}` (a number renders the small Badge) or `badge={<Badge …/>}` | Hidden while actions show. |
| `.row-actions` (+ `onClick={stopPropagation}`) | `actions={<>…</>}` | Shown on hover and keyboard focus; clicks don't reach the row, so drop the wrapper's `stopPropagation`. |
| A `Checkbox` before the name (`onClick` stopPropagation) | `leading={<Checkbox …/>}` | Clicks don't reach the row; the hit-area fix is built in. Remove the `.tree-row [data-slot='checkbox']` rules with the last user. |
| `.active`, `.selected`, `.root-row`, `.group-row`, `.editing` | `active`, `selected`, `emphasis="root"`, `emphasis="group"`, `editing` | The bright selection under `.tree:focus` works under any focused ancestor. Row-specific extras (`.change-kind` letters, `.line-no`, a conflict count) go in `children`, after the hint. |
| Group header `.tree-row.static.group-row` + `.group-title` + rows under it, collapsed state in `useState`/a keyed record (CommitPanel `ChangeGroups`, `StashSection`, `ReviewSection`, TODO/Tasks groups) | `<TreeSection title count? hint? leading? actions? open? onOpenChange? defaultOpen? depth?>{rows at depth + 1}</TreeSection>` | Uncontrolled unless `open` is given. Rows inside are `TreeRow`s. A repository header with its own click (CommitPanel `RepoSection`) stays a `TreeRow` with `onTwistyClick`. |
| Two-line list rows (a title line + a muted meta line: pull requests, query history) | `<ListItem label leading? badges? trailing? meta={[…parts]} actions? showActions="hover"\|"always" active? selected? mono? dimmed? compact?>` | Not `TreeRow` (fixed 22 px). Empty meta parts are dropped, the rest get a `·` separator. With `onClick` the row is a tab stop that Enter/Space activate. Hover actions also show on the `selected` row (a keyboard cursor: `useListCursor(0, '[data-selected]')`). Related rows behind an accent line: `ListGroup label? meta? actions? stacked?`. |
| Cards with a state, meta, notes, an actions bar and sub-rows (`.task`, `.task-meta`, `.task-actions`, `.project-item`, `.project-card`, `.project-problem`) | `<TaskCard status label badges? trailing? meta ci? notes={[{ text, tone }]} actions twisty? onOpen? frame="row"\|"card"\|"bare">{sub-rows}</TaskCard>` | Agent tasks, agents, project items and repositories, project problems; `frame="card"` (a WunderUI `Card`, `draggable`, `dragging`) on the board. `ci` is a `BadgeProcessing`. Sub-rows (`TreeRow`s, compact `ListItem`s) sit outside the head's click. |
| Editor-view bars: `.query-toolbar`, `.run-toolbar`, `.diff-header`, `.merge-header`, `.profile-toolbar`, `.http-bar`, `.ci-log-bar`, `.pr-review-bar`, `.keymap-toolbar` | `<ViewToolbar aria-label="…">` with kit controls; `<span className="fui-grow" />` instead of `.spacer` | 32 px, bottom border, 12 px text, 4 px gap (the old bars were 30–36 px). Icon buttons wrapped as `<ToolbarButton render={<IconButton size="sm" variant="ghost" aria-label="…" />}>` get arrow-key focus; inputs and selects stay ordinary tab stops. `ToolbarSeparator` for dividers. |
| Dialog forms: `<form className="form">`, `.form label` (text + control), `.form-row`, `.form-actions` | `<Form>` (it is Base UI's form; keep `onSubmit`), `<Input label=… />` / `<Textarea label=… />`, other controls in `<Field><FieldLabel>…</FieldLabel>…</Field>`, hints `FieldDescription`, errors `FieldError`, groups `Fieldset` + `FieldsetLegend`, buttons in `<FormActions align="start">` | IDE density makes labels 12 px muted, 4 px above the control, 10 px between fields: the `.form` look. `.form-actions` was start-aligned, so `align="start"` keeps it; dialogs whose buttons belong in the dialog's footer use `DialogFooter`. Side-by-side fields (`.form-row`) keep a small flex wrapper in the view's CSS. `.form-status error` → `<Alert size="inline" variant="negative">` or `FieldError`. `.form label.check` → `<Checkbox label=… />`. |
| Settings rows: `.settings-row`, `.settings-label`, `.settings-control` (`components/settings/fields.tsx` `Row`) | Keep `Row` | WunderUI's `SettingsRow` puts the control at the far right of a justify-between row with a 14 px title; Flint's settings page is a fixed 260 px label column with wide fields and a `role="group"` named by the label. `SettingsRow` (density-tuned) is fine for a short list of switches inside a dialog. |
| Empty panels: `.placeholder.muted` with one line | Keep `<div className="placeholder muted">` | `EmptyState` is a centred block with a title and a frame; a one-line sidebar hint is not that. |
| Empty editor areas / first-run screens with a title, a sentence and a button (`.grid-empty`, `.project-column-empty`, `.welcome` body, "No connections yet" + button) | `<EmptyState variant="default" icon? title description? action? />` | IDE density: 24 px padding, 36 px icon disc, 13/12 px text. `variant="dashed"` (default) only where a drop target is meant. |
| Simple HTML tables (`TopTable.tsx`, `CompareView.tsx`, `ViewChart.tsx`, the table of declarative extension views) | `Table`, `TableHeader`, `TableBody`, `TableRow`, `TableHead`, `TableCell` | IDE density: 12 px text, 24 px header, rows at `--row-py`. Not the virtualised DB results grid. |
| Card frames: `.account-card`, `.extension-card`, `.permission-card` | `Card` + `CardHeader`/`CardTitle`/`CardDescription`/`CardContent`/`CardFooter`; small link-like tiles `ItemCard` | `--card-padding` is 12 px under IDE density. |
| `<hr>`, `.dd-separator`, `.debug-sep` | `Separator` (`orientation="vertical"` in bars) | |
| Usage and quota bars, determinate progress | `Meter` (a value in a range: budget, quota), `Progress` (a task advancing) | Not the 2 px `.usage-bar` under a row; that stays. |
| Loading placeholders (`.pr-loading`, "Loading…" lines) | `Skeleton` for the shape of what is coming; `Spinner` + text for a short wait | One muted "Loading…" line in a tree can stay a `TreeRow`. |
| Shortcut hints | `Keys` (Flint's, follows the platform's modifier names); WunderUI's `KbdHint` only for a key list with a label in a webview | |
| Choice lists with a title and a description (agent pickers, import options) | `OptionRow` in an `OptionRowGroup` | |
| Activity history (PR timeline, task log) | `Timeline compact` | Only where the entries are simple; the PR conversation keeps its own layout. |

WunderUI parts that don't fit an IDE row: `FileTree` (not virtualised, owns a single selection, no inline rename, context menu, drag and drop or Flint's file icons: `FileTree.tsx` keeps its own rows, now `TreeRow`), `Collapsible`/`AccordionRow` for tree groups (the trigger is a button, so the header can't hold a checkbox or buttons, and it is 32 px; use them for disclosure sections in dialogs and settings, the IDE density puts `appearance` triggers at row height), and `Toolbar` bare (a floating rounded card: use `ViewToolbar`).

These stay app CSS, because they draw content rather than lay out controls: the diff and merge views and their gutters, the git graph, the flame graph and profiler table, CodeMirror themes and editor decorations (`lib/editorDecorations.ts`, LSP squiggles, inlay hints), the xterm terminal, the virtualised DB results grid and its column filters, EXPLAIN plans, the chat transcript and composer layout, the Markdown preview, the HTTP response view, the project board columns, the activity bar, drag-and-drop overlays and the shell's split layout.

## Release process

Releases are made by `.github/workflows/release.yml` and published straight to the [Releases](https://github.com/flint-ide/flint/releases) page. A release starts in one of three ways:

- **Automatically after a merge to `main`.** When CI passes on a push to `main`, `go run ./cmd/nextversion` works out the next version from the [Conventional Commits](https://www.conventionalcommits.org/) since the last `vX.Y.Z` tag: `feat` → minor, `fix`/`perf` → patch, `type!:` or a `BREAKING CHANGE:` footer → major (minor before 1.0). Merge commits are skipped; the commits they bring in count. With only `chore`, `docs`, `ci`, `refactor`, `test` and the like, nothing is released. If a newer commit has landed on `main` meanwhile, its own run releases both.
- **By hand:** Actions ▸ Release ▸ Run workflow on `main`, with bump `auto`, `patch`, `minor` or `major` (or `gh workflow run release.yml -f bump=minor`).
- **By pushing a tag:** `git tag vX.Y.Z && git push origin vX.Y.Z`.

To see which version the next release would get: `go run ./cmd/nextversion` (it prints nothing when nothing would be released).

The workflow then:

1. Runs the tests and builds a universal (Apple silicon + Intel) `Flint.app` with `wails3 task package:universal VERSION=vX.Y.Z`, packed as `.zip` and `.dmg` (signed and notarized when the Apple secrets below are set).
2. Builds the Linux archives on x86_64 and arm64 with `wails3 task package:linux VERSION=vX.Y.Z` (`Flint-vX.Y.Z-linux-amd64.tar.gz`, `Flint-vX.Y.Z-linux-arm64.tar.gz`).
3. Writes `SHA256SUMS.txt`, attests build provenance for every file, creates the tag (unless it was pushed) and publishes the release with the notes described below, marked as the latest.
4. Runs the `homebrew` job (on macOS), which also runs when someone publishes a release by hand: it downloads the release's `.zip`, checks it against `SHA256SUMS.txt`, checks that the app is notarized (a valid signature and a stapled ticket: `codesign --verify` and `xcrun stapler validate`), renders `packaging/homebrew/flint.rb` with the version and SHA-256, and pushes it to the tap as `Casks/flint.rb`. Prereleases, tags that aren't `vX.Y.Z` and releases that aren't notarized are skipped with a notice: an ad-hoc signed app installed by Homebrew would be blocked by Gatekeeper. The tap token is passed to the push step only.

### Release notes

The body of a release is written by `cmd/releasenotes` from the pull requests merged since the release before. It is what users read on the Releases page, and Flint's "What's New" tab (`internal/update`) is made from the same text, so the notes are written for users, in each pull request:

- The pull request template has a **Release notes** section: what users will notice, written for them (what it does, how to find it, anything that changed behaviour or needs action), in Markdown. "none", or nothing, for changes users don't see. Several parts get `###` headings; what users must do or know when they update goes under a `### Upgrade notes` heading. Edit the section before the release if a pull request's description changed since; it is read when the release is made.
- The release body has these sections, each left out when empty:
  - **Highlights**: for each pull request with notes, a heading made from its title (without the Conventional Commits type, first letter capitalised), the section's text with its headings moved below that heading, and a link to the pull request.
  - **Upgrade notes**: the `### Upgrade notes` parts, each with its pull request.
  - **All changes**: one line per change, "Title by @author in <pull request>", under Breaking changes (`type!:` or a `BREAKING CHANGE:` footer in the title or any of its commits), Features, Fixes, Performance and Other changes (a title without a type takes the type of its commits). Commits that reached `main` without a pull request are listed by themselves, with their short hash. `chore`, `docs`, `ci`, `test`, `refactor`, `build`, `style` and dependency updates are only counted, in one line. Pull requests with highlights are listed here too: Flint's "What's New" tab lists only these lines (`*` items under these headings), so list items from the pull requests' own text are written with `+`, which GitHub renders the same.
  - **Full Changelog**: the compare link to the release before (for the first release, its commits).
- Descriptions are untrusted text, so they can't change the structure of the release: HTML comments (the template's) are removed, other HTML is shown as text, `@mentions` become code (nobody is notified; the author in each list line is mentioned, as in GitHub's generated notes), headings are moved below the entry's own, an unclosed code fence is closed, and the sizes are bounded (10,000 bytes of notes per pull request, 120,000 for the whole body; GitHub's limit is 125,000 characters). What doesn't fit is left out with a line that says so.

The command reads git itself and never the network. `.github/scripts/release-notes.sh` asks it which pull requests the range has (by their merge commits, "Merge pull request #N from …", or a squashed commit's "(#N)"), fetches them with `gh api`, and runs it with them. The job `plan` of the Release workflow runs it with `pull-requests: read`; if anything fails there, the release is still published, with GitHub's generated notes, and the step summaries of `plan` and `publish` say so.

To see the notes of a range locally, write the pull requests as a JSON array (`gh pr view N --json number,title,body,author` objects) and run:

```bash
go run ./cmd/releasenotes -version v0.7.0 -to v0.7.0 -list-prs          # the pull requests: 83
go run ./cmd/releasenotes -version v0.7.0 -to v0.7.0 -prs prs.json      # the body
```

To rewrite the notes of a published release, for example one made before this or with generated notes after a failure, or after editing a pull request's Release notes section: Actions ▸ **Release notes** ▸ Run workflow with the tag (or `gh workflow run release-notes.yml -f tag=v0.7.0`). It writes the notes the same way and replaces the release's body; only that job has `contents: write`, and the notes are in its step summary.

### Homebrew tap: one-time setup

The `homebrew` job does nothing (it logs a notice) until this is done. It also needs notarized releases, so the Apple signing secrets listed above must be set first.

1. Create a public repository for the tap. Homebrew requires the name to start with `homebrew-`; the default is `flint-ide/homebrew-tap`, which users install from with `brew install --cask flint-ide/tap/flint`.
2. Create a fine-grained personal access token with access to that repository only and the **Contents: Read and write** permission.
3. In this repository's **Settings ▸ Secrets and variables ▸ Actions**, add the token as the secret `HOMEBREW_TAP_TOKEN`.
4. If the tap has another name, add the repository variable `HOMEBREW_TAP` (`owner/homebrew-name`) and update the `brew install` command in `site/installation.md`.
5. Publish a release (or re-run the `homebrew` job of the last release's workflow run) and check that `Casks/flint.rb` appears in the tap, then remove the "not published yet" note from `site/installation.md`.

To check the cask locally after a release: `brew install --cask flint-ide/tap/flint`, `brew audit --cask --online flint-ide/tap/flint` and `brew uninstall --zap --cask flint`.

A release that also ships the headless host would add to the workflow: `wails3 task host:package:all VERSION=vX.Y.Z` on any runner (no cgo, no GTK), the four `flint-host-<os>-<arch>.tar.gz` files and their `.sha256` lines in `SHA256SUMS.txt`, attestation for them, and the same files uploaded to the release. The install steps in Settings ▸ Machines already name those files.

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
