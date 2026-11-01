# Security Policy

## Supported versions

Only the latest release of Flint receives security fixes. Please check that you can reproduce an issue on the latest release before reporting it.

## Reporting a vulnerability

**Do not open a public issue for security vulnerabilities.**

Use GitHub private vulnerability reporting:

1. Go to the [Security tab](https://github.com/flint-ide/flint/security) of the repository.
2. Click **Report a vulnerability**.
3. Fill in the form.

Please include:

- the Flint version (Help ▸ Copy Diagnostics gives you a redacted report with versions) and the macOS version;
- a description of the issue and its impact;
- steps to reproduce, ideally with a minimal repository, file or server response that triggers it;
- a proof of concept, if you have one;
- any suggested fix or mitigation.

Do not include real secrets or credentials in the report.

## Response targets

- Acknowledgement within **3 days**.
- A fix, or a plan with a timeline, within **30 days**.

You will be kept informed of progress. Once a fix is released, the advisory is published and you are credited unless you prefer otherwise.

## Scope

In scope:

- **Code execution from opening an untrusted folder**: anything a repository can do to run code when it is opened, browsed or searched in Flint, especially in Restricted Mode, and any git filter, textconv driver or fsmonitor run by Flint's own background git calls.
- **XSS in the webview**: script execution through rendered content (Markdown, pull request descriptions and comments, HTTP responses, terminal output, file names), or bypasses of the Content Security Policy. Script execution in the webview gives access to every Go binding, so it is treated as code execution.
- **Secret leakage**: credentials, tokens or passwords written to logs, bug reports, diagnostics, session files, agent prompts, or exposed to the UI when they should stay in Go or the Keychain.
- **Command injection**: user- or repository-controlled values reaching a CLI (git, gh, docker, kubectl, aws, agents) as flags or shell code.

Out of scope:

- **The dev-only server mode** (`make server` / `wails3 task server`). It exposes every binding on localhost with no authentication by design and must not be used outside development.
- Issues that require local administrator access, or an attacker who already controls your user account.
- Behavior of third-party tools Flint launches (git, gh, kubectl, language servers, agents) that is not caused by how Flint calls them.
- Actions an agent takes after you approve them, or in a folder you chose to trust.

The threat model is described in [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md#security-model).
