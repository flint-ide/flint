# Files and data

This page lists where Flint stores what, on your Mac and in your projects. Flint writes its own files with permissions that only your user can read, and writes JSON files atomically so a crash never leaves a half-written file.

## Flint's data folder

Most of Flint's data lives in:

```text
~/Library/Application Support/flint/
```

| File or folder | What it holds |
|---|---|
| `state.json` | Recent folders, the agent approval policy, trusted folders, [agent tasks](../guides/ai-agents.md#agent-tasks-agents-in-parallel) per repository (description with credentials removed, agent, branch, base, worktree folder, last state, pull request link), whether the tool check has been shown, and the [update check](privacy-and-security.md#update-check)'s last time, the release it found and a version you skipped. |
| `settings.json` | Your preferences: theme (and an imported custom theme), fonts, Vim mode, default agent, auto-save, notifications, update check, shortcut overrides, TODO excludes, agent CLIs, language servers, agent context, inline completions, agent tasks, usage budgets and model prices. No secrets. See [Settings file](settings.md). |
| `connections.json` | Database connection profiles: host, port, user, default database, TLS and read-only options. No passwords. A one-time `connections.json.native-backup` appears next to it when Flint moves old native ClickHouse connections to HTTP. |
| `sessions/` | One file per project: open tabs, terminals, agent conversations, attached projects, the selected run configuration and breakpoints. |
| `scratch/` | Scratchpads: SQL consoles and notes, with the connection and database each console uses. |
| `history/` | Query history, one file per database connection. |
| `usage.json` | [Agent usage](../guides/ai-agents.md#usage-and-budgets): totals per day, project, agent, model, agent task and chat (tokens, cache tokens, reported cost, turns); each chat's name, made from its first message with recognized credentials removed; which budget warnings were already shown; and the budgets you chose to continue past. Written a couple of seconds after each turn and when Flint quits. Kept for `usage.retentionDays` days (default 90), at most 20,000 rows and 500 chats. A file that can't be decoded is renamed `usage.json.bad-<date-time>` and Flint starts a new one; a file that can't be read, or that a newer Flint wrote, is left alone and nothing new is saved until that is fixed (the Usage panel says so). |
| `extensions/` | [Extensions](../how-to/customize-flint.md#use-a-theme-from-an-extension) you installed, one folder per extension, named by its id. Uninstalling deletes the folder. |
| `prompts/` | Your personal [prompts](../guides/ai-agents.md#prompt-library), one Markdown file each. Project prompts live in the repository's `.flint/prompts`. |
| `extensions/` | Installed [extensions](../guides/extensions.md), one folder per extension id, copied there when you install one and deleted when you uninstall it. Their settings are in `settings.json` under `extensions`; their secrets in the keychain. |

### Project sessions

A session file in `sessions/` contains:

- the other projects attached to the window;
- file tabs with cursor positions, scratchpads and diffs;
- terminals: their project, last directory and the last ~400 lines of output (with recognized credentials removed);
- agent conversations: the last 200 transcript entries, token usage and cost;
- the selected run configuration of each project, and breakpoints.

Sessions are written shortly after changes and when Flint quits, and are limited to 4 MB. Delete a project's file from `sessions/` to reset its session.

### Query history

Each connection's history keeps its last 5000 statements, with the database, time, duration, row count and error. Passwords in role statements are masked. The history is deleted when you delete the connection, or with "Clear History…".

## Keychain

Database passwords are stored in the macOS Keychain under the service `flint-db`, one item per connection. They are deleted together with the connection.

## Logs

```text
~/Library/Logs/Flint/flint.log
```

The log rotates at 5 MB, keeping one old file (`flint.log.1`). Secrets and your home path are redacted. **Help ▸ Show Logs in Finder** opens the folder.

## In your projects

| File | What it holds | Commit it? |
|---|---|---|
| `.flint/run.json` | [Run configurations](../guides/run-debug-test.md#run-configurations) | Yes, it is meant to be shared |
| `http-client.env.json` | Public [HTTP client environments](../guides/http-client.md#environments), next to the `.http` file | Yes |
| `http-client.private.env.json` | Secret HTTP client variables (tokens, passwords) | No, add it to `.gitignore` |
| `.claude/settings.local.json` | "Always allow" choices for Claude commands, written by Claude Code itself | Usually not |

## Kept by other tools

| What | Where |
|---|---|
| Agent conversation history | The agent CLIs: `~/.claude/projects/…` and `~/.codex/sessions/…`. Flint resumes conversations from there after a restart. |
| GitHub token | The Keychain (service `flint-accounts`), written by Sign in with GitHub or the GitHub CLI import; `GH_TOKEN` is read from the environment and never stored. The account's login, avatar, host and scopes are in `state.json`. |
| AWS and Kubernetes credentials | `~/.aws/`, your kubeconfig and the tools that use them. Flint itself doesn't read them; the AWS Profiles and Kubernetes extensions do, if you install them. |
| Language server caches | The servers' own folders, for example `~/Library/Caches/gopls`. |

## Temporary and in-memory data

| What | Where |
|---|---|
| Binaries of `go` run configurations | `$TMPDIR/flint-run/`, overwritten on every run |
| CPU and memory profiles from profiling runs | `$TMPDIR/flint-profile-*/`, removed when the profile tab closes and when Flint quits |
| HTTP responses and cookies | Memory only; "Save to file" writes a body where you choose |
| Pull requests, files and review threads | Memory only, cached for a short time |
| Deleted files | The macOS Trash; use **Put Back** in Finder to restore them |

## Moving the data folder

The `FLINT_CONFIG_DIR` environment variable replaces `~/Library/Application Support/flint` with another folder. It is mainly meant for development builds. A Flint started with a different `FLINT_CONFIG_DIR` runs as a separate instance.
