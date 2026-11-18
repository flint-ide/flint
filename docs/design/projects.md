# Projects: planning work above repositories and handing it to agents

Status: implemented, not released yet. This document is the contract for the implementation; where the code and this text disagree, fix one of them in the same pull request.

## Problem

Flint can run work: an [agent task](../../site/guides/ai-agents.md) gives an agent a branch and a worktree in one repository, queues it, compares attempts, merges it or opens a pull request. Without projects, Flint cannot hold work that is planned but not started, and it knows nothing above one repository. A piece of work that spans an API and its client is two unrelated agent tasks; work for next week lives in another tool.

A **project** is that missing layer: a folder of work items (its **backlog**), for one or more repositories, that Flint shows as a list and a board. The user can have several projects at once. **Delegate** turns an item into agent tasks, one per repository it names (or one agent for all of them), run by the agent it names (an [agent definition](agent-definitions.md) or any other agent), and the item's status follows them. An item can wait for other items, an agent can plan the items, and a repository the user has no checkout of can be cloned. Trackers such as Jira or GitHub Issues are not part of it; they can later be imported or synchronized by extensions.

## Words

"Project" has two meanings in Flint. In the rest of Flint (code such as `project.go` and `projectRoots()`, the UI's "Open Folder…" and "Recent Projects…", the docs' "a project open in the window") a project is a folder open in the window, and that meaning stays there. In this feature, its code (`internal/features/projects`, `frontend/src/features/projects`), its texts and its docs, "project" means only the folder of work items described here; the folders open in the window are called **workspace folders** (roots), **folders** or **checkouts**, and the one the user works in is the **active folder**. A project's folder may be open in the window, but it does not have to be.

| Word | Meaning |
|---|---|
| Project | A folder with `flint-project.json` and `items/`. It may be its own git repository, a folder inside one, or a plain folder. |
| Backlog | The items of a project. |
| Item | One Markdown file in `items/`: a piece of work with a status. |
| Delegate | Start agent tasks for an item. |
| Agent task | Unchanged: a branch, a worktree and an agent's chat in one repository. |
| Joint group | The agent tasks of an item with `together: true`: one per repository, one agent and one chat for all of them. |
| Waiting start | The user's request to start an item when the items it waits for are done. |

## The folder

```
acme-plan/
  flint-project.json
  PROJECT.md          optional: the project's context for every agent
  items/
    rate-limit-checkout.md
    cart-badge.md
```

`flint-project.json`:

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
| `name` | Shown in the panel; one line of at most 80 characters, without invisible characters; default: the folder's name. |
| `repositories` | At most 50. `name` is how items refer to a repository (lowercase letters, digits and dashes, at most 40, unique). `url` is a git remote URL of at most 1,000 characters; it identifies the repository on every teammate's machine, wherever their checkout is. |

Unknown fields are ignored. A project without `repositories` is valid: its items run in the window's active folder.

An item, in the front matter format of the prompt library (`prompts.FrontMatter`):

