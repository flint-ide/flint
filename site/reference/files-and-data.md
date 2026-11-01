# Files and data

This page lists where Flint stores what, on your Mac and in your projects. Flint writes its own files with permissions that only your user can read, and writes JSON files atomically so a crash never leaves a half-written file.

## Flint's data folder

Most of Flint's data lives in:

```text
~/Library/Application Support/flint/
```

| File or folder | What it holds |
|---|---|
| `state.json` | Recent folders, the agent approval policy, trusted folders, [agent tasks](../guides/ai-agents.md#agent-tasks-agents-in-parallel) per repository (description with credentials removed, agent, branch, base, worktree folder, last state, pull request link), whether the tool check has been shown, and the [update check](privacy-and-security.md#update-check)'s last time, the release it found and a version you skipped, and the devices paired with [Flint Remote](../guides/remote.md) (id, name, scopes, when paired and last seen; no tokens). |
| `settings.json` | Your preferences: theme (and an imported custom theme), fonts, Vim mode, default agent, auto-save, notifications, update check, shortcut overrides, TODO excludes, agent CLIs, language servers, refresh intervals, agent context, inline completions, agent tasks, Flint Remote. No secrets. See [Settings file](settings.md). |
| `connections.json` | Database connection profiles: host, port, user, default database, ClickHouse protocol, TLS and read-only options. No passwords. |
| `sessions/` | One file per project: open tabs, terminals, agent conversations, attached projects, the selected run configuration and breakpoints. |
| `scratch/` | Scratchpads: SQL consoles and notes, with the connection and database each console uses. |
| `history/` | Query history, one file per database connection. |

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

[Flint Remote](../guides/remote.md) keeps a SHA-256 hash of each paired device's token (never the token) under the service `flint-remote`, one item per device. It is deleted when you revoke the device.

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
| GitHub sign-in | `gh` (macOS keyring or `GH_TOKEN`). Flint never sees the token. |
| AWS credentials | `~/.aws/` and the AWS tools. Flint only checks that credentials exist and when they expire. |
| Kubernetes credentials | Your kubeconfig. Flint's Kubernetes terminals add an override file with only the context and namespace. |
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
