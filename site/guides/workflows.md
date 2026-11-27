# Workflows

A workflow is a repeatable way to deliver a change: a short list of steps that agents, scripts and you take in order, the same way every time. It is one Markdown file. A typical one has an agent write a plan, an agent implement it, a script run your tests, a second agent review the change, and then waits for you to approve.

The point is that the plan, the tests, an independent review and your approval are steps of the file, not things you hope the agent remembers. When the agent skips a step, the run does not go on. When a check fails, the work goes back to the step that can fix it, with the reason. And the decision to accept the change is a step that only you can take.

A workflow runs as an [agent task](ai-agents.md#agent-tasks-agents-in-parallel): its own branch and worktree, so it never touches the files you have open. Nothing about a run is hidden. Every agent step is an ordinary chat tab, every script step is an ordinary run tab, and the result is an ordinary agent task that you review, merge or turn into a pull request.

## Your first run

Flint has two workflows built in: `feature` (plan, implement, review, approve) and `bug-fix` (reproduce, fix, review, approve). You need a folder that is open in Flint, is a git repository and is [trusted](restricted-mode.md), and an agent installed (Claude Code or Codex, see [AI agents](ai-agents.md)).

1. Open the **Workflows** panel on the activity bar (it sits after Agent Tasks), or choose **Show Workflows** in the command palette (`⇧⌘P`). Under "Workflows in <your folder>" you see `feature` and `bug-fix`, each with its steps counted.
2. Click **Run…** on `feature`. (The **＋** button in the panel's header, and **Run Workflow…** in the palette, open the same dialog.)
3. The dialog shows the workflow's steps as a row of cards: Plan, Implement, Review, Approve. Under **What should be done?** describe the change, for example "Add a --verbose flag that prints each file as it is processed". Every step gets this text. Pick the **Agent** that takes the steps that name none of their own.
4. Click **Start**. Flint makes a branch and a worktree, like New Agent Task, opens the run in a new tab and starts the first step.

The first step, Plan, is an ordinary chat in the new worktree. Open it from the run tab (see below) if you want to watch the agent work. When it ends with `RESULT: DONE`, the run goes on to Implement, then to Review, where a new chat of another agent checks the change. If the review finds problems, the run goes back to Implement with the findings and tries again. Finally it reaches Approve and waits for you.

While you wait for it, work as usual. A run that needs you shows up in three places: the **Needs you** group at the top of the Workflows panel, a badge on the panel's activity bar button, and "Workflows: 1 needs you" in the status bar (a click opens the run).

## Reading the run tab

The run tab is the one place a run is steered from.

- **The toolbar** has the run's title, its state (Running, Waiting, Failed, Paused, Done or Stopped) and the buttons that make sense right now.
- **The row of cards** is one card per step: its number and title, its kind (Agent, Script, Review, Your approval or Workflow), who takes it, its state (Not started, Running, Done, Failed or Skipped), "Attempt 2" when it was taken again, and "fails → back to Implement" when a failure sends the work back. The card the run is at is highlighted. Click a card to see that step below.
- **A step's details** are its text, the end of what the agent said or the script printed, and **Open chat** (agent and review steps) or **Open run tab** (script steps), which take you to the real chat or run.
- **Task** is what you asked for, with **Open task** (the folder and the chat) and **Show changes**, which opens the task in the Agent Tasks panel with the changed files, Merge and Create PR.
- **Log** lists what happened, with times.

You can take over at any time: the step's chat is a chat. Click **Pause**, talk to the agent in its tab, then **Resume**.

## When it waits for you

A run stops and shows its state when it needs a decision. The buttons are:

| Button | When | What it does |
|---|---|---|
| **Approve** | Your approval is awaited | The step passes and the run finishes (or goes on). Read the diff first: **Show changes**. |
| **Send back…** | Waiting, failed or paused | Asks which earlier agent step to take again and for a note ("What should be different this time?"). That step and the ones after it run again, and the note goes to its agent. |
| **Check again** | An agent or review step failed and has a chat | Reads the end of the step's chat again, for after you answered the agent there. A result line of `RESULT: DONE` (or `PASS` for a review) passes the step and the run goes on; `RESULT: FAIL` from a review sends the work back as usual; otherwise the run stays failed. |
| **Retry** | A step failed | Takes the failed step again, in a new chat. |
| **Continue anyway** | A step failed | Counts the failed step as passed and goes on, after you confirm. |
| **Pause** / **Resume** | Running / paused | Pauses the run before its next step; the step that is running ends first. |
| **Stop** | Any state but Done | Stops the running chat turn or script and ends the run. The agent task and its files stay. |
| **Remove** | The run is over | Forgets the run. The agent task and its files stay. |

Two cases need more explanation.

**A step fails.** A `script` or `review` step with an `on-fail` line sends the work back by itself, up to `workflows.maxAttempts` times per step (3 by default, Settings ▸ Workflows ▸ Tries per step). After that the run is Failed and waits for you. Read the error under the toolbar and the step's output, then choose Send back…, Check again (an agent or review step, after you answered it in its chat), Retry or Continue anyway.

**An agent step asks a question.** An agent step passes when the agent ends with `RESULT: DONE`. If it ends with `RESULT: BLOCKED`, or with no result line (it may simply have asked you something), the run waits with the agent's last lines. Answer in that step's chat, then click **Check again**: Flint reads the end of the chat once more and, when it now ends with `RESULT: DONE`, the step passes and the run goes on. **Retry** starts a new chat that has not seen your answer; **Continue anyway** counts the step as passed without checking.

When the run is Done, click **Show changes**. The result is a normal agent task: review the diff, then Merge or Create PR from the Agent Tasks panel.

## Look at a workflow before you run it

In the Workflows panel, click **View** on a workflow (or its name). The tab shows its steps as cards joined by arrows: who takes each step, what a script step runs, and where the work goes back when a check fails. Click a card to read that step's text. **Run…** in the tab starts a run.

## Write your own workflow

Choose **New Workflow…** from the `⋯` menu of the Workflows panel (or the palette). Give it a name (lowercase letters, digits and dashes). Flint writes a starter file to `.flint/workflows/<name>.md`, opens it, and the file is shared with your team through git. A workflow for yourself only goes in the `workflows/` folder of Flint's config directory. A repository's file wins over a personal one, and a personal one over a built-in of the same name.

Here is a workflow that plans, implements, runs the tests, has the change reviewed and waits for approval:

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

Line by line:

- **`---` … `---`** is the front matter. It has one key, `description`, which the panel and the Run Workflow… dialog show.
- **`## Plan`** starts a step. The title is its name. Every `##` line is a step, taken in order.
- **`agent: planner`** is a key. The lines right after a title, up to the first empty line, are the step's keys. Here `planner` stands for an agent that takes this step: any agent name, such as a [defined agent](ai-agents.md#defined-agents). Without an `agent` key, the agent you picked when you started the run takes it.
- **The text after the empty line** is what the agent is told. Every agent step also gets your task. A step with no `type` is an `agent` step: a new chat in the task's worktree gets the text, and the step passes when the agent ends with `RESULT: DONE`. Flint adds that instruction itself.
- **`## Implement`** has no keys, so it is an agent step by the agent you picked.
- **`type: script`** makes `Test` a step that no agent takes. **`run: test`** names a run configuration, by name. Flint runs it in the worktree, in a run tab, and the step passes when it exits with 0.
- **`on-fail: Implement`** says where the work goes back when this step fails: to the earlier agent step `Implement` (its title or its id), with the end of the script's output as the reason.
- **`type: review`** makes a new chat of another agent review the change. It passes when the reviewer ends with `RESULT: PASS`; otherwise its findings go back to `Implement`.
- **`type: gate`** is a step for you. The run waits until you click Approve.

A fifth kind, `type: workflow` with `workflow: <name>`, runs another workflow's steps in its place (at most 3 levels, no cycles). The limits: 30 steps after sub-workflows are put in, 64 KiB per file, 16,000 characters per step, and the first step must be an agent step. A file that breaks a rule is listed under **Problems** in the Workflows panel with the reason and is not offered.

A workflow file holds names, not commands: the agents and run configurations you already have. It cannot define a command line, an environment or a permission.

## Add a test step

A `script` step runs a [run configuration](run-debug-test.md#run-configurations) by its `name`: one from `.flint/run.json` or one Flint detects in the folder. To add a test step:

1. Make sure the run configuration exists, for example in `.flint/run.json`:

   ```json
   { "configurations": [ { "id": "test", "name": "test", "type": "make", "target": "test" } ] }
   ```

2. Add the step to the workflow:

   ```markdown
   ## Test
   type: script
   run: test
   on-fail: Implement
   ```

Flint reads the configuration from the checkout the run started from, not from the task's worktree, so an agent cannot rewrite the command a later step runs. Save and commit the run configuration before you start.

The first time you start a workflow with a script step in a repository, and you have switched the [sandbox](#the-sandbox) off, Flint asks whether to run scripts without a sandbox there. It shows the command line the step will run; your answer is kept for that repository. With the sandbox on, which is the default, Flint asks nothing.

**First run in a Go or npm project.** The step runs in the [sandbox](#the-sandbox), which writes only in the run's folder and temp folders and has no network. A first failure is usually a build cache or a download. Add the cache folder under **Settings ▸ Workflows ▸ Extra folders a script may write**: `~/Library/Caches/go-build` (macOS) or `~/.cache/go-build` (Linux) for Go, `~/.npm` for npm. Turn on **Allow network in the sandbox** if the test downloads dependencies. A failed sandboxed step says so in the run's error.

`on-fail` decides what a failure does. With it, the work goes back to that agent step with the reason and the steps from there run again, until the step passes or has been taken `maxAttempts` times. Without it, a failed step makes the run Failed at once and waits for you.

## When the agent hits a usage limit

A usage limit is not a failure. When an agent's provider refuses for its quota (a subscription limit, a rate limit, no credits), or Flint's own daily budget (`usage.pauseOverBudget`) is spent, the run is **Paused** with a message such as "Paused: codex reached its usage limit". The step goes back to not started and the start does not count as an attempt. Click **Resume** to take the step again.

Two settings in **Settings ▸ Workflows** can do it for you:

- **Agent for a usage limit** (`workflows.fallbackAgent`): another agent takes the step at once if the first reached its own limit. The step card then says "instead of <agent>". If that one hits a limit too, the run pauses.
- **Resume after** (`workflows.resumeAfterMinutes`, at most 720): a paused run goes on by itself after that many minutes, at most 12 times per step. `0` means it waits for you.

Flint does not read when a provider's limit resets, so choose a wait that fits your plan. Flint's own budget always pauses the run.

## Run a workflow from a project item

An item of a [project](projects.md) can name a workflow in its front matter:

```markdown
---
title: Fix the export button on Safari
workflow: bug-fix
---
The button does nothing. Done when the new test passes.
```

**Delegate** then makes the item's agent tasks as usual, one per repository, and a run of the workflow takes each task. The item's text is the task. The item follows the run, not each turn of the agent: its task counts as working while the run is running or paused, as needing review while the run waits at a gate or is done, and as failed when the run failed or was stopped. The project panel shows "Workflow: bug-fix" on the item and **Open run** on its task.

An item with `workflow` cannot also have `together: true`, because a run works in one repository. The workflow's steps come from the built-ins, your personal folder or the trusted checkout, never from the project's own folder, which may be untrusted.

## Let an agent coordinate

An agent can start and steer runs for you, and it can never approve one. It needs two things: turn on **Settings ▸ Agent Context ▸ Actions ▸ Run and steer workflows** (off by default), and a trusted folder. The agent then gets five tools through Flint's MCP server: list the workflows, read runs, wait for a run (without polling), start a run and steer a run (pause, resume, retry, send back or stop).

Flint asks you before every call that starts or steers a run. There is no "always allow". The tools have no way to approve a gate or to continue past a failed step; those are your decisions.

To get an agent going, choose **Coordinate with Agent…** from the `⋯` menu of the Workflows panel, say what should be built or changed, and pick an agent. Flint puts a prompt into a chat draft and you send it yourself. The prompt tells the agent to list the workflows, start runs for your goal, wait for them, tell you when a run waits for approval and never try to approve it.

**Above several repositories.** When the folder belongs to a [Flint project](projects.md) (it holds the project, or it is a checkout of one of the project's repositories), the agent coordinates the project's items instead. Switch on **Settings ▸ Agent Context ▸ Actions ▸ Delegate project items** too (off by default). The prompt then tells it to read the project with `get_project`, hand each ready item to its agents or workflow with `delegate_project_item`, one at a time, start plain runs only for work that is not an item, wait for the runs, tell you plainly when one waits for your approval, and stop when you say so. From that folder the agent sees, waits for and steers the runs of the project's items in every repository, not only its own. An agent can delegate a ready item only when "Delegate project items" is on, only with your yes to that one call, never an item that waits for others, never from a project whose folder is untrusted, and never from inside handed-out work (an agent task's chat or worktree, or a workflow step). It can still never approve a step, check a failed one again or continue past it.

A chat that is itself a step of a run gets none of these tools, so a run cannot start or steer workflows, or pause itself. An agent that was handed work (an agent task's chat or worktree) cannot delegate items, and does not reach its project's runs.

## Schedules

A run can start at a time you choose, for example a nightly dependency update. Add it to your own `settings.json` (or on **Settings ▸ Workflows**):

```json
"workflows": {
  "schedules": [
    { "name": "nightly-deps", "workflow": "feature", "folder": "/Users/me/code/app",
      "task": "Update the dependencies and fix what breaks.", "agent": "",
      "at": "02:30", "days": ["mon", "tue", "wed", "thu", "fri"] }
  ]
}
```

Schedules are only read from your settings, never from a repository file. When one is due, Flint starts the run like Run Workflow…, without asking anything, and the run's log says "Started by the schedule nightly-deps". A schedule starts nothing, and the panel says why, when its folder is not open in Flint or is in Restricted Mode, when its last run is not over yet, when the usage budget pauses new turns, or when the workflow cannot run. Flint does not catch up: a time that passed while Flint was closed or the computer slept is skipped. The fields are in the [settings reference](../reference/settings.md#workflows).

## The sandbox

A script step runs a run configuration of yours on code an agent wrote. **Settings ▸ Workflows ▸ Run script steps in a sandbox** (`workflows.sandbox`) is on by default, so Flint confines it with the operating system's own sandbox (`sandbox-exec` on macOS, Landlock and seccomp on Linux), the one [extensions](extensions.md) run in. A sandboxed script:

- reads what you can read, except keys, credentials, shell history, keychains, browser profiles and Flint's own folder;
- writes only in the run's worktree, the temporary folders and the folders you add under **Extra folders a script may write**;
- uses the network only if you turn on **Allow network in the sandbox**.

The first line of the run tab says what is confined. Where the system has no sandbox, the step fails instead of running unconfined, and says what to do: switch off **Run script steps in a sandbox** in Settings ▸ Workflows to run it without one (Flint asks you once per folder).

If you switch the sandbox off, Flint asks you once per repository. The first time you start a workflow with a script step there, a dialog **Run scripts without a sandbox?** names the folder and each script's command line. **Run without a sandbox** remembers your answer for that repository; **Cancel** starts nothing. An agent cannot be the first: its `start_workflow` is refused until you have run such a workflow there yourself, and a schedule skips with a note in the panel. Run Workflow… shows when you allowed it, with **Take back**.

Two honest limits. First, **agent steps are not sandboxed**: the setting confines script steps only. An agent still works under the approval policy of any chat. Second, build tools write global caches outside the worktree. A Go build needs `~/Library/Caches/go-build` (macOS) or `~/.cache/go-build` (Linux), and npm needs `~/.npm`. Add those folders under **Extra folders a script may write** (`sandboxWritable`), or the build fails with a permission error. A test that downloads dependencies also needs **Allow network in the sandbox**. A sandboxed step that fails shows a line telling you this. Folders that are, hold or sit inside a protected place (your home folder itself, `~/.ssh`, `~/.config`) are not granted, and the run tab says so.

## From the terminal

Everything the panel and the run tab do is also a command. Flint must be running. The full list is in the [command line reference](../reference/cli.md).

```sh
# What can I run here, and what are its steps?
flint workflow list
flint workflow show feature

# Start a run; the task can come from a file or standard input
flint workflow run bug-fix --task "The export button does nothing on Safari"
flint workflow run feature --task-file issue.md --agent codex

# Look at runs
flint run list
flint run show 3f2a9c0d1e4b5a6f

# Wait until a run needs you (exit status 3), then approve it yourself
flint run wait 3f2a9c0d1e4b5a6f; [ $? -eq 3 ] && flint run show 3f2a9c0d1e4b5a6f
flint run approve 3f2a9c0d1e4b5a6f
```

Add `--json` for scripts. `flint run send-back <run> --step <step> --note <text>`, `check`, `retry`, `continue`, `pause`, `resume`, `stop` and `remove` match the buttons. A coding agent you run outside Flint (Claude Code in your terminal, say) can start and steer runs with these commands only after you allow each call in Flint's dialog, and only while **Run and steer workflows** is on; `flint run approve`, `continue` and `check` are refused to every agent, so those decisions stay yours. Agents Flint started are refused every command that changes something. See [Agents ask you first](../reference/cli.md#agents-ask-you-first).

## How this stays safe

- An agent can never approve a gate or continue past a failed step. Not through Flint's tools, and not through the command line.
- Agents can start and steer runs only if you turn it on, only in trusted folders, and only with your yes to each call, through Flint's tools or, for an agent you run outside Flint, through the command line. A step of a run gets no such tools, and the command line refuses it and anything sent from an agent task's worktree.
- A run starts only on your click, on a schedule you wrote in your own settings, or on an agent call you approved. Opening a folder starts nothing.
- A project item names a workflow, it never defines one. Steps are read from the built-ins, your folder or the trusted checkout.
- A run uses a copy of its workflow. An agent that edits `.flint/workflows` in its worktree changes nothing about the run it is part of.
- A script step runs a run configuration read from your checkout, not from the worktree, and with the sandbox on (the default) it cannot read your keys or write outside its worktree.
- A workflow grants nothing: a step's chat asks for permission like any chat. A loop is bounded by `maxAttempts`, your usage budget and **Stop**.
- Your task text and what flows between steps are redacted, bounded and fenced, so a script's output cannot pretend to be Flint's own instructions.

## Related

- [AI agents](ai-agents.md): agents, chats and agent tasks
- [Projects](projects.md): the board and Delegate
- [Command line](../reference/cli.md)
- [Settings file](../reference/settings.md#workflows)