```markdown
---
title: Rate limit the checkout API
status: todo
priority: high
agent: backend
repos: api, web
after: add-customer-ids
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
| `repos` | Optional, comma-separated names from `flint-project.json`, each once. Empty: the window's active folder. |
| `after` | Optional, comma-separated ids of items of the same project that must be `done` first, at most 20, each once ([Dependencies](#dependencies)). A value that is not an id, more than 20 ids or an id listed twice makes the file a problem. |
| `together` | Optional: `true` or `false` (default) ([One agent across repositories](#one-agent-across-repositories)). Any other value, and `true` with fewer than two or more than 10 repositories in `repos`, makes the file a problem with the reason ("together: true needs two or more repositories in repos …"). |
| Body | What to do and how to tell it is done. It becomes the agent task's description, so title and body together are at most 8,000 bytes of UTF-8 (`agenttask.MaxPrompt`, counted in bytes as New Agent Task counts a description). With several repositories and without `together: true`, Delegate adds a sentence naming them, which counts too. |

Title and body contain no control character other than tab and newline, and no character that hides text from the person reading the item while the agent reads it: Unicode tags (U+E0000–U+E007F), bidirectional controls (U+202A–U+202E, U+2066–U+2069), zero-width space (U+200B), word joiner (U+2060) and U+FEFF (a file that starts with a byte order mark has no front matter, so no title). Zero-width joiner and non-joiner (U+200D, U+200C) are allowed: emoji and several scripts need them. The problem names the first such character ("the text contains invisible characters (U+200B)").

Unknown keys are kept when Flint rewrites a file and otherwise ignored. A file that breaks a rule is listed as a problem (file and reason) and not shown as an item; the problems that are about the item's agent or its `after` line keep it listed.

Limits, fixed because a project may come from someone else's repository: 64 KiB per file, 500 items, one directory level. Files are resolved inside the project folder; a symbolic link that leaves it is refused.

`PROJECT.md` (optional, `ContextFile`) is plain Markdown written by the user or the team: what the project is for, how its repositories fit together, conventions, the definition of done. It is resolved inside the project folder like every file (a symbolic link that leaves it is refused), at most 64 KiB (`MaxFileSize`), UTF-8, and at most `MaxContextChars` = 16,000 characters are used (after `\r\n` became `\n`, a leading byte order mark was dropped and the text was trimmed); it follows the character rule of items. A file that breaks a rule is a problem of the project (`PROJECT.md` and the reason, listed like an item's, "(agents don't get the file until it is fixed)") and is not sent; Delegate still works and its answer warns "the agents got no project context from PROJECT.md: <reason>". See [Project context](#project-context).

## Which projects Flint knows

- **Open Project…** picks a folder with `flint-project.json`; **New Project…** creates one (the file and an empty `items/`, never overwriting) in a folder the user picks. Flint remembers the folders (state key `projects`), most recent first, at most 20, and forgets one on **Close Project**.
- A workspace folder that holds `flint-project.json` at its top is listed without being added (and cannot be closed; it goes when the folder leaves the workspace).
- A project is known by its real path (symlinks resolved). Every service method that takes a project folder (everything but Open and Create) refuses a folder Flint does not know.
- Closing a project also forgets the links of its items to agent tasks (the tasks stay) and its waiting starts, and cancels its clone.
- The links of an item are kept while its file is missing (a branch switch in the project's repository, a rename in progress) and shown again when it is back; they go only with Close or when pruned (once more than 100 projects have links in `projectTasks`, Delegate drops those of projects Flint no longer knows). An item is known by its file name, so renaming the file loses its links (the tasks stay in the Agent Tasks panel).

## Repositories and checkouts

Delegate, and the panel's **Repositories** group, resolve each repository of `flint-project.json` to a checkout on this machine:

1. a trusted workspace folder whose repository (the one the folder is in) has a remote, any remote, with the same URL;
2. else the folder the user chose for that URL with **Locate…** (remembered per URL in state key `projectCheckouts`, at most 200, never in the project), when it is still a checkout of that URL and trusted;
3. else a workspace folder in Restricted Mode with that remote (which the UI then offers to trust);
4. else the located folder as it is.

Any folder can claim any URL, so one in Restricted Mode never hides a trusted checkout. A repository nested inside an open folder that is not itself its checkout (a folder of several repositories, a submodule) is not found automatically: **Locate…** it. An agent task's worktree is never used as a checkout. **Locate…** accepts only a folder whose repository has a remote with that URL; a located folder that is gone or no longer matches is shown as a problem of the repository. A located folder that is not in the workspace is attached to it by the UI before Delegate.

URLs are compared after normalizing (`RepoKey`): scheme, user, port, trailing `.git` and slashes are dropped and the host is lower-cased (`git@github.com:acme/api.git`, `ssh://git@github.com:22/acme/api` and `https://github.com/acme/api/` are one repository); the path keeps its case, and local paths (`/srv/git/api.git`, `file://…`) compare as cleaned paths.

### Cloning

A repository with no checkout gets **Clone…** next to **Locate…**: the user picks the folder to clone into, Flint shows the URL and the destination in a native confirmation (`Host.Confirm`; for a project whose folder is not trusted with a note to check the URL) and asks, then runs `git clone` and treats the result as located (`projectCheckouts`).

