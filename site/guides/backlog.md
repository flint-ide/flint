# Backlog

A backlog holds work that is planned but not started: a folder of items, for one or more repositories, that Flint shows as a list and as a board. **Delegate** turns an item into [agent tasks](ai-agents.md#agent-tasks-agents-in-parallel), one per repository the item names, run by the agent the item names, and the item's status follows them. This guide explains the folder and its files, how repositories are matched to your checkouts, what Delegate does and refuses, how the status moves, and what is shared with a team.

## When to use it

Use a backlog when you:

- plan work before anyone starts it, and want it in files next to the code rather than in another tool;
- have work that spans several repositories, such as an API and its client, and want one item to start one agent task in each;
- hand work to agents and want to see at a glance which items are waiting, running, in review or done.

For one task in one repository that you start right now, [New Agent Task…](ai-agents.md#agent-tasks-agents-in-parallel) is shorter. A backlog adds a place to keep the work before it starts and a status that follows the agent tasks. It does not replace an issue tracker: Flint does not import from or synchronize with Jira or GitHub Issues.

Nothing in a backlog runs by itself. Opening a backlog only reads its files. Agents start only when you click **Delegate**.

## Create and open a backlog

- **New Backlog…** (command palette, `⇧⌘P`, or **New** in the **Backlog** panel) makes a folder you pick into a backlog: it writes `flint-backlog.json` and an empty `items/` folder. It never overwrites a `flint-backlog.json` that is already there.
- **Open Backlog…** (or **Open** in the panel) picks a folder that already has a `flint-backlog.json`, for example one you cloned from your team.
- **Close** in the panel's backlog picker removes a backlog from Flint's list. The files stay where they are.

Flint remembers the backlogs you opened or created, most recent first, up to 20. A project open in the window whose top folder holds `flint-backlog.json` is listed without being opened. It can't be closed there; remove the folder from the window instead.

The backlog folder can be its own git repository, a folder inside one of your repositories, or a plain folder. A backlog is known by its real path, with symbolic links resolved.

The **Backlog** panel lists the items of the backlog you picked. **Show Backlog Board** in the command palette opens the same items as a [board](#the-board).

## The folder

```text
acme-plan/
  flint-backlog.json
  items/
    rate-limit-checkout.md
    cart-badge.md
```

### flint-backlog.json

```json
{
  "name": "Acme Shop",
  "repositories": [
    { "name": "api", "url": "git@github.com:acme/api.git" },
    { "name": "web", "url": "https://github.com/acme/web.git" }
  ]
}
```

| Field | Rule |
|---|---|
| `name` | Shown in the panel. At most 80 characters, one line. Default: the folder's name. |
| `repositories` | Optional, at most 50. A backlog without repositories is valid: its items run in the project that is active in the window. |
| `repositories[].name` | How items refer to the repository. Lowercase letters, digits and `-`, starting with a letter or digit, at most 40 characters, and unique in the file. |
| `repositories[].url` | A git remote URL, at most 1,000 characters. It identifies the repository on every teammate's machine, wherever their checkout is. See [How repositories are matched](#how-repositories-are-matched). |

Unknown fields are ignored. There is no form for the repositories: edit the file in the editor, then refresh the panel. If the file can't be used (it isn't valid JSON, a name or URL breaks a rule), the picker shows the reason and the backlog has no items until you fix it.

### Items

Each item is one Markdown file in `items/`. The front matter between the `---` lines is one `key: value` per line, as for [prompts](ai-agents.md#your-own-prompts) (`#` starts a comment, keys are not case-sensitive).

```markdown
---
title: Rate limit the checkout API
status: todo
priority: high
agent: backend
repos: api, web
---
Limit POST /checkout to 10 requests a minute per customer.

Done when:
- the API answers 429 with Retry-After past the limit
- the web client shows "Try again in a moment" on 429
```

| Part | Rule |
|---|---|
| Id | The file name without `.md`: letters, digits, `-` and `_`, at most 64 characters. Only `.md` files directly in `items/` count; files that start with a `.` are skipped, and files in subfolders are not read. |
| `title` | Required, at most 120 characters. |
| `status` | `todo` (default), `doing`, `review` or `done`. |
| `priority` | `low`, `normal` (default) or `high`. Orders the items inside a status, high first, then by title. |
| `agent` | Optional. The name of any agent: `claude`, `codex`, a custom agent's id or a [defined agent](ai-agents.md#defined-agents). Empty means the default agent (Settings ▸ Agents). |
| `repos` | Optional, comma-separated names from `flint-backlog.json`, each at most once. Empty means the project that is active in the window. |
| Text after the front matter | What to do and how to tell it is done. |

The title and the text together become the agent task's description, the first message the agent gets. They may be at most 8,000 bytes. With several repositories, Delegate adds a note naming them, and the note counts too.

Unknown keys are kept when Flint rewrites the file and are otherwise ignored.

**New Item…** (in the panel, or "New Backlog Item…" in the command palette) asks for a title, makes the id from it (lowercase letters, digits and `-`, at most 40 characters; `-2`, `-3`… when the name is taken), writes the file from this template and opens it:

```markdown
---
title: Rate limit the checkout API
status: todo
priority: normal
# agent: backend     the agent that works on it (default: the default agent)
# repos: api, web    repositories from flint-backlog.json (default: the active project)
---
What to do.

Done when:
- 
```

### Problems

A file that breaks a rule is not shown as an item. It is listed under the items as a problem, with the file and the reason, and **Ask the agent** puts the problem into a chat draft. These are the usual ones:

| Reason | Fix |
|---|---|
| `title is missing`, or a `status` or `priority` that isn't one of the values above | Correct the front matter. |
| `repository "x" is not in flint-backlog.json` | Add the repository to the file or fix the name in `repos`. |
| `title and text are longer than 8000 characters` | Shorten the text. |
| `larger than 64 KB` | Items are plain files of at most 64 KB. Split the work into several items. |
| `front matter has no closing ---`, or a line that is not `key: value` | Fix the line. A comment starts with `#`. |
| `the agent "x" is not available here` | The item stays listed. Define or install the agent, or change `agent`. Delegate says the same. |
| `more than 500 files; the rest are not loaded` | A backlog shows at most 500 items. |
| `links outside its folder` | A symbolic link that leaves the backlog folder is refused. |

## How repositories are matched

An item names repositories, not folders, so the same backlog works on every machine. For each repository, Flint looks for your checkout:

1. A project open in the window whose repository has a remote with the same URL. Any remote counts, not only `origin`.
2. Otherwise the folder you chose with **Locate…** for that URL.

A repository with neither shows **Locate…**. It asks for a folder and accepts only a checkout that has a remote with that URL. A folder that is an agent task's worktree is refused: choose the repository's own checkout. The choice is kept in Flint's `state.json` per URL, never in the backlog, so every teammate has their own. If the located folder is moved or deleted, the repository is listed with the reason and you locate it again. A located folder that isn't open in the window is added to it before Delegate uses it.

Flint does not clone a repository that has no checkout.

### How URLs are compared

Flint compares URLs after dropping what doesn't name the repository: the scheme, the user, the port, a trailing `.git` and trailing slashes. The host is lower-cased. These are one repository:

```text
git@github.com:acme/api.git
ssh://git@github.com:22/acme/api
https://github.com/acme/api/
```

The path keeps its case. Local paths (`/srv/git/api.git`, `file:///srv/git/api.git`) are compared as paths. A host that uses different paths for ssh and https gives two different repositories: write the URL the way your remotes spell it. Something that isn't a git remote URL is refused in `flint-backlog.json`.

## Delegate

**Delegate** is on every item in the panel and on the board. It does this, in order:

1. **Checks the agent.** The item's `agent`, else the default agent. It must be available now.
2. **Finds a checkout for every repository** the item names, as described above. An item without `repos` uses the project that is active in the window, which must be a project folder open in it.
3. **Creates one agent task per repository**, the way New Agent Task does: a branch from the checkout's current branch, a worktree, the [queue](ai-agents.md#the-queue) and its limit, and the trust of the worktree. The description is the item's title and text. When the item names several repositories, each task also gets a note naming the item's other repositories and saying which one this task works in.
4. **Starts the agents.** A task that gets a slot starts its chat at once, with the description as its first message. The others wait in the queue, like any queued task.
5. **Remembers the tasks for the item and sets its status to `doing`.**

Delegate is all or nothing. If one task can't be created, Flint removes the ones it already created for this Delegate and shows the error.

The item row lists its agent tasks with their states (queued, working, waiting for you, finished, failed, as in the [Agent Tasks panel](ai-agents.md#agent-tasks-agents-in-parallel)), and **Open** on a task shows its folder and chat, as in that panel. Review, merge or create a pull request there.

**Delegating again** (after a failure, or to try another agent) adds agent tasks. The item shows all of them.

### What Delegate refuses

Delegate checks everything before it creates anything, and lists every repository that is a problem in one message.

| Message says | Why | What to do |
|---|---|---|
| The agent isn't available here, or no agent can start | The item's `agent` isn't defined or installed here. A project's defined agent isn't loaded in a folder in [Restricted Mode](restricted-mode.md#what-is-restricted). With no `agent`, no agent is installed. | Define or install the agent, change `agent`, or install Claude Code or Codex, or add an agent in Settings ▸ Agents. |
| No checkout of a repository was found | No open project and no located folder has a remote with its URL | Open the repository in the window, or click **Locate…**. |
| A located folder can't be used | It is gone, is not a git checkout, is an agent task's worktree or has no remote with that URL | **Locate…** the repository again. |
| A folder is in Restricted Mode | The checkout isn't [trusted](restricted-mode.md#trusting-a-folder). Delegate would start an agent in it. | Trust the folder, then Delegate again. |
| The item names no repository and no project is open, or the project is not an open folder | An item without `repos` runs in the active project | Open the project the item is for. |
| The title and text with the note about its repositories are too long | Together they may be at most 8,000 bytes | Shorten the text. |
| The item has 100 agent tasks | An item remembers at most 100 | Remove some in the Agent Tasks panel. |
| A task could not be created | For example the checkout is on a detached HEAD (Delegate starts from the current branch, so check one out), or the branch or the worktree failed. The message names the folder. | Fix what it names, then Delegate again. |

An item that is listed as a problem can't be delegated, because it isn't an item yet. If the tasks start but Flint can't write the status, it tells you; the tasks keep running.

## How the status moves

An item has one status: `todo`, `doing`, `review` or `done`. It is stored in the item's file, in its `status:` line.

| Change | When |
|---|---|
| any → any | You pick a status: the menu on the item, or dragging a card on the board. |
| any → `doing` | **Delegate** started the item's agent tasks. |
| `doing` → `review` | Every agent task of the item finished (the agent's turn ended). A task that failed keeps the item in `doing`, marked failed. |
| `review` → `done` | Every agent task of the item is merged into its base branch, or its pull request was created and the task removed. |

The automatic changes start only from `doing` or `review`: an item you moved somewhere else stays there. Both can happen in one step: an item goes from `doing` to `done` when every task is already merged.

- A task you remove without merging it and without a pull request, such as a failed attempt you discard, no longer counts. With no task left, the status doesn't move.
- A task whose pull request was created but that still exists doesn't count as delivered yet. Remove it after creating the pull request, or merge it.
- A failed task is marked on the item. Open it and ask the agent to fix it, or remove it and Delegate again.

Flint makes these changes when agent tasks change, when Flint starts, and whenever the panel reads the backlog (when it is shown and on refresh), which also notices a merge you made outside Flint. Nothing polls. A task that was merged outside Flint and removed before Flint looked counts as discarded.

### Moving it by hand

Use the status menu on an item (or drag its card). You can move an item to any status, for example back to `todo` after a failed attempt. Flint changes only the value on the `status:` line and keeps the rest of the file as it was, with its line endings. When there is no `status:` line, it adds one before the closing `---`.

Flint refuses to write a file it can't change safely and says why. Set the status in the editor instead. The reasons are: two `status:` lines, no closing `---`, a line that is not `key: value`, or a file that changed between Flint reading and writing it (try again).

## The board

**Show Backlog Board** opens the **Board** tab: one column per status (`todo`, `doing`, `review`, `done`), with the items as cards. A card shows what an item row shows and has the same actions: **Delegate**, **Open**, set status, and **Open** on an agent task. Drag a card to another column to set its status.

## Share a backlog with a team

Put the backlog in a git repository, or in a folder of one, and commit it like code. The files and the statuses are shared:

| Shared (in the backlog folder) | Local to your machine |
|---|---|
| `flint-backlog.json`, the items and their text, priority, agent and repositories | Which agent tasks belong to an item |
| Each item's status | The agent tasks themselves, their worktrees and chats |
| | The folders you chose with **Locate…** |
| | The list of backlogs you opened |

So a teammate sees that an item is `doing`, not the tasks on your machine. When you commit and push, your status changes go with it. When a teammate delegates an item you haven't, the status moves on their machine and reaches you with their commit. Two people changing the same item's status at once is an ordinary git merge conflict on the `status:` line.

Because statuses are written to the files, a status that moves by itself leaves an uncommitted change in the backlog's repository. Commit it with the rest.

Flint reads the files again when the panel is shown, on refresh, after its own writes, when agent tasks change, and, for a backlog inside a project open in the window, when a file changes. It does not watch a backlog folder outside the window, and it does not notice a teammate's push by itself: pull, then refresh.

Don't put secrets in an item. It is a plain file in your repository, and its text is sent to an agent.

## What agents can see

An agent chat can read the backlog through Flint's [MCP server](ai-agents.md#what-agents-can-see-in-flint), so you can ask what is planned or what to do next. The tool is `get_backlog`. It only reads: statuses change in Flint.

| Argument | Meaning |
|---|---|
| `status` | Optional. Only items with this status (`todo`, `doing`, `review` or `done`). |
| `backlog` | Optional. Only the backlog with this name. |

The answer lists each open backlog with its repositories and its items: id, title, status, priority, agent, repositories and text. It does not include agent tasks. The text of one item is cut at 2,000 bytes, and the whole answer at 60 KB, with a line saying how many items were left out; ask for one status or one backlog to see them.

It shows every backlog Flint has open, not only the one in the chat's project. In Claude Code it is not one of the tools that run without asking: you are asked before each call, as for terminal output, unless you chose "Do everything". It is offered while the Backlog feature is on and, like all of Flint's tools, not in a folder in Restricted Mode.

## Backlogs and Restricted Mode

A backlog is read in any folder: reading files runs nothing. Delegate is what starts an agent, so it is refused for a repository checkout in [Restricted Mode](restricted-mode.md) until you trust the folder. The item's text is a message to an agent, so read an item from a backlog you didn't write before you delegate it.

## Limits

Flint fixes these because a backlog may come from someone else's repository.

| What | Limit |
|---|---|
| Items per backlog | 500, in `items/` only |
| Size of an item file or of `flint-backlog.json` | 64 KB |
| Title | 120 characters |
| Title and text together | 8,000 bytes, with Delegate's note about the repositories |
| Backlog name | 80 characters |
| Repositories per backlog | 50; a name is at most 40 characters, a URL at most 1,000 |
| Item id and agent name | 64 characters |
| Backlogs Flint remembers | 20 |
| Located folders it remembers | 200 |
| Agent tasks remembered per item | 100 |
| `get_backlog` answer | 60 KB, and 2,000 bytes of text per item |

A symbolic link that leaves the backlog folder is refused.

## Not supported yet

- Cloning a repository that has no checkout.
- Dependencies between items, and starting an item when another is done.
- One agent working in several repositories at once, or a coordinating agent above the per-repository ones.
- Asking an agent to split a goal into items, milestones, budgets per item, and assignees who are people.
- Watching a backlog folder outside the window, and live updates from a teammate's push.
- Import from or synchronization with trackers.

## Turning it off

Backlog is a built-in feature that you can switch off in Settings ▸ Extensions ▸ **Built-in**. A switched-off Backlog is not started, and `get_backlog` is not offered, until Flint restarts with it on. Your backlogs stay where they are. See [features](../reference/settings.md#features).

## Related

- [AI agents](ai-agents.md)
- [Restricted Mode](restricted-mode.md)
- [Files and data](../reference/files-and-data.md)
- [Build a feature with an agent](../use-cases/build-a-feature-with-an-agent.md)
