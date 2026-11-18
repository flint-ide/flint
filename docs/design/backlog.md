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
| `agent` | Optional. Any agent name (defined, native or custom). Empty: the default agent. An agent that is not available on this machine does not hide the item: it is listed as a problem and Delegate says so. |
| `repos` | Optional, comma-separated names from `flint-backlog.json`. Empty: the window's active project. |
| `after` | Optional (version 2, [Dependencies](#dependencies)). |
| `together` | Optional (version 2, [One agent across repositories](#one-agent-across-repositories)): `true` or `false` (default). Any other value, and `true` with fewer than two or more than 10 repositories in `repos`, makes the file a problem with the reason ("together: true needs two or more repositories in repos …"). |
| Body | What to do and how to tell it is done. It becomes the agent task's description, so title and body together are at most 8,000 bytes of UTF-8 (`agenttask.MaxPrompt`, counted in bytes as New Agent Task counts a description). With several repositories, Delegate adds a sentence naming them, which counts too (not with `together: true`: each task then gets title and body as they are). |

Title and body contain no control character other than tab and newline, and no character that hides text from the person reading the item while the agent reads it: Unicode tags (U+E0000–U+E007F), bidirectional controls (U+202A–U+202E, U+2066–U+2069), zero-width space (U+200B), word joiner (U+2060) and U+FEFF (a file that starts with a byte order mark has no front matter, so no title). Zero-width joiner and non-joiner (U+200D, U+200C) are allowed: emoji and several scripts need them. The problem names the first such character ("the text contains invisible characters (U+200B)").

Unknown keys are kept when Flint rewrites a file and otherwise ignored. A file that breaks a rule is listed as a problem (file and reason) and not shown as an item.

Limits, fixed because a backlog may come from someone else's repository: 64 KiB per file, 500 items, one directory level. Files are resolved inside the backlog folder; a symbolic link that leaves it is refused.

## Which backlogs Flint knows

- **Open Backlog…** picks a folder with `flint-backlog.json`; **New Backlog…** creates one in a folder the user picks. Flint remembers the folders (a state key), most recent first, at most 20, and forgets one on **Close Backlog**.
- A workspace root that holds `flint-backlog.json` at its top is listed without being added (and cannot be closed; it goes when the folder leaves the workspace).
- A backlog is known by its real path (symlinks resolved). Closing one also forgets the links of its items to agent tasks (the tasks stay).
- The links of an item are kept while its file is missing (a branch switch in the backlog's repository, a rename in progress) and shown again when it is back; they go only with Close or when pruned (once more than 100 backlogs have links in `backlogTasks`, Delegate drops those of backlogs Flint no longer knows). An item is known by its file name, so renaming the file loses its links (the tasks stay in the Agent Tasks panel).

## Status and delegation

Statuses are stored in the item's file, so a team that keeps the backlog in git shares them. Flint rewrites only the value of the `status` line (adding the line before the closing `---` when there is none, or front matter when the file has none, with the file's line endings) and keeps the rest of the file byte for byte. A file it cannot change safely (two `status` lines, no closing `---`, a line that is not `key: value`, a file that changed between reading and writing) is refused with the reason.

| Change | When |
|---|---|
| any → any | The user picks a status (menu on the item, or dragging a card on the board). |
| any → `doing` | **Delegate** started the item's agent tasks. |
| `doing` → `review` | Every agent task of the item finished. A failed task keeps the item in `doing`, marked failed. |
| `review` → `done` | Every agent task of the item is merged into its base, or its pull request was created and the task removed. |

Automatic changes only start from `doing` or `review`, and happen only when something about the item's agent tasks changed since Flint last looked (a state, a merge, a pull request, a removal): an item the user moved stays where they put it until its tasks change, even when it is in `doing` or `review` and its tasks are all finished. A change that can't be written (the file changed meanwhile, or can't be rewritten safely) is tried again the next time Flint looks. A task removed without being merged and without a pull request (a discarded attempt) no longer counts; with no task left, nothing moves. Both changes can happen in one step (`doing` → `done` when every task is already merged). Flint remembers in the link what the item needs after a task is removed: the last state, the pull request and whether it was merged. The changes are made when Flint hears that agent tasks changed (`feature.Instance.TasksChanged`, run with every `tasks:changed`; a watcher goroutine reconciles, then emits `backlog:changed`) and whenever the panel reads the backlog (the UI re-reads on `tasks:changed` and `git:changed`, which also catches a merge made outside Flint). Nothing polls. A task merged outside Flint and removed before Flint looked counts as discarded.

**Delegate** on an item:

1. Resolves the agent: the item's `agent`, else the default agent. It must be available now, or Delegate fails and says which agent is missing.
2. Resolves each repository in `repos` to a checkout: a trusted workspace root whose repository (the one the root is in) has a remote with the same URL, else the folder the user chose for that URL with **Locate…** (remembered per URL in Flint's state, never in the backlog) when it is trusted, else a workspace root in Restricted Mode with that remote (which the UI then offers to trust), else the located folder. Any folder can claim any URL, so one in Restricted Mode never hides a trusted checkout. A repository nested inside an open folder that is not itself its checkout (a folder of several repositories, a submodule) is not found automatically: **Locate…** it. URLs are compared after normalizing: scheme, user, port, trailing `.git` and slashes are dropped and the host is lower-cased (`git@github.com:acme/api.git`, `ssh://git@github.com:22/acme/api` and `https://github.com/acme/api/` are one repository); the path keeps its case, and local paths (`/srv/git/api.git`, `file://…`) compare as paths. An agent task's worktree is never used as a checkout. **Locate…** accepts only a folder whose repository has a remote with that URL. A repository with no checkout stops the whole Delegate before anything is created; the panel's **Repositories** group and Delegate's walk-through offer **Locate…**, which first says which repository (name and URL) to find. A located folder that is not in the workspace is attached to it first, by the UI, like a task's worktree. Flint does not clone in version 1. An item without `repos` runs in the window's active project, which the UI passes and which must be an open workspace folder.
3. Creates one agent task per repository through the same path as New Agent Task (branch, worktree, queue, limits, trust of the worktree), from the checkout's current branch (an item with `together: true` gets them as one joint group with one agent: [One agent across repositories](#one-agent-across-repositories)). The description is the item's title and body; with several repositories it also names the item's other repositories and says which one this task works in. All or nothing: when one cannot be created, the ones already created for this Delegate are removed. Until the call has succeeded or rolled back its tasks are held from the queue (`taskQueue.held`): a queued one is not started, and the slots of the others do not expire, so a rollback never removes a task whose chat runs. A checkout on a detached HEAD fails with `feature.DetachedHeadError`, which Delegate words as "<folder> is on a detached HEAD: check out a branch there, then Delegate again". Flint starts the chats of the tasks that got a slot itself, as it does for a queued task that leaves the queue (the UI adopts them from `chat:opened`); the others are queued.
4. Remembers which agent tasks belong to the item, and sets `status: doing`.

The link between an item and its agent tasks is local state, not part of the backlog: agent tasks, worktrees and chats exist on one machine. A teammate sees the item's status, not the other machine's tasks.

Delegating an item again (after a failure, or for another attempt) adds agent tasks; the item shows all of them.

Nothing in a backlog runs by itself: opening a backlog reads files, and only the user's Delegate starts agents. Delegate is refused for a repository checkout in Restricted Mode with the reason; the user trusts the folder first (the UI's confirmation says that trust is for the whole folder from now on, not for this Delegate, and names the folder).

The backlog's own folder has a trust too (`Host.Trusted`), because its items become an agent's instructions:

- `get_backlog` leaves out backlogs whose folder is not trusted, and says how many it left out and why.
- Delegate of an item from such a backlog is not refused, but Go asks first in a native confirmation (`Host.Confirm`, which the web view can't answer or skip) that says the backlog's folder is not trusted and shows the agent, the folder(s) the tasks work in and the description that will be sent (its first 1,000 characters, saying when it is cut). For an item with `together: true` it says that one agent works in all of these folders together, in one chat, and that Flint adds to its first message which folder is which repository. No: nothing is created.
- `BacklogInfo` and `Backlog` carry `trusted`; the panel and the board mark an untrusted backlog, and the panel offers **Trust Folder…** for it.

## UI

- A **Backlog** sidebar panel. Its header has **＋** (New Item…), a refresh button and a `⋯` menu with **Open Backlog…**, **New Backlog…**, **Show Board** and **Close Backlog** (disabled, marked "workspace folder", for a workspace backlog). Under it the backlog picker; a note on a backlog whose folder is not trusted (with **Trust Folder…**); a **Repositories** group (each repository of `flint-backlog.json` with its URL and its checkout folder or "no checkout on this computer", markers *no checkout*, *problem*, *located*, *not in workspace* and *Restricted Mode*, and **Locate…**, plus **Add to Workspace** for a located folder outside the workspace and **Trust Folder…** for one in Restricted Mode; collapsed when every repository can be used); then the items grouped by status in the order Doing, Review, To do, Done (Done collapsed). The group headers are buttons that take the keyboard focus and open or close with Enter or Space. An item row shows title, priority, agent and repositories, and the state of its agent tasks. Actions: **Delegate**, **Open** (the file, in the editor), **Status ⌄** (To do, Doing, Review, Done), and for each agent task **Open** (the task's folder and chat, as in the Agent Tasks panel).
- Delegate walks the user through what is missing before it asks Go, one step at a time: **Locate…** (a confirmation naming the repository and its URL, then the folder picker), adding a located folder to the workspace, trusting a checkout. A step that makes no progress stops with the reason ("Delegate can't go on: …"), as does a problem it can't fix (the agent, no active project).
- A **Board** tab titled "Backlog: <name>" (a contributed tab kind, restored with the session): columns To do, Doing, Review, Done with the items as cards and the same actions, **＋ New Item…** and refresh in its header; a card can be dragged to another column.
- **New Item…** asks for a title, derives the id from it, writes the file from a template and opens it.
- Commands, listed under "Backlog" in the command palette: Open Backlog…, New Backlog…, New Backlog Item…, Show Backlog Board.
- The panel re-reads when it is shown, on refresh, after Flint's own writes, when agent tasks change, and on file changes when the backlog is inside a workspace root. Flint does not watch folders outside the workspace in version 1.
- Problems (bad files, unknown repositories or agents in an item) are listed under the items, each with **Ask the agent**.

## Agents can see it

A read-only agent tool, `get_backlog`, returns the known backlogs whose folder is trusted with their items (id, title, status, priority, agent, repositories, body) so an agent can be asked what is planned or what to do next. It shows every such backlog to any chat, not only the chat's project, so it is not on the pre-approved read list (`agentsession.MCPReadTools`): Claude Code asks before each call, like Flint's other tools outside that list. Everything it writes (headers, repository lists, items, notes) counts against its 60 KiB budget, so the closing note about what was left out is never cut by the MCP server's own limit. It is off when the feature is.

## Code

- A feature module, switched on by default and optional: `internal/features/backlog` (the file format, loading, status rewriting and URL matching are pure and tested without Flint) and `frontend/src/features/backlog`.
- The module needs agent tasks and repositories, which a feature's `Host` does not offer. The contract is widened on purpose with the narrowest methods that do the job (create agent tasks for a description in a folder with an agent; the states of given agent tasks; the remotes of a folder's repository; the default and available agents), implemented by the app with the code New Agent Task already uses.
- State keys: `backlogs` (the remembered backlog folders, at most 20), `backlogCheckouts` (the checkout chosen per repository URL, at most 200) and `backlogTasks` (the agent tasks of each item, at most 100 per item; delegating again drops the oldest removed ones first).
- The `Host` methods: `CreateAgentTasks` (requests of folder, description and agent; all or nothing, its tasks held from the queue until it returns; `*DetachedHeadError` for a checkout on a detached HEAD), `CreateJointAgentTasks` (version 2: the same requests and one agent, made into one joint group; below), `AgentTasks` (the states of given task ids, merged and pull request included), `Repository` (a folder's checkout, its remote URLs and whether it is a task's worktree) and `Agents` (the available agents and the default one). `featuretest.Host` fakes them.
- Every service method takes a backlog folder that must be one Flint knows (above) and an item id that is validated; paths are taken from the UI only for Open, New Backlog and Locate (existing folders the user picked with the native folder picker, `WorkspaceAPI.PickFolder`) and as Delegate's active project (which must be an open workspace folder).

## Version 2: planning, dependencies, cloning, one agent across repositories

Status: in progress (planning, dependencies, cloning and one agent across repositories are implemented in Go; the UI of dependencies and of one agent across repositories is being built). The four parts below extend version 1; everything above still holds unless a part says otherwise.

### Planning with an agent

The user states a goal; an agent splits it into items.

- **Plan with Agent…** (panel, board, command) asks for the goal and the agent (a defined agent such as a planner, or any other; default: the default agent) and puts a prompt into a chat's draft, as every "Ask the agent" does. The prompt gives the goal, the backlog's name, folder and repositories, the item format and its limits, the agents available here, and tells the agent to read the backlog with `get_backlog` and add items with `add_backlog_item`. Go builds it (`PlanPrompt(folder, goal)`, so the format is described in one place, next to the parser): the goal is 1 to 2,000 characters with the item's character rules, and a backlog whose folder is not trusted is refused (agents could neither read it nor add to it). The answer also says whether the tool is switched on, so the UI can say what to switch on. The user sends the prompt; nothing is sent for them.
- A new agent tool, **`add_backlog_item`**, writes one item: `{backlog, title, text, priority?, agent?, repos?, after?, together?}` (`backlog` is the folder or the name `get_backlog` shows, a name only among the backlogs whose folder is trusted; `repos` and `after` are arrays of names; `together` is a boolean, written as `together: true` only when true; `text` is required and may be empty; unknown arguments are refused). The planning prompt describes `together` with its limits. It is a tool that **acts** (`mcpserver.Tool.Acts`): Flint asks the user before each call, showing what will be written, and it is off until the user switches it on in Settings ▸ MCP Server (`mcpServer.actions.backlog`, like `actions.sql` for `run_sql_query`; the MCP server must be on). It goes through the same code as New Item… and the parser: the id comes from the title (`-2`, `-3` … when taken), the file is never overwritten, every limit and character rule applies, the status is always `todo`, and a call that would produce a problem file or a problem of the item (an agent not available here, an `after` that names no existing item or closes a cycle) is refused with the reason so the agent can correct it; the write after the user's yes checks everything again. It refuses a backlog whose folder is not trusted. One chat adds at most 50 items; the backlog's 500-item limit holds as for New Item….
- Agents never get a tool that changes or deletes an item, sets a status or delegates. An agent whose chat runs in the backlog's folder can still edit its files like any file of a project it works in; Flint's parser decides what counts as an item.

### Dependencies

- An item may name items that must be done first: `after: id1, id2` (ids of the same backlog, at most 20). A value that is not an id, more than 20 and an id listed twice make the file a problem (it is not shown, like any rule the file breaks). An unknown id (or one whose file is a problem), an item naming itself and a cycle are problems of the item: it is shown, listed under Problems, never ready, and Delegate refuses it with the reason. An item that only waits for an item on a cycle is not ready while that one is not done.
- An item is **ready** when every item in `after` has status `done`. The panel and board show what an item waits for; `get_backlog` shows `after` and whether the item is ready.
- **Delegate** on an item that is not ready does not start it: Go answers that it is not ready and what it waits for (not an error), and the UI asks to **start it when it is ready**; a yes (`StartWhenReady`) records that locally (state key `backlogWaiting`, at most 100 per backlog, with the window's active project for an item without repositories) and the item shows "starts when … is done". **Cancel start** removes it. This is the only way anything starts without a click at that moment, and the click that allowed it names the item. An item that is ready by the time the user says yes starts at once the same way.
- When the last dependency becomes `done` (by Flint or by the user), Flint delegates the waiting item exactly as a click would, through the same function, with every check of Delegate at that time: the agent, the checkouts, trust, the untrusted-backlog confirmation, the limits. Flint looks when statuses can have changed: after the watcher's reconcile (`TasksChanged`), when the user sets a status, when the panel reads a backlog with waiting starts (which also catches a status edited in a file of the workspace), and when the workspace or its trust changes; nothing polls. One delegate of an item runs at a time, whoever asks, so two triggers never start it twice. The untrusted-backlog confirmation of such a start names the item and says that it starts now because the items it waited for are done. If a check fails (or that confirmation is declined), nothing starts, the item keeps waiting with the reason shown (and a `backlog:auto-start` event tells the UI), and the user can Delegate by hand or cancel; Flint tries it again only once something those checks read has changed (the item's and the backlog's files, the agents, the checkouts, trust, the project), never in a loop. An item whose own status is `done` by then is not started and its waiting start is dropped. A waiting start is forgotten when its backlog is closed.
- Starting follows the agent task queue and its limits; nothing new is added there.

### Cloning a repository

- A repository with no checkout gets **Clone…** next to **Locate…**: the user picks the folder to clone into, Flint shows the URL and the destination in a native confirmation and asks, then runs `git clone` and treats the result as located (remembered in `backlogCheckouts`, as Locate… does).
- Only `https://`, `ssh://` and scp-like (`user@host:path`) URLs can be cloned; `file://`, local paths, `ext::` and every other transport are refused, and so are a host or user that is not a plain name (nothing that starts with `-`), a password in the URL, a query or fragment, and `.` or `..` in the path. The URL comes from a backlog, so it is passed after `--`, never as an option, with `protocol.allow=never` and only the URL's transport allowed (`ext` and `fd` never), no submodule recursion and no hooks (`core.hooksPath=/dev/null`) or fsmonitor run by Flint. git asks for nothing on a terminal Flint does not have (`GIT_TERMINAL_PROMPT=0`, `SSH_ASKPASS_REQUIRE=never`): credentials come from the user's own helper or agent, and a failure shows git's message.
- The destination must not exist or must be an empty folder (not a link) inside the folder the user picked; the name is the last element of the URL's path without `.git`, validated as a file name (letters, digits, `.`, `-`, `_`, not starting with `.`, not a reserved Windows name).
- A cloned folder is like any new folder: not trusted, so Delegate still needs the user to trust it, and it is added to the workspace by the UI when the item is delegated.
- One clone at a time per backlog; it can be cancelled (and is, by Close Backlog and when Flint quits) and stops after 30 minutes; it is made in a temporary folder next to the destination and renamed into place only when it worked, so a clone that fails or is cancelled removes what it created. Progress and the end reach the UI as events (`backlog:clone-progress`, `backlog:clone-done`).

### One agent across repositories

- An item with several repositories may say `together: true`. Delegate then starts **one agent** that works in all of them, instead of one agent per repository. The item shows it (`ItemView.together`), and `get_backlog` writes "repositories: api, web (together: one agent works in all of them)".
- Each repository still gets its own branch and worktree, as its own agent task, so Merge, Create PR, Compare and removal work per repository as today. The tasks are a **joint group** (`agenttask.Task.Joint`, the same id on every task; `Lead` on the first repository's task): one chat, started in the lead's worktree, which is given the other worktrees as additional folders it may read and write (Claude Code's `--add-dir`). Every task of the group follows that one chat's state, so they finish together. A group has 2 to 10 repositories (`agenttask.MaxJoint`), one task per repository, and takes one slot of `agentTasks.maxParallel`; when no slot is free the whole group is queued and leaves the queue as a unit.
- Delegate makes all of its usual checks first (the agent available, every repository resolved, in the workspace and trusted, the untrusted-backlog confirmation, the item's task limit), then makes the group in one call, `Host.CreateJointAgentTasks(reqs, agent)`, all or nothing like `CreateAgentTasks`. Every task's description is the item's title and body, without the per-repository sentence. The chat's first message is written by Flint (`jointPrompt`): the lead's description, then which folder is which repository on which branch (and a task's own description only where it differs from the lead's, which never happens for a backlog item), and that the change must be consistent across them and committed in each folder on its own branch. So the description is said once, and the 8,000-byte limit is checked against each task's description, which is exactly what Delegate sends; the list of folders Flint adds is not counted.
- The tasks are linked to the item as any others (`backlogTasks`, one link per repository, the lead first), and the status rules are unchanged: all members share one state, so the item moves to `review` when the chat finished, and to `done` when every task was merged or removed after its pull request was created.
- An engine can run a joint group only if it can be given additional folders safely. Claude Code can (`--add-dir`, one pair per folder, after the user's own arguments). **Codex cannot**, checked against its source: the per-thread extra folders (`ThreadStartParams.runtime_workspace_roots` of the app-server protocol) are experimental, and Flint does not opt into the experimental API; giving the folders as `sandbox_workspace_write.writable_roots` in the configuration Flint passes is dropped when the user has a permission profile, so the folders would silently not be writable (or, worse, writable under rules Flint does not see). Custom ACP agents have no way to be given folders. So a joint group is refused for Codex, for a defined agent whose engine is Codex, and for every custom ACP agent, before anything is created: "codex runs on codex, which cannot work in several folders at once: delegate per repository instead", to which Delegate adds "(the item says together: true; without that line each repository gets its own agent)". The item is not moved to `doing`. The UI cannot tell this before it asks (the `Agents` of the `Host` do not say which engine an agent runs on), so the refusal comes from Go, after the untrusted-backlog confirmation when there is one.
- Trust and Restricted Mode apply to every worktree of the group as to any task's worktree; the additional folders are only the group's own worktrees, derived from the task store whenever the chat starts or resumes in the lead's worktree, never taken from the UI. Every repository and every worktree must be trusted, or the group is refused ("… is not trusted …", `feature.ErrJointUntrusted`); a chat that resumes later while one of them is untrusted or gone does not open. Flint's approval policy treats the group's worktrees like the chat's own folder: auto-approved reads and edits apply inside any of them, and nowhere else.
- A checkout on a detached HEAD fails as for `CreateAgentTasks` (`feature.DetachedHeadError` naming that folder), and the group is rolled back.
- Removing one task of a joint group while its chat runs stops the chat for all of them, after asking (the Agent Tasks panel's question says so); the other tasks stay, idle, and their worktrees stay. Removing the lead makes the next task the lead; a group left with one task is no group. For the item, a task removed without a pull request or a merge no longer counts, as before.
- A waiting start of an item with `together: true` starts the group the same way, through the same function, and the confirmation of an untrusted backlog then also says "It runs as one agent across the repositories api, web."
- `add_backlog_item` accepts `together` (a boolean) and refuses it with fewer than two repositories, with the parser's reason.

## Not yet

- A coordinating agent above per-repository agents.
- Milestones, budgets per item, assignees who are people.
- Watching a backlog folder outside the workspace; live updates from a teammate's push.
- Import from or synchronization with trackers.