- `CloneSource` accepts only `https://host/path`, `ssh://[user@]host[:port]/path` and scp-like `[user@]host:path`. The URL is at most 1,000 characters of printable ASCII without spaces and does not start with `-`; it contains no `::`, backslash or square bracket; it has no password, query or fragment; the host is a plain name of at least 2 characters (letters, digits, `.` and `-`, starting and ending with a letter or digit; one letter would be a drive on Windows); the user is a plain name; the port is a number; the path uses letters, digits and `._~/@+-` only, has no `.` or `..` part and does not start with `-`. `file://`, local paths, `ext::` and every other transport are refused ("only https://, ssh:// and user@host:path URLs can be cloned …").
- The destination is `<picked folder>/<last part of the URL's path without .git>`. That name is at most 100 characters of letters, digits, `.`, `-` and `_`, does not start with `.` or `-`, does not end with `.` and is not a reserved Windows name; the destination must not exist or must be an empty folder (not a link).
- The URL comes from a project, so git gets it after `--`, never as an option, with `protocol.allow=never` and only the URL's transport allowed (`ext` and `fd` never), no submodule recursion, no hooks (`core.hooksPath=/dev/null`) and no fsmonitor. git asks for nothing on a terminal Flint does not have (`GIT_TERMINAL_PROMPT=0`, `SSH_ASKPASS_REQUIRE=never`): credentials come from the user's own helper or agent, and a failure shows git's message.
- One clone at a time per project; it can be cancelled (and is, by Close Project and when Flint quits) and stops after 30 minutes. It is made in a temporary folder next to the destination and renamed into place only when it worked, so a clone that fails or is cancelled removes what it created. Progress and the end reach the UI as events (`projects:clone-progress`, `projects:clone-done`; `projects:changed` when it starts and ends).
- A cloned folder is like any new folder: not trusted and not in the workspace. The UI adds it to the workspace and Delegate asks to trust it, as for a located folder.

## Status

Statuses are stored in the item's file, so a team that keeps the project in git shares them. Flint rewrites only the value of the `status` line (adding the line before the closing `---` when there is none, or front matter when the file has none, with the file's line endings) and keeps the rest of the file byte for byte. A file it cannot change safely (two `status` lines, no closing `---`, a line that is not `key: value`, a file that changed between reading and writing) is refused with the reason.

| Change | When |
|---|---|
| any → any | The user picks a status (menu on the item, or dragging a card on the board). |
| any → `doing` | **Delegate** started the item's agent tasks, or a waiting start did. |
| `doing` → `review` | Every agent task of the item finished. A failed task keeps the item in `doing`, marked failed. |
| `review` → `done` | Every agent task of the item is merged into its base, or its pull request was created and the task removed. |

