# Report a problem

Recipes for reporting bugs with the information needed to fix them, and for reporting security issues privately.

## Report a bug

1. Choose **Help ▸ Report an Issue…** (or "Report an Issue…" in the command palette).
2. Your browser opens a new GitHub issue, prefilled with diagnostics. The full report is also copied to the clipboard.
3. Describe what you did, what you expected and what happened instead.
4. Review the prefilled text, then submit the issue.

Nothing is sent until you submit the issue yourself. If the prefilled text is cut short, paste the full version from the clipboard.

You can also open an issue directly on [GitHub](https://github.com/ODudek/flint/issues).

## Copy diagnostics

To share the same information somewhere else (a chat, an email, an existing issue):

1. Choose **Help ▸ Copy Diagnostics**.
2. Paste it where you need it.

## Look at the log

1. Choose **Help ▸ Show Logs in Finder**.
2. Open `flint.log` in any text editor.

The log is at `~/Library/Logs/Flint/flint.log`. It records Flint's own messages, interface errors and crashes, and rotates at 5 MB.

## What is included

- The versions of Flint, macOS, Go and Wails.
- The versions of installed tools: git, rg, gh, docker, kubectl, aws, dlv, gopls, node, claude and codex.
- The last 150 lines of the log.

## What is redacted

Before the text is created, Flint removes credentials it recognizes (private keys, AWS key IDs, GitHub and Slack tokens, API keys, JWTs, bearer and basic auth, credentials in URLs, `password=`-style values) and replaces your home folder path with `~`. The log file itself is redacted the same way.

> **Tip:** Redaction is automatic but pattern-based. Read the text before you submit it, and remove anything you don't want to share, such as internal host names or project names.

## Report a security issue

Don't open a public issue for a vulnerability.

1. Go to the [Security tab](https://github.com/ODudek/flint/security) of the repository.
2. Click **Report a vulnerability**.
3. Fill in the form. Include the versions from **Help ▸ Copy Diagnostics**, a description of the impact, and steps to reproduce. Don't include real secrets.

See [Privacy and security](../reference/privacy-and-security.md#reporting-a-vulnerability) for response times.
