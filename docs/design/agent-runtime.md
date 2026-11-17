# Agent runtime: orchestration, background sessions, remote machines

Status: design, being implemented in steps. Each step keeps `make test` and `make lint` green and can be merged on its own.

This document covers four phases:

1. Attention UI: a notification when an agent waits for approval, an Agents panel, peek, settle.
2. Agent-to-agent orchestration through Flint's MCP server.
3. Sessions that survive closing the window (a host process).
4. Remote terminals and agents over SSH.

Three facts about today's code shape the design:

- **MCP tokens identify a project, not a chat.** `mcpserver.Server.tokens` maps a token hash to a root and `Tool.Call` receives only `root`. Orchestration needs to know which chat is calling, so step 2.1 comes first.
- **Messages sent from Go are invisible.** The chat reducer ignores `user_message` events (`frontend/src/lib/agentReducer.ts`), because the UI's own messages are already in the transcript.
- **The Linux GUI binary cannot run headless.** Wails on Linux uses cgo with GTK/WebKit, so phase 4 needs a cgo-free `cmd/flint-host` build of the same host code.

## Phase 1: attention UI

- **Waiting notification.** When a chat agent starts waiting for approval and the window is not focused, Flint sends a notification and counts it in the Dock badge; it clears when the request is settled.
- **Agents panel.** One list of every chat agent and worktree-task agent in the window with its state, sorted so the ones needing the user come first. "Finished, unseen" becomes idle once the tab is viewed.
- **Peek.** Shows an agent's chat over the current tab without switching to it, with "keep as tab" and close.
- **Settle.** Hides an agent's tab until that agent needs attention (waiting, failed or finished).

## Phase 2: agent-to-agent orchestration

### Decisions

- **One Go function opens a chat:** `App.openChat`. It emits `chat:opened` and the frontend adopts the chat. `AgentChatAPI.Start` is reduced to calling it.
- **Caller identity comes from the MCP token.** `MCPProvider` gains a `ctx`. `Manager.open` puts a caller slot into that ctx and fills in the session id after `Start` returns, so a token issued during that start resolves to the chat's id. Nothing new appears on any command line.
- **Per-chat state is reused.** A new `internal/orchestrate` package folds every chat's events through the existing `agenttask.Next`. The task `Tracker` stays as it is.
- **Role templates reuse the prompt-file format** (`internal/prompts.Parse`, front matter `description` and `agent`), loaded from `<config>/roles/*.md` and `.flint/roles/*.md`. Project roles are ignored in Restricted Mode.
- **Draft guard: refuse, don't queue.** The frontend reports whether a chat has an unfinished draft; Go refuses an agent-sent message while there is one, and the tool error tells the agent to wait and retry. If the web view lies, orchestration is blocked, which is the safe direction.
- **A sent message is refused while the target waits for approval.**
- **Approval is one grant per chat,** not one dialog per call.
- **Terminal agents report state through an escape sequence** parsed in the terminal pump.

### Starting a chat from Go

- `chat_open.go` (package main):
  - `type ChatRequest struct{ Agent, Cwd, Prompt, Role, TaskID string; Parent string; Origin *as.Origin }`
  - `func (a *App) openChat(ctx context.Context, r ChatRequest) (as.SessionInfo, error)`
  - It does what `AgentChatAPI.Start` does today (`tasks.Bind`, `usageChats.bind`), registers the tree in `a.orch`, and emits `chat:opened` with `ChatOpened{Info as.SessionInfo; Parent, TaskID, Role string; Background bool}`.
  - A prompt goes through the same path as `AgentChatAPI.Prompt`: usage gate first, then `tasks.Prompted`.
- `as.Event` gains `Origin *Origin` (`{SessionID, Agent, Role string}`). Before any prompt that did not come from the web view, Go emits a `KindUserMessage` carrying `Origin` and the redacted text. The reducer renders `user_message` only when `origin` is set, labelled "from <agent>".
- Frontend: `store/agentChat.ts` gets `adopt(info, opts)`, which skips ids it already has and opens the tab without taking focus. `subscribeAgentChatEvents` listens for `chat:opened`.
- Worktree tasks: the core of `TaskAPI.CreateGroup` becomes `a.createTasks(ctx, NewTaskGroup)`. `startQueuedTasks` calls `a.openChat` and then `releaseStart` instead of emitting `tasks:start` (kept for one release as a no-op listener).

