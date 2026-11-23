# Command line

The `flint` command does everything the Workflows panel, the Projects panel and board, and the Agent Tasks panel do, from a terminal or a script. It talks to the Flint that is running for your user and prints the answer; it never opens a window.

```sh
flint workflow run bug-fix --task "The export button does nothing on Safari"
flint run wait 3f2a9c0d1e4b5a6f && flint task pr 7b1c2d3e
```

Flint must be running: otherwise every command says `flint: Flint is not running: start it first (flint <folder>)` and exits with status 1.

## Installing

- **macOS, Homebrew:** the cask links `flint` into your `PATH`.
- **macOS, `make install`:** installs `scripts/flint` as `~/.local/bin/flint`.
- **Linux:** the archive's `bin/flint` is Flint itself; the same binary runs the commands.

On macOS `flint` is a small script: `flint [folder]` opens a folder as before, and for a command it runs the app's own binary (`flint.app/Contents/MacOS/flint`), which sends the command and exits within milliseconds, without a window or a Dock icon.

A folder whose name is a command word (`workflow`, `run`, `project`, `task`, `help`) opens with a path: `flint ./task`. A command quoted as one argument, `flint "workflow list"`, is not taken for a folder (unless one of that name exists): `flint` says `flint: "workflow list" is one argument: write flint workflow list` and exits with 2.

## Conventions

