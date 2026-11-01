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

### Vim mode

- `editor.vimMode` (Settings ▸ Editor, or "Toggle Vim Mode" in the palette) turns on Vim key bindings in every editor, live, through [`@replit/codemirror-vim`](https://github.com/replit/codemirror-vim) loaded as a separate chunk only when the mode is on. The status bar shows `-- NORMAL --`, `-- INSERT --` or `-- VISUAL --`.
- `:w`, `:q`, `:wq` and `:x` save and close the file's tab; in diffs and other read-only views they only show a note.
- A focused Vim editor keeps `⌃` plus a letter, `[` or `]` (redo, half-page scrolls, visual block, and so on); Flint's own `⌃R` / `⌃D` still work outside editors, and `⌘` shortcuts work everywhere.

### AI in the editor

- **Edit with Agent** (`⌘I`, "Edit Selection with Agent…"): a prompt above the selection (or the cursor's line). The default agent runs headless with the file path, the selection and `inlineEdit.contextLines` lines around it (secrets redacted), and its answer replaces the selection as one edit (`⌘Z` undoes it), but only if that text hasn't changed meanwhile. `Esc` stops the agent. Claude Code runs with only `Read`, `Grep` and `Glob` and no MCP servers; Codex with `--sandbox read-only`; a custom agent only in a trusted folder. Files that hold secrets (`.env*`, keys, `credentials`, kubeconfig, …) are refused.
- **Inline completions** (off by default): ghost text from a fill-in-the-middle model, Ollama (`/api/generate`) or an OpenAI-compatible server (`/v1/completions`), requested after a typing pause. `Tab` accepts, `Esc` or any other edit dismisses, and each new request cancels the previous one. Plain `http` is allowed only for `localhost`; for a remote endpoint secrets in the context are redacted, secret files are never sent, and the API key lives in the Keychain per endpoint origin. The status bar shows ✦ while on and ⚠ with the error (for example "Ollama is not running").

### Scratchpads

Persistent SQL consoles and notes (MD/TXT) with autosave. `⌘↵` runs the query under the cursor or the selection. Results go to a virtualized table (up to 200k rows) with export to TSV/JSON.

## AI agents

### Agent chat

- Native drivers, **no npm**: Claude Code (`claude -p` in `stream-json` mode with `--permission-prompt-tool stdio`) and Codex (`codex app-server`).
- Any other agent with an [Agent Client Protocol](https://agentclientprotocol.com) mode, added in Settings ▸ Agents ▸ Custom agents (ACP) with no code: Flint starts its command (for example `gemini --experimental-acp` or `opencode acp`) and speaks ACP over stdio (`internal/acp`, `agentsession/acpdriver`). See [Custom agents (ACP)](#custom-agents-acp).
- A shared panel in the right dock (`+` → claude / codex / a custom agent): streamed response, tool calls with a mini diff, the plan, and the list of changed files.
- Edits and reads go through without asking (the "auto: edits" policy); commands ask for permission.
- `⌘L` attaches the selection or the current file to the conversation.
- "Send to agent" (LSP problems, terminal output) uses the shared `askAgent` (`frontend/src/lib/askAgent.ts`): it picks the active or most recently used chat of the project the content belongs to (the file's project, or the active tab's project when there is no file). If there is none, it opens a new chat in that project with the preferred agent. The content goes into the draft so you can add to it; `⌥`-click sends it immediately.
- The chat header shows usage: cumulative input/output tokens, cost in USD when the agent reports it (codex usually does not), and a context fill bar (for example "42% context"), with details in a tooltip. The status bar shows the total for the project (with several projects, for the whole workspace, broken down in the tooltip). Usage is part of the saved session.

### Usage and budgets

- Usage is recorded in Go as it arrives: every `usage` event of a chat is one turn, added to a daily row per project (a task's chats count for the repository's main checkout), agent, model and agent task in `usage.json` (`internal/usage`, `usage_state.go`). Tokens in/out, cache read/write tokens when reported (Claude Code), the cost only when the agent reports it. The file is rewritten atomically after each turn and bounded by `usage.retentionDays` (default 90) and row/chat caps.
- No invented prices: tokens without a reported cost stay tokens, unless `usage.prices` gives the model a price ($/Mtok in/out); those costs are kept apart and always labelled as estimates.
- The Usage panel (∑, `UsageAPI`) shows today / 7 / 30-day totals, a per-day SVG bar chart (7 or 30 days), breakdowns by project, agent task, agent and model (click a project or task to filter), top chats, and a CSV export of the daily rows (native save dialog; a download in the browser build). It reloads on `usage:changed`, never polls.
- Budgets: daily (USD and/or tokens, all chats) and per agent task. When a turn reaches one, `usage:alert` fires once per threshold per day or task (remembered in `usage.json`, so not again after a restart): a warning under the chat header, a toast, or a background notification through `AlertsAPI`. With `usage.pauseOverBudget`, `AgentChatAPI.Prompt` refuses new turns until **Continue anyway** in the warning (per day and task).
- Nothing is sent anywhere; see [Files and data](../site/reference/files-and-data.md).

### Custom agents (ACP)

- Each entry in `customAgents` (settings.json) is an agent: an id (lowercase, not `claude` or `codex`), an optional name, a command (a name on the login PATH or an absolute path) and the arguments that start its ACP mode. Settings offers Gemini CLI (`gemini --experimental-acp`) and OpenCode (`opencode acp`) as one-click fill-ins; you install and log in to the CLI yourself.
- **Check** first asks, in a native dialog that shows the exact command line, before it runs a command line for the first time in a session. It starts the agent in an empty temporary folder, runs the ACP `initialize` handshake (25 s timeout) and shows its name and version, or the error with the end of what the agent printed. No session is opened.
- A custom agent appears in the agent menus, the default-agent pickers and "Ask the agent" once its command is found. Saving settings registers the agents again; open chats keep the agent process they started with.
- It starts only in a trusted folder (see [Restricted Mode](#restricted-mode-workspace-trust)). Its permission requests go through the same approval policy as the native agents. Flint offers it file reads and writes (ACP `fs/*`) only inside the chat's folder, never into `.git`, and no terminal; the agent runs in its own process group, which is killed when the chat closes and when Flint quits.
- There is no environment setting: `settings.json` is plain text, so API keys don't belong there. Log in with the agent's own command (`gemini`, `opencode auth login`) before using it.
- Custom agents can't be resumed after a restart; a restored chat continues in a new conversation and says so.

### Agent tasks (parallel agents in worktrees)

- "New Agent Task…" (palette, or `＋` in the Agent Tasks panel ⑂): a description, an agent (default from settings; Claude Code, Codex or a custom agent) and a base branch (default: current). Flint creates a branch (`agentTasks.branchPrefix` + a slug of the first line, made unique) and a worktree (`agentTasks.worktreeDir`, default `<repo>.worktrees/<branch>` like the branch widget), attaches it to the window, starts a chat there and sends the description as the first message (`TaskAPI`, `internal/agenttask`).
- The panel lists the tasks of the open repositories with their state (working / waiting for approval / idle / finished / failed), derived in Go from the chats' agent session events (`agenttask.Tracker`, bound when a chat starts or resumes in the worktree; no polling), the branch, commits ahead of base, and changed files with +/- against the merge base (committed, uncommitted and new files), refreshed on file-watcher and git events.
- Actions: Open (folder + chat), Show diff (per-file side-by-side diff against the merge base), ✦ Ask agent on a failed task (error in the chat draft), Merge (in the checkout where base is checked out; refused while the task or that checkout is dirty; conflicts are found with `git merge-tree` without touching anything, and Flint offers to merge base into the task and hand the conflicts to the agent in a terminal), Create PR (push + `gh pr create` after a native confirmation), Remove (stops the chats, deletes the worktree; uncommitted work and unmerged branches only after confirmation).
- Tasks are kept per repository (keyed by the common git dir) in `state.json` with their last settled state; a task whose worktree vanished is shown as stale and can be removed (only its own entry under `.git/worktrees` goes; other missing worktrees are left alone). A task's worktree is trusted when the checkout it started from is trusted; a task from an untrusted checkout may not put its worktree inside a trusted folder (trust is by path), and no task worktree may lie inside the checkout. Worktrees made by hand from the branch widget don't inherit trust. Branches are always named by full ref (`refs/heads/…`), so a same-named tag can't stand in for one; the base must be a plain local branch name.

### Agent context (Flint's MCP server)

- Chats started by Flint get Flint's own MCP server (`internal/mcpserver`, MCP over HTTP on `127.0.0.1`, standard library only) with read-only tools (`internal/mcptools`): `get_diagnostics` (language server problems), `get_editor_state` (open files, the active file and the selection), `get_test_results` (the last test runs), and, when turned on in Settings ▸ Agent Context, `get_terminal_output` and `get_kubernetes_status`.
- Claude Code gets it through `--mcp-config`, Codex through `-c mcp_servers.flint.*` (only when the installed Codex supports HTTP servers), custom ACP agents in `session/new` when they advertise HTTP MCP support. Commit messages and inline edits don't get it.
- Every chat gets its own token (in `FLINT_MCP_TOKEN`, never on a command line), revoked when the chat ends or its project closes; a token only sees its own project. Requests from web pages are refused (Host and Origin checks), answers are redacted and bounded, and turning the server off closes its port.
- Diagnostics, editor state and test results count as reads (no prompt). Terminal output and cluster status are off by default and always ask, because they can show anything, including what a command printed from outside the project. Untrusted folders get no server.

### Commit messages and conflicts

- ✦ *Generate message*: `claude -p` (or `codex exec`) receives the diff of the selected files and recent commit messages; the result lands in the message field for you to edit. A custom agent gets the same prompt in a one-shot ACP session in the repository: Flint serves reads inside it, declines every permission request and refuses writes sent through it; what the agent does with its own tools and its own configuration (for example a setting that lets it edit without asking) is outside Flint's control. The text after its last tool call is the message.
- ✦ *Resolve with claude*: merge conflicts are handed to an interactive agent session in a new terminal. The agent runs `git add` but does not commit. A custom agent has no interactive terminal mode, so when it is the default the first installed native agent (Claude Code, then Codex) does this, and the button names it; with no native agent installed the button is hidden.
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

`⌘,` (flint menu ▸ Settings…, or "Settings…" in the palette) opens a settings tab with Appearance, Editor, Terminal, Agents, Agent Context, Usage, AI in the Editor, Language Servers, Refresh, TODO List, Keymap, Updates and Import sections. Every change applies immediately, without a restart, and saves itself (agent settings apply to sessions started afterwards).

- **Theme**: Dark, Light or System (follows the macOS appearance, `prefers-color-scheme`). Colors are CSS variables in `style.css` (`:root` and `[data-theme="light"]`); terminals have their own palette (`lib/terminalTheme.ts`); Go switches the title bar and window buttons (`NSApp.appearance`). The theme from the last run is applied before the first render (a copy in `localStorage`, and the window appearance set in Go before the window is created), so a light theme never flashes dark.
- **Editor**: font family, size and line height for all CodeMirror editors (files, diffs, scratchpads); optional save on window focus loss.
- **Terminal**: font, size and line height for terminals and run consoles (xterm refits immediately and the PTY gets the new size).
- **Agents**: the default agent for new chats, commit messages and conflict resolution (a custom agent can be the default; conflicts then go to a native agent). Per agent (Claude Code, Codex): the executable (empty = looked up on the login PATH; otherwise a command name or an absolute path, never one relative to the project), the model (empty = the CLI's default) and extra command-line arguments, one per line. Extra arguments go before Flint's own flags, so Flint's flags are parsed last and win; arguments that name a flag Flint sets (the stream protocol, the permission prompt and mode, `--setting-sources`, `--settings`, `--resume`, `--model`; for Codex the sandbox, approval policy, `--yolo`, `--listen` and `-c approval_policy/sandbox*/projects*`) and `--` are refused, as are newlines, more than 32 arguments or arguments over 512 characters. In Restricted Mode the extra arguments are left out entirely, since a flag such as `--mcp-config` or `--add-dir` with a relative path would load the project's files. Changes apply to new chats, commit messages and conflict resolution; running chats keep their settings. Custom ACP agents are added, edited, checked and removed in the same section (see [Custom agents (ACP)](#custom-agents-acp)). Background notifications can be turned off here.
- **Language Servers**: per language (Go, TypeScript/JavaScript, Python, Rust): on/off, which server (a known server's name such as `pylsp`, an absolute path to one, or another server; empty tries the known servers in order) and extra arguments, added after the server's own (`--stdio`). A change restarts that language's servers for the open files. Restricted Mode still applies: servers that run the project's code and servers Flint doesn't know start only in trusted folders, and extra arguments are ignored there. typescript-language-server, also under another name or path, is always pinned to its own TypeScript, and `--tsserver-path` is refused.
- **Refresh**: how often the Kubernetes panel (default 5 s, 2–600 s) and the AWS panel (default 30 s, 5–3600 s) re-read their data while shown; values out of range are clamped. Git status, Docker and pull requests don't poll.
- **Keymap**: a searchable list of all commands (by name or by shortcut, for example `⌘K`). Click a shortcut and press a new combination to change it; conflicts with other commands are shown, `↺` restores the default and `✕` removes it. Shortcuts handled by CodeMirror (`⌘F`, `⌘S`, `⌘↵`, and so on) are display-only. Overrides go into a layer of the command registry (`lib/commands.ts`), so the palette and the dispatcher use the effective shortcuts.
- **Updates**: the daily release check (see [Update check](#update-check)) can be turned off; **Check Now** checks right away.
- **Agent Context**, **Usage** and **AI in the Editor**: see [Agent context](#agent-context-flints-mcp-server), [Usage and budgets](#usage-and-budgets) and [AI in the editor](#ai-in-the-editor).
- **Import** ("Import from VS Code…" in the palette): reads VS Code's, Cursor's or VSCodium's `settings.json`, `keybindings.json` and active color theme (from its extension, following `include`), and shows what it would change and what it skips and why (chords, `when` clauses, settings Flint doesn't have, built-in themes). Nothing is written until you confirm. An imported theme becomes `theme: "custom"`: a palette over the dark or light base, limited to Flint's own color variables and hex colors.
- **Trusted folders**: see [Restricted Mode](#restricted-mode-workspace-trust).

### macOS integration (Wails v3)

- **Drag from Finder**: a folder dropped on the window opens as a project (or is attached to the workspace when one is already open); a file opens in a tab (up to 20 at a time). While dragging, the window shows what will happen.
- **Single instance**: `flint <folder>` with Flint already running hands the folder over (relative paths resolve from the calling directory) and brings the window to the front. A Flint with a different `FLINT_CONFIG_DIR` (for example a dev build) is a separate instance.
- **Background notifications**: when a run, a test run or an agent turn finishes while Flint is not active, you get a macOS notification (clicking it shows the tab) and a badge on the Dock icon, cleared when you return to the window. Can be turned off in Settings ▸ Agents.
- The application menu (with `Settings… ⌘,`) and the compact title bar are native Wails v3 options, with no Objective-C additions.

## Diagnostics

- Flint writes its own log to `~/Library/Logs/Flint/flint.log`. It is rotated at 5 MB (one old file is kept), secrets and the home path are redacted, and the file mode is `0600`.
- Frontend errors (uncaught exceptions, rejected promises, render errors) and Go panics are logged.
- **Tool check** (**Help ▸ Check Tools…**, "Check Tools…" in the command palette, or the link on the Welcome screen) lists the external tools Flint drives, grouped (essentials, AI agents, languages and debugging, containers and cloud): a status dot, the version found or "Not installed", the feature each one enables, a link to its documentation page, and for missing tools an install command with a **Copy** button (Homebrew where there is a formula, `go install` for gopls and Delve, `rustup` for rust-analyzer). Language servers with alternatives count as installed when any one is found (vtsls or typescript-language-server; basedpyright, pyright or pylsp), and the row says which. A tool that is on `PATH` but whose version command fails (for example a rustup proxy without the rust-analyzer component) shows as "Not working" with its error. Only Git is required. **Recheck** runs the check again.
  - It opens by itself once, on the first launch (remembered in `state.json` as `toolCheckShown`).
  - The check waits for the login-shell `PATH`, then runs only fixed version commands (`--version` or equivalent), in parallel with a 5-second timeout, in the home folder and never in a project folder, so a repository's `.tool-versions`, `rust-toolchain.toml` or `.nvmrc` can't choose what runs. The Pyright language servers have no version flag and are not run at all. Nothing is installed by Flint.
  - One table in `internal/tools` feeds both the tool check and the bug report; language-server binaries come from the LSP server specs.
- **Help ▸ Report an Issue…** opens a prefilled GitHub issue in your browser and copies the full report to the clipboard. The report contains the versions of Flint, macOS, Go and Wails, the versions of the tools in the tool check, and the last 150 log lines, all redacted. You review the issue before submitting it.
- **Help ▸ Copy Diagnostics** copies the same redacted report to the clipboard.
- **Help ▸ Show Logs in Finder** reveals the log file in Finder.
- There is no telemetry. The only request Flint makes on its own is the update check below.

### Update check

- Flint asks GitHub's API for the latest release (`GET https://api.github.com/repos/ODudek/flint/releases/latest`): one anonymous request with `User-Agent: Flint/<version>`, no token, cookies or identifiers, a 10 s timeout and a 1 MB response limit (`internal/update`).
- Automatically: 5 s after launch and then once a day while Flint runs, at most once every 24 hours (the last check, its answer and a skipped version are kept in `state.json`, so a restart reuses the answer instead of asking again). Never in development builds (`dev`, `dev-<sha>`), and never when `checkForUpdates` is off (Settings ▸ Updates). A failed check is only logged.
- The running version (`vX.Y.Z` from `-ldflags`) is compared with the release tag by Flint's own SemVer compare; a pre-release sorts below its release.
- A newer release shows a notice in the status bar: **Download** opens the release page in the browser (only a `https://github.com/ODudek/flint/…` link from the API; anything else falls back to the releases page), **Skip This Version** stays quiet until a later release, **✕** hides it until the next launch.
- **Help ▸ Check for Updates…** (also in the palette and Settings ▸ Updates ▸ Check Now) checks immediately, ignoring the daily limit and the setting, and says "You're up to date (vX.Y.Z)" when there is nothing new.
- Flint never downloads or installs anything: there is no auto-update framework.

## Restricted Mode (workspace trust)

A folder opened for the first time is untrusted until you click **Trust Folder** in the banner (or run "Trust Folder" from the command palette). A downloaded archive or a fresh clone can carry configuration that runs programs, so in Restricted Mode Flint:

- does not auto-start a terminal (shell prompts run `git status` and version-manager hooks that read the folder);
- does not start language servers that execute project code: rust-analyzer (build scripts, proc macros, `.cargo/config` tools), the Python servers (they run the virtualenv interpreter the project points at) and any server set in Settings that Flint doesn't know. They start as soon as you trust the folder; extra language-server arguments from Settings are ignored until then;
- runs agents (chat, commit messages, conflict resolution) without the project's agent settings and hooks (Claude Code gets `--setting-sources user`, because `-p` mode skips its own trust prompt) and without the extra agent arguments from Settings;
- does not start custom ACP agents at all (chat or commit messages): Flint can't tell them to ignore the project's configuration, so the error asks you to trust the folder;
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