### MCP tools (`internal/mcptools/orchestrate.go`, opt-in)

Every tool requires `mcpserver.CallerFrom(ctx) != ""` and an active grant. Targets must be chats whose cwd is inside the token's root, or a worktree of a task of the same repository.

| Tool | Behaviour |
|---|---|
| `list_agents` | Chats and terminal agents in scope: `id, agent, role, state, parent, depth, cwd, kind`. |
| `start_agent {agent?, role?, task, worktree?, base?}` | Starts a child. A role's body is prepended to `task`. With `worktree`, `createTasks` runs first. Returns `{id}`. |
| `send_prompt {id, text}` | Errors: `waiting for approval`, `busy`, `draft open`, `not your descendant`. Text is redacted, then sent. |
| `wait_for_agent {id, until, timeoutSeconds}` | Waits on a channel, no polling. Returns `{state, error, timedOut}`. Server cap 10 minutes. |
| `read_agent_output {id, lines?}` | Last N lines of assistant text and tool titles from a bounded ring (64 KiB, redacted). |
| `stop_agent {id}` | Children only. |

**Telling the parent.** When a child enters `waiting`, `failed` or `finished`, `orchestrate` queues one notice for the parent: `[Flint] child <id> (<role>) is <state>: <last 20 lines>`. It is delivered through `openChat`'s prompt path when the parent is idle and has no draft, otherwise after the parent's turn ends. Notices are merged per child, at most 8 queued.

### Approval model

Settings (`internal/settings/orchestration.go`):

| Key | Default | Limit |
|---|---|---|
| `orchestration.enabled` | `false` | tools are not listed while off |
| `orchestration.maxChildren` | 4 | 16 |
| `orchestration.maxDepth` | 2 | 4 |
| `orchestration.maxWaitSeconds` | 600 | |
| `orchestration.notifyParent` | `true` | |
| `orchestration.outputLines` | 200 | |
| `orchestration.maxAutoTurns` | 40 | Turns agents may start in each other between two user messages (max 400). |

Fixed rules (constants, not settings): same project only; trusted folders only; the grant dies with the chat.

The first orchestration call from a chat triggers a native dialog the web view cannot answer: "Let this <agent> chat in <root> start and direct up to N agents (depth D)? Messages it sends appear in their chats." A yes creates `orchGrant{root, maxChildren, maxDepth}` in memory. Children inherit it at depth+1; below `maxDepth` a child gets no `start_agent`.

The tools are `Acts: true` with a new `Tool.Granted func(ctx) bool`; `approveAction` returns nil when granted. `as.ActionGate` gains `Granted func(sessionID, tool string) bool` so that `Manager.route` auto-allows the chat's own permission request for these tools under a grant.

**Trade-off against principle 4:** text sent between agents reaches a provider without a per-message preview. It is justified by the explicit grant, by redaction, and because every message appears in the target chat's transcript as it is sent.

### Status of agents running in terminal tabs

`internal/terminal/agentstate.go`: a streaming parser for `ESC ] … BEL|ST` (at most 512 bytes per sequence, safe across chunk boundaries; sequences pass through unchanged). It recognises:

1. Flint's own `OSC 6977;state=<working|waiting|done|idle>` (check the number does not collide with another terminal's private sequence).
2. `OSC 9` and `OSC 777;notify` as "needs attention".
3. The foreground program leaving the agent (`title.go`) as `idle`.

`Events` gains `AgentState(id, state string)`, emitted as `term:agent`.

