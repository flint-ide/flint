# Privacy and security

Flint runs on your Mac and talks to the outside world only through tools you have installed and actions you take. This page explains what leaves your machine and when, how secrets are handled, and how to report a vulnerability.

## No telemetry

Flint has no telemetry, analytics or crash reporting service, and it has no account or server of its own. The only request it makes on its own is the [update check](#update-check), which you can turn off.

## What leaves your machine, and when

Apart from the update check, data leaves your Mac only when you do something that needs it:

| Action | What is sent | Where |
|---|---|---|
| Agent chat, Generate message, Review Changes with Agent, Resolve with agent, Diagnose with AI | Your messages, the context you attach, and whatever the agent reads | Your AI provider, through the `claude` or `codex` CLI or a custom ACP agent you added, and your own account |
| Pull request review | Requests for pull requests, and your comments and reviews when you post them | GitHub, through the `gh` CLI |
| Help ▸ Report an Issue… | A prefilled issue that you review in your browser before submitting | GitHub, only when you submit |
| HTTP client | The request you send | The server in the request |
| Databases, Kubernetes, AWS, Docker | The commands and queries you run | Your servers and cloud accounts, through your own credentials |
| Markdown preview | Requests for remote images, only after you click **Load remote images** | The image hosts |
| Update check (on its own once a day, or Help ▸ Check for Updates…) | One anonymous request for the latest release, described below | GitHub's API (`api.github.com`) |

Agents decide what they read and send to their provider. The [approval policy](../guides/ai-agents.md#what-the-agent-may-do-without-asking) controls what they may do without asking.

## Update check

Flint looks for a newer release so it can tell you about it. It never downloads or installs anything itself.

- **What is requested**: `GET https://api.github.com/repos/ODudek/flint/releases/latest`, GitHub's public endpoint for the latest published release (drafts and pre-releases are left out).
- **What is sent**: only the request itself, with the headers `Accept: application/vnd.github+json`, `X-GitHub-Api-Version` and `User-Agent: Flint/<version>` (for example `Flint/v1.3.0`). No account, token, cookie, machine identifier or information about your projects. Like any web request, it shows your IP address to GitHub.
- **When**: a few seconds after Flint starts and then once a day while it runs, at most once every 24 hours. The time of the last check, the release it found and a version you skipped are kept in `state.json`. Development builds (version `dev`) never check on their own.
- **What you see**: when a newer release exists, a notice in the status bar with **Download**, which opens the release page on `github.com/ODudek/flint` in your browser, **Skip This Version** and **✕** (hide until the next launch). If the check fails, for example when you are offline, Flint only writes a line to its log.
- **Turning it off**: untick **Settings ▸ Updates ▸ Check for new releases**, or set `"checkForUpdates": false` in [`settings.json`](settings.md). Flint then makes no request on its own; **Help ▸ Check for Updates…** (also in the command palette) still checks when you ask.

## Secrets

- **Database passwords** are stored only in the macOS Keychain (service `flint-db`), never in files.
- **AWS keys and tokens** are read only by Flint's native process to check that they exist and when they expire. They never reach the user interface.
- **GitHub sign-in** stays inside `gh`. Flint never sees your token.
- **Kubernetes terminals** use an override file with only the context and namespace; no credentials are copied.
- **HTTP client** headers and variable values are never written to the log. Responses and cookies stay in memory.
- **Terminal sessions** are saved with recognized credentials removed from the output.
- Settings, connections, query history, sessions and the log are readable only by your user.

## Redaction

Before text reaches the log file, a bug report, the diagnostics report, or context Flint prepares for an agent (such as Diagnose with AI), Flint removes credentials it recognizes and replaces your home folder path with `~`. Recognized credentials include private keys, AWS access key IDs, GitHub and Slack tokens, API keys, JWTs, bearer and basic auth headers, credentials in URLs, and `password=`-style assignments.

Query history masks passwords in `CREATE/ALTER ROLE … PASSWORD` and ClickHouse `IDENTIFIED … BY` statements.

> **Note:** Redaction is pattern-based. Always review a bug report or an agent draft before sending it.

## Untrusted content

- Folders you open for the first time are in [Restricted Mode](../guides/restricted-mode.md) until you trust them, so opening a repository doesn't run its code.
- Markdown, pull request descriptions and comments are rendered without raw HTML, and `javascript:` links are dropped.
- HTTP response bodies are shown as text in a read-only editor, never rendered as HTML.
- Names that come from the UI or from tool output (containers, contexts, namespaces, pods) are validated before Flint passes them to a command.

## Reporting a vulnerability

Don't open a public issue for security problems. Use GitHub's private reporting instead:

1. Go to the [Security tab](https://github.com/ODudek/flint/security) of the repository.
2. Click **Report a vulnerability** and fill in the form.

Include the Flint and macOS versions (**Help ▸ Copy Diagnostics** gives you a redacted report), a description of the issue and its impact, and steps to reproduce. Don't include real secrets.

You can expect an acknowledgement within 3 days and a fix, or a plan with a timeline, within 30 days. Only the latest release receives security fixes, so check that the issue reproduces there first.

The full policy, including what is in scope, is in [SECURITY.md](https://github.com/ODudek/flint/blob/main/SECURITY.md).
