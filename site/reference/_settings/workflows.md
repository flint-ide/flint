| `workflows` | object | see below | How workflow runs (**Run Workflow…**) send work back, what they do at a usage limit, whether script steps run in a sandbox, and the runs that start at a time of day. See [workflows](#workflows). |

### workflows

| Key | Type | Range | Default | Meaning |
|---|---|---|---|---|
| `maxAttempts` | integer | 1–10 | `3` | How many times one step is taken. A `script` or `review` step with `on-fail` that fails sends the work back to that step by itself only while the failing step was taken fewer times than this; after that the run stops and waits for you. |
| `fallbackAgent` | string | an agent's name | `""` | When a step's agent reaches its own usage limit (its subscription's limit, a rate limit, no credits), this agent takes the step again at once, if it can start and is not the agent that hit the limit. The run view shows which agent took the step. Empty: the run is paused instead. Flint's own budget (`usage.pauseOverBudget`) always pauses. |
| `resumeAfterMinutes` | integer | 0–720 | `0` | A run paused by a limit resumes by itself after this many minutes, at most 12 times per step. `0`: it waits for you to click **Resume**. Flint does not read when the provider's limit resets, so pick a wait that fits your plan. |

| `sandbox` | string | `"on"`, `"off"` | `"on"` | `"on"` (the default) runs every `script` step in the operating system's sandbox (the one extensions run in: `sandbox-exec` on macOS, Landlock and seccomp on Linux). The script may read what you can read except keys, credentials, shell history, keychains, browser profiles and Flint's own folder; write only in the run's worktree, the temporary folders (`$TMPDIR`, `/tmp`, `/dev/shm` on Linux) and `sandboxWritable`; and use the network only with `sandboxNetwork`. The run tab's first line says so. Where the system has no sandbox, the step fails instead of running unconfined: switch this to `"off"` to run it without one (Flint then asks you once per folder). If a build or test fails in the sandbox because it writes a cache folder or needs the network, add the folder to `sandboxWritable` or set `sandboxNetwork`. Agent steps are not sandboxed by this setting. |
| `sandboxNetwork` | boolean | | `false` | With `sandbox` on, lets a script use the network (Internet and local sockets such as Docker's). |
| `sandboxWritable` | string[] | at most 10 | `[]` | More folders a sandboxed script may write, e.g. a build cache (`~/Library/Caches/go-build`, `~/.cache/go-build`, `~/.npm`). Each is absolute or starts with `~/`, is clean, and must be an existing folder when you save. A folder that is, holds or sits inside a protected place (the home folder itself, `~/.ssh`, `~/.config`…) is not granted, and the run tab says so. |
| `schedules` | object[] | at most 20 | `[]` | Runs Flint starts by itself at a time of day; see below. |

A value out of range is replaced with its default; a bad entry of a list is dropped.

Each schedule:

| Key | Type | Range | Meaning |
|---|---|---|---|
| `name` | string | lowercase letters, digits and dashes; unique | Shown in the run's log ("Started by the schedule …") and origin (`schedule:<name>`). |
| `workflow` | string | a workflow's name | The workflow to run. |
| `folder` | string | an absolute, clean path | The workspace folder the run's agent task branches from. It must be open in Flint and trusted when the schedule is due. |
| `task` | string | 1–8,000 bytes, no hidden characters | What the run should deliver. |
| `agent` | string | `""` or an agent's name | Takes the steps that name no agent; `""` is the default agent. |
| `at` | string | `HH:MM`, 24 hours | The local time of day. A time your clock skips (the spring change) is skipped that day; a time it passes twice runs once. |
| `days` | string[] | `mon` … `sun` | The days it runs on; empty for every day. |

When a schedule is due, Flint starts the run like **Run Workflow…**, without asking anything. It starts nothing, and shows why next to the schedule, when the folder is not open or is in Restricted Mode, when the schedule's last run is not over yet, when the usage budget pauses new turns, or when the workflow cannot run. A time missed while Flint was closed or the computer slept is not caught up.

Agents can start and steer runs (never approve them) only when `mcpServer.actions.workflows` is on; see [mcpServer](#mcpserver).

```json
"workflows": {
  "maxAttempts": 3, "fallbackAgent": "codex", "resumeAfterMinutes": 60,
  "sandbox": "on", "sandboxNetwork": false, "sandboxWritable": ["~/Library/Caches/go-build"],
  "schedules": [
    { "name": "nightly-deps", "workflow": "feature", "folder": "/Users/me/code/app",
      "task": "Update the dependencies and fix what breaks.", "agent": "", "at": "02:30",
      "days": ["mon", "tue", "wed", "thu", "fri"] }
  ]
}
```
