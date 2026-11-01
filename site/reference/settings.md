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
    "lineHeight": 1.55
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
  "completions": {
    "enabled": true,
    "provider": "ollama",
    "endpoint": "http://localhost:11434",
    "model": "qwen2.5-coder:1.5b-base",
    "debounceMs": 300,
    "maxTokens": 64,
    "contextLines": 100
  },
  "inlineEdit": { "contextLines": 60 }
}
```

## Keys

| Key | Type | Default | Meaning |
|---|---|---|---|
| `version` | number | `1` | Schema version. Written by Flint; you don't need to change it. |
| `theme` | string | `"dark"` | `"dark"`, `"light"` or `"system"` (follows the macOS appearance). |
| `editor` | object | see below | Font for all editors: files, diffs and scratchpads. |
| `terminal` | object | see below | Font for terminals and run consoles. |
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
| `completions` | object | see below | Inline suggestions from a code model (off by default). See [completions](#completions). |
| `inlineEdit` | object | see below | Edit with Agent (`⌘I`). See [inlineEdit](#inlineedit). |

### Font objects

`editor` and `terminal` have the same three keys:

| Key | Type | Range | Editor default | Terminal default |
|---|---|---|---|---|
| `fontFamily` | string | up to 200 characters | `'JetBrains Mono', 'SF Mono', Menlo, Consolas, monospace` | `"JetBrains Mono", "SF Mono", Menlo, monospace` |
| `fontSize` | integer (px) | 8–32 | `13` | `13` |
| `lineHeight` | number (multiple of the font size) | 1.0–2.5 | `1.55` | `1.2` |

`fontFamily` is a CSS font list; the first installed font is used. It may not contain `;`, `{`, `}`, `<`, `>` or `\`.

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

- name a flag Flint sets. For Claude Code: `-p`/`--print`, `--input-format`, `--output-format`, `--include-partial-messages`, `--replay-user-messages`, `--permission-prompt-tool`, `--permission-mode`, `--dangerously-skip-permissions`, `--allow-dangerously-skip-permissions`, `--session-id`, `-r`/`--resume`, `-c`/`--continue`, `--fork-session`, `--setting-sources`, `--settings`, `--allowedTools`/`--allowed-tools`, `--disallowedTools`/`--disallowed-tools`, `--tools` and `--model` (use `model`). Commit messages and inline edits from Claude Code always run with only the `Read`, `Grep` and `Glob` tools and without MCP servers (`--tools Read,Grep,Glob --strict-mcp-config`). For Codex: `-m`/`--model`, `-a`/`--ask-for-approval`, `-s`/`--sandbox`, `--full-auto`, `--dangerously-bypass-approvals-and-sandbox`, `--yolo`, `-C`/`--cd`, `--listen`, `-p`/`--profile`, and `-c` overrides of `approval_policy`, `sandbox*`, `projects*`, `profile*` or `dangerously*`. Commit messages from Codex always run with `--sandbox read-only`. Short flags are checked in any combination (`-dp` counts as `-p`);
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

Git status, Docker and pull requests don't poll: they follow change events, and pull requests reload when the window regains focus.

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

## Related settings stored elsewhere

Some preferences are not in `settings.json`:

- Recent folders, the agent approval policy, trusted folders and the update check's last result (including a skipped version) are in `state.json`.
- The sidebar width is kept by the app's web view.
- The selected HTTP environment is remembered only until Flint quits.

See [Files and data](files-and-data.md).
