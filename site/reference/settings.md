# Settings file

Flint stores your preferences in `settings.json`. The [Settings tab](../guides/settings.md) (`⌘,`) edits this file for you, so you rarely need to open it. This page documents every key for when you want to read, copy or edit it by hand.

## Location

```text
~/Library/Application Support/flint/settings.json
```

The file is readable only by your user. It never contains secrets: database passwords and the completion endpoint's API key live in the macOS Keychain, and other credentials stay with the tools that own them.

> **Note:** Flint reads `settings.json` when it starts. If you edit the file by hand, restart Flint to apply your changes. Changes made in the Settings tab apply immediately; agent settings apply to agent sessions started afterwards.

## How the file is read

- Missing keys get their default value.
- Unknown keys are ignored.
- A value of the wrong type or out of range is replaced with its default (refresh intervals are clamped into their range instead). Invalid extra arguments of an agent or language server are dropped as a whole. A hand-edited file never stops Flint from starting.
- When Flint saves the file, it checks every value strictly and writes the whole document.

## Example

```json
{
  "version": 1,
  "theme": "system",
  "editor": {
    "fontFamily": "'JetBrains Mono', 'SF Mono', Menlo, Consolas, monospace",
    "fontSize": 14,
    "lineHeight": 1.55,
    "vimMode": false
  },
  "terminal": {
    "fontFamily": "\"JetBrains Mono\", \"SF Mono\", Menlo, monospace",
    "fontSize": 13,
    "lineHeight": 1.2
  },
  "defaultAgent": "claude",
  "autoSave": true,
  "notifyInBackground": true,
  "checkForUpdates": true,
  "keymap": {
    "terminal.new": "Mod+Alt+T",
    "git.push": ""
  },
  "todoExclude": ["docs/legacy", "*.min.js"],
  "agents": {
    "claude": {
      "path": "/opt/homebrew/bin/claude",
      "model": "opus",
      "args": ["--add-dir", "/Users/me/notes"]
    },
    "codex": { "path": "", "model": "", "args": ["-c", "model_reasoning_effort=high"] }
  },
  "customAgents": [
    { "id": "gemini", "name": "Gemini CLI", "command": "gemini", "args": ["--experimental-acp"] }
  ],
  "languageServers": {
    "python": { "disabled": false, "command": "pylsp", "args": [] },
    "rust": { "disabled": true, "command": "", "args": [] }
  },
  "refresh": { "kubernetesSeconds": 10, "awsSeconds": 60 },
  "mcpServer": {
    "enabled": true,
    "tools": { "diagnostics": true, "editor": true, "terminals": true, "tests": true, "kubernetes": false },
    "terminalLines": 200
  },
  "completions": {
    "enabled": true,
    "provider": "ollama",
    "endpoint": "http://localhost:11434",
    "model": "qwen2.5-coder:1.5b-base",
    "debounceMs": 300,
    "maxTokens": 64,
    "contextLines": 100
  },
  "inlineEdit": { "contextLines": 60 },
  "agentTasks": { "worktreeDir": "~/worktrees", "branchPrefix": "agent/" },
  "usage": {
    "retentionDays": 90,
    "dailyBudgetUsd": 20,
    "taskBudgetTokens": 5000000,
    "pauseOverBudget": false,
    "prices": { "gpt-5-codex": { "in": 1.25, "out": 10, "cacheRead": 0.125 } }
  },
  "agentReview": { "scope": "selected", "maxDiffKB": 128, "agent": "", "timeoutSeconds": 300 },
  "ci": { "pollSeconds": 60, "logTailLines": 80, "logErrorLines": 40, "notify": true }
}
```

## Keys

