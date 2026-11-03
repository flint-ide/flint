# Privacy and security

Flint runs on your Mac and talks to the outside world only through tools you have installed and actions you take. This page explains what leaves your machine and when, how secrets are handled, and how to report a vulnerability.

## No telemetry

Flint has no telemetry, analytics or crash reporting service, and it has no account or server of its own. The only request it makes on its own is the [update check](#update-check), which you can turn off.

## What leaves your machine, and when

Apart from the update check, data leaves your Mac only when you do something that needs it:

| Action | What is sent | Where |
|---|---|---|
| Agent chat, Generate message, Review Changes with Agent, Resolve with agent, Diagnose with AI | Your messages, the context you attach, and whatever the agent reads | Your AI provider, through the `claude` or `codex` CLI or a custom ACP agent you added, and your own account |
| Pull requests, reviews and CI (while a view needs them, and while a check of your pull request runs) | API requests with your GitHub token; your comments, reviews, pull requests and re-runs when you post them | GitHub's API of the configured host (`api.github.com`, or your Enterprise server) |
| Sign in with GitHub | The OAuth App's client ID and scopes, then a poll for the token | The configured GitHub host (`github.com/login/…`) |
| Downloading a failed job's log | A request to the short-lived URL GitHub redirects to, **without** your token | The host GitHub names (for example `*.actions.githubusercontent.com` or `*.blob.core.windows.net`) |
| Settings ▸ Accounts | Your avatar, loaded by the window | `avatars.githubusercontent.com` |
| Help ▸ Report an Issue… | A prefilled issue that you review in your browser before submitting | GitHub, only when you submit |
| HTTP client | The request you send | The server in the request |
| Databases, and the Kubernetes, AWS and Docker extensions | The commands and queries you run | Your servers and cloud accounts, through your own credentials |
| Settings ▸ Extensions (when first opened, and **Refresh Repository**) | A `git` clone or fetch of the extensions repository, with your own git credentials | GitHub (`github.com/flint-ide/extensions`), or the URL in `FLINT_EXTENSIONS_REPO` |
| Markdown preview | Requests for remote images, only after you click **Load remote images** | The image hosts |
| Update check (on its own once a day, or Help ▸ Check for Updates…) | One anonymous request for the latest release, described below | GitHub's API (`api.github.com`) |

Agents decide what they read and send to their provider. The [approval policy](../guides/ai-agents.md#what-the-agent-may-do-without-asking) controls what they may do without asking.

## Update check

Flint looks for a newer release so it can tell you about it. It never downloads or installs anything itself.

- **What is requested**: `GET https://api.github.com/repos/flint-ide/flint/releases/latest`, GitHub's public endpoint for the latest published release (drafts and pre-releases are left out).
- **What is sent**: only the request itself, with the headers `Accept: application/vnd.github+json`, `X-GitHub-Api-Version` and `User-Agent: Flint/<version>` (for example `Flint/v1.3.0`). No account, token, cookie, machine identifier or information about your projects. Like any web request, it shows your IP address to GitHub.
- **When**: a few seconds after Flint starts and then once a day while it runs, at most once every 24 hours. The time of the last check, the release it found and a version you skipped are kept in `state.json`. Development builds (version `dev`) never check on their own.
- **What you see**: when a newer release exists, a notice in the status bar with **Download**, which opens the release page on `github.com/flint-ide/flint` in your browser, **Skip This Version** and **✕** (hide until the next launch). If the check fails, for example when you are offline, Flint only writes a line to its log.
- **Turning it off**: untick **Settings ▸ Updates ▸ Check for new releases**, or set `"checkForUpdates": false` in [`settings.json`](settings.md). Flint then makes no request on its own; **Help ▸ Check for Updates…** (also in the command palette) still checks when you ask.

## Secrets

- **Database passwords** are stored only in the macOS Keychain (service `flint-db`), never in files.
- **GitHub tokens** (from Sign in with GitHub, or imported once from the GitHub CLI after you confirm) are stored only in the Keychain (service `flint-accounts`, account `github:<host>`). The token is used by Flint's native process only: it never reaches the user interface, the log or an error message, and it is sent only to the GitHub API of the configured host, never to a redirect on another host (job logs are downloaded from their redirect without it). `state.json` keeps the account's login, avatar, host and scopes, no token. **Sign Out** deletes the token; revoke it on GitHub's authorized apps page.
- **`GH_TOKEN` / `GITHUB_TOKEN`** (or `GH_ENTERPRISE_TOKEN` for an Enterprise Server) in Flint's environment is used while no account is signed in and is never stored.
- **HTTP client** headers and variable values are never written to the log. Responses and cookies stay in memory.
- **Terminal sessions** are saved with recognized credentials removed from the output.
- Settings, connections, query history, sessions and the log are readable only by your user.

## Redaction

Before text reaches the log file, a bug report, the diagnostics report, or context Flint prepares for an agent (such as Diagnose with AI), Flint removes credentials it recognizes and replaces your home folder path with `~`. Recognized credentials include private keys, AWS access key IDs, GitHub and Slack tokens, API keys, JWTs, bearer and basic auth headers, credentials in URLs, and `password=`-style assignments.

Query history masks passwords in `CREATE/ALTER ROLE … PASSWORD` and ClickHouse `IDENTIFIED … BY` statements.

> **Note:** Redaction is pattern-based. Always review a bug report or an agent draft before sending it.

## Extensions

[Extensions](../guides/extensions.md) are programs you install yourself, from the [flint-ide/extensions](https://github.com/flint-ide/extensions) repository in Settings ▸ Extensions or from a folder, after a confirmation that shows the program, its permissions and the tools it offers agents. Flint never installs, updates or enables one on its own, and sends nothing about them anywhere.

- An extension runs with your user's permissions. Flint can't confine the program itself, so install only extensions you trust.
- Flint checks what an extension asks of it against the permissions in its manifest: reading files (`workspace.read`), keychain secrets (`secrets`), agent chat drafts (`agent.ask`), terminal tabs (`terminal`). It never passes folders in Restricted Mode, never returns files inside `.git` or credential files such as `.env`, and asks before the first link an extension opens to each site.
- Declared network hosts (`network:<host>`) are shown, not enforced.
- An extension's secrets are in the keychain (service `flint-extensions`, account `<id>:<key>`), never in `settings.json`, and one extension can't read another's.
- Its process gets a minimal environment, plus only the variables its manifest names in `run.env` (the confirmation lists them), so no tokens or keys from Flint's own environment unless named there, and runs in its own process group, which Flint kills when it stops it or quits.
- Its tools for agents are offered only in trusted folders while Flint's MCP server is on, and can be turned off per extension.
- Everything it shows is drawn by Flint from plain data; it can't run code in Flint's window.

## Untrusted content

- Folders you open for the first time are in [Restricted Mode](../guides/restricted-mode.md) until you trust them, so opening a repository doesn't run its code.
- Markdown, pull request descriptions and comments are rendered without raw HTML, and `javascript:` links are dropped.
- HTTP response bodies are shown as text in a read-only editor, never rendered as HTML.
- Names that come from the UI or from tool output (containers, contexts, namespaces, pods) are validated before Flint passes them to a command.
- Extensions are installed only when you choose to, from the extensions repository or a folder you pick, after a dialog that Flint's native code shows (code in the window can't answer it) with the extension's program and permissions. What an extension adds is data that Flint draws with its own components: it can't run code in the window. Its theme colors pass the same check as an imported theme (known color names, hex values only). Symlinks inside an extension's folder are refused when it is installed.

## Reporting a vulnerability

Don't open a public issue for security problems. Use GitHub's private reporting instead:

1. Go to the [Security tab](https://github.com/flint-ide/flint/security) of the repository.
2. Click **Report a vulnerability** and fill in the form.

Include the Flint and macOS versions (**Help ▸ Copy Diagnostics** gives you a redacted report), a description of the issue and its impact, and steps to reproduce. Don't include real secrets.

You can expect an acknowledgement within 3 days and a fix, or a plan with a timeline, within 30 days. Only the latest release receives security fixes, so check that the issue reproduces there first.

The full policy, including what is in scope, is in [SECURITY.md](https://github.com/flint-ide/flint/blob/main/SECURITY.md).
