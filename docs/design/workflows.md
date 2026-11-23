# Workflows

A workflow is a repeatable way to deliver a change: a short list of steps that agents, scripts and you take in order, the same way every time. It answers "the agent skipped the plan, forgot the tests and reviewed its own code": the plan, the tests, an independent review and your approval are steps of the file, not hopes.

This document is the design. The reference is [FEATURES.md](../FEATURES.md#workflows), the guide is `site/guides/workflows.md`.

## What the user sees

- **A workflow is one Markdown file**, `.flint/workflows/<name>.md` in a repository (shared through git), in `workflows/` of the config directory (personal), or built in. Each `## Heading` is a step.
- **Run Workflow…** asks for the workflow, what to do (the task) and the agent. Flint makes one agent task (a branch and a worktree, like New Agent Task) and takes the steps in order there.
- **The run view** is one row of cards, one per step, with its state. It is the only place a run is steered from: **Approve**, **Send back…**, **Check again**, **Retry**, **Continue anyway**, **Pause**, **Stop**.
- **The Workflows panel** lists the runs, the ones that wait for you first, and the workflows. Its badge counts the runs that need you.

Nothing about a run is hidden: every agent step is an ordinary chat tab in the task's worktree, every script step is an ordinary run tab, and the result is an ordinary agent task with Show diff, Merge and Create PR.

## The file

```markdown
---
description: Plan, implement, test, review, then wait for approval
---

## Plan
agent: planner

Write the plan to PLAN.md. Do not change code.

## Implement

Implement PLAN.md. Commit your work.

## Test
type: script
run: test
on-fail: Implement

## Review
type: review
on-fail: Implement

## Approve
type: gate

Check the diff and the test run.
```

A step starts at a `## Title` line. The lines right after it, up to the first empty line, are its keys; the rest is its text. Front matter has one key, `description`.

| Key | Meaning |
|---|---|
| `type` | `agent` (default), `script`, `review`, `gate` or `workflow` |
| `agent` | who takes an `agent` or `review` step; default: the agent chosen for the run |
| `run` | a `script` step's run configuration, by name |
| `on-fail` | an earlier `agent` step the work goes back to when a `script` or `review` step fails |
| `workflow` | the workflow a `workflow` step runs in its place |

| Type | What happens | Passes when |
|---|---|---|
| `agent` | A new chat in the task's worktree gets the step's text. | The agent ends with `RESULT: DONE`. |
| `script` | The run configuration runs in the worktree, in a run tab. | It exits with 0. |
| `review` | A fresh chat reviews the change (set `agent:` to use a different agent). | The reviewer ends with `RESULT: PASS`. |
| `gate` | The run waits for you. | You click Approve. |
| `workflow` | The named workflow's steps take this step's place. | They pass. |

Limits: 30 steps after sub-workflows are put in (at most 3 levels, no cycles), 64 KiB per file, 16,000 characters per step, 50 workflows per folder. The first step is an `agent` step. A workflow's file name (its name) is lowercase letters, digits and dashes, 64 characters at most; another file name is listed as a problem ("name.md: the file name must be lowercase letters, digits and dashes"). Step ids are the titles as slugs and must differ. The character rules are those of project items (no control or invisible characters). A file that breaks a rule is listed as a problem with the reason and is not offered.

## How a run moves

A run is a small state machine (`run.go`, pure and tested without Flint):

- `running`: the current step is being taken.
- `waiting`: a gate waits for you.
- `failed`: a step failed and nothing sends the work back by itself; it waits for you.
- `paused`: you paused it; the step that was running ended, the next one has not started.
- `done`, `stopped`: over.

A step that fails with `on-fail` sends the work back to that step with the reason (the end of the script's output, or the reviewer's findings), and the steps from there are taken again. This happens at most `workflows.maxAttempts` times per step (default 3); after that the run is `failed` and waits for you. That bound, the usage budget (`usage.pauseOverBudget`) and Stop are what keep a loop from running away.

An agent step ends with a result line because a turn that ends is not a step that is done: an agent may end its turn with a question. `RESULT: BLOCKED`, or no result line, makes the run wait for you with the agent's last lines; you answer in that chat and then choose **Check again**: it reads the end of that chat once more (`Run.Rechecked`, with the same decision as the first evaluation, `Decide` in `outcome.go`), so DONE or PASS passes the step (it is not marked continued), a reviewer's FAIL sends the work back as usual, and BLOCKED or still no result line leaves the run failed. **Retry** starts a new chat that never saw your answer; **Continue anyway** passes the step unchecked.

What you can do, and when:

| Action | When | Effect |
|---|---|---|
| Approve | `waiting` | The gate passes. |
| Send back… (with a note) | `waiting`, `failed`, `paused` | The steps from the chosen one are taken again; the note goes to its agent. |
| Check again | `failed` at an agent or review step with a chat | The end of the step's chat is read again and judged as the first time. |
| Retry | `failed` | The failed step is taken again. |
| Continue anyway | `failed` | The failed step counts as passed (marked continued); a pause asked for earlier ends. |
| Pause / Resume | `running` / `paused` | The run stops before the next step / goes on. |
| Stop | any but `done` | The running chat's turn or script is stopped; the run is over. The agent task stays. |
| Resume (after a limit) | `paused` by a limit | The step a limit stopped is taken again; that start was not counted as an attempt. Send back and Stop work as for any paused run. |

Taking over needs no feature: the step's chat is a chat. Pause the run, work with the agent, Resume.

## Limits

A step can be stopped by a limit rather than by its work: Flint's own usage budget (`usage.pauseOverBudget`: "paused: today's budget … is spent"), or the agent's provider refusing for its quota (a subscription's usage limit, a rate limit, no credits left, "Quota exceeded"). A limit is not a failure, so it neither counts against `maxAttempts` nor sends the work back:

- **A hold.** The run is `paused` with a `hold` (`kind`: `budget` or `limit`, the reason, how many times the step was held). The step goes back to pending and its start is not counted. **Resume** takes it again. `limits.go` classifies the text (`LimitKind`): the error of a step's chat start, the error and the last 5 lines of a failed turn, a first step's task error, and and, for a finished turn without a result line, the last 5 lines read by the stricter `LimitInOutput`: only the providers' own announcements at the start of a line ("You've hit your limit", "usage limit reached", "insufficient_quota", "429 Too Many Requests", "credit balance is too low"), so an agent that worked on a rate limiter and ends with "Added the rate limit middleware" is not held. `LimitKind` is for error texts only. Only whole phrases count, so "limited", "a line limit" or "line 429" do not.
- **`workflows.fallbackAgent`.** When the step's agent reached its own limit (not Flint's budget), this agent takes the step again at once if it can start and is not the one that hit the limit. The step's record keeps the agent that took it (shown in the run view) until it passes or you send the work back. If the fallback hits a limit too, the run is held.
- **`workflows.resumeAfterMinutes`.** When above 0 (at most 720), a held run resumes by itself after that many minutes: one timer per held run, armed again from the stored time after a restart, cancelled by Resume, Stop, Remove and quitting. At most 12 automatic resumes per step; after that it waits for you. A resume into a worktree that is no longer trusted fails like any refused start.

