# AI agents

Flint is built around coding agents. The agent writes the code; Flint gives it context, shows you what it is doing, and lets you review and run the result. This guide explains the agent chat, how to give the agent context, what it may do without asking, and the other places agents help.

## Supported agents

Flint talks to the agent CLIs you have installed:

| Agent | CLI | Notes |
|---|---|---|
| Claude Code | `claude` | Reports token usage and cost |
| Codex | `codex` | Usually reports tokens but not cost |
| Any agent with an ACP mode | your choice | Added in Settings; see [Other agents (ACP)](#other-agents-acp) |

Flint uses the CLIs directly, with your own sign-in and configuration; it has no account or API key of its own. You can also run either agent's interactive interface in a regular [terminal](terminals.md) tab.

The **default agent** (Settings ▸ Agents) is used for new chats, commit messages and conflict resolution. If you don't pick one, Flint uses the first installed agent.

## Other agents (ACP)

Many coding agents speak the [Agent Client Protocol](https://agentclientprotocol.com) (ACP). Flint can use any of them for chats, "Ask the agent" and commit messages, without code for each agent.

1. Install the agent's CLI and log in with its own command in a terminal, for example `gemini` (Gemini CLI) or `opencode auth login` (OpenCode). Flint stores no API keys for it.
2. Open Settings (`⌘,`) ▸ Agents ▸ **Add a custom agent**. Click a preset (**Gemini CLI** fills in `gemini` with `--experimental-acp`, **OpenCode** fills in `opencode` with `acp`) or enter the command and the arguments that start its ACP mode, one per line. The command is a name on your login shell's `PATH` or an absolute path. Name and id are optional.
3. Click **Add Agent**, then **Check**. The first time you check a command line after Flint starts, Flint shows that exact command line and asks you to confirm. It starts the agent in an empty temporary folder, runs the ACP handshake and shows the agent's name and version, or the error. If the check times out, run the same command in a terminal to see what it is waiting for.
4. The agent now appears in the `+` menu (for example **gemini chat**), in "New Chat with gemini" and in the default-agent pickers. Make it the default to use it for "Ask the agent" and commit messages.

Other agents with an ACP mode work the same way once you install them, for example Goose (`goose acp`) or Zed's adapters for Claude Code and Codex (the `claude-agent-acp` and `codex-acp` commands). Check their documentation for the exact command.

What a custom agent can and can't do:

- It starts only in a [trusted folder](restricted-mode.md); in Restricted Mode Flint shows an error asking you to trust the folder.
- Its questions use the same approval policy as Claude Code and Codex (see below).
- Flint lets it read and write files only inside the chat's project and never inside a `.git` folder (at any depth). Writes into agent or editor configuration (`.claude`, `.codex`, `.vscode`, `.idea`) ask you first in the chat. Flint offers it no terminal of its own. The agent still runs its own tools with your user's rights, as it does in a terminal.
- **Generate message** runs it once in the repository: Flint lets it read files there, declines every permission request and refuses writes sent through Flint. Its own tools and configuration are outside Flint's control: if the agent's settings let it edit files or run commands without asking, it can still do so, as in a terminal.
- Conflict resolution runs an agent interactively in a terminal, which ACP agents don't have, so Claude Code or Codex does it (the button names which one). Without either installed, the button is hidden.
- A chat with a custom agent can't continue the old conversation after a restart; Flint starts a new one and tells you.

Editing or removing an agent in Settings affects new chats; open chats keep running until you close them.

## The chat

Open a chat from the `+` button in the tab bar (**claude chat** / **codex chat**, or a custom agent) or with "New Chat with claude" / "New Chat with codex" in the command palette. A chat starts in the active tab's project.

A chat shows:

- the streamed response;
- each tool call, with a mini diff for file edits;
- the agent's plan, when it makes one;
- after each turn, the list of changed files (click one to open it);
- the agent's reasoning, if you tick **thinking** in the header.

Type in the box at the bottom. `↵` sends, `⇧↵` adds a new line. While the agent works, `Esc` or **■ Stop** stops it.

### Usage and cost

The chat header shows cumulative input and output tokens, the cost in USD when the agent reports it, and a context bar (for example "42% context") with details in a tooltip. The status bar shows the total for the project, or for the whole workspace when several projects are open.

### Chats after a restart

Open chats are part of the project session. After a restart you see the transcript again, and the agent picks the conversation up with your next message. If a conversation can't be resumed, Flint starts a new one and tells you.

## Giving the agent context

| How | What is attached |
|---|---|
| `⌘L` in an editor | The selection, or the current file when nothing is selected |
| `⌘L` in a terminal, or right-click ▸ **Send to agent** | The selected output, as a code block |
| **＋ current file** in the chat | The file open in the editor |
| File tree ▸ **Add to Agent Chat** | The file |
| **Fix with agent** on a language server problem | Path, line and column, severity, source, message and the surrounding lines |
| **Fix all N in file** (`⌥⇧⌘L`) | Every problem in the file |
| **Fix with agent** in the Tests tab, or "Fix Failed Tests with Agent" | The failed tests and their output |
| **Diagnose with AI** in the Kubernetes panel | Rollout state, problems, events and recent logs, with secrets redacted |

Context goes to the chat of the project it belongs to: the active or most recently used chat there, or a new chat with the default agent if there is none. It lands in the draft so you can add your own words before sending. `⌥`-click sends it immediately.

## Prompt library

Prompts you use again and again live in Markdown files, and every agent chat offers them the same way: type `/` in the chat box and a list opens with each prompt's name, description and where it comes from (**project**, **personal** or **built-in**). Keep typing to filter it; `↑`/`↓` move, `↵` or `Tab` chooses. The chosen prompt replaces the draft with its text, placeholders filled in, so you can read and change it before you press `↵` to send. Nothing is sent on its own.

"Run Prompt…" in the command palette does the same from anywhere: it puts the prompt into the current chat's draft, or opens a chat with your default agent.

### Built-in prompts

| Prompt | Asks the agent to |
|---|---|
| `/review` | Review the uncommitted changes (the diff is included) without changing files |
| `/test` | Write tests for the selection or the current file and run them |
| `/explain` | Explain the selection or the current file without changing files |
| `/fix` | Fix the problems the language server reports in the current file |

Commit messages already have their own button in the Commit panel, so there is no prompt for them. Turn the built-ins off in Settings ▸ Agents ▸ Prompts. A personal or project prompt with the same name replaces a built-in one.

### Your own prompts

"New Prompt…" asks for a name and a place and opens the new file from a template:

- **Project**: `.flint/prompts/<name>.md` in the repository, shared with your team when you commit it.
- **Personal**: `prompts/<name>.md` in Flint's configuration folder (`~/Library/Application Support/flint/prompts`, shown in Settings ▸ Agents ▸ Prompts); yours only, in every project.

A project prompt replaces a personal one with the same name, which replaces a built-in one. The name is the file name: letters, digits, `-` and `_`. Files are read each time the list opens, so an edit shows up right away.

```markdown
---
description: Review the changes against our API guidelines
agent: claude
args: ticket
---
Review my uncommitted changes on {{branch}} for ticket {{ticket}}.
Check them against docs/api-guidelines.md.

{{diff}}
```

The front matter between the `---` lines is optional, one `key: value` per line (`#` starts a comment):

- `description`: the line shown in the list.
- `agent`: only offer the prompt in chats with this agent (`claude`, `codex` or a custom agent's id). "Run Prompt…" opens a chat with that agent if needed.
- `args`: names you are asked for when you choose the prompt (comma-separated, up to 8), used as `{{name}}` in the text.

Flint fills these placeholders:

| Placeholder | Becomes |
|---|---|
| `{{selection}}` | The selected text of the editor you used last, as a code block with secrets redacted (empty when nothing is selected; only a note for a credentials file such as `.env`) |
| `{{file}}` | That editor's file, relative to the project |
| `{{problems}}` | The language server's problems in that file, one per line, secrets redacted |
| `{{diff}}` | The uncommitted changes, including new files, as a `diff` code block (at most 60 KB, secrets redacted). Credentials files (`.env`, keys, kubeconfig, …) are left out and only named |
| `{{branch}}` | The current branch |

Placeholders are filled once: text that a value brings in (a selection containing `{{diff}}`) is left as it is, and an unknown `{{name}}` stays in the draft for you to see.

### The agent's own slash commands

Claude Code and other agents have slash commands of their own (`/compact`, …). When what you typed doesn't match a Flint prompt, the list says so and `↵` sends it to the agent unchanged. To send a command that has the same name as a Flint prompt (Claude Code's `/review`, say), press `Esc` to close the list, then `↵`. Anything after a space (`/compact keep the API notes`) also goes to the agent as typed.

### Prompts in Restricted Mode

A prompt is only text for the draft: it never runs anything, in any folder. In a folder you haven't trusted, the project's prompts are still listed, marked **untrusted repo**, so read them before you send them. They can't replace a built-in or personal prompt: with a hostile `.flint/prompts/review.md`, `/review` stays Flint's own, and the list says that a file was skipped. Prompt files must be regular files of at most 64 KB inside `.flint/prompts`; links that lead out of the folder are ignored.

## What agents can see in Flint

An agent chat in Flint can also look things up by itself. Flint runs a small [MCP](https://modelcontextprotocol.io) server and offers it to every chat, so the agent can read what Flint knows and a terminal doesn't:

| Tool | What the agent gets | Default |
|---|---|---|
| `get_diagnostics` | The errors and warnings the language servers report, for the project, a folder or a file. Only files a language server has checked are included: usually the open files and, for Go, their packages. | On |
| `get_editor_state` | The files open in the editor (and which have unsaved changes), the active file, and your selection with its text, or the cursor position. Ask about "this function" and the agent can see which one. | On |
| `get_test_results` | The last test run in Flint: totals, and each failed test with its location and output. | On |
| `get_terminal_output` | The recent output your terminals and run consoles printed while they were in the project (dev servers, builds), as plain text without colors. | Off |
| `get_kubernetes_status` | What is failing in a namespace of the current Kubernetes context: unhealthy apps, workloads that didn't roll out, failing pods with their reason, failed jobs and warning events. No secrets and no logs. | Off |

The tools only read. Claude Code shows them as `mcp__flint__…`.

- Diagnostics, editor state and test results show only the project's own state. With the default approval policy ("Read and search the project" on) Claude Code runs them without asking, so the agent can look at what you are looking at without a prompt each time.
- Terminal output and Kubernetes status can show anything you saw: a token printed in a terminal, another cluster's names. They are off until you turn them on, and then the agent asks before each call, even with "Read and search" on. This keeps Flint's rule that nothing goes to an AI provider without you seeing it.
- Codex and custom agents decide about all of them with their own rules.

How it is protected:

- The server listens only on `127.0.0.1`, on a port the system picks. It starts with the first chat and closes when you switch it off or quit.
- Each chat gets its own random token, which stops working when the chat's agent stops or its project is closed. An agent only ever sees its own project (the Kubernetes tool is the exception: it reads the current context, like `kubectl` does). Requests without a valid token, or from a web page, are refused.
- Terminal output is kept by where it was printed: output from before a terminal moved into the project (with `cd`) isn't shown. A command that changes folders and prints in one go (`cd ~ && cat secrets`) can still land in the project's part, because Flint learns the new folder just after the output.
- The token reaches the agent through the `FLINT_MCP_TOKEN` environment variable (Claude Code expands it in the server's `Authorization` header; Codex reads it with `bearer_token_env_var`) or the ACP connection, never on a command line. Programs the agent runs inherit the variable, so they can use the tools too while the chat lasts.
- Answers are bounded in size, and credentials (keys, tokens, passwords in URLs) are redacted before they reach the agent.
- Folders in [Restricted Mode](restricted-mode.md) get no access.

Which agents get it:

- **Claude Code**: through `--mcp-config`, next to your own MCP servers.
- **Codex**: through `-c mcp_servers.flint…` overrides, when `codex mcp add --help` shows it supports HTTP MCP servers (`--url` and `--bearer-token-env-var`); an older Codex starts without it.
- **Custom (ACP) agents**: when the agent says it can connect to HTTP MCP servers (`mcpCapabilities.http`); otherwise not.
- Commit messages and other one-shot runs don't use it.

To turn it off, or to switch single tools on or off, open Settings (`⌘,`) ▸ **Agent Context**. Switching the server off closes it and cuts running chats off at once. See [`mcpServer`](../reference/settings.md#mcpserver) for the keys.

## What the agent may do without asking

The link in the chat header (for example **auto: edits**) shows and changes the approval policy. The policy is shared by all chats and kept between runs.

| Option | Default |
|---|---|
| Edit files in the project | On |
| Read and search the project | On |
| Do everything (commands too) | Off |

With the defaults, the agent edits and reads files inside the project folder without asking, and asks before running any command or touching files outside the project. When it asks, a permission card appears in the chat with the choices the agent offers. If you choose to always allow a Claude command, Claude Code records that in `.claude/settings.local.json` in the project, as it does in the terminal.

The same switches are in the command palette as "Toggle Auto-Approve: Edit Files", "Toggle Auto-Approve: Read and Search" and "Toggle Auto-Approve: Do Everything (Commands Too)".

> **Warning:** "Do everything" lets the agent run any command without asking. The agent's own sandbox still applies, but Flint no longer stops to ask you.

You never have to accept diffs one by one. Edits are applied directly, show up in the editor and the gutter markers, and you review them in the Commit panel (`⌘K`), where you can roll back anything you don't want.

## Commit messages and merge conflicts

- **✦ Generate message** in the Commit panel gives the agent the diff of the ticked files and your recent commit messages. The result lands in the message field for you to edit.
- **Resolve with claude** (or your default agent, when it is Claude Code or Codex) appears when there are merge conflicts. It opens an interactive agent session in a new terminal. The agent runs `git add` on resolved files but doesn't commit.

## Edit with Agent in the editor

Select code (or put the cursor on a line) and press `⌘I` ("Edit Selection with Agent…"). A small prompt opens above it; type what to change, for example "add error handling", and press `↵`.

- The default agent runs once, headless, like **Generate message**. It gets the file's path, its language, the selection and the lines around it (Settings ▸ AI in the Editor ▸ Context lines, 60 by default), and is asked for the replacement text only. Secrets in the surrounding lines are redacted; a selection that contains something that looks like a credential is not sent.
- Claude Code runs with read-only tools (Read, Grep, Glob) and without MCP servers, and Codex in its read-only sandbox, so text in the file can't make the agent change files or run commands. Files that hold credentials (`.env*`, keys, kubeconfig) are refused.
- The answer replaces the selection directly, as one change: one `⌘Z` takes it back. There is no accept or reject step.
- If you edit the selected code while the agent works, its answer is not applied and the prompt says so. Typing elsewhere in the file is fine.
- `Esc` closes the prompt; while the agent works, it also stops the agent. Errors (no agent installed, a timeout) appear in the prompt.
- In Restricted Mode the run follows the rules below: Claude Code without the project's settings, no extra arguments, and custom ACP agents don't start.

Change the shortcut in Settings ▸ Keymap.
## Agent tasks: agents in parallel

An agent task gives an agent its own branch in its own [git worktree](git-and-github.md), so several agents can work at once without touching each other's files or yours.

1. Run "New Agent Task…" from the command palette (`⇧⌘P`), or click `＋` in the **Agent Tasks** panel (`⑂` on the activity bar).
2. Describe the task. Its first line names the branch: "Add rate limiting" becomes `agent/add-rate-limiting` (`-2`, `-3`… when the name is taken).
3. Pick the agent (the default agent is preselected; Claude Code, Codex or a custom agent) and the branch to start from (the current one by default), then click **Start** (`⌘↵`).

Flint creates the branch and the worktree (by default in `<repository>.worktrees/` next to your checkout, like **New Worktree…** in the branch widget), attaches the folder to the window as another project, opens a chat there and sends your description as the first message. Nothing else is added to it.

The panel lists the tasks of every repository open in the window, newest first. Each row shows:

| What | Meaning |
|---|---|
| State | **working**, **waiting for you** (the agent asks for approval in its chat), **finished** (the turn ended: review it), **failed** (the error is shown under the row) or **idle**. It follows the chat's own events; nothing polls. |
| Branch | The task's branch, and **↑N** for the commits its base doesn't have yet. |
| Changes | Files changed since the branch left its base, committed or not, new files included, with added and removed lines (**merged** once its base has every commit). Refreshed when files or Git change. |

The actions:

- **Open** shows the task's folder and its chat (a new chat if you closed it).
- **Show diff** lists the changed files; click one for a side-by-side diff against the point where the branch started.
- **✦ Ask agent** (failed tasks) puts the error into the task's chat draft, so you can add a hint and send it.
- **Merge** merges the branch into its base, in the checkout where the base is checked out. Flint refuses while the task or that checkout has uncommitted changes, or while the agent is working. When the merge would conflict, nothing is changed: Flint offers to merge the base into the task's branch instead (always with a merge commit) and let an agent resolve the conflicts there, in a terminal (as **Resolve with claude** does); then merge again.
- **Create PR** pushes the task's branch (whatever the worktree has checked out at the moment) and creates a pull request into its base with `gh`, titled with the task's first line and described by its text. Flint asks first, because it publishes the branch. **PR ↗** opens it afterwards.
- **✕** removes the task: it stops the task's chats, closes its folder and deletes the worktree. Uncommitted work, new files, ignored files such as `.env` or build output, and a branch with commits its base doesn't have are deleted only if you confirm; otherwise the branch is kept.

Tasks are remembered per repository in `state.json`, so they survive a restart. A task whose worktree folder was deleted outside Flint shows as **stale** and can only be removed; removing it forgets only that worktree, not other missing ones (for example on a disk that isn't connected).

In [Restricted Mode](restricted-mode.md), a task's worktree is trusted when the checkout it started from is trusted (it holds the same repository's commits); otherwise it stays restricted like any new folder. Removing the task, or the worktree folder disappearing, removes that trust again. Because trust follows folders, a task from an untrusted checkout can't put its worktree inside a trusted folder, and no task worktree may lie inside the checkout itself; Flint refuses and asks you to pick another worktree folder. Worktrees you create by hand with **New Worktree…** don't inherit trust: trust them like any other folder.

The worktree folder and the branch prefix are settings (Settings ▸ Agents ▸ Agent tasks; [`agentTasks`](../reference/settings.md#agenttasks) in `settings.json`).

## Notifications

When an agent turn finishes while Flint isn't the active app, you get a macOS notification (click it to jump to the chat) and a badge on the Dock icon. Turn this off in Settings ▸ Agents.

## Agents in Restricted Mode

In a folder you haven't trusted, agents run without the project's own agent settings and hooks (Claude Code is started with `--setting-sources user`). Trust the folder to let them apply. Custom ACP agents don't start there at all, and chats there don't get [Flint's MCP server](#what-agents-can-see-in-flint): a hostile repository could otherwise steer the agent into reading your terminals or cluster without a prompt. The repository's [prompts](#prompts-in-restricted-mode) are marked and can't replace yours. See [Restricted Mode](restricted-mode.md).

## Related

- [Work with agents](../how-to/work-with-agents.md)
- [Build a feature with an agent](../use-cases/build-a-feature-with-an-agent.md)