Flint never edits `~/.claude/settings.json`. It writes `<config>/agent-hooks/claude-status.json` with hooks that print the sequence to `/dev/tty`, and uses it only when Flint itself types `claude` into a terminal (`claude --settings '<path>'`). Settings offers "Copy hook snippet" for the user's own setup. **Checked while building it:** a hook-like command (a child of the foreground program, output piped) can write the sequence to `/dev/tty` and the parser sees it (`internal/terminal/agentstate_test.go`); `claude --help` describes `--settings` as loading "more settings" and keeps it separate from `--setting-sources` (user, project, local). **Still unverified:** that `--settings` hooks are merged with the user's own rather than replacing them, and that Claude Code spawns hooks in the terminal's session so `/dev/tty` opens (both need a real Claude Code session). The number 6977 was checked against the private OSC numbers of iTerm2, kitty, VTE, WezTerm, Ghostty, foot, VS Code and FinalTerm from their documentation.

### Key types

```go
// agentsession
type MCPProvider func(ctx context.Context, cwd string) (MCPServer, bool)
func WithCaller(ctx context.Context, slot *CallerSlot) context.Context
type CallerSlot struct{ mu sync.Mutex; id string } // Manager.open sets id after Start/Resume

// mcpserver
func (s *Server) Endpoint(root string, caller func() string) (url, token string, revoke func(), err error)
func CallerFrom(ctx context.Context) string

// orchestrate
type Node struct{ ID, Agent, Role, Parent, Cwd string; Depth int; Live agenttask.Live; Draft bool }
func (r *Registry) Observe(e as.Event)
func (r *Registry) Wait(ctx context.Context, id string, until []string) (agenttask.Live, error)
func (r *Registry) Output(id string, lines int) string
func (r *Registry) Children(id string) []Node
```

### Steps

1. MCP caller identity.
2. `internal/orchestrate` registry, rings and waiters.
3. `openChat`, `chat:opened`, `Origin`.
4. Draft guard (`AgentChatAPI.DraftChanged(sessionID string, open bool)`).
5. Roles loader.
6. Settings section, grant store, `ActionGate.Granted`, the grant dialog.
7. The six MCP tools.
8. Telling the parent.
9. Go-side task start replaces `tasks:start`.
10. OSC parser and `term:agent`; then the hooks file, after the verification above.

Risks: runaway cost (every Go-side prompt goes through the usage gate) and prompt injection between agents (same project, the grant, the depth limit).

## Phase 3: sessions that survive closing the window

### Decisions

- **Headless mode of the same binary.** `main.go` calls `hostmode.Main()` right after `sandbox.Main()`; with `--agent-host` it never returns and never touches Wails. The code lives in `internal/agenthost`, which must not import Wails.
- **Off by default.** `sessions.background` defaults to `false`, which is today's in-process behaviour. When on, closing the window and Quit detach; "Quit and Stop Background Sessions" sends `host.shutdown`. The host exits by itself 60 s after it has neither sessions nor clients.
- **One host per user and config dir.** Lock: `flock` on `<run>/host.lock`. Socket `<run>/host.sock`, where `<run>` is `$XDG_RUNTIME_DIR/flint-<8-hex hash of config dir>` on Linux and `<configDir>/run` on macOS; directory 0700, socket 0600. A path longer than 100 bytes falls back to `/tmp/flint-<uid>-<hash>/`.
- **Start and find.** The GUI dials the socket; on failure it spawns `os.Executable() --agent-host` with `Setsid`, stdio on `/dev/null`, and a pipe on which the host writes `ready\n` after `Listen`. No polling.
- **Authentication.** The peer's uid must equal ours (`GetsockoptUcred` on Linux, `GetsockoptXucred` on macOS), and `hello` must carry the 256-bit token from `<run>/host.token` (0600). The token matters because a sandboxed extension runs as the same uid; add the run directory to `sandbox.Build`'s protected paths.
- **Wire format.** JSON-RPC 2.0, one message per line, reusing `internal/extensions/rpc` after a pure move to `internal/jsonrpc`. Maximum message 4 MiB.

### Protocol (version 1; additive changes only)

