# Flint feature reference

This is the full list of what Flint does and how each feature behaves. For a short overview see the [README](../README.md); for how it is built see [ARCHITECTURE.md](ARCHITECTURE.md).

Contents:

- [Editor and navigation](#editor-and-navigation)
- [AI agents](#ai-agents)
- [Git and GitHub](#git-and-github)
- [Run, test, debug and profile](#run-test-debug-and-profile)
- [Databases](#databases)
- [HTTP client](#http-client)
- [Infrastructure panels](#infrastructure-panels)
- [Settings and macOS integration](#settings-and-macos-integration)
- [Diagnostics](#diagnostics)
- [Restricted Mode (workspace trust)](#restricted-mode-workspace-trust)
- [Keyboard shortcuts](#keyboard-shortcuts)
- [Command palette](#command-palette)

## Editor and navigation

### Tabs

Files, terminals, agent chats, SQL consoles and diffs share one tab area, with file-type icons. A file opened from a terminal appears next to it, and `⌘[` goes back to the terminal (navigation history works like IntelliJ). `⌘J` toggles between the terminal and the last file. `+` opens a terminal, an agent chat or a SQL console.

### Terminal

- A real PTY (`creack/pty`) running your login shell with your `PATH`, rendered with WebGL. Multiple tabs.
- `⌘`+click on `path:line` in agent output opens the file at that line.
- Shift+Enter inserts a newline in claude/codex prompts.
- Selected output can be sent to the agent chat as a code block with `⌘L` or right-click → *Send to agent*.

### File tree

- Virtualized tree with selection (click, `⌘`+click, `⇧`+click, arrow keys).
- Right-click opens a context menu: New File / New Folder and Rename, edited in place (`F2` / `↵`); Move to Trash (`⌘⌫` / Delete, with confirmation, also for multiple selected items); Copy Path / Copy Relative Path; Reveal in Finder; Open in Terminal; Find in Folder; Add to Agent Chat. On test files it adds Run Tests (and Debug Tests for Go); inside a repository, Show History; on changed files, Show Diff and Rollback.
- This is the same context menu used by the terminal and the editor gutter (`openContextMenu`).
- Open tabs (including file history tabs) and breakpoints follow a rename. Tabs of deleted files are closed.

### Multiple projects in one window

You can attach more folders to an open project (like "Attach project" in IntelliJ):

- "Add Project to Workspace…" (`⇧⌘O`, File menu, or `⌘O` / Recent Projects with `⌥↵` or `＋`).
- The tree then has one section per project, each with "Remove from Workspace".
- `⌘P` and Search cover all projects (results are prefixed with the project; Find in Folder narrows to one).
- The Commit panel shows the repository of the active tab's project and has a switcher with the number of changes per project.
- New terminals and chats start in the active tab's project (the `+` menu has a section per project).
- Run configurations follow the active tab too: the ▶ selector in the title bar shows the configurations (`.flint/run.json` and detected ones) of the active tab's project, with its name. Each project remembers its own selection, and Run / Debug start in that project.
- "Fix with agent", `⌘L` and test results go to the chat of the project the file belongs to.
- The command palette has "Add Project to Workspace…" and "Remove <project> from Workspace".
- LSP servers and the file watcher run per project.
- The set of projects is restored after a restart, together with the session of the main project.

### Live agent changes

FSEvents with one recursive subscription per project. The file tree refreshes and open files reload in place with a minimal diff. If you have unsaved changes, you get a warning.

### Markdown and image preview

- **Markdown**: an Editor / Split / Preview switch on `.md` files (`⇧⌘V`). Rendering happens in Go (goldmark, GFM: tables, task lists, strikethrough, autolinks), live while you type. In Split mode the preview follows the heading visible in the editor. Raw HTML and `javascript:` links are dropped. Relative links open files in the editor, external links open in the browser, and relative images load only from open folders.
- **Images** (`png jpg gif webp svg ico bmp`) open in a preview tab: fit to window, zoom with `⌘+` / `⌘-` / `⌘0` and `⌘`+scroll wheel, a checkerboard behind transparency, dimensions and file size. SVG can be switched to its source. Limit: 25 MB per image.

### Search and replace

- `ripgrep --json`. Results stream in batches and are rendered virtually. Without `rg` a slower pure-Go fallback is used.
- **Project-wide replace** (`⇧⌘R` or ⇄ in the panel): a preview at every match (old text struck through, new text next to it), checkboxes per file and per line, and `$1` / `${name}` groups in regular expressions.
- The preview and the write use the same Go matcher, so exactly what you see is what gets written.
- On write every file is read again: files changed since the search, binary files, files that are too large and files with unsaved changes in the editor are skipped.
- Writes are atomic and preserve permissions and line endings. The last replace can be undone (*Undo*).

### Go to file (`⌘P`)

The file list comes from `rg --files` (respects `.gitignore`) and is kept in Go. Fuzzy matching also runs in Go; only the top 60 results are sent to the WebView. There is no up-front indexing.

### TODO list

Panel ☑ (palette: "TODO List"):

- Markers `TODO`, `FIXME`, `HACK`, `XXX`, `BUG` (whole words, upper case) across all workspace folders, using the same search as `⇧⌘F` (ripgrep, honoring `.gitignore`).
- Grouped by file, with the author from `TODO(who)`, and filters by marker and text. Click opens the file at the line.
- Refreshes after file changes (debounced), only while the panel is open.

### Language servers (LSP)

- `gopls`, `rust-analyzer`, `vtsls` / `typescript-language-server`, `basedpyright` / `pyright`. A server starts in the background on the first file of its language.
- Completion, hover, `⌘`+click / `⌘B` / `F12` go to definition, `⌥F7` / `⇧F12` Find Usages, live diagnostics (`⇧⌘M` problems list, `F8` next problem).
- On errors and warnings there is a *Fix with agent* button (and *Fix all N in file*, also `⌥⇧⌘L`): the chat receives the path, line:column, severity, source (for example gopls), message and the surrounding lines as an attachment.
- Server status and an install hint are shown in the status bar.

### Scratchpads

Persistent SQL consoles and notes (MD/TXT) with autosave. `⌘↵` runs the query under the cursor or the selection. Results go to a virtualized table (up to 200k rows) with export to TSV/JSON.

## AI agents

### Agent chat

- Native drivers, **no npm**: Claude Code (`claude -p` in `stream-json` mode with `--permission-prompt-tool stdio`) and Codex (`codex app-server`). (An ACP client is in the code base but not wired into the app yet; see the [principles audit](audits/2026-10-principles.md).)
- A shared panel in the right dock (`+` → claude / codex): streamed response, tool calls with a mini diff, the plan, and the list of changed files.
- Edits and reads go through without asking (the "auto: edits" policy); commands ask for permission.
- `⌘L` attaches the selection or the current file to the conversation.
- "Send to agent" (LSP problems, terminal output) uses the shared `askAgent` (`frontend/src/lib/askAgent.ts`): it picks the active or most recently used chat of the project the content belongs to (the file's project, or the active tab's project when there is no file). If there is none, it opens a new chat in that project with the preferred agent. The content goes into the draft so you can add to it; `⌥`-click sends it immediately.
- The chat header shows usage: cumulative input/output tokens, cost in USD when the agent reports it (codex usually does not), and a context fill bar (for example "42% context"), with details in a tooltip. The status bar shows the total for the project (with several projects, for the whole workspace, broken down in the tooltip). Usage is part of the saved session.

### Commit messages and conflicts

- ✦ *Generate message*: `claude -p` (or `codex exec`) receives the diff of the selected files and recent commit messages; the result lands in the message field for you to edit.
- ✦ *Resolve with claude*: merge conflicts are handed to an interactive agent session in a new terminal. The agent runs `git add` but does not commit.
- Nothing requires accepting diffs.

## Git and GitHub

### Git (IntelliJ-style)

- Commit panel (`⌘K`) with checkboxes, Amend, Commit and Push, and Rollback. Side-by-side diff in a tab.
- Changed-line markers in the gutter, which also show what the agent touched.
- Branch widget in the status bar: checkout, new branch, fetch, pull, push (`⇧⌘K`).
- A commit during a merge includes the whole index.
- **Annotate** (blame): a column with date and author next to every line (*Blame* button in the status bar, or right-click on the editor gutter), with a tooltip (full hash, author, date, message). Click opens the change from that commit. Uncommitted lines (including unsaved ones) show "Not committed".
- **Show History** of a file (`git log --follow`, including renames) in its own tab: the list of commits, the diff of the selected commit below it, and *Compare with current*.
- **Stash** in the Commit panel: save with a message (optionally including untracked files), and a list with Apply / Pop / Drop (with confirmation) and a diff of each file.
- Works for files in nested worktrees too.

### Worktrees

List, create (`<repo>.worktrees/<branch>`), remove, switch, open in a new window, or open directly with a terminal for an agent. Checkouts and commits made in a terminal refresh the UI, because the git directory is watched as well.

### GitHub pull requests

PR review inside the IDE through the GitHub CLI (`gh`), with no token handling of its own: `gh` signs in the same way it does in your terminal (keyring, `GH_TOKEN`, GitHub Enterprise).

- **Pull Requests** view (⇅ icon on the activity bar, or "Pull Requests…" in the palette) lists the PRs of the active tab's project repository.
  - Filters: *All / Created by me / Review requested* and *Open / Closed / Merged / All states*.
  - Each PR shows CI state (✓ / ✗ / ●, with counts of passed, failed and running checks in the tooltip), review decision (Approved, Changes requested, Review required, Draft, Merged) and time of last update.
  - Without `gh`, without a login, or without a GitHub remote, the view explains what to do (`brew install gh`, `gh auth login`).
- Clicking a PR opens a PR tab: title, branches, author, an expandable list of checks with links, description (Markdown rendered in Go, no raw HTML), files with +/− and the number of open threads, commits, reviews, plus *Check out* (`gh pr checkout`; refuses when tracked files have uncommitted changes) and *Open on GitHub*.
- Clicking a file opens a side-by-side diff (merge base ↔ head, as on GitHub) with **inline comments**:
  - A thread under the line with reply (`⌘↵`) and *Resolve* / *Unresolve*. The other side gets a spacer of the same height so the sides stay aligned.
  - `+` in the gutter on lines GitHub allows comments on (inside hunks) opens a new comment: *Add single comment* (posted immediately) or *Start a review* / *Add review comment* (a pending review visible only to you).
  - Outdated threads and file-level comments are shown above the diff.
- *Submit review* bar (in the PR tab and in the diff): a summary plus *Comment*, *Approve*, *Request changes*; with a pending review, the number of comments and *Discard*.
- Nothing refreshes in the background. The list loads when the view opens and when you return to the window; a PR loads when its tab opens and when you return to the window; both also refresh with ↻. Go caches `gh` responses in memory for about 30 seconds.

## Run, test, debug and profile

### Run configurations (IntelliJ-style)

- Configurations live in `.flint/run.json` (meant to be committed, like `.idea/runConfigurations` or `.vscode/launch.json`). Types:
  - `command`: a program with arguments, or a shell line;
  - `go`: build a package or file, then run the binary;
  - `npm`: a `package.json` script; the package manager is chosen from the lockfile;
  - `make`.
- Working directory, environment variables and a "before launch" step.
- Without configurations Flint detects Go `main` packages, `Makefile` targets and `package.json` scripts on its own (they are written to the file only after you edit them).
- Selector with ▶ Run / ↻ Rerun / ■ Stop in the title bar. Output goes to a tab with xterm.js and clickable `path:line`.
- The process runs in a PTY in its own process group, so Stop (SIGINT, then SIGKILL after 3 s) also ends child processes (`go run`, `npm run`).
- The `debug` field is reserved for the DAP debugger (`dlv dap`), and `profileURL` points at the program's `net/http/pprof` endpoint for profiling (see [Profiling](#profiling-go)).

### Tests

- ▶ in the gutter next to `func TestXxx`, `t.Run("…")` subtests, and `describe` / `it` / `test` in `*.test.ts(x)` / `*.spec.*`.
- ▶ next to `package` (or on line 1 of a JS file) runs the whole file; `⌥`-click runs the whole package.
- The result is shown next to each test: ✓ / ✗ / ↷.
- *Tests* tab: a tree of packages and tests with status and duration, failure output with clickable locations, "Rerun failed", and rerun of a single test.
- Go: `go test -json`, parsed in Go. JS: `node --test --test-reporter=tap` (also through the repository's `test` script, `webbuild --test`). Other runners (jest, vitest) run through the `test` script and report only the exit code.

### Debugger

- Go through Delve (`dlv dap`; the DAP client is written in Go: `internal/dap`).
- The bug button next to ▶ in the title bar (`⌃D`) debugs the selected `go` configuration; `⇧`-click on ▶ next to a test debugs that test.
- Breakpoints: click in the gutter of `.go` files, or `⌘F8`. They move with edits and are part of the saved session.
- When stopped, the editor shows the current line, and the *Debug* tab shows stack frames, variables (expanded lazily) and a console with program output and expression evaluation.
- Stepping: `F9` resume, `F8` step over, `F7` step into, `⇧F8` step out, `⌘F2` stop.
- Requires `dlv` (`go install github.com/go-delve/delve/cmd/dlv@latest`) and macOS developer mode (`sudo DevToolsSecurity -enable`).

### Profiling (Go)

- **Tests** (the main path): "Profile Tests in File" / "Profile Package Tests" in the ▶ menu in the title bar, in the palette, in the file tree menu (Profile Tests), and on right-click of a gutter ▶ (also for a single test). Flint runs `go test -cpuprofile … -memprofile … -o …` (one package at a time) in the *Tests* tab, with the profiles and test binary in a temporary directory. When it finishes, a profile tab opens with CPU and memory (`alloc_space`, switchable to other sample types).
- **Configurations**: Go programs do not profile themselves, so "Profile '<configuration>'" requires the `profileURL` field (a `net/http/pprof` endpoint such as `http://localhost:6060/debug/pprof/profile?seconds=10`; without `seconds`, 10 s; localhost only). Flint starts the configuration if it is not running, waits until the program is listening, and fetches the profile. Without `profileURL` it opens the configuration editor.
- Any `.pprof` file can be opened too ("Open Profile File…").
- Go parses the profile (`github.com/google/pprof/profile`) and builds a call tree with flat/cum values, dropping frames below 0.5% of the total (a few dozen KB reach the UI).
- The profile tab (loaded lazily) draws a flame graph on a single `<canvas>`:
  - tooltip with function, `file:line`, flat/cum and %;
  - click zooms into a frame (ancestors stay at the top and zoom out); `Esc` / Reset Zoom goes back;
  - `⌘`-click or double-click opens the source at that line;
  - search highlights functions and shows their share.
- The *Top* view is a table of functions (flat, cum, %, sortable). "Raw .pprof" copies the path or reveals the file in Finder (for example for `go tool pprof`).
- Colors come from the theme (`--flame-*` in `style.css`).

## Databases

### Connections and browsing

- PostgreSQL (`pgx`) and ClickHouse (native TCP or HTTP, chosen per profile).
- Passwords are stored in the macOS Keychain, not in files.
- Under each connection, a "Databases (N)" node lists all databases on the server, as in pgAdmin. Postgres excludes templates, and databases without the `CONNECT` privilege get a red cross. ClickHouse shows what `SHOW DATABASES` returns.
- Expanding a database opens a separate pool to it (same host, user and password) and shows its schemas and tables.
- Flint closes pools for non-default databases after one minute of inactivity, and immediately before a modifying statement (for example `DROP DATABASE`) so its own connections do not block the server.
- The SQL console has a database selector next to the connection, and the scratchpad remembers the choice.
- The database in a profile is only a default and is optional. Without it Flint connects to `postgres` (ClickHouse: `default`).
- SQL autocompletion from the selected database's schema; read-only mode.

### Query history

- Go records every executed statement (including failed ones, EXPLAIN, and edits from the results table) per connection and database: text, start time, duration, row count and error.
- The *History* button in the console or "Query History…" (`⇧⌘H`) opens the list, newest first, with a fuzzy filter (computed in Go; at most 300 entries reach the WebView), an "All databases" toggle and actions: `↵` inserts into the editor, `⌘↵` runs again, `⌘C` copies.
- The last 5000 statements per connection are kept.
- Passwords in `CREATE/ALTER ROLE … PASSWORD '…'` and ClickHouse `IDENTIFIED … BY '…'` are masked; otherwise the text is stored as is (statements longer than 256 KB are truncated).

### EXPLAIN

- *Explain* (`⇧⌘E`) and *Analyze* (`⇧⌘A`) in the console toolbar and the palette.
- Postgres: `EXPLAIN (FORMAT JSON)`, and with Analyze `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)`. Go parses the JSON (`internal/db/explain.go`) into a tree of nodes with type, relation, index, cost, estimated and actual rows, loops, time, buffers and filter.
- The view is an expandable tree with a bar showing each node's share of total self time (Analyze) or self cost (estimate). Clicking a node shows details (conditions, sort keys, buffers).
- Warnings: a Seq Scan reading at least 10k rows (without Analyze: at least 10k estimated rows or cost of at least 2500), a row estimate off by 10x or more, and a sort or hash spilling to disk.
- ANALYZE always runs the statement in a transaction that is rolled back at the end. Before a statement other than SELECT (for example `DELETE`, `WITH … UPDATE`, `SELECT … INTO`) Flint asks for confirmation (Go decides, `ExplainResult.needsConfirm`). On a read-only connection such a statement is rejected.
- EXPLAIN uses the extended protocol, so it accepts only one statement.
- ClickHouse: `EXPLAIN PLAN indexes = 1` or (Analyze → *Pipeline*) `EXPLAIN PIPELINE`, shown as a tree built from indentation (best effort). Neither executes the query.

### Editing results

- Postgres: when all table-backed columns of a result come from a single table (the table OID from pgx `FieldDescriptions`, no extra query), the console fetches that table's columns and primary key from `pg_attribute` / `pg_index` in the background after showing the results.
- If the table has a primary key and all of its columns are in the result, cells become editable: double-click or `↵` / `F2` opens a type-aware editor (text, number with validation, boolean as a list, JSON in a multi-line field with validation, a NULL toggle). "+ Row" adds a row (unfilled columns get DEFAULT), "− Row" / `⌘⌫` marks a row for deletion.
- Changes are local and highlighted until submitted. *Revert* discards them; *Submit…* shows the generated SQL and, after *Apply*, runs `UPDATE` / `INSERT` / `DELETE` by primary key in a single transaction.
- Identifiers are quoted and checked against the catalog. Values are sent only as parameters (`$n::text::type`, so the server parses them like a literal). An `UPDATE` / `DELETE` that does not hit exactly one row rolls back everything.
- Computed columns, `GENERATED ALWAYS AS IDENTITY`, arrays and `bytea` are read-only. So are join results, views without a key, and read-only connections.
- ClickHouse: read-only.

## HTTP client

IntelliJ-style `.http` / `.rest` files with requests to your own APIs.

### File format

- Requests are separated by `###` (with an optional name).
- The request line is `METHOD URL [HTTP/1.1]` (no method means GET, no scheme means `http://`). A long URL can continue on indented lines starting with `?` / `&`.
- Then headers, an empty line and the body: inline, or from a file with `< ./body.json` (path relative to the `.http` file; `<@` also substitutes variables in it).
- Comments `#` and `//`. Directives: `# @name name`, `# @no-redirect`, `# @no-cookie-jar`, `# @timeout 60` (seconds, or for example `2m`).
- Variables `{{name}}`:
  - from `@name = value` lines (they apply to requests below the definition and take precedence);
  - from the environment in `http-client.env.json` and `http-client.private.env.json` next to the file (a `$shared` section applies to all environments; the private file overrides the public one);
  - dynamic: `{{$uuid}}`, `{{$timestamp}}`, `{{$isoTimestamp}}`, `{{$randomInt}}` (0–1000).
- Syntax highlighting, ▶ in the gutter next to every request, live syntax errors, and on send also unknown variables, always with a line number.

### Sending requests

- A request is sent **only** on an explicit action: `⌘↵` in a `.http` file (in SQL consoles `⌘↵` still runs the query), ▶, or "Send Request".
- A bar above the editor: environment, "Follow redirects" and ▶ Send.
- Go sends the request (`net/http`): 30 s timeout, Cancel, a separate cookie jar per file, and readable TLS errors.
- The response opens in a tab right after the file: status, time, size, collapsible headers, and the body in a read-only editor with JSON / XML / HTML highlighting (Go formats JSON; the type comes from `Content-Type` or is detected from the body). For bodies over 5 MB the beginning is shown, and "Save to file" writes the whole body.
- "Copy as curl" and a history of the last few responses for each request (in memory only, with a limit).
- Palette: "Send Request", "New HTTP Request File", "Select HTTP Environment", "Copy Request as curl", "Toggle HTTP Follow Redirects", "Clear HTTP Cookies".

**Add `http-client.private.env.json` to `.gitignore`**: it holds secrets (tokens, passwords). Flint does not write headers or variable values to its logs.

## Infrastructure panels

### Docker / Compose

Panel ⛴ (palette: "Docker"), through the `docker` CLI (Docker Desktop, Colima, OrbStack):

- Compose projects (from the labels of running containers and from `compose.yaml` / `docker-compose.yml` files in project folders and one level below) with their services, plus all other containers.
- Status (running / unhealthy / paused / stopped) and ports that open in the browser on click.
- Actions: Logs (`docker logs -f` in a terminal tab), Shell (`docker exec -it` with bash or sh), Start / Stop / Restart / Remove (with confirmation). For a project: Up / Down (with confirmation) / Restart / Stop / Pull.
- Changes arrive from `docker events` only while the panel is visible.
- Container names are validated before they reach a terminal.

### AWS profiles

Panel ☁ (palette: "AWS Profiles"):

- Profiles from `~/.aws/config` and `~/.aws/credentials` (or `AWS_CONFIG_FILE` / `AWS_SHARED_CREDENTIALS_FILE`) with their kind (SSO, session keys from saml2aws and similar tools, static keys, role, `credential_process`), account, role, region and validity.
- Validity comes from the SSO token in `~/.aws/sso/cache` and, for session keys, from `x_security_token_expires` / `aws_expiration`. Time to expiry counts down live; the list refreshes every 30 s and when you return to the window.
- **Terminal as profile** (double-click or `❯_`): a shell with `AWS_PROFILE` and `AWS_REGION`, in a tab named `aws:<profile> · …`. The profile survives a restart.
- **Log In** runs `aws sso login` or `saml2aws login --role …` in a terminal.
- **Refresh (↻)** does the same without a terminal: `aws sso login` (waits up to 5 minutes for approval in the browser), saml2aws with `--skip-prompt --force` (stored password and role), and for roles and `credential_process` an STS call that fetches and caches the credentials. There is no stdin: when the tool asks for something (for example an MFA code), refresh fails immediately with a "Log In" button. A running refresh can be cancelled (■).
- aws-azure-login is not supported because it is deprecated; its settings (`azure_*`) are ignored.
- **Check Identity** asks STS (`aws sts get-caller-identity`).
- Secrets (keys, tokens) are read only in Go, to determine that they exist, and never reach the UI.

### Kubernetes

Panel ☸ (palette: "Kubernetes"), through `kubectl` (any sign-in method: `aws eks get-token`, GKE, OIDC, and so on).

**Context and namespace.** Pick a context from your kubeconfig (EKS names are shortened to "cluster (account)") and a namespace; the choice is remembered between runs. **The global current-context never changes**; ★ changes it on request. Every command passes an explicit `--context` and `--namespace`.

**Apps view (default).** Objects are grouped into apps by, in order:

1. Flux Kustomization label `kustomize.toolkit.fluxcd.io/name`;
2. Argo CD label `argocd.argoproj.io/instance`;
3. Helm release annotation `meta.helm.sh/release-name`;
4. `app.kubernetes.io/instance` or `app.kubernetes.io/name`, or the `app` label;
5. otherwise, a single workload is its own app.

Each app shows:

- Flux Kustomization readiness and revision;
- Ingress routes → Service (ready/total backing pods) → Deployment / StatefulSet / DaemonSet;
- rollout state computed like `kubectl rollout status`: complete (with revision), rolling out (with the reason), failed (on `ProgressDeadlineExceeded`), paused, or scaled down; plus the images;
- pods with status, restarts, pods of the previous revision marked, and the concrete problem, such as "CrashLoopBackOff, last OOMKilled (exit 137)" or the image pull error;
- recent Warning events attached to the app.

Unhealthy apps are sorted first and expanded.

**Resources view.** Flat lists: workloads (Deployment, StatefulSet, DaemonSet: ready / desired), pods (status as in `kubectl get pods`, such as CrashLoopBackOff or Terminating, readiness, restarts, age), services with ports, and jobs. A filter is available. The panel refreshes every 5 s while it is visible.

**Actions** (context menu; double-click opens logs): logs (`kubectl logs -f`), shell in a pod (bash or sh, with container choice), describe, port-forward, rollout status / history, rollout restart, undo rollout, scale, delete pod, and `flux reconcile`. Destructive actions ask for confirmation (including scaling to 0).

**Diagnose with AI** (✦ AI on an unhealthy app) gathers the rollout state, problems, events, and the last 60 log lines (and the previous crashed run) of up to 4 failing pods. It redacts secrets and home paths and puts a read-only investigation request into the agent chat draft, which you review before sending.

**Terminal in a context** (`❯_`): `KUBECONFIG` starts with a small override file holding only `current-context` and the namespace (kubectl takes the first match), followed by your own kubeconfig. No credentials are copied. The tab is named `k8s:<cluster>/<namespace>`.

**EKS.** EKS contexts show the AWS profile from the kubeconfig's `AWS_PROFILE` and its validity, with one-click refresh; when the login has expired there is a "Log In" button.

## Settings and macOS integration

### Settings

`⌘,` (flint menu ▸ Settings…, or "Settings…" in the palette) opens a settings tab with Appearance, Editor, Terminal, Agents and Keymap sections. Every change applies immediately, without a restart, and saves itself.

- **Theme**: Dark, Light or System (follows the macOS appearance, `prefers-color-scheme`). Colors are CSS variables in `style.css` (`:root` and `[data-theme="light"]`); terminals have their own palette (`lib/terminalTheme.ts`); Go switches the title bar and window buttons (`NSApp.appearance`). The theme from the last run is applied before the first render (a copy in `localStorage`, and the window appearance set in Go before the window is created), so a light theme never flashes dark.
- **Editor**: font family, size and line height for all CodeMirror editors (files, diffs, scratchpads); optional save on window focus loss.
- **Terminal**: font, size and line height for terminals and run consoles (xterm refits immediately and the PTY gets the new size).
- **Agents**: the default agent for new chats, commit messages and conflict resolution. Background notifications can be turned off here.
- **Keymap**: a searchable list of all commands (by name or by shortcut, for example `⌘K`). Click a shortcut and press a new combination to change it; conflicts with other commands are shown, `↺` restores the default and `✕` removes it. Shortcuts handled by CodeMirror (`⌘F`, `⌘S`, `⌘↵`, and so on) are display-only. Overrides go into a layer of the command registry (`lib/commands.ts`), so the palette and the dispatcher use the effective shortcuts.
- **Trusted folders**: see [Restricted Mode](#restricted-mode-workspace-trust).

### macOS integration (Wails v3)

- **Drag from Finder**: a folder dropped on the window opens as a project (or is attached to the workspace when one is already open); a file opens in a tab (up to 20 at a time). While dragging, the window shows what will happen.
- **Single instance**: `flint <folder>` with Flint already running hands the folder over (relative paths resolve from the calling directory) and brings the window to the front. A Flint with a different `FLINT_CONFIG_DIR` (for example a dev build) is a separate instance.
- **Background notifications**: when a run, a test run or an agent turn finishes while Flint is not active, you get a macOS notification (clicking it shows the tab) and a badge on the Dock icon, cleared when you return to the window. Can be turned off in Settings ▸ Agents.
- The application menu (with `Settings… ⌘,`) and the compact title bar are native Wails v3 options, with no Objective-C additions.

## Diagnostics

- Flint writes its own log to `~/Library/Logs/Flint/flint.log`. It is rotated at 5 MB (one old file is kept), secrets and the home path are redacted, and the file mode is `0600`.
- Frontend errors (uncaught exceptions, rejected promises, render errors) and Go panics are logged.
- **Help ▸ Report an Issue…** opens a prefilled GitHub issue in your browser and copies the full report to the clipboard. The report contains the versions of Flint, macOS, Go and Wails, the versions of installed tools (git, rg, gh, docker, kubectl, aws, dlv, gopls, node, claude, codex), and the last 150 log lines, all redacted. You review the issue before submitting it.
- **Help ▸ Copy Diagnostics** copies the same redacted report to the clipboard.
- **Help ▸ Show Logs in Finder** reveals the log file in Finder.
- Nothing is sent automatically. There is no telemetry.

## Restricted Mode (workspace trust)

A folder opened for the first time is untrusted until you click **Trust Folder** in the banner (or run "Trust Folder" from the command palette). A downloaded archive or a fresh clone can carry configuration that runs programs, so in Restricted Mode Flint:

- does not auto-start a terminal (shell prompts run `git status` and version-manager hooks that read the folder);
- does not start language servers that execute project code: rust-analyzer (build scripts, proc macros, `.cargo/config` tools) and the Python servers (they run the virtualenv interpreter the project points at). They start as soon as you trust the folder;
- runs agents (chat, commit messages) without the project's agent settings and hooks (Claude Code gets `--setting-sources user`, because `-p` mode skips its own trust prompt);
- runs Git without hooks, even on commit and push (`core.hooksPath=/dev/null`).

Regardless of trust:

- Git never runs repository-defined clean/smudge filters, textconv drivers or fsmonitor for Flint's own git calls (status, blame, diff); filters from your global config, such as Git LFS, still work;
- typescript-language-server only starts when it can be pinned to its own TypeScript, never the project's `node_modules/typescript`;
- a `.http` request can only send body files (`< file`) from inside the open project;
- the Markdown preview doesn't load remote images until you click "Load remote images" (no tracking pixels);
- database TLS verifies the server certificate and host name; "Don't verify certificate (insecure)" is an explicit opt-out for private CAs;
- credentials are removed from terminal scrollback before a session is saved.

Trusted folders (and their subfolders) are remembered in `state.json`; "Revoke Folder Trust" in the command palette turns Restricted Mode back on.

## Keyboard shortcuts

All shortcuts except those owned by CodeMirror can be changed in Settings ▸ Keymap.

### General and navigation

| Shortcut | Action |
|---|---|
| `⇧⌘P` | Command palette |
| `⌘,` | Settings (including the keymap) |
| `⌘[` / `⌘]` | Back / forward |
| `⌘O` | Open folder |
| `⇧⌘O` | Add project to workspace |
| `⌘P` | Go to file |
| `⇧⌘F` | Find in files |
| `⇧⌘R` | Replace in files |
| `⌘B` | Toggle sidebar (in an editor: go to definition) |
| `⌘W` | Close tab |
| `⌘S` | Save |

### File tree

| Shortcut | Action |
|---|---|
| `F2` / `↵` | Rename |
| `⌘⌫` | Move to Trash |

### Editor and language servers

| Shortcut | Action |
|---|---|
| `⌘B` / `F12` | Go to definition |
| `⌥F7` | Find usages (also `⇧F12`) |
| `⇧⌘M` | Problems list |
| `F8` | Next problem (when not debugging) |
| `⇧⌘V` | Markdown preview |

### Agents

| Shortcut | Action |
|---|---|
| `⌘L` | Send to agent (selection in the editor or terminal) |
| `⌥⇧⌘L` | Fix all problems in file with the agent |

### Git

| Shortcut | Action |
|---|---|
| `⌘K` | Commit (in a terminal: clear the screen) |
| `⇧⌘K` | Push |

### Terminal and scratchpads

| Shortcut | Action |
|---|---|
| `⌘J` | Toggle terminal / last file |
| `⇧⌘T` | New terminal |
| `⌘N` | New SQL console |
| `⌘↵` | Run query (SQL) / send request (`.http`) |

### Run, test and debug

| Shortcut | Action |
|---|---|
| `⌃R` | Run the selected configuration (rerun if running) |
| `⌃⇧R` | Run the test under the cursor (outside a test file: the selected configuration) |
| `⌃D` | Debug |
| `⌘F2` | Stop |
| `⌘F8` | Toggle breakpoint |
| `F9` / `F8` / `F7` / `⇧F8` | Resume / step over / step into / step out (while debugging) |

### Databases

| Shortcut | Action |
|---|---|
| `⇧⌘E` | EXPLAIN |
| `⇧⌘A` | EXPLAIN ANALYZE (ClickHouse: PIPELINE) |
| `⇧⌘H` | Query history |

## Command palette

The command palette (`⇧⌘P`, like "Find Action" in IntelliJ) fuzzy-searches every command in the app (navigation, projects, git, GitHub, terminal, agent, run, profiling, databases, scratchpads), shows each command's category and shortcut, and keeps recently used commands at the top.

Commands and global shortcuts come from a single registry (`frontend/src/lib/commands.ts`). A new feature adds its actions with `registerCommand`.
