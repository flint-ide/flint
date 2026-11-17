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
- [Extensions](#extensions)
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
- A relative path is looked up in the folder the shell is in now (it may have `cd`-ed away from where the terminal started), then the folder the terminal started in, its project and the workspace folders; the first that has the file opens. If none has it, the first is opened and its error shows. Paths in run consoles are tried against the run's folders (`openRunLink`); CI logs map paths in Go.
- Shift+Enter inserts a newline in claude/codex prompts.
- Selected output can be sent to the agent chat as a code block with `⌘L` or right-click → *Send to agent*.

### Background terminals

- Off by default. **Settings ▸ Terminal ▸ Keep terminals running when Flint closes** (`sessions.background`) runs terminals in a separate Flint process, the background host, instead of inside the window. The change applies the next time Flint starts.
- With it on, closing the window and Quit (`⌘Q`) detach: Flint exits and the shells, and whatever runs in them (a build, a server, the claude TUI), keep running. On the next start a saved terminal tab whose shell still runs reattaches: the host replays the output it kept (`sessions.replayKiB`, 1 MiB per terminal by default, at most 8 MiB) and a full-screen program redraws. Nothing is typed into it and no new shell starts. A saved terminal whose shell is gone comes back as before, as a new shell with its old output dimmed above.
- Closing a terminal tab ends its shell, as without background terminals. **Quit and Stop Background Sessions** (app menu on macOS, File menu on Linux, next to Quit) ends every background shell and the host, then quits; with the setting off the item is hidden.
- Terminals the host runs that no tab claims (the session file was lost, or they belong to another project) are never opened or ended on their own: Flint says so once, and **Show Background Terminals…** in the command palette lists them with Attach (opens a tab) and End.
- What does not survive: Docker and extension shells (they need the window), the host itself (if it crashes or the computer restarts, the shells end and the tabs come back as new shells) and terminals of a host from another Flint version if you choose **Restart Background Sessions** when Flint asks. Choosing **Keep Them** leaves that host running and this run's terminals run inside Flint.
- If the background host cannot be started or reached within a few seconds, terminals run inside Flint for that run and Flint shows why. If the connection breaks while Flint is open, Flint reconnects once and reattaches every terminal where it left off; a terminal the host no longer has, or a second failure, shows a message on its tab and ends there.
- Agent chats are not covered yet: they stay inside Flint and end when it quits, whatever the setting says.

### Remote terminals

- Terminals can run on another computer over your own `ssh`. Add machines under **Settings ▸ Machines** (id, name, target `host`, `user@host` or an alias of `~/.ssh/config`, port, command), then use **New Terminal on Machine…** in the command palette or **＋ ▸ Terminal on <machine>**. The shell starts in your home folder on that machine; nothing from the open project (folder, environment) is sent.
- Key-based login only: Flint runs `ssh` with `BatchMode`, so it never asks for a password or a host-key question; a refused key or unknown host shows ssh's own (redacted) error on the tab and in the status bar. Flint stores no credential. Jump hosts and other keys belong in `~/.ssh/config`.
- The first time per run that Flint runs `ssh` for a machine (to connect, or for its Check) a native dialog shows the exact command line and asks. Decline and nothing connects; the tab says why.
- The machine needs Flint's headless host (`flint`, from `cmd/flint-host`). **Check** on the machine's settings runs `<command> --version` over ssh (10 s connect timeout, 20 s in all, output capped) and says: ready (version, host protocol), a different protocol, `flint` not found, or ssh's error. When Flint is missing or incompatible the page shows copyable install steps (system, download from the releases page, SHA-256 check, unpack into `~/.local/bin`, run it, set Command). **Flint installs nothing by itself.** No release publishes the archives yet: the file names (`flint-host-<os>-<arch>.tar.gz`) are where releases will publish them, so until then build one with `wails3 task host:package` (see [Development](DEVELOPMENT.md#the-headless-host-remote-machines)).
- The tab reads `<machine> ▸ <title>` with a small machine badge. While Flint connects, reconnects (backoff 1, 2, 4 … 60 s) or fails, the tab shows a banner and the status bar a "machine: connecting… / reconnecting… / connection failed" item. Output, titles, exit codes and the state of an agent CLI running in the shell (the tab badge) arrive as for a local terminal.
- Closing a tab ends its shell on the machine. Closing the window or quitting leaves the shells running there. On the next start a saved remote tab re-attaches if the machine still has the session (the machine replays what it kept); if not, or if the machine was removed from settings, the tab opens ended and says "session ended on <machine>". A restored remote tab never starts a new shell.
- Allowed in Restricted Mode: it runs nothing from the folder, like the local New Terminal command. The automatic first terminal still waits for trust.
- Remote terminals are not visible to agents' MCP tools (list or read terminals), so an agent cannot read or type into a shell on a machine.
- Out of scope: remote files, editor, git, language servers, search, port forwarding, uploads, Windows machines, automatic install or update, password or 2FA prompts, and **agent chats on a machine** (the host has no approvals, MCP tools or per-agent settings yet): there is no "agent on machine" option.

### Status of agents in terminal tabs

- An agent CLI running in a terminal tab can report what it is doing with an escape sequence, and the tab then shows a small badge (working, waiting, done) and the agent is listed in the Agents panel (kind *terminal*, same order and "needs you" rules: waiting needs you, done counts as finished until you show the tab). *list_agents* reports it with `kind: "terminal"`.
- What Flint reads (`internal/terminal/agentstate.go`, in the output stream, which is passed to the screen unchanged): Flint's own `ESC ] 6977 ; state=<working|waiting|done|idle> BEL` (or `ESC \` as terminator); the desktop-notification sequences `OSC 9;<text>` and `OSC 777;notify;…` as *waiting* (`OSC 9;4` progress and the other ConEmu `9;<number>;…` forms are not notifications and are ignored); and the shell prompt coming back after the program that reported a state, as *idle* (the badge and row disappear). Sequences are at most 512 bytes; anything else is ignored. 6977 was chosen because none of the private OSC numbers of the common terminals (iTerm2 1337, kitty 99 and 5522, VTE 3008, WezTerm, Ghostty, foot, VS Code 633, FinalTerm 133) uses it; that check is from their documentation, so the payload is matched exactly.
- **New Claude Terminal** (command palette; deliberately not the default of *New Terminal*) opens a terminal and types `claude --settings '<config dir>/agent-hooks/claude-status.json'`. The file holds Claude Code hooks (`UserPromptSubmit` and `PreToolUse` → working, `Notification` → waiting, `Stop` → done) whose commands write the sequence to `/dev/tty`. Flint writes it only for a trusted folder and rewrites it when its content differs. Flint never modifies `~/.claude/settings.json` or any other file you own. **Settings ▸ Agents ▸ Claude in a terminal tab ▸ Copy hook snippet** copies the same hooks for you to merge into your own Claude Code settings.
- **Unverified:** whether `claude --settings <file>` *merges* its hooks with the hooks in your own settings (the CLI's help says the flag loads "more settings"; the behaviour was not exercised, because it would need a Claude Code session). If it replaced them, your own hooks would not run in terminals started with *New Claude Terminal*; use the copied snippet instead. A terminal restored after a restart resumes Claude without `--settings`. What was verified: a hook-like command (a child of the foreground program with its output piped) can write the sequence to `/dev/tty` and Flint reads it (a test); that Claude Code starts hooks that way was not.

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
- Update Project (pull) asks which repository to update when the workspace has several (see [Git](#git-intellij-style)).
- New terminals and chats start in the active tab's project (the `+` menu has a section per project).
- Run configurations follow the active tab too: the ▶ selector in the title bar shows the configurations (`.flint/run.json` and detected ones) of the active tab's project, with its name. Each project remembers its own selection, and Run / Debug start in that project.
- "Fix with agent", `⌘L` and test results go to the chat of the project the file belongs to.
- The command palette has "Add Project to Workspace…" and "Remove <project> from Workspace".
- LSP servers and the file watcher run per project.
- The set of projects is restored after a restart, together with the session of the main project.
- Terminals come back as new shells in their last directory. If Claude Code or Codex was running in one, it is started again on the same conversation (`claude --resume <id>`, or `claude --continue` / `codex resume --last` when the conversation's id is not known).

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
- **Navigation**: `⌥⌘B` Go to Implementation, `⇧⌘B` Go to Type Declaration, `⌘F12` *Go to Symbol in File…* (the file's outline with a filter; nesting is shown until you type) and `⌥⌘O` *Go to Symbol in Workspace…*, which asks the running language servers of the open folders (at most 200 results; a server starts when a file of its language is opened, so there is nothing to search before that).
- **Rename** (`⇧F6` or `F2`, "Rename Symbol…"): a small input at the symbol with its current name; `↵` asks the server for the edits (`textDocument/rename`, after `prepareRename` where the server has it) and applies them. Files open in an editor are changed in their buffer, as one undoable step each, and stay unsaved; files that aren't open are rewritten on disk by Go (atomically, keeping permissions and line endings), only inside an open folder with a running language server, never in `.git`, never through a symlink, never a file over 10 MB or one that isn't UTF-8. A note says how many edits went into how many files. Edits the server computed for an older version of an open file are refused, and so are changes that create, rename or delete files (Flint doesn't announce support for them).
- **Quick fixes and refactorings** (`⌥↵` or `⌘.`, "Show Quick Fixes and Refactorings…", and **Quick fix…** in a problem's tooltip): the server's code actions for the cursor or the selection (fixes, refactorings, organize imports and fix-all; actions that only open a web page or the server's own UI are not listed), the preferred fix first; quick fixes that extensions attach to their editor marks at the cursor are listed after them. Choosing one resolves it (`codeAction/resolve`), runs its command if it has one (`workspace/executeCommand`; the edits the server sends back with `workspace/applyEdit` are collected) and applies the result like a rename. The server gets back its own diagnostics for the range, unchanged, so fixes that depend on a diagnostic's code or data work.
- **Formatting**: `⌥⌘L` or `⇧⌥F` *Reformat File* (`textDocument/formatting` with the file's indentation: gofmt through gopls, rustfmt through rust-analyzer, …) and `⇧⌥O` *Organize Imports*, each one undoable step. `editor.formatOnSave` and `editor.organizeImportsOnSave` (Settings ▸ Editor, both off by default) run them before every save; a save never waits more than 3 seconds for the server and is never blocked by a missing formatter.
- **Parameter info**: after `(` or `,` (the server's own trigger characters) a tooltip shows the signature of the call with the current parameter highlighted and its documentation; it follows the cursor and closes with `Esc`. `⇧⌘Space` opens it by hand.
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
- Your own agents: a Markdown file with an optional engine and model (or model tier) and standing instructions, usable wherever an agent name is. See [Defined agents](#defined-agents).
- A shared panel in the right dock (`+` → claude / codex / a custom agent): streamed response, tool calls with a mini diff, the plan, and the list of changed files.
- Edits and reads go through without asking (the "auto: edits" policy); commands ask for permission.
- `⌘L` attaches the selection or the current file to the conversation.
- "Send to agent" (LSP problems, terminal output) uses the shared `askAgent` (`frontend/src/lib/askAgent.ts`): it picks the active or most recently used chat of the project the content belongs to (the file's project, or the active tab's project when there is no file). If there is none, it opens a new chat in that project with the preferred agent. The content goes into the draft so you can add to it; `⌥`-click sends it immediately.
- Prompt library (`internal/prompts`, `PromptsAPI`, `components/agent/PromptPicker.tsx`): typing `/` in any chat lists Markdown prompts from `.flint/prompts` (project), the config dir's `prompts/` (personal) and the built-in `/review`, `/test`, `/explain`, `/fix` (`prompts.builtins`), project over personal over built-in. Optional front matter: `description`, `agent`, `args`. Choosing one fills the draft with `{{selection}}` and `{{problems}}` (redacted; no selection from a credentials file), `{{file}}`, `{{diff}}` (bounded, redacted, credentials files such as `.env` left out and only named; `redact.SecretFile`), `{{branch}}` and the args filled in (one pass); nothing is sent automatically. Text that matches no prompt, or anything sent after `Esc`, reaches the agent unchanged (`/compact`). "Run Prompt…" and "New Prompt…" in the palette. Files are read when the list opens (no watcher, no polling); only regular files ≤ 64 KB that stay inside the folder (and the project) after resolving links. In Restricted Mode project prompts are marked "untrusted repo" and can't replace a built-in or personal one.
- The chat header shows usage: cumulative input/output tokens, cost in USD when the agent reports it (codex usually does not), and a context fill bar (for example "42% context"), with details in a tooltip. The status bar shows the total for the project (with several projects, for the whole workspace, broken down in the tooltip). Usage is part of the saved session.

### Usage and budgets

- Usage is recorded in Go as it arrives: every `usage` event of a chat is one turn, added to a daily row per project (a task's chats count for the repository's main checkout), agent, model, agent task and chat in `usage.json` (`internal/usage`, `usage_state.go`). Tokens in/out, cache read/write tokens when reported (Claude Code, Codex), the cost only when the agent reports it. A turn Claude Code reports per model is split by model. A resumed conversation counts only its new turns (Codex's first total after `thread/resume` and Claude's first `total_cost_usd` after `--resume` cover the history). Writes are batched (about 2 s, and on quit) and atomic; the file is bounded by `usage.retentionDays` (default 90) and row/chat caps. A file that can't be decoded is moved to `usage.json.bad-<time>`; one that can't be read or comes from a newer Flint is never overwritten. Custom ACP agents send no usage and aren't recorded.
- No invented prices: tokens without a reported cost stay tokens, unless `usage.prices` gives the model a price ($/Mtok in/out, optional cached input); those costs are kept apart and always labelled as estimates, and cached input without a cached price is left out (and said so) rather than priced as fresh input.
- The Usage panel (∑, `UsageAPI`) shows today / 7 / 30-day totals, a per-day SVG bar chart (7 or 30 days), breakdowns by project, agent task, agent and model (click a project or task to filter), top chats, and a CSV export of the daily rows (native save dialog; a download in the browser build). It reloads on `usage:changed`, never polls.
- Budgets: daily (USD and/or tokens, all chats) and per agent task; token budgets leave out cache reads unless `usage.countCacheReads`. When a turn reaches one, `usage:alert` fires once per threshold per day or task (remembered in `usage.json`, so not again after a restart): a warning under the chat header, a toast, or a background notification through `AlertsAPI`. With `usage.pauseOverBudget`, `AgentChatAPI.Prompt` refuses new turns until **Continue anyway** in the warning, which lasts for another budget's worth at that limit.
- Nothing is sent anywhere; see [Files and data](../site/reference/files-and-data.md).

### Custom agents (ACP)

- Each entry in `customAgents` (settings.json) is an agent: an id (lowercase, not `claude` or `codex`), an optional name, a command (a name on the login PATH or an absolute path) and the arguments that start its ACP mode. Settings offers Gemini CLI (`gemini --experimental-acp`) and OpenCode (`opencode acp`) as one-click fill-ins; you install and log in to the CLI yourself.
- **Check** first asks, in a native dialog that shows the exact command line, before it runs a command line for the first time in a session. It starts the agent in an empty temporary folder, runs the ACP `initialize` handshake (25 s timeout) and shows its name and version, or the error with the end of what the agent printed. No session is opened.
- A custom agent appears in the agent menus, the default-agent pickers and "Ask the agent" once its command is found. Saving settings registers the agents again; open chats keep the agent process they started with.
- It starts only in a trusted folder (see [Restricted Mode](#restricted-mode-workspace-trust)). Its permission requests go through the same approval policy as the native agents. Flint offers it file reads and writes (ACP `fs/*`) only inside the chat's folder, never into `.git`, and no terminal; the agent runs in its own process group, which is killed when the chat closes and when Flint quits.
- There is no environment setting: `settings.json` is plain text, so API keys don't belong there. Log in with the agent's own command (`gemini`, `opencode auth login`) before using it.
- Custom agents can't be resumed after a restart; a restored chat continues in a new conversation and says so.

### Defined agents

- A defined agent is a Markdown file in the prompt-file format (`internal/agentdefs`, [design](design/agent-definitions.md)): optional `description` (200 characters), `engine` (`claude` or `codex`) and `model` (a model tier, `fast`, `balanced` or `strong`, with or without an engine; any other value is a model name, only with an engine, checked by `agent.CheckModel`), then the standing instructions (required, 16,000 characters). The name is the file name without `.md`: lowercase letters, digits and dashes, starting with a letter, at most 32 characters; `claude`, `codex` and a custom agent's id are refused. At most 64 KiB per file and 50 per folder; unknown keys are ignored. A file that breaks a rule is skipped and reported as a problem (path and reason).
- Personal definitions live in `agents/` of the config directory; project ones in `.flint/agents/` of each workspace root, **only when the root is trusted** (an untrusted root's files are only counted and reported as ignored). A personal definition wins over a project one with the same name, the first root over later ones; symbolic links that leave a root are refused.
- A defined agent is an agent wherever a name is: chat menus, New Agent Task and Compare agents, `defaultAgent`, `agentReview.agent`, the prompt library's `agent` key, `start_agent` and the usage breakdown. Settings accept any name of the custom-agent-id form there; a name that is not available falls back as a missing custom agent does.
- Without `engine`, the agent runs on `defaultAgent` when that is an installed native agent, else the first installed one (`agentdefs.ResolveEngine`; a defined or custom default is never followed). The engine is resolved when a conversation starts and recorded as `SessionInfo.engine`; the chat saves it and `AgentChatAPI.Resume` takes it back, so a conversation resumes on the engine it started on. A defined agent is listed only when its engine is installed.
- The instructions are standing instructions for the whole conversation, at start and at resume: `--append-system-prompt` for Claude Code, `developerInstructions` for Codex, never through a shell. Headless runs use the engine and the model; Review Changes with Agent also passes the instructions (one argument: `--append-system-prompt=…`, or Codex's `-c developer_instructions=…` as an escaped TOML string), so a reviewer set as the review agent reviews by its rules. Commit messages and Edit with Agent leave them out (their answer has a fixed form); the conflict terminal runs the plain engine (`agentView.native`).
- Files are read again when agents are listed and before a chat starts or resumes (no watcher), once per call: a change applies to the next conversation, without a restart; running chats keep their driver. A chat starts on the definition its own call read, whatever another call reads meanwhile (`Manager.StartWith`). A chat whose definition is gone fails to resume with a message naming it.
- A role limited to `claude` or `codex` (`start_agent`'s roles, a prompt's `agent` key) also fits a defined agent that runs on that engine; one that names a defined agent fits only that agent.
- Model tiers: `agents.<claude|codex>.tiers` maps `fast`, `balanced` and `strong` to a model of that engine (each checked by `agent.CheckModel`; an unknown tier or a bad model is refused on save and dropped on load). Claude Code defaults to `haiku`, `sonnet` and `opus`; Codex has no defaults. A missing tier uses the default, `""` none; a tier without a model leaves the engine's `model` in place. The tier is resolved from the settings of the moment a conversation starts or resumes, on its engine (`definedModel`), and the same way for headless runs (review, commit message, inline edit). `AgentDefsAPI.List` returns `model` as written and `resolvedModel`, its meaning on the resolved engine now (`""` when none); Settings and the pickers show `strong (opus)`, or `strong` when unmapped. Settings ▸ Agents has a field per tier in each engine's block, with the default as placeholder.
- A definition cannot add arguments, tools, permissions or environment; a tier only resolves to a model from the user's own settings or the built-in defaults. Not in version 1: custom ACP agents as engines, per-agent permissions, tools, budgets or scope, the background session host, workflows between agents.
- Settings ▸ Agents lists the definitions (name, engine, model, source, description), the problems and the folders ignored in Restricted Mode, with **New Agent…** (a template in the personal folder or a trusted root's `.flint/agents`, never overwriting) and **Open**; the command palette has **New Agent Definition…** (`AgentDefsAPI`). Pickers show "name · engine" or "name · default (engine)".

### Agent tasks (parallel agents in worktrees)

- "New Agent Task…" (palette, or `＋` in the Agent Tasks panel ⑂): a description, an agent (default from settings; Claude Code, Codex, a custom agent or a defined agent; several with "Compare agents") and a base branch (default: current). Flint creates a branch (`agentTasks.branchPrefix` + a slug of the first line, made unique) and a worktree (`agentTasks.worktreeDir`, default `<repo>.worktrees/<branch>` like the branch widget), attaches it to the window, starts a chat there and sends the description as the first message (`TaskAPI`, `internal/agenttask`).
- The panel lists the tasks of the open repositories with their state (queued / working / waiting for approval / idle / finished / failed), derived in Go from the chats' agent session events (`agenttask.Tracker`, bound when a chat starts or resumes in the worktree; no polling), the branch, commits ahead of base, and changed files with +/- against the merge base (committed, uncommitted and new files), refreshed on file-watcher and git events.
- Actions: Open (folder + chat), Show diff (per-file side-by-side diff against the merge base), ✦ Ask agent on a failed task (error in the chat draft), Merge (in the checkout where base is checked out; refused while the task or that checkout is dirty; conflicts are found with `git merge-tree` without touching anything, and Flint offers to merge base into the task and hand the conflicts to the agent in a terminal), Create PR (push of `refs/heads/<task>` + `POST /repos/…/pulls` after a native confirmation; a branch pushed to a fork becomes `owner:branch`), Remove (stops the chats, deletes the worktree; uncommitted work and unmerged branches only after confirmation).
- Compare agents: the form's "Compare agents" sends the same description to several agents and/or several attempts per agent (at most `agenttask.MaxGroup` = 6 tasks). `TaskAPI.CreateGroup` makes one task per choice from the same base commit, each with its own branch (the slug plus the agent's id, made unique: `agent/add-retry-claude`, `agent/add-retry-codex`, `agent/add-retry-codex-2`) and worktree, all or nothing, and links them by a group id stored with each task (`group` in `state.json`; files without it load as tasks on their own). The panel shows a group together under its title with each task's state, agent, +/-, commits ahead and the tokens and cost of its chats (`usage.Store.TaskTotals`, also shown for single tasks).
- **Compare** on a group opens the comparison (`TaskAPI.Compare`, `CompareFile`, `DiffBetween`; `internal/agenttask/compare.go`): the union of files changed by any task with each task's +/- per file, and for the selected file every task's diff against its merge base side by side as unified diff lines computed in Go (work tree included, like Show diff; **Open diff** opens the usual side-by-side diff tab), plus a direct diff of the file between two tasks. Files are read through `os.Root`, so a path or a symlink can't leave the worktree; binary files and files over 1 MiB are named, not shown.
- **Keep: Merge** / **Keep: PR** on a task of a group are Merge and Create PR for that task; after they succeed Flint offers to remove the other tasks of the group. Each removal is the ordinary Remove: uncommitted work and unmerged branches go only after their own confirmation.
- Queue: at most `agentTasks.maxParallel` tasks (default 3, 0 = no limit) work at the same time, counted in Go over the window: tasks with a turn running (working or waiting for approval) plus tasks whose chat is being started. A task created beyond that is **queued** (`queued` in `state.json`): its branch and worktree are created at once (so a group shares one base commit and a queued task can be inspected and removed), but no chat starts and the description is not sent. When a task settles (finished, failed, cancelled, chat closed: the same `agenttask.Tracker` change that updates the panel, no polling), is removed, or the limit is raised, the oldest queued task whose repository is open in the window gets the slot: Go starts its chat itself (`App.openChat` in the task's worktree, the stored description with secrets redacted as the first prompt, shown as "from Flint (task)") and the window adopts it, showing the task's folder first; the chat is started once, even when the window is reloaded meanwhile. **Start now** on a queued task overrides the limit. A queued task never starts by itself in an untrusted (Restricted Mode) worktree, and neither the queue nor Start now starts one while `usage.pauseOverBudget` holds a spent budget (**Continue anyway** lets the queue move). After a restart queued tasks stay queued until their repository is open in a window and a slot is free. The limit is per window (each window is its own process).
- Tasks are kept per repository (keyed by the common git dir) in `state.json` with their last settled state; a task whose worktree vanished is shown as stale and can be removed (only its own entry under `.git/worktrees` goes; other missing worktrees are left alone). A task's worktree is trusted when the checkout it started from is trusted; a task from an untrusted checkout may not put its worktree inside a trusted folder (trust is by path), and no task worktree may lie inside the checkout. Worktrees made by hand from the branch widget don't inherit trust. Branches are always named by full ref (`refs/heads/…`), so a same-named tag can't stand in for one; the base must be a plain local branch name.

### Backlog (work items above repositories)

- A backlog is a folder with `flint-backlog.json` (`name`, at most 80 characters, default the folder's name; `repositories`, at most 50, each `name` (lowercase letters, digits and dashes, at most 40, unique) and a git remote `url`) and `items/*.md`, one level, at most 500 files of 64 KiB. Unknown JSON fields are ignored; a backlog without repositories is valid. An item is a prompt-file-format Markdown file (`prompts.FrontMatter`): id = the file name without `.md` (`prompts.ValidName`), `title` (required, 120 characters), `status` (`todo` default, `doing`, `review`, `done`), `priority` (`low`, `normal` default, `high`), `agent` (any agent name; empty = the default agent) and `repos` (comma-separated names from the file; empty = the window's active project), then the body. Title and body together are at most `agenttask.MaxPrompt` = 8,000 bytes, Delegate's note about several repositories included. A file that breaks a rule is a problem (file and reason), not an item; an unavailable agent keeps the item and adds a problem. Items sort by status (doing, review, todo, done), priority, title. Files resolve inside the backlog folder: a symbolic link that leaves it is refused. Code: `internal/features/backlog` (`format.go`, `load.go`), design: [design/backlog.md](design/backlog.md), guide: [site/guides/backlog.md](../site/guides/backlog.md).
- Which backlogs Flint knows: the folders opened with **Open Backlog…** or made with **New Backlog…** (`flint-backlog.json` and an empty `items/`, never overwriting) kept in `state.json` (`backlogs`, most recent first, at most 20, forgotten on Close), plus the workspace roots that hold `flint-backlog.json` at their top (not closable). A backlog is known by its resolved path; the methods that take a backlog folder (everything but Open and New) refuse one Flint doesn't know. Closing one also drops the links of its items to agent tasks.
- The **Backlog** sidebar panel: the backlog picker (Open, New, Close), **New Item…** (a title; the id is `agenttask.Slug` of it, `-2`… when taken; written from a template, never overwriting, and opened), refresh, and the items grouped doing, review, todo, done (collapsed). A row shows title, priority, agent, repositories and the states of its agent tasks; actions are **Delegate**, **Open** (the file), set status and **Open** on an agent task. Problems are listed under the items, each with **Ask the agent**. The **Board** tab has one column per status; a card can be dragged to another column. Commands: Open Backlog…, New Backlog…, New Backlog Item…, Show Backlog Board. The panel reads again when shown, on refresh, after Flint's own writes, when agent tasks change and when files change in a backlog inside a workspace root (`backlog:changed`); a folder outside the workspace is not watched.
- Repositories and checkouts: a declared repository's checkout is the first workspace root whose repository has a remote (any remote) with the same URL, else the folder chosen with **Locate…** (`state.json` `backlogCheckouts`, per normalized URL, at most 200, never in the backlog). URLs compare by `RepoKey`: scheme, user, port, a trailing `.git` and slashes are dropped and the host is lower-cased (`git@github.com:acme/api.git`, `ssh://git@github.com:22/acme/api` and `https://github.com/acme/api/` are one repository); the path keeps its case; `file://` and absolute paths compare as cleaned paths; at most 1,000 characters, no spaces, control characters or `.`/`..` segments. **Locate…** accepts only a folder whose repository has a remote with that URL, and never an agent task's worktree; a located folder that has gone or no longer matches is reported as a problem of the repository. A located folder outside the workspace is attached to it by the UI before Delegate. Flint does not clone.
- **Delegate** (`backlog.API.Delegate`): resolves the agent (the item's, else the default; it must be available now), then every repository to a checkout that is in the workspace and trusted, and says in one message what is missing (no checkout, unusable located folder, not in the workspace, Restricted Mode). An item without `repos` uses the project the UI passes, which must be an open, trusted workspace folder. It then creates one agent task per repository through `createTasks` (the code of New Agent Task: branch from the checkout's current branch, worktree, queue, limits, trust), all or nothing, and starts the chats of the tasks that got a slot (`startTaskChat`, as for a task that leaves the queue); the others are queued. The description is the title and body, with several repositories also a sentence naming the item's repositories and the one the task works in (refused when that makes it longer than 8,000 bytes). It then remembers the tasks (`state.json` `backlogTasks`: per backlog and item at most 100 links; removed ones are dropped first, and Delegate is refused when none can go) and sets `doing`; when the status can't be written, the tasks stay and the result carries a warning. Delegating again adds tasks.
- Status is written to the item's file: only the value of the `status` line changes (a line is added before the closing `---` when there is none), the rest stays byte for byte with the file's line endings, and the new text must parse to the same fields. A file with two `status` lines, no closing `---`, a line that is not `key: value`, or one that changed between reading and writing is refused with the reason. The user can set any status; automatic changes are `doing` → `review` when every counted task finished, `review` → `done` when every counted task is merged or was removed after its pull request was created (`NextStatus`; both in one step is possible), and a failed task keeps the item in `doing`, marked. A task removed with neither merge nor pull request no longer counts; with none left nothing moves. Links keep the last state, pull request and merged flag, so removal doesn't lose them. Changes are made on `TasksChanged` (with every `tasks:changed`, and at start), whenever the panel loads the backlog, and nothing polls.
- The link between items and agent tasks is local state; statuses, items and `flint-backlog.json` are files a team shares through git. Agent tasks, chats, worktrees, located folders and the list of backlogs stay on one machine.
- `get_backlog` (read-only, on Flint's MCP server while the feature and the server are on, and not in untrusted folders): optional `status` and `backlog` (name) arguments, unknown arguments refused; the answer lists every known backlog with its repositories and items (id, title, status, priority, agent, repositories, text cut at 2,000 bytes), bounded to 60 KB with a count of what was left out, and reports a backlog it cannot read. It is not in `agentsession.MCPReadTools`, so Claude Code asks for each call like it does for terminal output.
- Delegate is the only thing that starts an agent: opening a backlog reads files. It is refused for a checkout in Restricted Mode. The feature is optional (`features.enabled.backlog`, Settings ▸ Extensions ▸ Built-in).
- Not in version 1: cloning a missing repository, dependencies between items, one agent across several repositories or a coordinating agent, splitting a goal into items, milestones, budgets per item, people as assignees, watching a folder outside the workspace, importing from or synchronizing with trackers.

### Agents panel

- The Agents panel (✦ on the activity bar, or *Show Agents* in the palette) lists every agent of this window in one list: the open chats and the agent tasks. A row shows the state, the chat's first message (or the task's title), the agent, whether it is a task, and the project folder. Clicking a row shows its chat tab; a queued task, which has no chat yet, opens the Agent Tasks panel.
- Order, most urgent first: waiting for you, failed, finished (the turn ended while you were in another tab), working, queued, idle. The header counts the agents that need you (waiting, failed, finished and not yet seen). A finished chat becomes idle once you show its tab; a failed one stays until the next turn.
- States use the vocabulary of the Agent Tasks panel. A chat in a task's worktree shows the state Go derived for the task (`agenttask.Tracker`); other chats have no tracker, so their state is read from the transcript by the same rules (`lib/agentList.ts` `chatState`). A restored chat is idle until its next message. Tasks with no open chat are listed only while queued; finished and failed ones stay in the Agent Tasks panel.

- **Peek**: the *Peek* action on a row (or `⌘`-click on the row; Ctrl on Linux and Windows) shows that agent's chat in an overlay over the current tab, which stays the active one. Read it, answer a permission request or send a prompt, then close it with `Esc` or ✕; *Keep as Tab* closes it and switches to the chat's tab. Only one peek is open at a time, and a chat that is already the active tab is not peeked. The overlay reuses the chat view: the chat holds no event subscription of its own (events land in the chat store) and the draft lives in the store, so a chat shown in its tab and in the peek stays in step. What you read in a peek counts as seen. *Peek at Agent Needing Attention* in the palette peeks at the first agent in the panel's order that needs you, not counting the chat in front.
- **Hide until needed**: a chat tab's context menu (*Hide Until Agent Needs Me*) or the row's *Hide until needed* removes the tab from the tab strip. It comes back by itself, without taking focus, when its agent needs you (waiting, failed, or finished while the tab was not shown: the panel's states, `lib/settledTabs.ts`). A hidden tab is still open: it stays in the Agents panel (marked *hidden*, with *Show tab*), in the saved session and in Close All, and clicking its row shows and activates it. Hiding the active tab switches to a drawn neighbour; the only drawn tab, or an agent that already needs you, cannot be hidden. Closing a tab or going back never lands on a hidden one. The flag is saved in the session (`settled` on the chat tab, an optional field, so no snapshot version change) and a restored hidden chat stays hidden until its agent needs you.

### Agent context (Flint's MCP server)

- Chats started by Flint get Flint's own MCP server (`internal/mcpserver`, MCP over HTTP on `127.0.0.1`, standard library only) with read-only tools (`internal/mcptools`) and, when switched on, tools that act (below): `get_diagnostics` (language server problems), `get_editor_state` (open files, the active file and the selection), `get_test_results` (the last test runs), and, when turned on in Settings ▸ Agent Context, `get_terminal_output`. The Backlog feature adds `get_backlog` (below, under Backlog).
- Claude Code gets it through `--mcp-config`, Codex through `-c mcp_servers.flint.*` (only when the installed Codex supports HTTP servers), custom ACP agents in `session/new` when they advertise HTTP MCP support. Commit messages and inline edits don't get it.
- Every chat gets its own token (in `FLINT_MCP_TOKEN`, never on a command line), revoked when the chat ends or its project closes; a token only sees its own project. Requests from web pages are refused (Host and Origin checks), answers are redacted and bounded, and turning the server off closes its port.
- Diagnostics, editor state and test results count as reads (no prompt). Terminal output is off by default and always asks, because they can show anything, including what a command printed from outside the project. Untrusted folders get no server.
- **Tools that act** let an agent do what you do in Flint's UI, so the result shows in Flint's own tabs as well as in the tool's answer. They are off by default, one switch per group in Settings ▸ Agent Context (`mcpServer.actions`), and a tool whose group is off isn't listed to agents at all:
  - `run_tests` (`actions.tests`): a Go package folder, a `_test.go` file or a JS/TS test file, optionally single tests, through the test runner like the gutter ▶. The run shows in the Tests tab; the tool waits up to `mcpServer.testWaitSeconds` (120 s) and answers with the totals and each failed test with its output, or with the results so far.
  - `run_configuration` and `get_run_output` (`actions.run`): starts (or restarts) a run configuration of `.flint/run.json` or a detected one, by name, in its run tab, and returns the run's id; `get_run_output` reads a run's last lines and whether it exited.
  - `send_http_request` (`actions.http`): a named request (`### name`) of a `.http` / `.rest` file in the project, as saved on disk, with an optional environment; body files only from inside the project. The answer has the status, the headers (cookies hidden) and the first 16 KiB of the body; the response is kept in the file's response history.
  - `run_sql_query` (`actions.sql`, Database feature): one reading statement on a saved connection, by name, only when that connection is read-only; at most 500 rows (100 by default), recorded in the connection's query history.
- Every call of an acting tool needs your yes to that one call. When the agent asks in the chat (Claude Code), the permission card is the question: it has no "Always allow", and no approval policy answers it, not even "Do everything". When no such card was answered (Codex, custom agents, a Claude Code rule that allows the tool), Flint asks in a native dialog with the tool, what it would do and its arguments. Arguments are checked first (paths, tests, configurations and requests must exist in the chat's project; nothing becomes a command line), and unknown arguments are refused.

### Agent orchestration (agents that start agents)

An agent chat can start other agent chats in the same project, send them messages, wait for them and read their output, through Flint's MCP server. **It is off by default** (Settings ▸ Agents ▸ Agent orchestration, `orchestration.enabled`; Flint's MCP server must be on). While it is off the tools are not even listed.

- **The grant.** The first time a chat calls one of the tools, Flint asks once in a native dialog the web view cannot answer: which agent, which project folder, how many agents (`orchestration.maxChildren`), how deep (`orchestration.maxDepth`), and that the messages it sends appear in their chats. A yes is a grant for that chat only, held in memory: it covers that project and nothing else, never an untrusted folder (Restricted Mode), stops working if the folder loses its trust or the setting is switched off, and ends when the chat is closed. Closing the chat (its tab, or `stop_agent` on it) also stops every agent it started, deepest first, and a child's copy of the grant works only while the grant of the chat you were asked about still exists, so closing the root really ends the run. Agents the chat starts inherit it one level down; an agent at the depth limit cannot start agents. If you say no, the chat is not asked again for as long as it exists. Under a grant the agent's own permission request for these six tools is allowed once per call; no other tool is allowed by it.
- **What you see.** Every agent started this way opens as its own chat tab without taking focus, and every message sent to an agent is shown in its transcript as coming from the sending agent, after redaction. Messages are not previewed first: that is the trade-off of a grant, in place of a question for each message. Every prompt still passes the usage budget (`usage.pauseOverBudget`).
- **Telling the parent** (`orchestration.notifyParent`, on by default). When an agent a chat started starts waiting for approval, fails or finishes, the chat gets one message, `[Flint] child <id> (<role>) is <state>:` followed by that agent's last 20 lines (redacted), so it need not poll. It shows in the chat as coming from Flint. It is sent when the chat is idle and you have no draft open there; otherwise it is held until the turn ends or the draft is closed or emptied. Notices are merged per child (a newer state replaces the queued one), at most 8 wait per chat, they are dropped when the chat is closed, and several waiting notices go out as one message. Only a change of state is news, and a turn that a notice itself started does not produce a notice when it ends (waiting for approval still does), so a notice cannot set off another notice on its own.
- **A budget of turns without you** (`orchestration.maxAutoTurns`, 40 by default, at most 400). Agents can start turns in each other three ways: `send_prompt`, the first prompt of `start_agent`, and a delivered `[Flint]` notice. Flint counts every such turn in the group of chats that one grant covers (the chat you were asked about and everything below it) since the last message you typed in any chat of that group. Past the budget, `send_prompt` and `start_agent` fail with a message telling the agent to stop and report to you, and notices are dropped instead of delivered; typing a message in any chat of the group starts the count again. What this bounds: the number of agent-to-agent turns between two messages of yours. What it does not bound: how long or how expensive each turn is (that is the usage budget, `usage.pauseOverBudget`, which is off by default), turns you start yourself, and chats outside the grant's group.
- **Limits.** `maxChildren` agents per chat (up to 16), `maxDepth` levels (up to 4), one wait at most `maxWaitSeconds` (up to 600, which is also the MCP server's cap on a call), `outputLines` lines of output per read. Agents only reach chats whose folder is the project folder (or inside it) or a worktree of a task of its repository, and of those only themselves and the agents they started, and theirs: a chat you opened, the caller's parent and its siblings are out of reach (not listed, not readable, not waitable).
- **The tools.**
  - `list_agents`: the caller and the agents it started, and theirs, with id, agent, role, state, parent, depth and folder. Agent CLIs running in terminal tabs (see *Status of agents in terminal tabs*) are not listed: a tab has no link to the chat that might have started it, so it cannot be attributed to the caller, and the other tools refuse a terminal id with a message saying so. The Agents panel still shows them to you.
  - `start_agent {agent?, role?, task, worktree?, base?}`: starts a child and sends it the task (a role's text from `<config>/roles/*.md` or the trusted project's `.flint/roles/*.md` is put before it). The agent defaults to the role's, else the caller's; a role limited to `claude` or `codex` also fits a defined agent on that engine. With `worktree: true` Flint first makes one task (branch and worktree, `base` defaulting to the project's current branch) in the caller's repository and starts the child there; it is listed in the Tasks panel like any task. It needs a trusted project, and is refused (not queued) while `agentTasks.maxParallel` tasks are already working. The tools reach chats in the project folder and in worktrees of tasks of the same repository, nothing else; the child limit and depth limit apply as for any child.
  - `send_prompt {id, text}`: to the agents the caller started, and theirs. Refused, with nothing sent, while the target waits for your approval, while you have a draft open in its chat, or while it is working.
  - `wait_for_agent {id, until, timeoutSeconds}`: for the caller itself or its descendants; blocks (no polling) until the agent is `idle`, `waiting`, `finished` or `failed`, and returns `{state, error, timedOut}`.
  - `read_agent_output {id, lines?}`: for the caller itself or its descendants; the last lines of the agent's text and tool-call titles, redacted, from a 64 KiB buffer.
  - `stop_agent {id}`: stops one of the caller's descendants and everything it started.

### Commit messages and conflicts

- ✦ *Review* ("Review Changes with Agent", also in a diff tab and the palette): the review agent (`agentReview.agent`, else the default agent) runs headless, like *Generate message*, on the ticked changes (`agentReview.scope`: or all; a diff tab adds its file), HEAD against the working tree. Go keeps only the requested paths `git status` reports as changed (with git's kinds and rename sources), in a repository that is an open folder or the work tree of one. Diffs come from one `rev-parse` and batched `git diff` calls split per path (an unpaired rename's source stays its own section). `internal/review` renders each file's diff with the new version's line numbers within a strict `agentReview.maxDiffKB` (small diffs whole; larger ones keep their first hunks or a one-line summary; files with no room are listed in a note; untracked files are read only as far as their share), cuts text before redacting it (with a margin), drops NUL bytes and leaves out credential files (`redact.SecretFile`); untracked files are read only as regular files inside the repository (no symlinks). Claude Code gets Read/Grep/Glob with `--disallowedTools Read(...)` rules for credential files (`agent.HeadlessDenyRules`); Codex's read-only sandbox can still read any file the user can; a custom agent's own tools are outside Flint's control, and one started with an auto-approve flag (`settings.AutoApproveArgs`) gets a warning in Settings and in the review's notes. The agent answers with JSON findings (file, line, end line, severity, title, explanation, optional replacement); `review.Parse` tolerates prose, fences, a bare array, other key names and a cut-off answer, drops findings about other files, caps their number and sizes, strips control and format (Cf) characters and redacts secrets; only files with findings are read afterwards to place them. Findings show in the Commit panel (counts per severity; a click opens the diff at the line) and as thread widgets under their lines in the diff tab (`lib/prThreadWidgets.ts`). Each keeps its line and neighbours: when the file changes it moves with its line, or is marked stale and shown above the diff (`lib/agentReview.ts`). ✦ *Fix it* / *Fix all* fill the agent chat's draft with the findings quoted as the reviewer's output (Fix all: the 20 worst) and the code around each; *Dismiss* removes one. *Cancel* (or `agentReview.timeoutSeconds`) stops the agent's process group.
- ✦ *Generate message*: `claude -p` (or `codex exec`) receives the diff of the selected files and recent commit messages; the result lands in the message field for you to edit. A custom agent gets the same prompt in a one-shot ACP session in the repository: Flint serves reads inside it, declines every permission request and refuses writes sent through it; what the agent does with its own tools and its own configuration (for example a setting that lets it edit without asking) is outside Flint's control. The text after its last tool call is the message.
- ✦ *Resolve with claude*: merge conflicts are handed to an interactive agent session in a new terminal. The agent runs `git add` but does not commit. A custom agent has no interactive terminal mode, so when it is the default the first installed native agent (Claude Code, then Codex) does this, and the button names it; with no native agent installed the button is hidden.
- Nothing requires accepting diffs.

## Git and GitHub

### Git (IntelliJ-style)

- Commit panel (`⌘K`) with checkboxes, Amend, Commit and Push, and Rollback. Side-by-side diff in a tab.
- The ↶ button on the "Changes" and "Unversioned Files" group headers rolls back all ticked files of that group at once. For unversioned files this deletes them from disk, after a confirmation ("Delete N unversioned files?").
- Changed-line markers in the gutter, which also show what the agent touched.
- Branch widget in the status bar: checkout, new branch, fetch, pull, push (`⇧⌘K`).
- **Update Project (pull)**: from the palette, the ⤓ button in the Commit panel, or the Branches popup. With one repository the pull starts at once. With several repositories in the workspace (like "Attach project" in IntelliJ) a chooser lists each one by folder name, with its branch and the number of commits to pull. The repository shown in the Commit panel is preselected, and an **All Projects** entry updates every repository in turn. Arrow keys and Enter work in the chooser.
- A commit during a merge includes the whole index.
- **Merge editor** for conflicted files (merge, rebase, cherry-pick, stash pop): *Resolve…* in the Commit panel, which also shows the conflicts left in each file. The file as git left it (clean hunks merged), each conflict an inline block with Accept Current / Incoming / Both (in either order) and ✦ Ask Agent (the block with both sides and its base from the index stages in the chat draft), or edited by hand. Labels say what current and incoming are for the operation: in a rebase, current is the branch being rebased onto and incoming your replayed commit. Next/previous conflict, Δ Current / Δ Incoming against the base, Save, and *Mark as Resolved* (save + `git add`, with a confirmation if markers remain). Shortcuts `⌥⌘N`/`⌥⌘P`, `⌥⌘1`–`⌥⌘4`, `⇧⌘↵`, all rebindable.
- **Annotate** (blame): a column with date and author next to every line (*Blame* button in the status bar, or right-click on the editor gutter), with a tooltip (full hash, author, date, message). Click opens the change from that commit. Uncommitted lines (including unsaved ones) show "Not committed".
- **Show History** of a file (`git log --follow`, including renames) in its own tab: the list of commits, the diff of the selected commit below it, and *Compare with current*.
- **Stash** in the Commit panel: save with a message (optionally including untracked files), and a list with Apply / Pop / Drop (with confirmation) and a diff of each file.
- Works for files in nested worktrees too.

### Worktrees

List, create (`<repo>.worktrees/<branch>`), remove, switch, open in a new window, or open directly with a terminal for an agent. Checkouts and commits made in a terminal refresh the UI, because the git directory is watched as well.

### GitHub pull requests

PR review inside the IDE through GitHub's REST and GraphQL APIs (`internal/github`), with the token of the account signed in under **Settings ▸ Accounts**; `gh` is not needed.

- **Accounts** (`internal/accounts`, `api_accounts.go`): *Sign in with GitHub* is the OAuth device flow (code shown in Flint, *Copy Code & Open GitHub*, polling that honours `interval`, `slow_down`, `expired_token`, `access_denied`); the OAuth App's client ID is `github.clientId` (empty until the maintainer registers one; Settings explains how). *Use the GitHub CLI's Token* runs `gh auth token` once after a native confirmation and stores it like a signed-in token. `GH_TOKEN` / `GITHUB_TOKEN` (`GH_ENTERPRISE_TOKEN` for a server) from the environment is used while no account is signed in, never stored. Tokens live only in the keyring (`flint-accounts`); `state.json` keeps login, avatar, host and scopes. *Sign Out* deletes the token and links to GitHub's authorized apps page for revoking. Providers register themselves (`accounts_registry.go`), so another one (Google) is one more registration.
- **GitHub Enterprise**: `github.host` (API at `https://<host>/api/v3` and `/api/graphql`, `<name>.ghe.com` too); each host has its own account.
- Wherever a GitHub feature finds no account it offers *Sign in with GitHub* in place (the view, a PR tab or diff, *CI: Sign in* in the status bar, a dialog for Fix with agent, Rerun and Create PR).

- **Pull Requests** view (⇅ icon on the activity bar, or "Pull Requests…" in the palette) lists the PRs of the active tab's project repository.
  - Filters: *All / Created by me / Review requested* and *Open / Closed / Merged / All states*.
  - A search box narrows the loaded list by title or number: every word must be in the title (any case) or the number, and `#12` means a number that starts with 12. Nothing is fetched for it, so it finds only pull requests the filters loaded; Esc clears it.
  - Right-click a PR in the list for the actions of its tab's header without opening it: *Open*, *✦ Review with Agent*, *✦ Address Review Comments* (both put a draft in the agent chat; ⌥-click sends it right away), *Check Out* (open PRs only, asks first), *Open on GitHub*, *Copy Link* and *Copy Number*.
  - **✦ Review with agent** in the PR tab's header (also "Review Pull Request with Agent" in the palette for the active PR tab or the current branch's PR) puts a request for a code review in the agent chat of the PR's project as a draft; ⌥-click sends it right away. The draft names the PR, its branches and URL and tells the agent to read the change itself (`gh pr diff`), to treat the PR's text as data, to report findings in the chat, and to change no files and post nothing to GitHub. The title goes in as one line of at most 200 characters.
  - Each PR shows CI state (✓ / ✗ / ●, with counts of passed, failed and running checks in the tooltip), review decision (Approved, Changes requested, Review required, Draft, Merged) and time of last update.
  - Without an account it offers *Sign in with GitHub*; without a remote on the configured host it says so. The repository is the remote marked by `gh repo set-default`, else `upstream`, `github`, `origin`, then the rest.
- Clicking a PR opens a PR tab: title, branches, author, an expandable list of checks with links, description (Markdown rendered in Go, no raw HTML), files with +/− and the number of open threads, commits, reviews, plus *Check out* (`git fetch <remote> pull/N/head`, then the PR's branch created or fast-forwarded, `<owner>/<branch>` for a fork's, a diverged branch left alone; refuses when tracked files have uncommitted changes) and *Open on GitHub*.
- Clicking a file opens a side-by-side diff (merge base ↔ head, as on GitHub) with **inline comments**:
  - A thread under the line with reply (`⌘↵`) and *Resolve* / *Unresolve*. The other side gets a spacer of the same height so the sides stay aligned.
  - `+` in the gutter on lines GitHub allows comments on (inside hunks) opens a new comment: *Add single comment* (posted immediately) or *Start a review* / *Add review comment* (a pending review visible only to you).
  - Outdated threads and file-level comments are shown above the diff.
- *Submit review* bar (in the PR tab and in the diff): a summary plus *Comment*, *Approve*, *Request changes*; with a pending review, the number of comments and *Discard*.
- **A failing CI hands itself to the agent.** The checks of the current branch's PR and of agent tasks' PRs are followed (REST: the branch's PR, its check runs, statuses and workflow runs, all conditional requests, so a poll that finds nothing new costs no rate limit): read when the folder opens, on a branch switch, after push/fetch and when you return to the window, and polled only while a check is running (`ci.pollSeconds`, default 30 s, backing off ×1.5 up to 8×; in the background only for notifications). A failure shows a red **✗ CI** badge in the status bar with **Fix with agent**, a *Your branch* banner in the Pull Requests view, actions on each failed check in the PR tab, and **✗ CI** / **✦ Fix CI** on the task row.
  - **Fix with agent**: Go reads the run and its jobs, and each failed job's log (`GET …/actions/jobs/{id}/logs`, which redirects to a log URL fetched without the token; the last 8 MB), keeps the failed steps' lines (by the steps' start and end times), finds the failing step, keeps its last `ci.logTailLines` lines plus up to `ci.logErrorLines` error lines (go test, compilers, tsc, node:test/jest, pytest, cargo, npm, `##[error]`), redacts it (`internal/redact`), and maps `file:line` references to workspace files (runner paths by suffix, go test's bare file names through the failing packages and `go.mod`). The draft names the workflow, job and step, has the excerpt, the files, the run and job links and the branch and commit, and lands in the right project's chat (a task's own chat and worktree); the user reviews and sends it.
  - **Log**: the failed steps in a tab, with links that open the workspace file at the line. **Rerun**: `POST …/actions/runs/{id}/rerun-failed-jobs` after a native confirmation.
  - A notification when the PR's CI fails or turns green while Flint is in the background (`ci.notify`, with `notifyInBackground`).
- **Review comments hand themselves to the agent** (`api_pr_agent.go`, `internal/prfeedback`, `store/prAgent.ts`), the same way a failing CI does:
  - **✦ Ask agent** on every unresolved thread (under its line in the diff, and on outdated and file-level threads above it) puts that thread in the agent chat draft: the PR's number, URL and branches, the file and line (or range), the comments in order with their authors, each GitHub `suggestion` block called out as the reviewer's proposed replacement for those lines, and the code around the line.
  - **✦ Address review comments** in the PR tab's header (shown while the PR has unresolved threads or a standing "changes requested" review; also "Address Review Comments with Agent" in the palette for the active PR tab or the current branch's PR, **✦ Address** in the *Your branch* banner and **✦ Review comments** on a task row with an open PR) gathers every unresolved, non-outdated thread, grouped by file in the PR's file order and then by line, plus the body of each review that requests changes and still stands (not approved or dismissed since). At most `prReview.maxThreads` threads (20) and 64 kB go into one draft; the rest are listed by `file:line` at its end ("address these first, then ask for the rest"), and outdated threads are counted.
  - The code comes from the working tree (`prReview.contextLines` lines on each side, 8; 0 leaves it out) only when the folder has the PR's branch checked out (the branch *Check out* names, or the PR's head commit; a fork's branch under its own name only when it contains that commit) and the file is a regular file inside the open folder; otherwise, and for comments on the base side, it is the hunk of the PR's diff. Credential files (`redact.SecretFile`) are never shown, symlinks are not followed.
  - When another branch is checked out the draft says so in its first lines and tells the agent to edit nothing until the PR's branch is checked out; such a draft is never sent by ⌥-click, and a toast points at *Check out*.
  - Everything a reviewer wrote (and the PR's title) is quoted with `> ` and declared untrusted data, with control and format characters removed, secrets redacted (`internal/redact`) and bounded: 4,000 bytes per comment and suggestion, the first and the latest 7 comments of a thread, the latest 5 change requests. The draft is built in Go from the PR already loaded (Go's 30 s cache; one refetch when a thread isn't in it); nothing polls, nothing is posted to GitHub, and nothing is sent until the user presses Send (⌥-click sends right away). The draft goes to the chat of the project the PR is checked out in; a task's goes to the task's chat and worktree.
- Apart from running CI checks, nothing refreshes in the background. The list loads when the view opens and when you return to the window; a PR loads when its tab opens and when you return to the window; both also refresh with ↻. Go caches responses in memory for about 30 seconds. Reads that GitHub answers with `Retry-After` wait up to `github.maxRetryWaitSeconds` (60) and try once more; longer waits and a used-up hourly limit are reported with the time to retry.

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

- PostgreSQL (`pgx`) and ClickHouse (over its HTTP interface, `internal/clickhouse`, standard library only; default port 8123, or 8443 with TLS).
- ClickHouse: queries are POSTed and rows stream back as `JSONCompactEachRowWithNamesAndTypes`, decoded one row at a time, so the row limit stops the transfer. 64-bit integers beyond JS's exact range and all Decimals are exact strings (Decimals keep their scale), date-times carry their zone offset, arrays, maps and tuples are shown as JSON. The user and password go in `X-ClickHouse-User` / `X-ClickHouse-Key` headers and the database in `X-ClickHouse-Database`, never in the URL. Each SQL console has a `session_id` (so `SET` and temporary tables persist; one hour idle timeout) and each run a `query_id`: Cancel, and the row limit, run `KILL QUERY` and close the request. A read-only connection also sends `readonly=1`. Errors (`Code: N. DB::Exception: …`) are parsed, including those that arrive after the first rows. Responses are gzip-compressed except on loopback.
- Native ClickHouse profiles from older versions are moved to the HTTP port on first start (9000 → 8123, 9440 → 8443; a custom native port becomes the default HTTP port). The old `connections.json` is kept as `connections.json.native-backup` and a one-time notice lists the changes.
- Passwords are stored in the macOS Keychain, not in files.
- Under each connection, a "Databases (N)" node lists all databases on the server, as in pgAdmin. Postgres excludes templates, and databases without the `CONNECT` privilege get a red cross. ClickHouse shows what `SHOW DATABASES` returns.
- Expanding a database opens a separate pool to it (same host, user and password) and shows its schemas and tables.
- Right-click in the tree opens a context menu at every level: a connection (new SQL console, refresh, edit, copy its name or a password-less connection URL, delete), a database (new SQL console, refresh schema, copy name), a schema (copy name), a table (preview data, count rows, copy its name, qualified name or column list, quoted for the dialect) and a column (copy name, or name and type).
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

Docker / Compose, Kubernetes and AWS Profiles are not part of Flint any more: they are extensions in [flint-ide/extensions](https://github.com/flint-ide/extensions) (`docker/`, `kubernetes/`, `aws/`; ids `flint.docker`, `flint.kubernetes`, `flint.aws`), installed with **Install Extension from Folder…**. They use webview panels, terminal tabs (logs, shells, logins, port forwards and context or profile terminals run the CLIs there) and `run.env` for `DOCKER_HOST`, `KUBECONFIG` and the AWS variables. Their settings are their own Settings pages, stored under `extensions.flint.<name>`. Guides: [Docker](../site/guides/docker.md), [Kubernetes](../site/guides/kubernetes.md), [AWS](../site/guides/aws.md).

## Settings and macOS integration

### Settings

`⌘,` (flint menu ▸ Settings…, or "Settings…" in the palette) opens a settings tab with Appearance, Editor, Terminal, Accounts, Agents, Agent Context, Usage, AI in the Editor, Language Servers, File Watching (Linux only), CI, TODO List, Keymap, Updates, Extensions and Import sections, then a page per installed extension with settings. Every change applies immediately, without a restart, and saves itself (agent settings apply to sessions started afterwards).

- **Theme**: Dark, Light or System (follows the macOS appearance, `prefers-color-scheme`). Colors are CSS variables in `style.css` (`:root` and `[data-theme="light"]`); terminals have their own palette (`lib/terminalTheme.ts`); Go switches the title bar and window buttons (`NSApp.appearance`). The theme from the last run is applied before the first render (a copy in `localStorage`, and the window appearance set in Go before the window is created), so a light theme never flashes dark.
- **Editor**: font family, size and line height for all CodeMirror editors (files, diffs, scratchpads); optional save on window focus loss; optional format and organize imports on save, through the file's language server.
- **Terminal**: font, size and line height for terminals and run consoles (xterm refits immediately and the PTY gets the new size); and [background terminals](#background-terminals), the one setting that applies at the next start.
- **Agents**: the default agent for new chats, commit messages and conflict resolution (a custom agent can be the default; conflicts then go to a native agent). Per agent (Claude Code, Codex): the executable (empty = looked up on the login PATH; otherwise a command name or an absolute path, never one relative to the project), the model (empty = the CLI's default), the model of each tier for defined agents (Fast, Balanced, Strong model; empty = the default, Claude Code's `haiku`, `sonnet`, `opus`, none for Codex) and extra command-line arguments, one per line. Extra arguments go before Flint's own flags, so Flint's flags are parsed last and win; arguments that name a flag Flint sets (the stream protocol, the permission prompt and mode, `--setting-sources`, `--settings`, `--resume`, `--model`; for Codex the sandbox, approval policy, `--yolo`, `--listen` and `-c approval_policy/sandbox*/projects*`) and `--` are refused, as are newlines, more than 32 arguments or arguments over 512 characters. In Restricted Mode the extra arguments are left out entirely, since a flag such as `--mcp-config` or `--add-dir` with a relative path would load the project's files. Changes apply to new chats, commit messages and conflict resolution; running chats keep their settings. Custom ACP agents are added, edited, checked and removed in the same section (see [Custom agents (ACP)](#custom-agents-acp)). The same section lists the [defined agents](#defined-agents) with **New Agent…** and **Open**. *Review* sets what Review Changes with Agent covers (ticked or all changes), its agent, the largest diff it sends and its time limit (`agentReview`). Background notifications can be turned off here.
- **Language Servers**: per language (Go, TypeScript/JavaScript, Python, Rust): on/off, which server (a known server's name such as `pylsp`, an absolute path to one, or another server; empty tries the known servers in order) and extra arguments, added after the server's own (`--stdio`). A change restarts that language's servers for the open files. Restricted Mode still applies: servers that run the project's code and servers Flint doesn't know start only in trusted folders, and extra arguments are ignored there. typescript-language-server, also under another name or path, is always pinned to its own TypeScript, and `--tsserver-path` is refused.
- **Keymap**: a searchable list of all commands (by name or by shortcut, for example `⌘K`). Click a shortcut and press a new combination to change it; conflicts with other commands are shown, `↺` restores the default and `✕` removes it. Shortcuts handled by CodeMirror (`⌘F`, `⌘S`, `⌘↵`, and so on) are display-only. Overrides go into a layer of the command registry (`lib/commands.ts`), so the palette and the dispatcher use the effective shortcuts.
- **Updates**: the daily release check (see [Update check](#update-check)) can be turned off, and with it the "What's New" tab; **Check Now** checks right away.
- **Agent Context**, **Usage** and **AI in the Editor**: see [Agent context](#agent-context-flints-mcp-server), [Usage and budgets](#usage-and-budgets) and [AI in the editor](#ai-in-the-editor).
- **Import** ("Import from VS Code…" in the palette): reads VS Code's, Cursor's or VSCodium's `settings.json`, `keybindings.json` and active color theme (from its extension, following `include`), and shows what it would change and what it skips and why (chords, `when` clauses, settings Flint doesn't have, built-in themes). Nothing is written until you confirm. An imported theme becomes `theme: "custom"`: a palette over the dark or light base, limited to Flint's own color variables and hex colors, plus the theme's terminal colors (`terminal.ansi*`, background, cursor, selection).
- **Extensions**: see [Extensions](#extensions). Their color themes appear under Appearance ▸ **Color theme** and as **Use …** buttons on the extension; choosing one copies it into the custom theme (interface, editor and terminal colors), checked by the same Go rules as an imported theme.
- **Trusted folders**: see [Restricted Mode](#restricted-mode-workspace-trust).

### macOS integration (Wails v3)

- **Drag from Finder**: a folder dropped on the window opens as a project (or is attached to the workspace when one is already open); a file opens in a tab (up to 20 at a time). While dragging, the window shows what will happen.
- **Single instance**: `flint <folder>` with Flint already running hands the folder over (relative paths resolve from the calling directory) and brings the window to the front. A Flint with a different `FLINT_CONFIG_DIR` (for example a dev build) is a separate instance.
- **Toasts**: messages inside the window (toasts) appear in the bottom-right corner, above the status bar.
- **Background notifications**: when a run, a test run or an agent turn finishes while Flint is not active, you get a macOS notification (clicking it shows the tab) and a badge on the Dock icon, cleared when you return to the window. Can be turned off in Settings ▸ Agents.
- **Approval notifications**: when a chat agent starts waiting for your approval while Flint is not active, the notification names the agent, the project folder and what it asks for (the tool call's title, one line; secrets masked in Go before it reaches the OS). Each request is announced once, counts in the Dock badge until it is answered or its turn ends, and clicking the notification shows the chat. Settings ▸ Agents ▸ *Tell me when an agent waits for my approval* (`notifyOnApproval`, default on; it needs `notifyInBackground`).
- The application menu (with `Settings… ⌘,`) and the compact title bar are native Wails v3 options, with no Objective-C additions.

## Extensions

External extensions are separate programs in any language that talk JSON-RPC 2.0 over stdio and are described by a manifest (`flint-extension.json`); Flint draws their UI from data (guide: [site/guides/extensions.md](../site/guides/extensions.md), protocol: [site/extensions/protocol.md](../site/extensions/protocol.md), design: [design/extensions.md](design/extensions.md)).

- **Manage**: *Show Extensions* (palette) opens Settings ▸ Extensions: what is installed, the built-in features that can be turned off, and the repository.
- **Install**: *Install Extension from Folder…* (palette, or Settings ▸ Extensions; `flint --install-extension <dir>` from a terminal, also handed to a running Flint) copies a folder to `<config>/extensions/<id>/` after a native confirmation that shows the id, name, version, program, start mode, permissions, agent tools and sandbox. Installing the same id again replaces it, and the confirmation then starts with what changes (version, added permissions and agent tools). No marketplace, no project-local extensions.
- **Extension index** (`internal/extensions/index`, format: [site/extensions/index-format.md](../site/extensions/index-format.md)): Settings ▸ Extensions ▸ Extension index installs prebuilt extensions from one signed index (`extensions.indexUrl`, default the release asset of flint-ide/extensions; verified with `extensions.indexPublicKey`, default the key built into Flint, which is empty until the maintainer generates it: then the section says "not configured" and nothing is requested). *Browse Extensions* / *Check for Updates* (buttons and palette commands *Browse Extensions*, *Check for Extension Updates*) are the only things that download it: two anonymous GETs, https only, 4 MiB at most; nothing is shown before the Ed25519 signature verified. *Install…* / *Update to x.y.z…* downloads this platform's archive (size and SHA-256 from the index, 256 MiB at most), unpacks it with limits (no links, no paths outside, 10,000 entries, 512 MiB), requires the manifest's id and version to equal the entry's and then shows the same confirmation as a folder install. Installed extensions with a higher version in the index show *Update to x.y.z…*; nothing updates by itself. The verified index is cached in `<config>/extension-index/` and verified again when read. `cmd/flint-extindex` (`keygen`, `build`, `sign`, `verify`) makes an index.
- **Contributions**, under the source `extension:<id>`: palette commands (a default shortcut is dropped when the keys are taken, and the conflict is listed), sidebar panels drawn as a tree or list (lazily expanded nodes, icons from Flint's set, a click runs a command, a context menu of item actions, `view/refresh`), panels and editor tabs drawn as a table, a form or a chart (below), a Settings page generated from a small JSON schema (string, number, integer, boolean, enum, string list; values in `settings.json` under `extensions.<id>`, kept for extensions that aren't installed), status bar items, toasts (`window/showMessage`, rate-limited), files opened in the editor, `http(s)` links (asked once per site), chat drafts (`agent/ask`) and read-only agent tools on Flint's MCP server (`<short>_<name>`, can be turned off per extension).
- **Tables, forms and charts** (`"view": "table" | "form" | "chart"` on a panel or a tab view; `capabilities.views` and `charts` in `initialize`): Go decodes the answer to `view/resolve`, types it and cuts it to its limits before the web view gets it, and text is drawn as text. A *table* has up to 30 typed columns (text, number, date, badge with a severity from the theme) and 5,000 rows, virtualized; a click on a header sorts the rows in Flint (ascending, descending, the extension's order; numbers and dates by value, empty cells last), `↑`/`↓`/`Page Up`/`Page Down`/`Home`/`End` move the selection, `↵` or a double click runs the row's command, a right click or `Shift+F10` opens its actions (`view/action`), `⌘C` copies the row and **Copy** the shown rows as TSV. A *form* has up to 50 fields with the settings schema's types and checks plus `required`, `multiline`, `secret` and `placeholder`; the values are checked in the page and again in Go against the fields last drawn, and only then sent as `form/submit`, whose answer may carry per-field errors, a form error and a message. A secret field never receives a value from the extension and its text is kept nowhere by Flint. A *chart* is a line, bar or area chart of up to 10 series of 2,000 points over a number, date or category axis, drawn as SVG with series colors from the theme, a legend, and its values as a table under **Values**. Tab views of these kinds are opened with `window/openTab`, resolved with the tab's key and data, refreshed per tab (`view/refresh` with `key`) and restored with the session when the manifest says `restore`. `markdown`, `cards` and `stack` views are not drawn: a manifest with one is refused.
- **Editor decorations** (`contributes.decorationSets`, needs `workspace.read`): an extension marks lines in open files with gutter markers, wavy underlines with a hover message, dim text after a line and highlights, colored by severity from the theme. Each set names its files by language, extension or name glob. Flint sends `editor/opened`, `editor/changed` (debounced 300 ms, with a document version) and `editor/closed` only for matching files in trusted folders that `workspace/readFile` would return (never `.git` or credential files), with unsaved text only for sets that declare `unsavedText`; the extension answers with `editor/setDecorations`. A publish for an older version is dropped, positions are clamped, at most 5,000 decorations per set and file, and marks move with edits until the next publish. A decoration's actions are quick fixes (hover buttons, a click on the gutter marker, or *Show Extension Quick Fixes*, `⌥⇧↵`): `editor/action` returns text edits applied as one undo step only if the version still matches, or the action runs one of the extension's commands. Each set has a switch in Settings ▸ Extensions and a *Toggle … in Editors* command (`extensions.decorations` in `settings.json`); *Toggle Extension Decorations in This Editor* hides them in one editor. Opening a matching file starts the extension, an open decorated document keeps it from the idle stop, and its decorations are removed when it stops, crashes, is turned off or is uninstalled.
- **Lifecycle**: nothing is read before the window is up and nothing runs until an extension is used (or declares `onStartup`); `initialize` handshake with contract version check; idle stop after 5 minutes; restart after a crash with a 1 s / 4 s backoff, stopped after 3 crashes in 10 minutes until *Restart*; request timeouts (5 s views, 30 s commands, 120 s tools); 4 MiB message cap; process-group kill on stop, turn-off, uninstall and quit.
- **Security**: only trusted folders are passed or readable; `workspace.read`, `secrets` and `agent.ask` are enforced on every request (`-32001` otherwise); `.git` and credential files are never returned; secrets live in the keyring (`flint-extensions`, `<id>:<key>`); the process gets a minimal environment; declared network hosts are shown, not enforced.
- **Sandbox**: an extension whose manifest has `run.sandbox` runs confined by the operating system: Landlock (files; TCP, signals and abstract sockets on newer kernels), a seccomp socket filter and `no_new_privs` on Linux, applied by Flint re-executing itself as a launcher; `sandbox-exec` with a generated profile on macOS. The strict profile (`"sandbox": {}`) reads the extension's folder, system files and, with `workspace.read`, the trusted folders; writes only its data folder and a private `TMPDIR`; has no sockets; runs only its own program. The manifest adds `read` and `write` paths (`~/…`, `${XDG_…}`, `${env:NAME}` for a `run.env` variable), `network`, `localSockets` and `exec`, or declares `"mode": "none"`. Never granted: Flint's own folder, paths that contain protected locations (`~/.ssh`, `~/.aws`, `~/.kube`, browser profiles, keyrings, shell history, …), protected locations not named by the manifest. Sandboxed processes get no core dumps and at most 4,096 open files; memory and CPU are not limited. A manifest without `run.sandbox` runs unconfined. The install dialog, the extension's card, its log and `flint-extcheck` show the state: *Sandboxed* with what it may read, write and reach (and what the system can't enforce: an older kernel), *Not sandboxed (declared by the extension)*, *Not sandboxed: the extension doesn't declare a sandbox*, or *Not sandboxed: not available on this system (reason)*. **Only run sandboxed extensions** (`extensions.requireSandbox`, off by default) refuses to start the unsandboxed ones. A sandboxed extension with `workspace.read` is restarted when the trusted folders change. Project-local extensions remain unsupported.
- **Settings ▸ Extensions**: each extension's state (not running, running, crashed, stopped after crashes, off, broken), version, program, permissions, sandbox, agent tools, warnings and shortcut conflicts; an on/off switch, *Show Log* (stderr, log messages and lifecycle, last 256 KiB), *Restart*, *Uninstall…* and *Reload*.
- **Color themes** (`contributes.themes`): interface, editor and terminal colors, checked with the custom theme rules (Flint's color names, hex values) when the manifest is read. An extension with only themes has no program and runs no code. A theme is chosen under Appearance ▸ **Color theme** or with **Use …** on its card, and copied into `customTheme`, so it stays after uninstalling.

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
- There is no telemetry. The only requests Flint makes on its own are the update check and the release notes below.

### Update check

- Flint asks GitHub's API for the latest release (`GET https://api.github.com/repos/flint-ide/flint/releases/latest`): one anonymous request with `User-Agent: Flint/<version>`, no token, cookies or identifiers, a 10 s timeout and a 1 MB response limit (`internal/update`).
- Automatically: 5 s after launch and then once a day while Flint runs, at most once every 24 hours (the last check, its answer and a skipped version are kept in `state.json`, so a restart reuses the answer instead of asking again). Never in development builds (`dev`, `dev-<sha>`), and never when `checkForUpdates` is off (Settings ▸ Updates). A failed check is only logged.
- The running version (`vX.Y.Z` from `-ldflags`) is compared with the release tag by Flint's own SemVer compare; a pre-release sorts below its release.
- A newer release shows a notice in the status bar: **Download** opens the release page in the browser (only a `https://github.com/flint-ide/flint/…` link from the API; anything else falls back to the releases page), **Skip This Version** stays quiet until a later release, **✕** hides it until the next launch.
- **Help ▸ Check for Updates…** (also in the palette and Settings ▸ Updates ▸ Check Now) checks immediately, ignoring the daily limit and the setting, and says "You're up to date (vX.Y.Z)" when there is nothing new.
- **What's New**: the first launch of a newer version opens a "What's New in vX.Y.Z" tab with that release's notes, grouped into New Features, Bug Fixes and Other Changes. Items link to their pull requests, and the header links to the release page on GitHub. The notes come from the GitHub Release of that tag (one anonymous GET, under the same rules as the update check). They are sorted by Conventional Commits type: `feat` goes to New Features, `fix` and `perf` to Bug Fixes, and everything else to Other Changes.
- The tab is not shown on a fresh install, in development builds, or when `checkForUpdates` is off, and then Flint does not ask GitHub for the notes at all. The version that ran last is kept as `lastSeen` in `state.json`.
- **View Release Notes** (palette, category Help) opens the notes of the running version at any time.
- Flint never downloads or installs anything: there is no auto-update framework.

## Restricted Mode (workspace trust)

A folder opened for the first time is untrusted until you click **Trust Folder** in the banner (or run "Trust Folder" from the command palette). A downloaded archive or a fresh clone can carry configuration that runs programs, so in Restricted Mode Flint:

- does not auto-start a terminal (shell prompts run `git status` and version-manager hooks that read the folder);
- does not start language servers that execute project code: rust-analyzer (build scripts, proc macros, `.cargo/config` tools), the Python servers (they run the virtualenv interpreter the project points at) and any server set in Settings that Flint doesn't know. They start as soon as you trust the folder; extra language-server arguments from Settings are ignored until then;
- runs agents (chat, commit messages, reviews, conflict resolution) without the project's agent settings and hooks (Claude Code gets `--setting-sources user`, because `-p` mode skips its own trust prompt) and without the extra agent arguments from Settings;
- does not start custom ACP agents at all (chat or commit messages): Flint can't tell them to ignore the project's configuration, so the error asks you to trust the folder;
- lists the project's `.flint/prompts` marked "untrusted repo", and never lets them replace a built-in or personal prompt of the same name;
- loads no project agent definitions (`.flint/agents`) and only counts them, so Settings ▸ Agents can say how many were ignored;
- refuses Backlog's **Delegate** for a repository checkout in the folder (reading a backlog is not restricted), so no agent starts there until you trust it;
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
| `⌥⌘B` / `⇧⌘B` | Go to implementation / type declaration |
| `⌘F12` / `⌥⌘O` | Go to symbol in file / in workspace |
| `⇧F6` / `F2` | Rename symbol |
| `⌥↵` / `⌘.` | Quick fixes and refactorings |
| `⌥⌘L` / `⇧⌥F` | Reformat file |
| `⇧⌥O` | Organize imports |
| `⇧⌘Space` | Parameter info |
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
