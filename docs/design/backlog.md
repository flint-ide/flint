# Backlog: planning work above repositories and handing it to agents

Status: version 1 in progress. This document is the contract for the implementation; where the code and this text disagree, fix one of them in the same pull request.

## Problem

Flint can run work: an [agent task](../../site/guides/ai-agents.md) gives an agent a branch and a worktree in one repository, queues it, compares attempts, merges it or opens a pull request. Flint cannot hold work that is planned but not started, and it knows nothing above one repository. A piece of work that spans an API and its client is two unrelated agent tasks; work for next week lives in another tool.

A **backlog** is that missing layer: a folder of work items, for one or more repositories, that Flint shows as a list and a board. **Delegate** turns an item into agent tasks, one per repository it names, run by the agent it names (an [agent definition](agent-definitions.md) or any other agent), and the item's status follows them. Trackers such as Jira or GitHub Issues are not part of it; they can later be imported or synchronized by extensions.

## Words

"Project" already means a folder open in Flint, so the new thing is not called a project.

| Word | Meaning |
|---|---|
| Backlog | A folder with `flint-backlog.json` and `items/`. It may be its own git repository, a folder inside one, or a plain folder. |
| Item | One Markdown file in `items/`: a piece of work with a status. |
| Delegate | Start agent tasks for an item. |
| Agent task | Unchanged: a branch, a worktree and an agent's chat in one repository. |

## The folder

```
acme-plan/
  flint-backlog.json
  items/
    rate-limit-checkout.md
    cart-badge.md
```

`flint-backlog.json`:

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
| `name` | Shown in the panel; at most 80 characters; default: the folder's name. |
| `repositories` | At most 50. `name` is how items refer to a repository (lowercase letters, digits and dashes, at most 40, unique). `url` is a git remote URL; it identifies the repository on every teammate's machine, wherever their checkout is. |

Unknown fields are ignored. A backlog without `repositories` is valid: its items run in the window's active project.

An item, in the front matter format of the prompt library (`prompts.FrontMatter`):

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
| Id | The file name without `.md`: letters, digits, dashes and underscores (`prompts.ValidName`). |
| `title` | Required, at most 120 characters. |
| `status` | `todo` (default), `doing`, `review` or `done`. |
| `priority` | `low`, `normal` (default) or `high`. Orders items inside a status; then by title. |
| `agent` | Optional. Any agent name (defined, native or custom). Empty: the default agent. |
| `repos` | Optional, comma-separated names from `flint-backlog.json`. Empty: the window's active project. |
| Body | What to do and how to tell it is done. It becomes the agent task's description, so title and body together are at most 8,000 characters (`agenttask.MaxPrompt`). |

Unknown keys are kept when Flint rewrites a file and otherwise ignored. A file that breaks a rule is listed as a problem (file and reason) and not shown as an item.

Limits, fixed because a backlog may come from someone else's repository: 64 KiB per file, 500 items, one directory level. Files are resolved inside the backlog folder; a symbolic link that leaves it is refused.

## Which backlogs Flint knows

- **Open Backlog…** picks a folder with `flint-backlog.json`; **New Backlog…** creates one in a folder the user picks. Flint remembers the folders (a state key), most recent first, at most 20, and forgets one on **Close Backlog**.
- A workspace root that holds `flint-backlog.json` at its top is listed without being added.

## Status and delegation

Statuses are stored in the item's file, so a team that keeps the backlog in git shares them. Flint rewrites only the `status` line (adding front matter when the file has none) and keeps the rest of the file byte for byte.

| Change | When |
|---|---|
| any → any | The user picks a status (menu on the item, or dragging a card on the board). |
| `todo` → `doing` | **Delegate** started the item's agent tasks. |
| `doing` → `review` | Every agent task of the item finished. A failed task keeps the item in `doing`, marked failed. |
| `review` → `done` | Every agent task of the item is merged into its base, or its pull request was created and the task removed. |

**Delegate** on an item:

1. Resolves the agent: the item's `agent`, else the default agent. It must be available now, or Delegate fails and says which agent is missing.
2. Resolves each repository in `repos` to a checkout: a workspace root (or a folder in one) whose repository has a remote with the same URL (compared after normalizing scheme, user, `.git` and case of the host), else the folder the user chose for that URL with **Locate…** (remembered per URL in Flint's state, never in the backlog). A repository with no checkout stops the whole Delegate before anything is created; the UI offers **Locate…**. A located folder that is not in the workspace is attached to it first, by the UI, like a task's worktree. Flint does not clone in version 1.
3. Creates one agent task per repository through the same path as New Agent Task (branch, worktree, queue, limits, trust of the worktree). The description is the item's title and body; with several repositories it also names the item's other repositories and says which one this task works in. All or nothing: when one cannot be created, the ones already created for this Delegate are removed.
4. Remembers which agent tasks belong to the item, and sets `status: doing`.

The link between an item and its agent tasks is local state, not part of the backlog: agent tasks, worktrees and chats exist on one machine. A teammate sees the item's status, not the other machine's tasks.

Delegating an item again (after a failure, or for another attempt) adds agent tasks; the item shows all of them.

Nothing in a backlog runs by itself: opening a backlog reads files, and only the user's Delegate starts agents. Delegate is refused for a repository checkout in Restricted Mode with the reason; the user trusts the folder first.

## UI

- A **Backlog** sidebar panel: the backlog picker (with Open, New and Close), **New Item…**, a refresh button, and the items grouped by status in the order `doing`, `review`, `todo`, `done` (done collapsed). An item row shows title, priority, agent and repositories, and the state of its agent tasks. Actions: **Delegate**, **Open** (the file, in the editor), set status, and for each agent task **Open** (the task's folder and chat, as in the Agent Tasks panel).
- A **Board** tab (a contributed tab kind, restored with the session): one column per status with the items as cards and the same actions; a card can be dragged to another column.
- **New Item…** asks for a title, derives the id from it, writes the file from a template and opens it.
- Commands: Open Backlog…, New Backlog…, New Backlog Item…, Show Backlog Board.
- The panel re-reads when it is shown, on refresh, after Flint's own writes, when agent tasks change, and on file changes when the backlog is inside a workspace root. Flint does not watch folders outside the workspace in version 1.
- Problems (bad files, unknown repositories or agents in an item) are listed under the items, each with **Ask the agent**.

## Agents can see it

A read-only agent tool, `get_backlog`, returns the open backlogs with their items (id, title, status, priority, agent, repositories, body) so an agent can be asked what is planned or what to do next. It is off when the feature is.

## Code

- A feature module, switched on by default and optional: `internal/features/backlog` (the file format, loading, status rewriting and URL matching are pure and tested without Flint) and `frontend/src/features/backlog`.
- The module needs agent tasks and repositories, which a feature's `Host` does not offer. The contract is widened on purpose with the narrowest methods that do the job (create agent tasks for a description in a folder with an agent; the states of given agent tasks; the remotes of a folder's repository; the default and available agents), implemented by the app with the code New Agent Task already uses.
- State keys: the remembered backlog folders, the checkout chosen per repository URL, and the agent tasks of each item.
- Every service method takes a backlog folder that must be one Flint knows (above) and an item id that is validated; paths are never taken from the UI.

## Not in version 1

- Cloning a repository that has no checkout.
- Dependencies between items and automatic start when a dependency is done.
- One agent working in several repositories at once; a coordinating agent above the per-repository ones.
- Asking an agent to split a goal into items, milestones, budgets per item, assignees who are people.
- Watching a backlog folder outside the workspace; live updates from a teammate's push.
- Import from or synchronization with trackers.