- **Folder.** Commands marked *folder* work in a folder that is open in Flint (or inside one): `--folder <path>`, or the current directory. A folder that is not open in Flint, or that is in [Restricted Mode](../guides/restricted-mode.md) where the command needs trust, is refused with the same sentence the window shows.
- **Projects.** Project commands take `--project <folder|name>` (or a `[<project>]` argument); without it, the project the current directory is in, or the only project Flint knows.
- **Output.** Plain, aligned text by default. `--json` prints JSON instead: the same objects the panels show (a run as the run view has it, a task as the Agent Tasks panel has it, a project as the board has it). Errors go to standard error as `flint: <reason>`, also with `--json`. Text from files and agents is printed without control characters.
- **Long text from a file.** Every option marked *file* also takes `--<option>-file <path>`, or `-` for standard input: `flint task new --description-file spec.md`, `git log -1 --format=%B | flint workflow run fix --task-file -`. The `flint` command reads the file (a regular file, at most 64 KiB) and sends its text; Flint never opens a path given there.
- **Questions.** Where the window asks you in a native dialog (creating a pull request, deleting a task's uncommitted work, delegating an item of an untrusted project, cloning, the first script steps without a sandbox in a repository), Flint shows the same dialog and the command waits for your answer. There is no option that skips it.
- **Help.** `flint help` lists the commands of the running Flint (a feature switched off in **Settings ▸ Extensions ▸ Built-in** has none); `flint <group> --help` and `flint <command> --help` say more.

## Exit status

| Status | Meaning |
|---|---|
| 0 | Done. |
| 1 | Failed or refused (the reason is on standard error). |
| 2 | Wrong usage: an unknown command or option, a missing argument. |
| 3 | `run wait`: the run needs you (a gate waits for approval, a step failed, the run is paused). |
| 4 | `run wait`: `--timeout` passed. |

## Your decisions are asked in Flint

Approving a step (`flint run approve`) and continuing past a failed step (`flint run continue`) are decisions only you take. Whoever runs these two commands, you in a terminal included, Flint first asks in a dialog in its own window: "Approve this step?" or "Continue past the failed step?", with the run's title, the workflow, the step, and which process asked ("Asked from the command line (pid 4242, zsh)"). Cancel is the default button. The command waits for your answer; when you cancel, it changes nothing and exits with status 1:

```text
flint: not approved in Flint
```

The dialog is drawn by Flint, not by the web page, so neither a page nor an agent can answer it. When the run moved on while the dialog was open (another step, another attempt), nothing is done and the command says so. At most 8 commands that wait (dialogs and `flint run wait`) run at once; another one is refused until one ends.

## Agents cannot act through it

The command line is for you and your scripts. A coding agent could type `flint run approve …` into its shell, so for every command that changes something Flint also checks which program sent it, and refuses it, before any dialog, when it comes:

- from an agent Flint started (a chat, a workflow step, or anything else Flint runs that is not one of its terminal tabs), from an agent CLI running in one of Flint's terminal tabs, or from a known agent CLI anywhere (`claude`, `codex`, `gemini` and others, plus the agents you configured):

  ```text
  flint: this command was run by an agent (claude). Agents change things through Flint's tools, which ask you first.
  ```

  A custom agent you configured is named by its id when its command line matches the settings' ("run by the agent fake-cli"), not by its program (`node`).

- from a process that is not in a terminal: one without a terminal (started in the background by a program without one, by `launchd`, `cron` or a service), or one that left the terminal session it was started in (a background job whose shell ended, `nohup`, `setsid`). The command must come from a terminal whose shell is one of its ancestors:

  ```text
  flint: this command did not come from a terminal (it has no terminal: it runs in the background or under a service), so Flint changes nothing for it. Run it in a terminal.
  ```

Commands you type in Terminal, iTerm, an `ssh` session, `tmux` or Flint's own terminal tabs pass. Scripts that change things must run in a terminal too; read-only commands (marked *reads* below) work from anywhere, agents included. Agents start and steer work through Flint's own tools, which ask you before each call and can never approve a step or continue past a failed one (see [AI agents](../guides/ai-agents.md)).

What this check does not cover, and why the dialog above is what protects your decisions: a process can hide where it came from. A program that gives itself a new terminal (`script`, a pseudo-terminal of its own), a command handed to an already running `tmux` or `screen` server, another terminal app driven by AppleScript, or an agent under a name Flint does not know running outside Flint all look like your own terminal. Such a program can still run the commands that act but are not your decisions (start, send back, retry, pause, resume, stop, remove, and the other groups' commands), and can ask the dialog the question; only you can answer it.

## Workflows

| Command | | What it does |
|---|---|---|
| `flint workflow list` | reads, folder | The workflows offered in the folder: name, source, number of steps, description. |
| `flint workflow show <name>` | reads, folder | A workflow's steps: kind, agent or run configuration, where a failure goes back to. |
| `flint workflow new <name>` | folder | Writes a starter `.flint/workflows/<name>.md` (trusted folders; never overwrites). |
| `flint workflow run <name> --task <text> [--agent <agent>]` | folder, file | Starts a run, like **Run Workflow…**: one agent task takes the steps in order. Prints the run's id. The first time script steps would run without a sandbox in the repository, Flint asks you in its own dialog; Cancel starts nothing (exit 1). |
| `flint workflow coordinate --goal <text>` | reads, folder, file | Prints the prompt of **Coordinate with Agent…**, to paste into an agent chat. |

```sh
flint workflow run feature --task-file issue.md --agent codex
```

## Workflow runs

| Command | | What it does |
|---|---|---|
| `flint run list` | reads | Every run, newest first: id, state (and "needs you"), workflow, current step, title. |
| `flint run show <run>` | reads | A run: its state, error or limit, task, folder, what you can do now, the steps and the end of its log. |
| `flint run wait <run> [--timeout <s>]` | reads | Waits without polling until the run needs you, is paused or is over. Exit status 0 done, 3 needs you (waiting for your approval, a failed step, paused by you or by a limit that does not resume by itself), 1 stopped or removed, 4 timed out. A run paused by a limit that resumes by itself keeps waiting. Ctrl-C stops waiting, not the run. |
| `flint run approve <run>` | asks in Flint | Approves the step the run waits at for your approval, after you say yes in Flint's dialog (see above). |
| `flint run send-back <run> --step <step> [--note <text>]` | file | Takes the steps from that step on again; the note goes to its agent (4,000 characters at most). |
| `flint run check <run>` | | Check again: after you answered the agent in the failed step's chat, reads its result line again. DONE or PASS passes the step and the run goes on; otherwise the run stays failed. |
| `flint run retry <run>` | | Takes the failed step again. |
| `flint run continue <run>` | asks in Flint | Continue anyway: the failed step counts as passed, after you say yes in Flint's dialog. |
| `flint run pause <run>` | | Pauses the run before its next step. |
| `flint run resume <run>` | | Lets a paused run go on. |
| `flint run stop <run>` | | Stops the run and its running step; the agent task stays. |
| `flint run remove <run>` | | Forgets a run that is over; its agent task stays. |

```sh
id=$(flint workflow run bug-fix --task "Fix #142" --json | jq -r .id)
flint run wait "$id"; [ $? -eq 3 ] && flint run show "$id"
```

## Projects

| Command | | What it does |
|---|---|---|
| `flint project list` | reads | The projects Flint knows: name, folder, notes (workspace folder, Restricted Mode, old format). |
| `flint project open <dir>` | | Opens a folder with `flint-project.json` as a project (**Open Project…**). |
| `flint project new <dir> [--name <name>]` | | Makes a folder a project (**New Project…**). |
| `flint project close <project>` | | Forgets the project and its items' links to agent tasks; the tasks stay. |
| `flint project show [<project>]` | reads | Name, folder, context file, item counts, repositories and their checkouts, problems. |
| `flint project items [<project>] [--status <status>]` | reads | The items in the board's order: id, status, priority, agent, title, what they wait for. |
| `flint project add --title <title> [--text <text>] [--priority low\|normal\|high] [--agent <agent>] [--repos <a,b>] [--after <x,y>] [--workflow <name>] [--together]` | file | Adds an item with status todo, checked like **New Item…** and the agents' `add_project_item`. |
| `flint project status <item> <todo\|doing\|review\|done>` | | Sets an item's status. |
| `flint project delegate <item>` | folder | **Delegate**: starts the item's agent tasks (or its workflow runs). An item of an untrusted project asks in Flint first. An item without repositories works in the folder. |
| `flint project start-when-ready <item>` | folder | Starts the item by itself once the items it waits for are done. |
| `flint project cancel-start <item>` | | Forgets that waiting start. |
| `flint project plan --goal <text>` | reads, file | Prints the prompt of **Plan with Agent…**. |
| `flint project context [<project>]` | | Adds `PROJECT.md`, the context every item's agent gets. |
| `flint project locate <repo> <dir>` | | Uses a checkout for one of the project's repositories (**Locate…**). |
| `flint project clone <repo> <parent>` | | Clones a repository into a new folder inside `<parent>` (**Clone…**), after Flint's own dialog. |
| `flint project cancel-clone` | | Stops the project's running clone. |
| `flint project rename-config [<project>]` | | Renames an old project's `flint-backlog.json` to `flint-project.json`. |

The item commands take `--project <folder|name>`.

```sh
flint project add --project Acme --title "Rate limit the API" --text-file rl.md --repos api --after auth
flint project delegate rate-limit --project Acme --folder ~/src/api
```

## Agent tasks

| Command | | What it does |
|---|---|---|
| `flint task list [--all]` | reads, folder | The tasks of the folder's repository (`--all`: of every folder open in Flint). |
| `flint task show <task>` | reads | A task: state, agent, branch and base, commits ahead, worktree, pull request, usage. |
| `flint task new --description <text> [--agent <agent>] [--base <branch>]` | folder, file | **New Agent Task…**: a branch and a worktree, and Flint starts the agent's chat (or queues the task). |
| `flint task start <task>` | | Starts a queued task now. |
| `flint task diff <task>` | reads | The files the task changed since its base, with line counts. |
| `flint task compare <group>` | reads | The tasks started together from one description, file by file. |
| `flint task update <task>` | | **Update from base**: merges the base into the task's branch; lists conflicts (status 1). |
| `flint task merge <task>` | | Merges the task's branch into its base; lists conflicts (status 1). |
| `flint task pr <task>` | | Pushes the branch and opens a pull request, after Flint's own dialog. |
| `flint task remove <task>` | | Stops the task's chats, deletes its worktree and forgets it. Uncommitted or ignored files, and a branch with commits its base lacks, are asked about in Flint first. |

```sh
flint task new --description "Add a --verbose flag to the CLI" --agent claude
flint task list --json | jq -r '.[] | select(.state == "finished") | .id'
```

## How it works

The running Flint listens on a Unix socket, `cli.sock`, in its run directory (`~/Library/Application Support/flint/run/` on macOS, `$XDG_RUNTIME_DIR/flint-<hash>/` on Linux), next to a token file the `flint` command reads. When that path would be too long for a socket (a long `FLINT_CONFIG_DIR`), the run directory is `/tmp/flint-<uid>-<hash>/` instead. Both are private to your user, and each `FLINT_CONFIG_DIR` has its own. See [Privacy and security](privacy-and-security.md).
