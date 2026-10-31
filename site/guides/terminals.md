# Terminals

Flint's terminals are real terminals: each one runs your login shell in a pseudo-terminal, with your `PATH`, aliases and prompt, rendered with WebGL. This guide explains how they fit into the tab area, how they connect to the agent, and how they come back after a restart.

## Opening terminals

| How | Where the terminal starts |
|---|---|
| `⌘J` | Toggles between the terminal and your last file; opens a terminal if there is none |
| `⇧⌘T`, or `+` ▸ **Terminal** | A new terminal in the active tab's project |
| File tree ▸ **Open in Terminal** | In the selected folder |
| AWS panel ▸ terminal as profile | With `AWS_PROFILE` and `AWS_REGION` set (see [AWS](aws.md)) |
| Kubernetes panel ▸ `❯_` | In the selected context and namespace (see [Kubernetes](kubernetes.md)) |

With several projects in the window, the `+` menu has a section per project so you can choose where the terminal starts.

## Tab names

A terminal tab is named after its current directory, or after the program running in it. Terminals opened for an AWS profile or a Kubernetes context keep a prefix that tells you where they point, such as `aws:<profile> · …` or `k8s:<cluster>/<namespace>`.

## Working in the terminal

- `⌘`-click a `path:line` (for example in compiler or agent output) to open the file at that line. The file opens next to the terminal, and `⌘[` brings you back.
- URLs are clickable, including ones wrapped over several lines.
- `⌘K` clears the screen (outside a terminal, `⌘K` opens the Commit panel).
- `⇧↵` inserts a new line in `claude` and `codex` prompts.
- Right-click selected text for **Copy** and **Send to agent**.

## Sending output to the agent

Select output and press `⌘L`, or right-click ▸ **Send to agent**. The text goes to the agent chat of the terminal's project as a code block, in the draft, so you can add a question before sending. See [AI agents](ai-agents.md#giving-the-agent-context).

## Running agents in a terminal

You can run `claude` or `codex` interactively in a terminal tab, exactly as in any other terminal. Flint's own [agent chat](ai-agents.md) is an alternative that adds tool call diffs, the plan and the changed-files list.

## Terminals after a restart

Terminals are saved with the project session. After a restart, each terminal starts a new shell in its last directory (it follows `cd`), with its last ~400 lines of output shown dimmed above the new prompt. Full-screen programs, such as the interactive claude interface, are not saved.

Terminals opened as an AWS profile keep their profile after a restart.

Before a session is saved, Flint removes credentials it recognizes from the terminal output. The session file is readable only by your user.

## Restricted Mode

In a folder you haven't trusted, Flint doesn't start a terminal automatically, because shell prompts often run `git status` and version-manager hooks that read the folder's configuration. You can still open one yourself. See [Restricted Mode](restricted-mode.md).

## Fonts

Settings ▸ Terminal sets the font family, size and line height for terminals and run consoles. The change applies immediately. See [Settings](settings.md).

## Related

- [Use terminals](../how-to/use-terminals.md)
