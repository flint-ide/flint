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
- Flint lets it read and write files only inside the chat's project, never inside `.git`, and offers it no terminal of its own. The agent still runs its own tools with your user's rights, as it does in a terminal.
- **Generate message** runs it once in the repository: it may read files, but Flint declines every permission request and refuses writes.
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

## Notifications

When an agent turn finishes while Flint isn't the active app, you get a macOS notification (click it to jump to the chat) and a badge on the Dock icon. Turn this off in Settings ▸ Agents.

## Agents in Restricted Mode

In a folder you haven't trusted, agents run without the project's own agent settings and hooks (Claude Code is started with `--setting-sources user`). Trust the folder to let them apply. Custom ACP agents don't start there at all. See [Restricted Mode](restricted-mode.md).

## Related

- [Work with agents](../how-to/work-with-agents.md)
- [Build a feature with an agent](../use-cases/build-a-feature-with-an-agent.md)