Automatic changes only start from `doing` or `review`, and happen only when something about the item's agent tasks changed since Flint last looked (a state, a merge, a pull request, a removal): an item the user moved stays where they put it until its tasks change, even when it is in `doing` or `review` and its tasks are all finished. A change that can't be written (the file changed meanwhile, or can't be rewritten safely) is tried again the next time Flint looks. A task removed without being merged and without a pull request (a discarded attempt) no longer counts; with no task left, nothing moves. Both changes can happen in one step (`doing` → `done` when every task is already merged). Flint remembers in the link what the item needs after a task is removed: the last state, the pull request and whether it was merged. The changes are made when Flint hears that agent tasks changed (`feature.Instance.TasksChanged`, run with every `tasks:changed` and once at start; a watcher goroutine reconciles, then emits `projects:changed`) and whenever the panel reads the project (the UI re-reads on `tasks:changed` and `git:changed`, which also catches a merge made outside Flint). Nothing polls. A task merged outside Flint and removed before Flint looked counts as discarded.

## Delegate

**Delegate** on an item (`API.Delegate(folder, id, root)`, where `root` is the window's active folder from the UI):

1. Works out what the item waits for ([Dependencies](#dependencies)): an item whose `after` line has a problem is refused ("items/x.md can't start: after: …: fix the item's after line"), and an item that waits for items that are not done starts nothing; the answer says `notReady` and what it waits for (not an error).
2. Resolves the agent: the item's `agent`, else the default agent. It must be available now, or Delegate fails and says which agent is missing.
3. Resolves each repository in `repos` to a checkout ([above](#repositories-and-checkouts)) that is in the workspace and trusted, and says in one message everything that is missing (no checkout, an unusable located folder, not in the workspace, Restricted Mode). An item without `repos` runs in `root`, which must be an open, trusted workspace folder.
4. Checks the description against the 8,000-byte limit, asks first for an item of a project whose folder is not trusted ([Trust](#trust-and-restricted-mode)), and checks that the item has room for more agent tasks (at most 100 links; removed ones are dropped first).
5. Creates one agent task per repository, each with an origin naming the project, the item and its repository (`feature.NewAgentTask.Origin`, [Project context](#project-context)), through the same path as New Agent Task (branch from the checkout's current branch, worktree, queue, limits, trust of the worktree); an item with `together: true` gets one joint group instead ([One agent across repositories](#one-agent-across-repositories)). The description is the item's title and body; with several repositories (and without `together`) it also names the item's other repositories and says which one this task works in. All or nothing: when one cannot be created, the ones already created for this Delegate are removed. Until the call has succeeded or rolled back its tasks are held from the queue (`taskQueue.held`): a queued one is not started, and the slots of the others do not expire, so a rollback never removes a task whose chat runs. A checkout on a detached HEAD fails with `feature.DetachedHeadError`, which Delegate words as "<folder> is on a detached HEAD: check out a branch there, then Delegate again". Flint starts the chats of the tasks that got a slot itself, as it does for a queued task that leaves the queue (the UI adopts them from `chat:opened`); the others are queued.
6. Remembers which agent tasks belong to the item (`projectTasks`), drops a waiting start of the item, and sets `status: doing`. When the status can't be written, the tasks stay and the answer carries a warning.

Before it calls Go, the UI walks the user through what is missing, one step at a time: **Locate…** (a confirmation naming the repository and its URL, then the folder picker), or **Clone…** for a repository with no checkout at all, adding a located folder to the workspace, trusting a checkout (the confirmation says that trust is for the whole folder from now on, not for this Delegate, and names the folder). A step that makes no progress stops with the reason ("Delegate can't go on: …"), as does a problem it can't fix (the agent, no active folder). Go checks everything again.

One delegate of an item runs at a time, whoever asks (a click or a waiting start, `API.starting`); another one meanwhile fails with "the item is being started already: wait a moment". Delegating an item again (after a failure, or for another attempt) adds agent tasks; the item shows all of them.

The link between an item and its agent tasks is local state, not part of the project: agent tasks, worktrees and chats exist on one machine. A teammate sees the item's status, not the other machine's tasks.

## Dependencies

- An item may name items that must be done first: `after: id1, id2`. An unknown id (or one whose file is a problem), an item naming itself and a cycle are problems of the item: it is shown, listed under Problems, never ready, and Delegate refuses it with the reason. An item that only waits for an item on a cycle is not ready while that one is not done.
- An item is **ready** when its `after` has no problem and every item in it has status `done`. The panel and board show what an item waits for; `get_project` shows `after` and whether the item is ready.
- After Delegate answered `notReady`, the UI asks to **start the item when it is ready**. A yes (`StartWhenReady(folder, id, root)`) records a waiting start locally (state key `projectWaiting`, at most 100 per project, with `root` for an item without repositories, which must be an open workspace folder then). `StartWhenReady` refuses an item that is `done` or whose `after` has a problem. The item shows "starts when … is done"; **Cancel start** (`CancelStart`) removes it. An item that is ready by the time the user says yes starts at once the same way.
- When the last dependency becomes `done` (by Flint or by the user), Flint delegates the waiting item exactly as a click would, through the same function, with every check of Delegate at that time: the agent, the checkouts, trust, the untrusted-project confirmation, the limits. It does not walk the user through anything (no Locate…, Clone…, adding or trusting a folder): those are checks that fail. Flint looks when statuses can have changed: after the watcher's reconcile (`TasksChanged`), when the user sets a status, when the panel reads a project with waiting starts (which also catches a status edited in a file of the workspace), and when the workspace or its trust changes (`RootsChanged`); nothing polls. The untrusted-project confirmation of such a start names the item and says that it starts now because the items it waited for are done.
- If a check fails (or that confirmation is declined), nothing starts, the item keeps waiting with the reason shown, a `projects:auto-start` event tells the UI, and the user can Delegate by hand or cancel. Flint tries it again only once something those checks read has changed (a fingerprint of the item's and the project's files, the agents, the checkouts, trust and the active folder), never in a loop. An item whose own status is `done` by then is not started and its waiting start is dropped. A started item's waiting start is done with. Waiting starts are forgotten when the project is closed, and run only while Flint runs.
- Starting follows the agent task queue and its limits; nothing new is added there.

## One agent across repositories

- An item with several repositories may say `together: true`. Delegate then starts **one agent** that works in all of them, instead of one agent per repository. The item shows it (`ItemView.together`), and `get_project` writes "repositories: api, web (together: one agent works in all of them)".
- Each repository still gets its own branch and worktree, as its own agent task, so Merge, Create PR, Compare and removal work per repository as for any task. The tasks are a **joint group** (`agenttask.Task.Joint`, the same id on every task; `Lead` on the first repository's task): one chat, started in the lead's worktree, which is given the other worktrees as additional folders it may read and write (Claude Code's `--add-dir`). Every task of the group follows that one chat's state, so they finish together. A group has 2 to 10 repositories (`agenttask.MaxJoint`), one task per repository, and takes one slot of `agentTasks.maxParallel`; when no slot is free the whole group is queued and leaves the queue as a unit.
- Delegate makes all of its usual checks first, then makes the group in one call, `Host.CreateJointAgentTasks(reqs, agent)`, all or nothing like `CreateAgentTasks`. Every task's description is the item's title and body, without the per-repository sentence. The chat's first message is written by Flint (`jointPrompt`): the lead's description, then which folder is which repository on which branch (and a task's own description only where it differs from the lead's, which never happens for a project item), and that the change must be consistent across them and committed in each folder on its own branch. So the description is said once, and the 8,000-byte limit is checked against each task's description, which is exactly what Delegate sends; the list of folders Flint adds is not counted.
- The tasks are linked to the item as any others (`projectTasks`, one link per repository, the lead first), and the status rules are unchanged: all members share one state, so the item moves to `review` when the chat finished, and to `done` when every task was merged or removed after its pull request was created.
- An engine can run a joint group only if it can be given additional folders safely. Claude Code can (`--add-dir`, one pair per folder, after the user's own arguments). **Codex cannot**, checked against its source: the per-thread extra folders (`ThreadStartParams.runtime_workspace_roots` of the app-server protocol) are experimental, and Flint does not opt into the experimental API; giving the folders as `sandbox_workspace_write.writable_roots` in the configuration Flint passes is dropped when the user has a permission profile, so the folders would silently not be writable (or, worse, writable under rules Flint does not see). Custom ACP agents have no way to be given folders. So a joint group is refused for Codex, for a defined agent whose engine is Codex, and for every custom ACP agent, before anything is created: "codex runs on codex, which cannot work in several folders at once: delegate per repository instead", to which Delegate adds "(the item says together: true; without that line each repository gets its own agent)". The item is not moved to `doing`. The UI cannot tell this before it asks (the `Agents` of the `Host` do not say which engine an agent runs on), so the refusal comes from Go, after the untrusted-project confirmation when there is one.
- Trust and Restricted Mode apply to every worktree of the group as to any task's worktree; the additional folders are only the group's own worktrees, derived from the task store whenever the chat starts or resumes in the lead's worktree, never taken from the UI. Every repository and every worktree must be trusted, or the group is refused ("… is not trusted …", `feature.ErrJointUntrusted`); a chat that resumes later while one of them is untrusted or gone does not open. Flint's approval policy treats the group's worktrees like the chat's own folder: auto-approved reads and edits apply inside any of them, and nowhere else.
- A checkout on a detached HEAD fails as for `CreateAgentTasks` (`feature.DetachedHeadError` naming that folder), and the group is rolled back.
- Removing one task of a joint group while its chat runs stops the chat for all of them, after asking (the Agent Tasks panel's question says so); the other tasks stay, idle, and their worktrees stay. Removing the lead makes the next task the lead; a group left with one task is no group. For the item, a task removed without a pull request or a merge no longer counts, as for any task.
- A waiting start of an item with `together: true` starts the group the same way, through the same function, and the confirmation of an untrusted project then also says "It runs as one agent across the repositories api, web."

## Trust and Restricted Mode

Nothing in a project runs by itself: opening a project reads files. Agents start only through the user's Delegate, and through a waiting start that the user asked for by name with Start When Ready (which then makes every check of Delegate). Agents have no tool that delegates, sets a status or changes an item.

- Delegate is refused for a repository checkout in Restricted Mode with the reason; the user trusts the folder first. A joint group needs every repository and every worktree trusted.
- The project's own folder has a trust too (`Host.Trusted`), because its items become an agent's instructions and a project may come from someone else's repository:
  - `get_project` leaves out projects whose folder is not trusted, and says how many it left out and why; `add_project_item` and Plan with Agent… refuse such a project (agents could neither read it nor add to it).
  - Delegate of an item from such a project is not refused, but Go asks first in a native confirmation (`Host.Confirm`, which the web view can't answer or skip) that says the project's folder is not trusted and shows the agent, the folder(s) the tasks work in and the description that will be sent (its first 1,000 characters, saying when it is cut). When the project has a usable `PROJECT.md`, it also says "The project's context file PROJECT.md is sent too, before the description" and shows its first 1,000 characters (`maxConfirmContext`, saying when it is cut); a context file with a problem is named as not sent. For an item with `together: true` it says that one agent works in all of these folders together, in one chat, and that Flint adds to its first message which folder is which repository. No: nothing is created. A waiting start asks the same question, naming the item and why it starts now; declining leaves it waiting.
  - Nothing of such a project reaches an agent without that confirmation, also for a task that waits in the queue or a chat started later: the origin of its tasks carries the hash of the context text the confirmation showed (`origin.Confirmed`), and while the folder is not trusted the context of a starting chat holds `PROJECT.md` only when its text still has that hash (otherwise "(The project's PROJECT.md was left out: the project's folder is in Restricted Mode, and the file was not shown when the Delegate was confirmed or changed since.)"), the item's id, title, status and priority, and the names of the repositories: no other item, no URL and no `get_project` hint. A project trusted at Delegate and untrusted at the start has no confirmed hash, so its file is left out the same way.
  - **Add Project Context…** is refused for such a project.
  - `ProjectInfo` and `Project` carry `trusted`; the panel and the board mark an untrusted project, and the panel offers **Trust Folder…** for it.
- A cloned folder is untrusted until the user trusts it.

## UI

- A **Projects** sidebar panel (panel id `projects`, "Show Projects"). It shows one project at a time: the picker lists the projects Flint knows. Its header has **＋** (New Item…), a refresh button and a `⋯` menu with **Open Project…**, **New Project…**, **Plan with Agent…**, **Show Board** and **Close Project** (disabled, marked "workspace folder", for a project listed because it is a workspace folder). Under the picker: a note on a project whose folder is not trusted (with **Trust Folder…**); a **Repositories** group (each repository of `flint-project.json` with its URL and its checkout folder or "no checkout on this computer", markers *no checkout*, *problem*, *located*, *not in workspace* and *Restricted Mode*, and **Locate…**, plus **Clone…** for a repository with no checkout, the progress of a running clone with **Cancel**, **Add to Workspace** for a located folder outside the workspace and **Trust Folder…** for one in Restricted Mode; collapsed when every repository can be used); then the backlog: the items grouped by status in the order Doing, Review, To do, Done (Done collapsed). The group headers are buttons that take the keyboard focus and open or close with Enter or Space. An item row shows title, priority, agent and repositories, the *one agent* marker for `together: true`, what it waits for (or its dependency problem, or its waiting start with the reason the last one failed), and the state of its agent tasks (a joint group's tasks together, the lead marked). Actions: **Delegate**, **Cancel start** for a waiting start, **Open** (the file, in the editor), **Status ⌄** (To do, Doing, Review, Done), and for each agent task **Open** (the task's folder and chat, as in the Agent Tasks panel).
- A **Board** tab titled "Project: <name>" (a contributed tab kind, `projects.board`, restored with the session): columns To do, Doing, Review, Done with the items as cards and the same actions, **＋ New Item…**, **Plan with Agent…** and refresh in its header; a card can be dragged to another column.
- **New Item…** asks for a title, derives the id from it, writes the file from a template and opens it.
- The panel shows a **Context** line under the picker (`Project.context`, a `ContextView` of path, exists and problem): "PROJECT.md" with **Open** (the file, in the editor), "PROJECT.md (not sent)" when it has a problem, or "none" with **Add Project Context…** (`API.AddContext(folder)`: writes `PROJECT.md` from a template with the headings What this project is for, Repositories (listing the project's repository names), Conventions and Definition of done, never overwriting a file and never following a link (`O_EXCL`), refused for an unknown project or one whose folder is not trusted; the UI then opens it). The board's header has the same link.
- Commands, listed under "Projects" in the command palette: Open Project…, New Project…, New Project Item…, Plan with Agent…, Show Project Board. (The existing "Project" category holds the commands for folders open in the window, such as Recent Projects… and Open Folder….)
- The panel re-reads when it is shown, on refresh, after Flint's own writes (`projects:changed`), when agent tasks change, and on file changes when the project is inside a workspace folder. Flint does not watch project folders outside the workspace.
- Problems (bad files, a `PROJECT.md` that breaks a rule, unknown repositories or agents in an item, dependency problems) are listed under the items, each with **Ask the agent**.
- The selected project is remembered per browser profile (`localStorage` key `flint.projects.selected`), as a convenience.

## Agents

### Project context

Every agent of a project must know the project it works on, not only its item. Delegate's agents get the **project context** at the start of their chat's first message, composed in Go when the chat **starts**, not when the task is created, so a queued task, a group waiting for a slot or a waiting start gets the project as it is then. The task's stored description stays the item's text (title and body, plus the note about the other repositories), so the Agent Tasks panel and the task's title are unchanged, and the 8,000-byte limit stays on the item's own text; the context does not count against it.

The block, in this order:

1. `===== Project context (from Flint) =====` and one sentence: the project's name and folder, and that the task follows.
2. `## About the project (PROJECT.md)`: the text of `PROJECT.md`; with a file that has a problem, "(The project's PROJECT.md was left out: <reason>.)"; nothing without a file.
3. `## Your item`: id, title, status, priority and file.
4. `## What your item depends on` (when `after` is set): each item's id, title and status; for those that are `done`, the branches and pull requests of their agent tasks this machine knows (the links in `projectTasks` that were not abandoned, with the branch from `Host.AgentTasks` while the task exists), per repository, or "no branch or pull request of it is known on this computer".
5. `## In progress in this project`: the other items in `doing` or `review` (id, title, status, agent or "the default agent", repositories or "the active folder"), at most `maxInProgress` = 20, then "(N more items not listed: …)".
6. `## Repositories and where you work`: every repository with its URL, marking "(you work in it)" and "(another agent works on your item in it)", then where this agent works: "You work in api only, in <worktree> on the branch <branch> (from <base>)." (and that other agents work on the item's other repositories); for an item without repositories, the worktree of the active folder; for a joint group, "You work in the repositories api, web at once; after your task Flint lists which folder is which.", because `jointPrompt` already lists the folders after the description, which is not repeated.
7. `## Reading more`: the `get_project` tool, only when Flint's MCP server is offered to the chat (`feature.TaskStart.MCP`: the server is on and the worktree trusted).
8. `===== Your task =====`, then (after a blank line) the description, or for a joint group `jointPrompt`.

The whole block is at most `maxContextBlock` = 24 KiB. Longer, it leaves out, least important first, the items in progress (one by one), the dependencies' branches and pull requests, the repositories the item does not name, and then the end of `PROJECT.md`, and says each time what was left out ("Flint sends at most 24 KB of project context", and where to read the whole file).

How the core finds the project of a task at chat start: the link between an item and its tasks (`projectTasks`) is saved by the module only after `CreateAgentTasks` returns, while the chats start inside it and queued tasks start much later, so the link cannot be the key. Instead each task carries an **origin** (`agenttask.Task.Origin`, persisted in `state.json`, set only from `feature.NewAgentTask.Origin` by Go, never from the UI; older state files load without one): for this feature `projects:` and a JSON object of the project folder, the item id, the repository name and, for a project whose folder was not trusted at Delegate, the hash of the confirmed context text. When the core starts a task's chat (`startTaskChat`, which serves the queue, Start now, `CreateAgentTasks` and a joint group's lead), it calls `feature.Set.TaskContext` with the lead's origin and where the chat runs (`feature.TaskStart`); the module's hook (`API.taskContext`) validates the origin (its prefix, an absolute folder of a project Flint knows, a valid item id that the project has now, a repository the project declares; anything else gives no context and the chat starts with the description alone, never failing) and renders the block. Chats of tasks without an origin, such as those the window starts for New Agent Task, are not touched. A queued task with an origin is started by Go also on **Start now** (`TaskView.flintStarts`), like a joint group, so its first message is Flint's.

### Reading: get_project

A read-only agent tool, `get_project`, returns the known projects whose folder is trusted with their repositories and items: id, title, status, priority, agent, repositories (with "(together: one agent works in all of them)" for an item with `together: true`), `after` with whether the item is ready ("(ready)", "(not ready: waits for …)", "(never ready: <problem>)"), and the body, cut at 2,000 bytes. Optional arguments `status` and `project` (a name) narrow it. It knows who is asking: the calling chat (`mcpserver.CallerFrom`) is mapped to the origin of its agent task (`Host.TaskOrigin`: the task the tracker bound the chat to; for a joint group's chat the lead's). For the agent of an item, without `project` the answer is that project first and in full, starting with "You are working on the item <id>: <title> (marked below).", that item first and marked "(you are working on this item)" (also when `status` would leave it out), and the other trusted projects by name only ("Other open projects (call get_project with project: <name> to read one): …"); asking for one by name returns it. For any other chat, the projects with a repository whose URL is a remote of the chat's folder (the MCP root) come first. It shows every such project to any chat, not only a project whose folder the chat works in, so it is not on the pre-approved read list (`agentsession.MCPReadTools`): Claude Code asks before each call, like Flint's other tools outside that list. Everything it writes (headers, repository lists, items, notes) counts against its 60 KiB budget, so the closing note about what was left out is never cut by the MCP server's own limit. It is off when the feature is.

### Planning with an agent

The user states a goal; an agent splits it into items.

- **Plan with Agent…** (panel, board, command) asks for the goal and the agent (a defined agent such as a planner, or any other; default: the default agent) and puts a prompt into a chat's draft, as every "Ask the agent" does. The prompt gives the goal, the project's `PROJECT.md` (at most `maxPlanContext` = 8,000 characters, saying when it is cut, with a sentence to split the goal in the project's terms; a file with a problem is named as left out), the project's name, folder and repositories, the item format and its limits (`after` and `together` included), the agents available here, and tells the agent to read the project's backlog with `get_project` and add items with `add_project_item`. Go builds it (`PlanPrompt(folder, goal)`, so the format is described in one place, next to the parser): the goal is 1 to 2,000 characters with the item's character rules, and a project whose folder is not trusted is refused. The answer also says whether the tool is switched on, so the UI can say what to switch on. The user sends the prompt; nothing is sent for them.
- **`add_project_item`** writes one item: `{project, title, text, priority?, agent?, repos?, after?, together?}` (`project` is the folder or the name `get_project` shows, a name only among the projects whose folder is trusted; `repos` and `after` are arrays of names; `together` is a boolean, written as `together: true` only when true; `text` is required and may be empty; unknown arguments are refused). It is a tool that **acts** (`mcpserver.Tool.Acts`): Flint asks the user before each call, showing what will be written, and it is off until the user switches it on in Settings ▸ Agent Context ▸ Actions ▸ **Add project items** (`mcpServer.actions.projects`, like `actions.sql` for `run_sql_query`; the MCP server must be on). It goes through the same code as New Item… and the parser: the id comes from the title (`-2`, `-3` … when taken), the file is never overwritten, every limit and character rule applies, the status is always `todo`, and a call that would produce a problem file or a problem of the item (an agent not available here, an `after` that names no existing item or closes a cycle, `together` with fewer than two repositories) is refused with the reason so the agent can correct it; the write after the user's yes checks everything again. It refuses a project whose folder is not trusted. One chat adds at most 50 items; the project's 500-item limit holds as for New Item….
- Agents never get a tool that changes or deletes an item, sets a status or delegates. An agent whose chat runs in the project's folder can still edit its files like any file of the folder it works in; Flint's parser decides what counts as an item.

## Code

- A feature module, switched on by default and optional (`features.enabled.projects`): `internal/features/projects` (the file format, loading, status rewriting and URL matching are pure and tested without Flint) and `frontend/src/features/projects`.
- The module answers `feature.Instance.TaskContext` (the project context of a starting chat, above), and its tasks carry an origin (`feature.NewAgentTask.Origin` → `agenttask.Task.Origin`, at most `agenttask.MaxOrigin` = 8 KiB). `Host.TaskOrigin(chat)` gives get_project the origin of the calling chat's task.
- The module needs agent tasks and repositories, which a feature's `Host` otherwise does not offer. The contract is widened on purpose with the narrowest methods that do the job, implemented by the app with the code New Agent Task already uses: `CreateAgentTasks` (requests of folder, description and agent; all or nothing, its tasks held from the queue until it returns; `*DetachedHeadError` for a checkout on a detached HEAD), `CreateJointAgentTasks` (the same requests and one agent, made into one joint group), `AgentTasks` (the states of given task ids, merged and pull request included), `Repository` (a folder's checkout, its remote URLs and whether it is a task's worktree), `Agents` (the available agents and the default one) and `TaskOrigin` (the origin of a chat's task). `featuretest.Host` fakes them.
- State: each agent task of an item keeps its origin in the core's `agentTasks` (`origin`). State keys: `projects` (the remembered project folders, at most 20), `projectCheckouts` (the checkout chosen or cloned per repository URL, at most 200), `projectTasks` (the agent tasks of each item, at most 100 per item; delegating again drops the oldest removed ones first) and `projectWaiting` (the waiting starts, at most 100 per project, each with the active folder for an item without repositories and the reason and fingerprint of the last failed start).
- Events: `projects:changed` (a project folder, or "" when the list changed), `projects:auto-start`, `projects:clone-progress`, `projects:clone-done`.
- Every service method takes a project folder that must be one Flint knows (above) and an item id that is validated; paths are taken from the UI only for Open, Create, Locate and Clone (existing folders the user picked with the native folder picker, `WorkspaceAPI.PickFolder`) and as the active folder of Delegate and StartWhenReady (which must be an open workspace folder).

## Not yet

- A coordinating agent above per-repository agents.
- Milestones, budgets per item, assignees who are people.
- Watching a project folder outside the workspace; live updates from a teammate's push.
- Import from or synchronization with trackers.