Not done: Flint does not read when the provider's limit resets (its wording differs per agent and changes), so the wait is the one you set.

## Project items

A [project](projects.md) item can name a workflow: `workflow: bug-fix` in its front matter (not with `together: true`: "a workflow runs in one repository: remove together or workflow"). **Delegate** then makes the item's agent tasks as usual, one per repository, and a run of the workflow takes each of them:

- The route goes through `feature`, not an import (feature packages never import each other). `feature.NewAgentTask.Workflow` names the workflow; `CreateAgentTasks` (`features_tasks.go`) asks the module that takes workflows (`Instance.TaskWorkflow`, found by `feature.Set.TaskWorkflow`; none: "workflows are switched off (Settings ▸ Extensions ▸ Built-in)") to plan the run of **every** such request **before any task is made**. Planning is the same function as Run Workflow… (`checkRun`/`planRun`: the folder is in the workspace and trusted, the workflow is offered there and resolves, its agents are available) and stores the run, invisible until it has a task. The task's agent is the first step's (the item's agent is the run's).
- All or nothing: a refused request, or a task that cannot be made, drops every planned run (`PlannedWorkflow.Drop`). Once every task exists, still held from the queue, each run is bound to its task (`PlannedWorkflow.Bind`), so when Flint starts the task's chat, `TaskContext` finds the run by the task (`TaskStart.TaskID`; the run's own tasks are still found by their `workflow:<id>` origin) and answers with the first step's prompt, only for step 1's first start. `CreateJointAgentTasks` refuses a request with a workflow.
- The first message is the project's block (Projects is listed before Workflows, and `Set.TaskContext` joins in list order), then the step's block, then the item's text, which closes the step block's "===== The task =====" section: `===== Project context … ===== Your task =====`, `===== Workflow step … ===== The task =====`, the text. Every part is fenced as before.
- The item follows the **run**, not the task's turns (a task finishes a turn after every step): `feature.AgentTask.Run` (`ID`, `Workflow`, `State`, from `Instance.TaskRun`) is working while the run is `running` or `paused`, needs review when it is `done` or waits at an approval step that is the run's last (`TaskRun.AtLastStep`; an approval with steps after it leaves the item in doing),  failed when it `failed` or was `stopped`. Every change of a run's state calls `Host.TasksChanged`, so Projects reconciles at once. The panel gets `ItemView.workflow` and `TaskView.workflow`, `run`, `runState`.
- An item an agent delegated (`delegate_project_item`, [The coordinator](#the-coordinator)) carries `feature.NewAgentTask.DelegatedBy`: its run has `Origin: "agent"`, its log starts "Run started for a project item, delegated by an agent in <folder> (chat <id>), approved by you", and its script steps take `start_workflow`'s consent rule (refused, never asked, until the user ran such a workflow in that checkout). Before the user is asked, the agent's call is checked by `Instance.TaskWorkflowCheck` (`taskWorkflowCheck`: `checkRun` and that consent, planning nothing).
- Trust: the item may come from an untrusted project, but the workflow is only ever read from the built-ins, the user's personal folder or the trusted checkout the task branches from, never from the project's folder; Delegate's confirmation for an untrusted project says "The item runs the workflow …".

## Schedules

A run can start by itself at a time the user wrote into their **own** settings (`workflows.schedules`), never from a repository file:

```json
"schedules": [
  { "name": "nightly-deps", "workflow": "feature", "folder": "/abs/path/to/repo",
    "task": "Update the dependencies and fix what breaks.", "agent": "", "at": "02:30", "days": ["mon","tue","wed","thu","fri"] }
]
```

- **The fields.** At most 20 schedules. `name` is unique and has the workflow-name pattern (`^[a-z0-9][a-z0-9-]{0,63}$`: lowercase letters, digits and dashes, `textsafe.ValidSlugName`, the rule of workflow file names and of a project item's `workflow` key); `workflow` is a workflow's name; `folder` an absolute, clean path; `task` 1 to 8,000 bytes with the hidden-character rule; `agent` empty (the default agent) or an agent's name; `at` is `HH:MM`, 24 hours, local time; `days` a subset of `mon` … `sun`, empty for every day. Saving refuses a bad entry with a message that names the schedule and the field; loading drops it.
- **When.** `NextDue` (`schedule.go`, pure and table-tested) is the first time after an instant at which a schedule is due in a time zone. A time the clock skips (the spring change) is skipped to the next valid day; a time that happens twice (the autumn change) is due once, at the first.
- **The timer** (`schedule_timer.go`). One timer, armed for the earliest next due time of all schedules: at Start, after every change of the settings, and after it fired. A timer counts the time the computer is awake, so each arm is at most 3 minutes (`maxArm`): one that fires before its time arms again, and the local time zone is read again at each arm (`$TZ`, else `/etc/localtime`). No ticker. Close stops it, and nothing is armed or started after that.
- **The fingerprint** (`schedule_trust.go`). The first time the scheduler sees a schedule after it was saved or changed (Start, a change of the settings, a folder opened), it keeps in `state.json` (`workflowSchedules`, not in the settings) a hash of the schedule's entry and a fingerprint of what it runs: the resolved steps (ids, kinds, agents, run configurations, on-fail, text) and, for each script step, the launch of its run configuration as the checkout resolves it (programs, arguments, working folder, environment; `Host.RunLaunch`). When it is due and the fingerprint differs, it starts nothing: "the workflow or a run configuration it uses changed since this schedule was saved: open Settings ▸ Workflows and save the schedule again to accept it", with `ScheduleView.changed`. `API.AcceptSchedule(name)` takes the current definition; saving the schedule again does too.
- **Starting.** A due schedule starts a run through `startRun`, the path of Run Workflow… and `start_workflow`, with `Origin: "schedule:<name>"`; the run's log says "Started by the schedule <name>" and `RunView.origin` shows it. Nothing on that path asks the user; a schedule never shows a dialog (script steps without a sandbox in a checkout the user has not allowed skip it: [Sandbox](#sandbox)).
- **Skipping.** The schedule starts nothing, and keeps one line for the UI, when its folder is not an open workspace folder ("… is not open in Flint") or is not trusted ("… is in Restricted Mode"), when its previous run is not over ("the last run is still going": no overlap), when the usage budget pauses new turns (`usage.pauseOverBudget`, the budget's own message), or when the start refuses (the workflow is missing or cannot run, an agent is not available, the task cannot be made).
- **Missed times are not caught up.** When Flint was not running at the time, nothing starts later. When the computer slept through it, the timer fires late (timers do not count sleep); more than 5 minutes late, the schedule starts nothing and says "missed at …".
- **The UI.** `API.Schedules()` lists each schedule with `next` (unix seconds; 0 when it cannot start a run now: its folder is not open or not trusted), `lastRun`, `lastSkipped` and `lastAt`: what happened the last time it was due, kept in memory only (not in `state.json`). Every new outcome emits `workflows:changed`.

## Sandbox

A script step runs a run configuration of the user's on code an agent wrote. With `workflows.sandbox: "on"`, the default (a missing or unknown value is on; only an explicit `"off"` turns it off), it runs confined, in the operating system's sandbox that extensions already run in (`internal/extensions/sandbox`; no Docker, no new dependency, works offline):

- **What a sandboxed script may do.** Read what the user can read, except the places `sandbox.ProtectedPaths` lists for extensions (SSH and GPG keys, cloud and registry credentials, `.netrc`, password stores, shell and tool history, keychains and browser profiles, Flint's own folder) and the background session host's run directory. Write only inside the task's worktree, the system's temporary folders (`$TMPDIR`, `/tmp`, and `/dev/shm` on Linux) and the folders in `workflows.sandboxWritable`, plus its terminal. Run any program it can read. Use the network (Internet and local sockets alike) only when `workflows.sandboxNetwork` is true.
- **How.** `sandbox.BuildScript` is the second kind of policy next to an extension's `Build`, pure and tested on every system; launching is the extensions' (`sandbox.Command`, Flint itself as the launcher). On macOS `sandbox-exec` with a generated profile: everything stays readable except the protected places, writes are closed except the folders above. On Linux the Landlock and seccomp launcher: Landlock can only allow, so the policy lists what is readable around the protected places (every entry of `/`, and inside a folder that holds a protected place, its other entries).
- **Where it plugs in.** The engine passes `feature.RunOptions{Sandbox}` to `Host.RunConfiguration`; `featureHost.RunConfiguration` resolves the launch as before (the configuration still comes from the checkout) and wraps each command of it (`features_steps_sandbox.go`). The run tab's first line says the step is sandboxed, by what, where it may write and whether it has the network, and lists what was asked for but not granted (a writable folder that does not exist, or is, holds or sits in a protected place).
- **Without it, the user's OK once per checkout.** Only after the user switched `workflows.sandbox` off, a script step runs the user's run configuration on code an agent wrote, with the user's access to the computer. So that is the user's decision, taken once per checkout like trusting a folder (`unsandboxed.go`). When a run is about to start (`planRun` after `checkRun`, the one path of Run Workflow…, `flint workflow run`, a project item's Delegate through `TaskWorkflow`, `start_workflow` and schedules) and the resolved workflow has a script step and the setting is off and the checkout (the repository root, `Run.Dir`) is not in `state.json`'s `workflowUnsandboxed` list (clean absolute paths, at most 200, the oldest dropped):
  - Run Workflow…, `flint workflow run` and Delegate ask once in the native dialog the web view cannot answer (`Host.Choose`), outside the engine's lock and one question at a time (a start that waited reads the answer): "Run scripts without a sandbox?", the folder, each script step with its resolved command line (redacted, 300 characters at most) and "These commands run on code an agent wrote, with your access to this computer. A sandbox limits them to the run's folder (Settings ▸ Workflows)." **Run without a sandbox** adds the checkout to the list; **Cancel**, the default, starts nothing: "not started: script steps need your OK to run without a sandbox, or switch the sandbox on in Settings ▸ Workflows". A Delegate that waited for its dependencies asks too (its untrusted-project question does the same).
  - `start_workflow` shows no second dialog: it is refused, before the user is asked to approve the call and again when it runs, telling the agent to ask the user to run the workflow once themselves. An agent is never the first to run scripts without a sandbox in a checkout.
  - A schedule never asks: it is skipped with "script steps here still need your OK to run without a sandbox: run this workflow once yourself, or switch the sandbox on" (`ScheduleView.lastSkipped`).
  - `Listing.unsandboxedOK` says the folder's checkout is in the list; `API.ForgetUnsandboxed(dir)` takes it out (the next start asks again). With the sandbox on and available, nothing is asked and the list is not read; on and unavailable, the step fails closed as below.
- **Fails closed.** When the sandbox is on and this system has none (another system, a kernel without Landlock, `sandbox-exec` missing or refusing), the step fails before anything starts: "the sandbox is not available here (<reason>): switch off “Run script steps in a sandbox” in Settings ▸ Workflows to run it without one (Flint asks you once per folder)" (`SANDBOX_TEXT.unavailable` in the frontend says the same). It never runs unconfined.
- **The first failure a new user meets.** With the sandbox on by default, a build that writes a cache outside the worktree (Go's `~/Library/Caches/go-build` or `~/.cache/go-build`, npm's `~/.npm`) or needs the network fails with a permission or network error. No folder is writable by default: the user adds it explicitly (`sandboxWritable`) and allows the network (`sandboxNetwork`); the docs name the common paths. To make that findable, a script step that ran sandboxed (the engine knows from the `RunOptions` it passed, not from the settings at failure time) and exited non-zero gets one hint line, `sandboxHint` in `exec.go`: "This step ran in a sandbox: it can write only in the run's folder and temp folders, and has no network. If the command needs a cache folder or the network, add the folder or allow the network in Settings ▸ Workflows." It follows the reason in the run's `error`; when the step's `on-fail` sends the work back by itself it goes to the log only, so the agent's feedback stays the reason and the script's output. Not for an unsandboxed step, a passed one, a stopped one or one that did not start.
- **What it is not.** It confines the script step's processes only: agent steps (the agent CLIs and what they run) are not sandboxed by this setting. A build that writes a global cache (Go's build cache, npm's) needs that folder in `sandboxWritable`. On macOS, `sandbox-exec` is deprecated by Apple but present and used by the system's own tools, the same caveat as for extensions; system services (Mach, XPC) stay reachable there, so a script can still ask another program to act (open a URL, say). On Linux, `/proc` stays readable (the user's other processes' command lines), the folders that hold a protected place (the home folder, `~/.config`) cannot be listed though what is in them can be read, files and folders created there after the step started cannot be read by it, and an older kernel closes less (the run tab lists the gaps: without seccomp, or before Linux 6.7, the network stays open).

## The coordinator

An agent can start and steer runs, never approve them, and (with the Projects feature) delegate a project's ready items. Five tools on Flint's MCP server (`agenttools.go`, `agentsteer.go`), offered only while the server and **Settings ▸ Agent Context ▸ Actions ▸ Run and steer workflows** (`mcpServer.actions.workflows`, off by default) are on and the chat's folder is trusted; unknown arguments are refused:

| Tool | Acts | What |
|---|---|---|
| `list_workflows {}` | no | The workflows the folder offers: name, source, description, steps with kinds. |
| `get_workflow_runs {run?}` | no | The runs the caller reaches, newest first: id, workflow, title, state, the current step, the hold, the error, what the agent may ask for, the last 5 log lines; text redacted (`internal/redact`) and bounded (60 KB). |
| `wait_for_workflow_run {run, timeoutSeconds?}` | no | Blocks without polling (`engine.waitRun`, woken by every change) until the run needs the user, is paused or is over; capped by `orchestration.maxWaitSeconds` and the MCP server's cap on a call, like `wait_for_agent`. |
| `start_workflow {workflow, task, agent?}` | yes | Starts a run in the caller's folder through `startRun`, the path of Run Workflow…. Its `Plan` shows the workflow, its steps, the agents, the folder and the first 600 characters of the task. The run has `Origin: "agent"` and its log says which chat in which folder started it, approved by the user. One chat starts at most 10 runs (by `mcpserver.CallerFrom`). |
| `steer_workflow_run {run, action, step?, note?}` | yes | `action` is `pause`, `resume`, `retry`, `send_back` (with `step` and an optional `note`) or `stop`, and nothing else. The log says it was asked by an agent and approved. |

- **No approval, no "Continue anyway".** Approving a gate and continuing past a failed step are the user's decisions and the point of a gate: `steer_workflow_run` refuses them ("only the user can approve …: tell the user"), and nothing in the tools' files names `Approve` or `Skip` (`TestNoToolReachesApproveOrSkip` checks the source of the workflow tools, `taskrun.go`, Projects' agent tools with `delegate.go`, and `feature.go`, and every tool with every such action; `TestNoToolReachesApproveOrSkipAcrossProjects` does the same through the project scope).
- **Every acting call needs the user's yes to that one call**, as `add_project_item` does (`mcpserver.Tool.Acts`: no "always allow", no approval policy answers it, a native dialog when the agent did not ask in the chat). What runs after the yes checks everything again.
- **Scope.** A chat reaches the runs whose checkout (`Run.Dir`) is its folder (its token's workspace folder) or inside it, or the repository its folder is in. **The project scope:** a chat in a workspace folder (not an agent task's worktree) also reaches the runs whose agent task is linked to an item of a project its folder is in (the project's folder is that folder or inside it, or the folder's checkout is one of the project's repositories) and whose folder is trusted, wherever their repository is: `Host.FolderProjects`, answered by Projects (`Instance.FolderProjects`), gives the items' task ids, and `toolCaller.reaches` takes them. Others are not listed, waited for or steered ("no workflow run … in this folder or of its projects' items"). `start_workflow` starts only from a workspace folder, never from an agent task's worktree.
- **No recursion.** A chat that is a step of a run is refused every workflow tool ("a workflow step cannot start or steer workflows"): its task's origin is a run's, it is the chat of a run's step, or it works in the worktree of a run's task (a project item's run has the project's origin; the worktree tells). So a run cannot un-pause or stop itself and a step cannot start workflows.
- **Delegating items.** Above several repositories the coordinator hands out a project's ready items with the Projects feature's `delegate_project_item {project?, item}` (acts; Settings ▸ Agent Context ▸ Actions ▸ **Delegate project items**, `mcpServer.actions.delegate`, off by default): the same function as the Delegate click, every check included, with the user's yes to each call. An agent can delegate a ready item only when "Delegate project items" is on, only with the user's yes to that one call, never an item that waits for others, never from a project whose folder is untrusted, and never from inside handed-out work (an agent task's chat or worktree, or a workflow step). At most 10 per chat ([projects.md](projects.md#delegating-delegate_project_item)). `get_project` shows each item's runs ("bug-fix · waiting for the user's approval").
- **Coordinate with Agent…** (`API.CoordinatePrompt(dir, goal)`) returns the text for a chat's draft (the user sends it), `toolOn` ("Run and steer workflows") and `delegateOn` ("Delegate project items"). When the folder is in a Flint project (`Host.FolderProjects`): read the project with `get_project`, delegate READY items with `delegate_project_item` one at a time (each needs the user's yes; never an item that waits), start plain runs with `start_workflow` only for work that is not an item, wait with `wait_for_workflow_run`, tell the user plainly when a run waits for approval and never try to approve or continue it, stop when the user says so. Otherwise: list the workflows, start runs for the goal, wait, the same rules. The goal is 1 to 2,000 characters with the hidden-character rule; the folder must be trusted; project names in it are one line, at most 80 bytes each, at most 10.

## Command line

Everything the Workflows panel and the run view do is also a command ([site/reference/cli.md](../../site/reference/cli.md)), declared in `cli.go` (`Instance.CLI`) and calling the same functions as the bound API:

- `flint workflow list | show <name> | new <name> | run <name> --task <text> [--agent <a>] | coordinate --goal <text>` work in the folder of `--folder` or the current directory, checked like the UI's (`workspaceDir`, trust). `--task-file <path|->` reads the task from a file or standard input in the client.
- `flint run list | show | wait | approve | send-back --step <s> [--note <text>] | check | retry | continue | pause | resume | stop | remove <run>`: one scheme, `workflow` for definitions and starts, `run` for runs. `run wait` is `wait_for_workflow_run`'s wait (`engine.waitRun` with `settled`, no polling) without its cap: it ends when the run needs the user (exit 3: waiting for approval, failed, paused by the user, or paused by a limit that does not resume by itself), is done (0), stopped or removed (1), after `--timeout` (4), or when the client goes away. A run paused by a limit that resumes by itself keeps it waiting (`waitDone`). Every command has `--json` with the run view (`RunView`; `run wait` wraps it as `{timedOut, run}`).
- A run the command line starts is the user's, like Run Workflow…; there is no origin or log line of its own. Like Run Workflow…, `workflow run` may ask in Flint first: the first script steps without a sandbox in a checkout ([Sandbox](#sandbox)).
- **Your decisions are asked in Flint.** `run approve` and `run continue` are human decisions (`cli.Command.Human`, `clidecide.go`): whoever runs them, a person in a terminal included, Flint asks first in its own native dialog, which the web view cannot answer ("Approve this step?" / "Continue past the failed step?", the run, the workflow, the step and the process that asked; Cancel the default), and decides only after a yes for the same run at the same step and attempt; otherwise `flint: not approved in Flint`, exit 1. **That dialog is what protects an approval step on this path.** As a second line, commands that act (start, approve, send back, check, retry, continue, pause, resume, stop, remove, new) are refused, before any dialog, when the process that sent them descends from an agent Flint runs, from a workflow step's chat or script, or from an agent CLI anywhere, or does not come from a terminal (no controlling terminal, or it left the session it was started in) ([ARCHITECTURE.md, "Command-line socket"](../ARCHITECTURE.md#command-line-socket)). Ancestry can be hidden (a program that gives itself a new terminal, a running `tmux` server), so that check alone would not protect a decision. Reading (`list`, `show`, `wait`, `coordinate`) works for agents.

## How it is built

`internal/features/workflows`, a feature module like Projects:

- `format.go`: the file format, pure. `load.go`: the three folders, project over personal over built-in, as for prompts.
- `run.go`: the run and its transitions, pure. `prompt.go`: the text an agent step gets.
- `engine.go`: takes the steps through `feature.Host`. `api.go`: the bound service. `state.go`: the `workflowRuns` key of `state.json`; `unsandboxed.go`: the `workflowUnsandboxed` key and the question.

The engine uses the paths Flint already has, through narrow additions to `feature.Host` (and `Host.TasksChanged`, which a run's change of state calls so the tasks' views and Projects follow it):

- The run's agent task is made with `CreateAgentTasks`, so the branch, the worktree, the queue (`agentTasks.maxParallel`), trust and the budget pause are those of New Agent Task. The first step's chat is the task's own first chat: Flint asks `TaskContext` when it starts, and the engine answers with the step's text.
- `StartStepChat(task, agent, prompt, opts)` opens a later step's chat in the task's worktree through `openChat`, the one place a chat is opened; the prompt takes `promptChat`'s path (usage budget, redaction, shown as "from Flint"). A review step's chat is started with `opts.Reviewer` ([Security](#security)).
- `Instance.TaskChatStarted(start, chat)` tells the engine which chat Flint opened for the task's start, after it registered and before its first message: that is the first step's chat (its record), whose last lines are the step's result and which Stop ends, never "the task's latest chat", which could be one the agent or the user opened in the worktree. `Instance.TaskStartCheck(start)` lets the engine refuse the start of the task of a run that is over, removed or unknown: the queue, Start now, a feature's create and a restart take the task off the queue without a chat ("its workflow run was stopped" in the log).
- `WaitChat(chat)` blocks until the chat's turn ended (the orchestration registry's wait, no polling); `ChatOutput(chat, lines)` reads its last lines, redacted.
- `StopChat(chat)` ends a chat. Stop ends every chat the run's steps opened that is still open, not only the running one.
- `RunConfiguration(task, name, opts)` runs a run configuration in the task's worktree and waits for it; `opts.Sandbox` confines it ([Sandbox](#sandbox)); `RunResult.Changed` lists what the script changed of what later steps run ([Security](#security)). `RunLaunch(checkout, name)` resolves a configuration without running it, with the same checks, for what the user sees and for a schedule's fingerprint; `ScriptSandbox()` says whether this system has the sandbox.
- **One change per transition.** A change that leaves the run with a step to start applies `Started` in the same change (`engine.change`), so a step's end and the next step's start (or the wait at an approval step) are saved and told to the UI once; after Close nothing starts. At launch a stored run is validated (`validate.go`: kinds, ids, names, agents, records, bounds) and dropped when it does not validate, and a run that would start a step at once (Flint quit between two steps) is paused: "Flint restarted: Resume to go on".
- `BudgetPause()` says whether the usage budget pauses new turns now, which a schedule asks before it starts a run.

## Security

- **Trust.** Repository workflows are read only from trusted folders; in Restricted Mode they are counted and not offered. A run starts only from a trusted checkout, and a step never starts in an untrusted worktree.
- **Who starts things.** A run starts on the user's click (Run Workflow…, or Delegate of a project item that names a workflow), on a schedule the user wrote in their own settings, or on an agent's `start_workflow` or `delegate_project_item` call the user approved for that one call. Opening a folder starts nothing.
- **Agents steer, the user approves.** Agent tools are off by default, offered only in trusted folders, each acting call needs the user's yes to that call, a chat reaches only its folder's runs and, from a workspace folder, those of the items of the trusted projects it is in, at most 10 starts and 10 delegations per chat, and a step of a run or an agent task's chat or worktree gets no delegation and no project scope (a step gets none of the tools). No tool can approve a gate, continue past a failed step or check it again.
- **Delegation is the user's click, approved per call.** `delegate_project_item` runs Delegate's own function with every check: never an item that waits, never from an untrusted project, never from inside handed-out work, script steps without a sandbox only where the user already allowed them; its runs are marked as the agent's (`Origin: "agent"`, the log).
- **A project names, it does not define.** An item's `workflow` is a name; the steps come from the built-ins, the user's folder or the trusted checkout, never from the project's folder, which may be untrusted.
- **The definition is frozen at the start.** The steps are copied into the run. An agent that edits `.flint/workflows` in its worktree changes nothing about the run it is part of.
- **What a script step runs.** A `script` step names a run configuration, and only that entry comes from the checkout the run was started from (never from the task's worktree), so an agent cannot rename or rewrite the command line a later step runs. **What that command runs is the agent's code**: the package's scripts, the Makefile, the tests, the program built from the worktree, even which package manager runs (`npm`, `pnpm`, `yarn`, `bun` are picked by the worktree's lock file). Without the sandbox that code runs with your rights: it can read your keys and credentials and use the network. So Run Workflow… (`StepView.command`, `Listing.sandbox`) and the confirmation of `start_workflow` show each script step's run configuration, its command line and whether the sandbox confines it. The sandbox is on by default, so the script reads no keys, credentials, history or Flint's folder, writes only its worktree, the temporary folders and the folders you add, and has no network unless you allow it; builds and tests that need a global cache or the network fail until the user adds the folder or allows the network ([Sandbox](#sandbox)). Running script steps without the sandbox needs the user to switch it off, and is then the user's explicit decision, asked once per checkout in a native dialog, never taken by an agent's call or a schedule. Agent steps are not sandboxed by this setting.
- **A script cannot plant what later steps run.** In the worktree a later agent step (or the reviewer) runs `.claude`'s settings and hooks, reads `CLAUDE.md`, `CLAUDE.local.md`, `AGENTS.md`, `AGENTS.override.md`, `.mcp.json`, `.codex`'s configuration, and git follows the worktree's `.git` file; `.flint`, `.vscode` and `.idea` hold more of the same (`sandbox.WorktreeProtected`). On macOS the sandbox profile denies writing, creating, renaming or removing them at the worktree's top, in any letter case (`Policy.DenyWrite`, after the worktree's write allowance). Landlock cannot deny inside an allowed folder, so on every system, sandboxed or not, Flint takes a fingerprint of them before the script (kind, permissions, link target, content hash up to 1 MiB per file and 16 MiB in all, then size and inode change time; more than 4,096 entries cannot be checked and the step fails before it runs) and compares it after: any change stops the run, "the script changed <path>, which later steps would run: the run stopped", with no rework. Not covered: the same names deeper in the tree (a nested `CLAUDE.md`, which Claude Code reads when it works in that folder) and everything else the next agent reads as code.
- **The reviewer is not configured by the code it reviews.** A review step's chat runs in the implementer's worktree, so it starts as a reviewer's (`feature.StepChatOptions.Reviewer`, `agentsession.WithReviewer`): Claude Code with `--setting-sources user` (no project `.claude` settings or hooks and no project `CLAUDE.md`, as in Restricted Mode), Codex with `project_doc_max_bytes=0` (no `AGENTS.md`; Codex has no switch for a project's `.codex/config.toml`, which it reads only for a project trusted in Codex itself), both without the user's extra arguments; a custom ACP agent has no such switch and starts as usual. And no request of a reviewer chat is approved by the approval policy, not even "do everything": every edit, read and command asks.
- **No new way to run a command.** A workflow file cannot hold a command line, an environment, a tool or a permission: only names of agents and run configurations the user already has.
- **Text is bounded and fenced.** What goes to an agent (the task, the step, feedback from a failed step) is redacted, bounded, and a line that starts with `=====` is quoted, so a script's output or a reviewer's finding cannot close Flint's block and open its own.
- **Approval policy is unchanged, except for the reviewer.** A step's chat asks for permission like any chat; a reviewer's asks for everything. A workflow grants nothing. An agent's edit in `.flint` (workflows, run configurations, agents, roles, prompts) asks, like one in `.git`, `.claude`, `.codex`, `.vscode` or `.idea`.
- **Stop stops.** Stop ends the running script and every chat the run opened that is still open, the first step's included (by the id Flint reported when it opened it), and a queued task of a run that is over never starts an agent, also after a restart.
- **Schedules are the user's, and run what the user saved.** They live only in the user's settings, start only in an open, trusted workspace folder, never overlap with their own previous run, respect the usage budget and never ask anything (script steps without a sandbox only where the user already said yes). A repository's workflow file overrides a built-in or personal one and the checkout's `.flint/run.json` says what a script runs, so a schedule runs only the definition it was saved with ([Schedules](#schedules)).
- **Bounded.** 30 steps, `maxAttempts` per step, 12 automatic resumes per step after a limit, 100 runs kept, 200 log entries per run, 20 schedules (8,000 bytes of task each), 10 writable folders for the sandbox, 200 checkouts allowed to run scripts without it.

## Not yet

Parallel steps, artifacts declared per step, a run across several repositories, conditions other than pass or fail.
