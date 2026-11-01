# Principles audit — October 2026

An audit of the code base against [the five architectural principles](../PRINCIPLES.md), done before the first public release (main plus the Kubernetes apps view, diagnostics, open-source and security branches). Scores are 1 (poor) to 5 (exemplary). Effort: S = hours, M = a day or two, L = more.

| Principle | Score | In one line |
|---|---|---|
| Minimal dependencies | 4/5 | 10 direct Go modules and 22 runtime npm packages, all in use; clickhouse-go was the one heavy subtree (since replaced by `internal/clickhouse` on `net/http`: 14 modules fewer) |
| Simplicity | 3/5 | small Go files and functions, but dead ACP code, copy-paste between agent drivers, and a 4,400-line stylesheet |
| Extensibility | 3/5 | the command registry and agent driver interface are good; panels, languages, DB drivers and run types are spread over several files |
| AI-first | 3/5 | one good "ask the agent" path, but two features bypass it and only the Kubernetes diagnosis is redacted |
| Everything configurable | 2/5 | `settings.json` has 8 keys; agent, tool, LSP, shell and interval settings are constants |

## 1. Minimal dependencies — 4/5

- Go: 10 direct modules (wails v3, creack/pty, fsevents, x/sys, pgx, clickhouse-go, google/pprof for `/profile`, goldmark, go-keyring, esbuild for `cmd/webbuild` only), 28 indirect. npm: 22 runtime packages (17 CodeMirror, xterm and its WebGL addon, React, `@wailsio/runtime`); state management is our own 77-line `lib/store.ts`.
- clickhouse-go accounts for about 12 of the 28 indirect modules (ch-go, otel, lz4, brotli, …) for one driver that also supports HTTP.
- go-keyring shells out to `security` on macOS but adds dbus and wincred to the module graph.
- esbuild is only used by the build tool but is listed in the app module's `go.mod`.

Recommendations: drive the macOS `security` tool directly instead of go-keyring, with the password on stdin (S); move esbuild to its own module or a `tool` directive (S); evaluate ClickHouse over HTTP only on `net/http` (L, loses native-protocol speed).

## 2. Simplicity — 3/5

- `internal/acp` (about 1,900 lines) and `agentsession/acpdriver` (381 lines) are not registered anywhere. *Settled since: ACP agents are added from settings (`customAgents`) and registered by `syncCustomAgents`.*
- `ringbuffer.go`, `linereader.go` and `process_{unix,other}.go` exist three times (acp, claude, codex); LSP and DAP each implement the same Content-Length framing.
- Agents are invoked three ways: chat drivers, `agent.Headless` (commit messages), and a command typed into a terminal (conflict resolution).
- `style.css` is 4,371 lines; `log.Printf` and `slog` are mixed.

Recommendations: register ACP agents from configuration or delete the code (S); extract `internal/procio` and a shared Content-Length codec (S–M); split `style.css` per feature (M).

## 3. Extensibility — 3/5

| To add… | Files to touch | Hard-coded lists to replace with a registry |
|---|---|---|
| Sidebar panel | 4 frontend + service + `main.go` | `SidebarView` union, `ActivityBar` list, `App.tsx` lazy imports and switch, `view.*` commands |
| Language server | 4–5 | server specs and extension map in Go, `LSP_EXTENSIONS` and the status-bar map in TS, `needsTrust` |
| Agent CLI | new package + 2 | `registerDrivers`, `agent.known`, the `Headless` switch (the frontend is agent-agnostic) |
| Database driver | ~4 Go + ~10 TS | `Driver` union and `'clickhouse' ? … : …` checks across the database UI |
| Run-configuration type | 6 | type list, resolver, fixed detector list, dialog and widget |
| Command / shortcut | 1 | real registry, but 22 tooltips hard-code shortcut text that goes stale after rebinding |

Recommendations: a panel registry that generates the union, the activity bar, the panel switch and the view commands, and shortcut hints rendered from the command registry (S–M); a single language table served by the backend (M); a driver descriptor table for the database UI (M).

## 4. AI-first — 3/5

- Through `askAgent` (draft for the user to review): `⌘L` selection, file tree, terminal output, LSP problems, failed tests, Kubernetes diagnosis.
- Outside it: commit messages send the diff to the provider without a preview; conflict resolution runs in a terminal.
- Redaction: only the Kubernetes diagnosis removes secrets; terminal output, test output, LSP snippets and commit diffs are sent as they are.
- Missing where it would be natural: non-zero run-configuration exits, database query errors, HTTP failures, exited Docker containers, a debugger stopped on a panic, PR review threads.

Recommendations: redact every agent-bound context in one place (inside `askAgent`, plus the commit diff) (S); "Ask the agent" for run exits, database, HTTP and Docker failures (M); route conflict resolution through the chat and show the commit diff before it is sent, or document the exception (S–M).

## 5. Everything configurable — 2/5

- `settings.json` exposes theme, editor and terminal fonts, default agent, auto-save, background notifications, keymap and TODO excludes.
- The plumbing for agent binaries and models already exists (`claude.Options`, `codex.Options`) but is constructed empty.
- Constants a user would reasonably want to change: language-server commands and arguments; paths of kubectl, docker, gh, aws, rg, dlv, git and flux; terminal shell and scrollback; Kubernetes and AWS refresh intervals; diagnosis log lines; default database row limit; PR list size; tab width; the agent approval policy (kept in `state.json`).
- Fine as constants: message, diff and history caps, timeouts, debounce delays, validation limits.

Recommendations: `agents.<name>.{path, model, args}` (S); `tools.<name>.path` and `terminal.{shell, args, scrollback}` (S–M); `lsp.<language>.{command, disabled}`, with user-defined servers requiring a trusted folder (M).

## Top 10, by value for effort

1. Redact every agent-bound context in one place (`askAgent` and the commit diff). S
2. Agent path, model and argument settings, using the existing driver options. S
3. Settle ACP: configurable agents (`agents.custom[{name, command, args}]`, no code needed for a new CLI) or delete it. S–M
4. Shared `internal/procio` and Content-Length codec. S–M
5. Panel registry and shortcut hints from the command registry. S–M
6. "Ask the agent" for run exits, database, HTTP and Docker failures. M
7. Backend-served language table and user language-server overrides behind trust. M
8. Tool-path, shell, scrollback and interval settings. M
9. Database driver descriptor table in the UI. M
10. Split `style.css`; esbuild out of the app module; `security` instead of go-keyring; consider ClickHouse over HTTP. M–L

Fixed while writing this audit: the Kubernetes diagnosis could cut a UTF-8 character when truncating; the README advertised the unregistered ACP driver.