- **Client to host:** `hello{protocol, version, token}`; `term.start/write/resize/cwd/resume/close`; `term.attach{id, afterSeq}`; `agent.agents/start/resume/prompt/busy/cancel/respond/stop/policy/setPolicy`; `agent.attach{id, afterSeq}`; `host.settingsChanged`, `host.trustChanged`, `host.shutdown{stopSessions}`, `usage.ack{upTo}`.
- **Host to client notifications:** `term.data{id, seq, data}`, `term.exit`, `term.title`, `term.agent`, `agent.event`, `chat.opened`.
- **Host to client requests:** `ui.confirm{title, message, ok}` (5 minute timeout) and `mcp.call{root, caller, tool, args}`.
- **Replay buffers:** per terminal a byte ring (`sessions.replayKiB`, default 1024, at most 8192); on attach a reset replays the ring and nudges a resize so full-screen programs redraw. Per chat the last 2,000 events or 8 MiB. Usage events are kept until acknowledged (at most 10,000). A gap in `seq` triggers a re-attach.

### Interfaces (`internal/sessions/sessions.go`)

```go
type Terminals interface {
  Start(id string, o terminal.Options) error; Write(id string, b []byte) error
  Resize(id string, cols, rows int) error; Cwd(id string) (string, error)
  Resume(id string) (terminal.Resume, error); Close(id string); CloseAll()
}
type Agents interface {
  Agents() []string
  Start(ctx context.Context, agent, cwd string) (as.SessionInfo, error)
  Resume(ctx context.Context, agent, cwd, id string) (as.SessionInfo, error)
  Prompt(id, text string, att []as.Attachment) error; Busy(id string) bool
  Cancel(id string) error; Respond(id, req, opt string) error; Stop(id string) error
  Policy() as.Policy; SetPolicy(as.Policy); Close() error
}
```

`*terminal.Manager` and `*agentsession.Manager` already satisfy these; `internal/agenthost/client` provides the remote implementation. Docker streams stay in a local `*terminal.Manager`, and a small router (`terminals_router.go`) sends ids to one or the other.

### While no window is attached

- **The MCP listener moves into the host,** so its port and tokens outlive the GUI. Tools that need the host's own state run there; UI-fed tools are forwarded to the GUI through `mcp.call` and fail with "Flint's window is closed" when there is none.
- **Approvals:** permission requests stay pending in the host and are replayed on attach. `ui.confirm` with no client fails closed.
- **Trust and settings:** the host reads trusted folders from `state.json` at start and after `host.trustChanged`, and reloads `settings.json` on `host.settingsChanged`. It never auto-starts anything.

### Version skew and crashes

- A different protocol in `hello` shows a native dialog: "Restart background sessions" (terminals are lost, chats resume) or "Keep them" (the GUI runs in-process for this run).
- GUI crash: the host notices EOF and keeps everything running.
- Host crash: PTYs get a hangup and agent stdin closes. On Linux agent children also get `Pdeathsig: SIGTERM`; macOS has no equivalent, so agents that ignore EOF may be orphaned.
- Stale socket: the lock is free, so remove the socket and start a new host.

### Steps