| Key | Type | Default | Meaning |
|---|---|---|---|
| `version` | number | `1` | Schema version. Written by Flint; you don't need to change it. |
| `theme` | string | `"dark"` | `"dark"`, `"light"`, `"system"` (follows the macOS appearance) or `"custom"` (uses `customTheme`). |
| `editor` | object | see below | Font for all editors (files, diffs and scratchpads), and `vimMode`: Vim key bindings in every editor (default `false`). |
| `terminal` | object | see below | Font for terminals and run consoles. |
| `customTheme` | object | see below | The palette of the `"custom"` theme, usually imported from VS Code. See [customTheme](#customtheme). |
| `defaultAgent` | string | `""` | Agent for new chats, commit messages and conflict resolution: `"claude"`, `"codex"` or the `id` of a custom agent. Empty means the first installed agent. Conflict resolution always uses Claude Code or Codex. |
| `autoSave` | boolean | `false` | Save modified files when the Flint window loses focus. |
| `notifyInBackground` | boolean | `true` | Show a macOS notification and a Dock badge when a run, a test run or an agent finishes while Flint is in the background. |
| `checkForUpdates` | boolean | `true` | Once a day, ask GitHub whether a newer Flint release exists and show a notice in the status bar. When `false`, Flint makes no request on its own; **Help ▸ Check for Updates…** still works. See [Update check](privacy-and-security.md#update-check). |
| `keymap` | object | `{}` | Shortcut overrides: command id → shortcut. See [keymap](#keymap). |
| `todoExclude` | array of strings | `[]` | Extra folders or globs the TODO list skips. See [todoExclude](#todoexclude). |
| `agents` | object | `{}` | Agent CLIs by name (`"claude"`, `"codex"`). See [agents](#agents). |
| `customAgents` | array | `[]` | Other agents, spoken to over the Agent Client Protocol. See [customAgents](#customagents). |
| `languageServers` | object | `{}` | Language servers by language (`"go"`, `"typescript"`, `"python"`, `"rust"`). See [languageServers](#languageservers). |
| `refresh` | object | see below | How often polling panels re-read their data. See [refresh](#refresh). |
| `mcpServer` | object | see below | What Flint's own MCP server tells the agents Flint starts. See [mcpServer](#mcpserver). |
| `completions` | object | see below | Inline suggestions from a code model (off by default). See [completions](#completions). |
| `inlineEdit` | object | see below | Edit with Agent (`⌘I`). See [inlineEdit](#inlineedit). |
| `agentTasks` | object | see below | Where agent task worktrees go and how their branches are named. See [agentTasks](#agenttasks). |
| `usage` | object | see below | How long agent usage is kept, budgets, and prices for estimates. See [usage](#usage). |
| `prompts` | object | see below | The prompt library of the agent chat (`/`). See [prompts](#prompts). |
| `agentReview` | object | see below | Review Changes with Agent. See [agentReview](#agentreview). |
| `ci` | object | see below | Following your pull requests' CI: poll interval, Fix with agent's log excerpt, notifications. See [ci](#ci). |

### Font objects

`editor` and `terminal` have the same three keys:

| Key | Type | Range | Editor default | Terminal default |
|---|---|---|---|---|
| `fontFamily` | string | up to 200 characters | `'JetBrains Mono', 'SF Mono', Menlo, Consolas, monospace` | `"JetBrains Mono", "SF Mono", Menlo, monospace` |
| `fontSize` | integer (px) | 8–32 | `13` | `13` |
| `lineHeight` | number (multiple of the font size) | 1.0–2.5 | `1.55` | `1.2` |

`fontFamily` is a CSS font list; the first installed font is used. It may not contain `;`, `{`, `}`, `<`, `>` or `\`.

`editor` also has `vimMode` (boolean, default `false`): Vim key bindings in every editor. See [Use Vim key bindings](../how-to/customize-flint.md#use-vim-key-bindings).

### customTheme

The palette used when `theme` is `"custom"`. [Import from VS Code](../how-to/customize-flint.md#import-your-settings-from-vs-code) writes it; you can also edit it by hand.

```json
"customTheme": {
  "name": "One Dark Pro",
  "base": "dark",
  "colors": { "--bg-editor": "#282c34", "--fg": "#abb2bf", "--syn-keyword": "#c678dd" }
}
```

| Key | Type | Meaning |
|---|---|---|
| `name` | string | Shown in Settings ▸ Appearance; up to 100 characters. |
| `base` | string | `"dark"` or `"light"`: the palette the colors are laid over. It also sets the window appearance and the terminal's ANSI colors. |
| `colors` | object | CSS variable → hex color (`#rgb`, `#rgba`, `#rrggbb` or `#rrggbbaa`). Variables not listed keep the base palette's color. |

Accepted variables: `--bg`, `--bg-sidebar`, `--bg-editor`, `--bg-raised`, `--bg-hover`, `--bg-active`, `--border`, `--fg`, `--fg-muted`, `--fg-faint`, `--fg-name`, `--accent`, `--accent-dim`, `--accent-fg`, `--danger`, `--warn`, `--info`, `--ok`, `--selection`, `--selection-match`, `--search-match`, `--active-line`, `--git-added`, `--git-modified`, `--git-deleted`, `--diff-ins-bg`, `--diff-del-bg`, and the syntax colors `--syn-keyword`, `--syn-string`, `--syn-number`, `--syn-comment`, `--syn-function`, `--syn-type`, `--syn-property`, `--syn-definition` and `--syn-punct`. Other names and other color formats are dropped when the file is read and refused when it is saved.

### keymap

Each entry maps a command id to a shortcut:

- The shortcut is written as modifiers joined with `+`, followed by one key, for example `"Mod+Shift+P"`, `"Alt+F7"` or `"Ctrl+R"`. `Mod` is `⌘`. Accepted modifier names are `Mod`, `Cmd`, `Meta`, `Ctrl`, `Control`, `Alt`, `Option` and `Shift`.
- An empty string removes the command's shortcut.
- Commands that aren't listed keep their default shortcuts.

The easiest way to find a command id is Settings ▸ Keymap: hover a command's name to see its id. Some common ids:

| Command | Id |
|---|---|
| Command palette | `app.commandPalette` |
| Go to File | `nav.goToFile` |
| Find in Files | `nav.findInFiles` |
| Commit | `git.commit` |
| Push | `git.push` |
| Toggle Terminal | `terminal.toggle` |
| New Terminal | `terminal.new` |
| Send Selection to Agent | `agent.sendSelection` |
| Edit Selection with Agent | `agent.inlineEdit` |
| Run | `run.selected` |
| Debug | `debug.selected` |

### todoExclude

A list of patterns in `.gitignore` style, such as `"docs/legacy"`, `"/generated"` or `"*.min.js"`. These are added to the folders the TODO list always skips. Each pattern:

- can't be empty, start with `!` or contain a comma;
- is at most 200 characters long.

Up to 100 patterns are kept; duplicates are removed.

### agents

Each entry configures one agent CLI. A missing entry or key uses the default.

| Key | Type | Default | Meaning |
|---|---|---|---|
| `path` | string | `""` | The executable. Empty looks up `claude` or `codex` on your login shell's `PATH`. Otherwise a command name (looked up on the `PATH`) or an absolute path; a path relative to the project is refused. |
| `model` | string | `""` | The model, passed as `--model` (Claude Code, Codex commit messages) or as the chat thread's model (Codex). Empty uses the CLI's default. Letters, digits and `. _ : / @ [ ] -`, up to 128 characters, not starting with `-`. |
| `args` | array of strings | `[]` | Extra command-line arguments for chats, commit messages and conflict resolution. |

Extra arguments are placed before Flint's own flags, so where a CLI lets a repeated flag override an earlier one, Flint's flag wins. They also may not:

- name a flag Flint sets (your own `--mcp-config` for Claude Code and `-c mcp_servers.<name>…` for Codex are fine: Flint adds its server next to yours). For Claude Code: `-p`/`--print`, `--input-format`, `--output-format`, `--include-partial-messages`, `--replay-user-messages`, `--permission-prompt-tool`, `--permission-mode`, `--dangerously-skip-permissions`, `--allow-dangerously-skip-permissions`, `--session-id`, `-r`/`--resume`, `-c`/`--continue`, `--fork-session`, `--setting-sources`, `--settings`, `--allowedTools`/`--allowed-tools`, `--disallowedTools`/`--disallowed-tools`, `--tools` and `--model` (use `model`). Commit messages and inline edits from Claude Code always run with only the `Read`, `Grep` and `Glob` tools and without MCP servers (`--tools Read,Grep,Glob --strict-mcp-config`). For Codex: `-m`/`--model`, `-a`/`--ask-for-approval`, `-s`/`--sandbox`, `--full-auto`, `--dangerously-bypass-approvals-and-sandbox`, `--yolo`, `-C`/`--cd`, `--listen`, `-p`/`--profile`, and `-c` overrides of `approval_policy`, `sandbox*`, `projects*`, `profile*`, `dangerously*`, `mcp_servers.flint*` or the whole `mcp_servers` table (quoted keys such as `mcp_servers."flint"` count too). Commit messages from Codex always run with `--sandbox read-only`. Short flags are checked in any combination (`-dp` counts as `-p`);
- be `--`, empty, or contain a newline or another control character;
- be more than 32 arguments or longer than 512 characters each.

In [Restricted Mode](../guides/restricted-mode.md) the extra arguments are left out, because a flag such as `--mcp-config` or `--add-dir` with a relative path would load the project's files.

### customAgents

Each entry is an agent Flint starts and talks to over the [Agent Client Protocol](https://agentclientprotocol.com). See [Other agents (ACP)](../guides/ai-agents.md#other-agents-acp).

| Key | Type | Default | Meaning |
|---|---|---|---|
| `id` | string | required | Names the agent in menus and in `defaultAgent`. Lowercase letters, digits and dashes, starting with a letter, up to 32 characters; not `claude` or `codex`; unique. |
| `name` | string | `""` | Shown in Settings. Empty shows the id. Up to 64 characters, no control characters or surrounding spaces. |
| `command` | string | required | The executable: a command name looked up on your login shell's `PATH`, or an absolute path. A path relative to the project, or one starting with `-`, is refused. |
| `args` | array of strings | `[]` | Arguments that start the CLI in its ACP mode, for example `["--experimental-acp"]` for `gemini` or `["acp"]` for `opencode`. At most 32, each non-empty, single-line and up to 512 characters. |

At most 16 custom agents. When the file is loaded, an invalid entry is dropped as a whole (half an entry could start the wrong command), as is a second entry with the same id; a `defaultAgent` that pointed at a dropped entry falls back to the first installed agent.

There is no key for environment variables: `settings.json` is plain text, so it must not hold API keys. Log in with the agent's own command instead. Custom agents start only in trusted folders.

### languageServers

Each entry configures the server of one language. A missing entry or key uses the default.

| Key | Type | Default | Meaning |
|---|---|---|---|
| `disabled` | boolean | `false` | Keep the language's server off. |
| `command` | string | `""` | The server. Empty tries the known servers in order: `gopls`; `vtsls`, then `typescript-language-server`; `basedpyright-langserver`, `pyright-langserver`, then `pylsp`; `rust-analyzer`. A known server's name, or an absolute path whose file name is a known server, gets that server's own arguments (`--stdio`). Any other command is started as is and, because Flint can't know what it runs, only in trusted folders. A path relative to the project is refused. |
| `args` | array of strings | `[]` | Extra arguments, added after the server's own. Ignored in Restricted Mode. Same limits as agent arguments; `--tsserver-path` is refused, because typescript-language-server always uses its own TypeScript. |

A change made in the Settings tab restarts that language's servers. Servers that run the project's code (rust-analyzer, the Python servers) still start only in trusted folders, whatever this setting says.

### refresh

| Key | Type | Range | Default | Meaning |
|---|---|---|---|---|
| `kubernetesSeconds` | integer (s) | 2–600 | `5` | How often the Kubernetes panel re-reads the namespace while it is shown and the window is visible. |
| `awsSeconds` | integer (s) | 5–3600 | `30` | How often the AWS panel re-reads profiles and credential expiry while it is shown. |

Git status, Docker and pull requests don't poll: they follow change events, and pull requests reload when the window regains focus. Running CI checks are polled at [`ci.pollSeconds`](#ci).

### mcpServer

Flint's own read-only MCP server for agent chats. See [What agents can see in Flint](../guides/ai-agents.md#what-agents-can-see-in-flint).

| Key | Type | Range | Default | Meaning |
|---|---|---|---|---|
| `enabled` | boolean | | `true` | Offer the server to new agent chats. When `false`, the server closes and running chats lose access. |
| `tools.diagnostics` | boolean | | `true` | `get_diagnostics`: language-server errors and warnings. |
| `tools.editor` | boolean | | `true` | `get_editor_state`: open files, the active file and the selection. When `false`, Flint also stops sending the editor state to its backend. |
| `tools.terminals` | boolean | | `false` | `get_terminal_output`: recent output terminals and run consoles printed in the project. The agent asks before each call. |
| `tools.tests` | boolean | | `true` | `get_test_results`: the last test run. |
| `tools.kubernetes` | boolean | | `false` | `get_kubernetes_status`: what is failing in the current Kubernetes context. The agent asks before each call. |
| `terminalLines` | integer | 10–2000 | `200` | The most lines of one terminal's output an agent gets (clamped into the range). |

Tool switches apply right away, also to running chats.
### completions

Ghost-text suggestions in file editors from a fill-in-the-middle code model. See [AI in the editor](../guides/editor-and-navigation.md#ai-in-the-editor).

| Key | Type | Range | Default | Meaning |
|---|---|---|---|---|
| `enabled` | boolean | | `false` | Ask for suggestions while you type. Off by default, because it sends the code around the cursor to `endpoint`. |
| `provider` | string | `"ollama"`, `"openai"` | `"ollama"` | `"ollama"` uses Ollama's `/api/generate` with a suffix; `"openai"` uses an OpenAI-compatible `/v1/completions` with `prompt` and `suffix`. |
| `endpoint` | string | up to 256 characters | `"http://localhost:11434"` | The server's base URL. `http` only for `localhost`, `127.0.0.1` and `::1`; anything else needs `https`. No credentials, query or fragment. For `"openai"`, `/v1/completions` is added (or `/completions` when the URL already ends in `/v1`). |
| `model` | string | up to 200 characters | `"qwen2.5-coder:1.5b-base"` | The model name. Letters, digits and `. _ : / @ + -`. |
| `debounceMs` | integer (ms) | 50–5000 | `300` | Typing pause before a suggestion is asked for. |
| `maxTokens` | integer | 1–512 | `64` | Longest suggestion. |
| `contextLines` | integer | 1–1000 | `100` | Lines before and after the cursor that are sent (at most about 20,000 characters each way). |

An API key, if the endpoint needs one, is set in Settings ▸ AI in the Editor and stored in the macOS Keychain (service `flint-completions`, one entry per endpoint origin), never in this file. It is sent as a bearer token, and only to the endpoint it was stored for: after you change `endpoint` to another server, set its key again. Completions are never asked for in files that hold credentials (`.env*`, `*.pem`, `*.key`, SSH keys, `credentials`, `.npmrc`, `.netrc`, kubeconfig). Code sent to an endpoint that isn't on this machine has secrets redacted first. Each request has a 30-second timeout and a 1 MB answer limit, and redirects are not followed. Each new request cancels the editor's previous one.

### inlineEdit

| Key | Type | Range | Default | Meaning |
|---|---|---|---|---|
| `contextLines` | integer | 0–500 | `60` | Lines above and below the selection the agent sees with your instruction (at most about 10,000 characters each way). |

See [Edit with Agent in the editor](../guides/ai-agents.md#edit-with-agent-in-the-editor).
### agentTasks

Used by [agent tasks](../guides/ai-agents.md#agent-tasks-agents-in-parallel).

| Key | Type | Default | Meaning |
|---|---|---|---|
| `worktreeDir` | string | `""` | Folder for task worktrees, which go to `<worktreeDir>/<repository>/<branch>`. An absolute path or one starting with `~/`, up to 1024 characters. Empty puts them in `<repository>.worktrees/<branch>` next to the main checkout, like **New Worktree…** in the branch widget. |
| `branchPrefix` | string | `"agent/"` | Starts every task branch, followed by a name made from the task's first line. Letters, digits, `.`, `_`, `-` and `/`, up to 40 characters; may be empty. It may not start with `-`, `/` or `.`, or contain `..`, `//` or `/.`. |

Slashes in a branch name become dashes in the folder name. The tasks themselves are kept in `state.json`.

### usage

Used by the [Usage panel and budgets](../guides/ai-agents.md#usage-and-budgets). A budget of `0` is off.

| Key | Type | Range | Default | Meaning |
|---|---|---|---|---|
| `retentionDays` | integer | 7–3650 | `90` | Days of daily totals kept in `usage.json`; older days are dropped. |
| `dailyBudgetUsd` | number | 0–1,000,000 | `0` | Money all agent chats of a day may use: reported costs plus estimates. |
| `dailyBudgetTokens` | integer | 0–10¹² | `0` | Tokens (input plus output; cached input only with `countCacheReads`) all chats of a day may use. |
| `taskBudgetUsd` | number | 0–1,000,000 | `0` | Money one agent task's chats may use, over the days kept. |
| `taskBudgetTokens` | integer | 0–10¹² | `0` | Tokens one agent task's chats may use. |
| `countCacheReads` | boolean | | `false` | Token budgets also count input read from the prompt cache. |
| `pauseOverBudget` | boolean | | `false` | Refuse new turns while a budget is reached, until **Continue anyway** in the chat's warning. |
| `prices` | object | up to 200 models | `{}` | Model name (exactly as the Usage panel shows it, case ignored) → `{ "in": …, "out": …, "cacheRead": … }` in USD per million tokens, 0–10,000; `cacheRead` is optional (without it, cached input is left out of estimates). Used only for turns whose agent reported no cost; the result is shown as an estimate. |

### prompts

Used by the [prompt library](../guides/ai-agents.md#prompt-library).

| Key | Type | Default | Meaning |
|---|---|---|---|
| `builtins` | boolean | `true` | Offer the prompts that ship with Flint (`/review`, `/test`, `/explain`, `/fix`). |

The prompts themselves are Markdown files, not settings: personal ones in the `prompts` folder next to `settings.json`, project ones in the repository's `.flint/prompts`.
### agentReview

Used by [Review Changes with Agent](../guides/ai-agents.md#review-changes-with-agent). Settings ▸ Agents ▸ Review.

| Key | Type | Range | Default | Meaning |
|---|---|---|---|---|
| `scope` | string | `"selected"`, `"all"` | `"selected"` | What a review covers: the changes ticked in the Commit panel, or every change. A review started from a diff tab always includes that tab's file. |
| `maxDiffKB` | integer | 8–512 | `128` | Largest diff sent to the agent, in KB; never exceeded. Above it, small files still go in whole and the larger ones share the rest: each keeps its first hunks, or is summarized in one line, with a note that tells the agent to read the file. Files with no room left at all are left out and listed in a note. Untracked files are read only as far as their share. |
| `agent` | string | | `""` | The reviewing agent: `"claude"`, `"codex"` or the `id` of a custom agent. Empty uses `defaultAgent`. An agent that isn't configured falls back to empty. What each agent can read on its own is described in [Review Changes with Agent](../guides/ai-agents.md#review-changes-with-agent): Codex can read any file you can, and a custom agent started with an auto-approve flag is not read-only. |
| `timeoutSeconds` | integer | 30–1800 | `300` | A review that takes longer is stopped, with the agent and everything it started. |
### ci

Used by [When CI fails](../guides/git-and-github.md#when-ci-fails).

| Key | Type | Range | Default | Meaning |
|---|---|---|---|---|
| `pollSeconds` | integer (s) | 10–3600 | `30` | How often a pull request's checks are re-read while some are still running; the wait grows by half each time nothing changed (up to 8×, at most 30 minutes) and doubles while Flint is in the background. Nothing is polled while no check runs. |
| `logTailLines` | integer | 5–1000 | `60` | Lines from the end of the failed step that **Fix with agent** puts in the draft. |
| `logErrorLines` | integer | 0–500 | `40` | Error lines (test failures, compiler errors, `file:line` references) picked from the rest of the failed step. |
| `notify` | boolean | | `true` | A notification when your pull request's CI fails or turns green while Flint is in the background. Also needs `notifyInBackground`. |

Flint keeps at most the last 8 MB of each failed job's log (and says when it cut one), describes at most 4 failed jobs per run, cuts log lines at 2,000 characters and workflow, job and step names at 120, and the Log tab keeps the last 20,000 lines; these limits are not settings.

## Related settings stored elsewhere

Some preferences are not in `settings.json`:

- Recent folders, the agent approval policy, trusted folders, agent tasks and the update check's last result (including a skipped version) are in `state.json`.
- The sidebar width is kept by the app's web view.
- The selected HTTP environment is remembered only until Flint quits.

See [Files and data](files-and-data.md).
