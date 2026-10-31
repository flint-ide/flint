# Privacy and security

Flint runs on your Mac and talks to the outside world only through tools you have installed and actions you take. This page explains what leaves your machine and when, how secrets are handled, and how to report a vulnerability.

## No telemetry

Flint has no telemetry, analytics or crash reporting service. It sends nothing automatically, and it has no account or server of its own.

## What leaves your machine, and when

Data leaves your Mac only when you do something that needs it:

| Action | What is sent | Where |
|---|---|---|
| Agent chat, Generate message, Resolve with agent, Diagnose with AI | Your messages, the context you attach, and whatever the agent reads | Your AI provider, through the `claude` or `codex` CLI and your own account |
| Pull request review | Requests for pull requests, and your comments and reviews when you post them | GitHub, through the `gh` CLI |
| Help ▸ Report an Issue… | A prefilled issue that you review in your browser before submitting | GitHub, only when you submit |
| HTTP client | The request you send | The server in the request |
| Databases, Kubernetes, AWS, Docker | The commands and queries you run | Your servers and cloud accounts, through your own credentials |
| Markdown preview | Requests for remote images, only after you click **Load remote images** | The image hosts |

Agents decide what they read and send to their provider. The [approval policy](../guides/ai-agents.md#what-the-agent-may-do-without-asking) controls what they may do without asking.

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