1. Interfaces, router, App fields typed as interfaces. No behaviour change.
2. Move `extensions/rpc` to `internal/jsonrpc`. Rename only.
3. `internal/agenthost` server over `net.Pipe`, with rings.
4. Client implementation; one contract test suite runs against the in-process managers and against client plus server.
5. `hostmode.Main`: lock, socket, peer credentials, token, idle exit.
6. GUI connect/spawn, `sessions.background`, close and quit semantics, menu item. **Done for terminals only.** The host's agent side is not at parity yet (no MCP server, approval gate, custom agents or per-agent settings until steps 8 and 9), so agent chats stay in the window's process whatever the setting says; `App.agents` in `NewApp` is the one place that switches them later. The host reads `settings.json` at start and on `host.settingsChanged`; the only value its terminals take from it is `sessions.replayKiB`. Additions to protocol 1: `term.list` (ids and exited) and `host.settingsChanged`. Restart after a version skew stops the old host and spawns a new one, waiting up to 15 s for the lock.
7. Frontend attach on startup (`SessionAPI.Live()`). **Done for terminals.** Snapshots keep terminal ids; a live, unclaimed id attaches (`TerminalAPI.Attach(id)`, always from seq 0: the frontend's sequence numbers are the window's own, and a reconnect inside one run re-attaches from the client's host seqs instead). Unclaimed host terminals are listed by Show Background Terminals, never opened or ended on their own.
8. MCP listener into the host, plus the `mcp.call` proxy.
9. `ui.confirm` proxy, detached behaviour, usage backlog.
10. Version-skew dialog; docs.

## Phase 4: remote terminals and agents over SSH

Scope: remote terminals and remote agents in tabs. Files, editor, git, language servers and search stay local.

### Decisions

- **Connection:** `ssh -T -e none -o BatchMode=yes -o ConnectTimeout=10 -o ServerAliveInterval=15 -o ServerAliveCountMax=3 [-p <port>] -- <target> <command> --agent-host --stdio`. `BatchMode` means no hidden password prompts. `--stdio` connects to the remote machine's host (starting it if needed) and relays stdin/stdout to its socket, so the remote host outlives the SSH connection.
- **Headless build:** `cmd/flint-host` (`CGO_ENABLED=0`, linux/darwin, amd64/arm64) runs `hostmode.Main` and is shipped as a release artifact.
- **No automatic install.** The machine's Check button runs `<command> --version` over SSH; if it is missing Flint shows copyable install steps and a SHA-256.
- **Validation:**
  - `target`: `^[A-Za-z0-9_][A-Za-z0-9._-]{0,62}(@[A-Za-z0-9][A-Za-z0-9.-]{0,252})?$`
  - `port`: 1–65535
  - `command`: `^[A-Za-z0-9_./~-]{1,256}$`, not starting with `-` (the remote shell interprets it)
  - machine `id`: `^[a-z0-9][a-z0-9-]{0,31}$`
  - Before a `command` runs for the first time in a session, a native dialog shows the full ssh command line.

### Registry, connections, addressing

- **Registry:** `settings.json` key `machines: [{id, name, target, port, command}]` (`internal/settings/machines.go`). Trust for remote folders lives in `state.json` as `remoteTrust: {"<id>": ["/abs/path"]}` and is sent with each start.
- **Connection:** `internal/remote.Conn` wraps an ssh `exec.Cmd` and a `jsonrpc.Conn` and implements `sessions.Terminals` and `sessions.Agents`. It connects on the first tab, reconnects with backoff 1, 2, 4 … 60 s with jitter while a tab uses the machine, and re-attaches with the last seq of each session. ssh's stderr (at most 8 KiB, redacted) is shown in the status item.
- **Addressing:** frontend ids are `<machineId>:<id>`; the routers strip the prefix. Tabs read `name ▸ title` with a machine badge.
- **Remote agents** get only the remote host's own MCP tools. Attachments with local paths are refused.

### Out of scope

Remote files, editor, git, language servers and search; task worktrees on a remote; port forwarding; uploading files; automatic install or update; password or 2FA prompts; Windows remotes; remote MCP tools that read local UI state; Docker streams on a remote.

### Steps

1. `machines` settings section and validation.
2. `internal/remote` argument builder (golden tests: `--` placement, no user flags).
3. `--stdio` relay in `hostmode`.
4. `remote.Conn` with a fake `ssh` on `PATH`; reconnect and backoff with an injected clock.
5. Prefixed routing in the GUI, the machine badge and menus.
6. The Check button, install instructions, and the `cmd/flint-host` build and release.

## Unverified behaviour to check before the step that relies on it

1. Whether `acpdriver.mcpServers` has a request context in scope.
2. Whether `useEditor.openChat` can open a tab without focusing it.
3. Claude Code `--settings` merging hooks with the user's, and hook processes writing to `/dev/tty`.
4. Whether Codex keeps a fixed MCP URL across `thread/resume`.
5. Whether `diag.Setup` supports a second log file name.
6. macOS privacy (TCC) attribution for a host started with `setsid` from inside the app bundle.
7. Socket path length on macOS when `FLINT_CONFIG_DIR` points to a deep temporary folder.
