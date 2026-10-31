# FAQ

## General

### Is Flint free?

Flint is open source under the MIT license. The agent CLIs you use with it (Claude Code, Codex) have their own terms and pricing.

### Does Flint run on Windows or Linux?

No. Flint is a macOS app and needs macOS 12 or later. Release builds run on both Apple silicon and Intel Macs.

### Is Flint built on Electron?

No. Flint is a single native process that uses the macOS system WebView (WKWebView), which keeps it fast to start and light on memory.

### Does Flint collect telemetry?

No. Flint sends nothing automatically. See [Privacy and security](privacy-and-security.md).

### Does Flint update itself?

No. Download new versions from the [releases page](https://github.com/ODudek/flint/releases) and replace the app. See [Updating](../installation.md#updating).

## Agents

### Which AI agents does Flint support?

Claude Code (`claude`) and Codex (`codex`). Install the CLI and sign in to it; Flint uses it with your own account and configuration.

### Do I need an API key in Flint?

No. Flint has no account or key of its own. The agent CLIs handle authentication.

### Does Flint send my code to an AI provider?

Only through the agent you use, and only when you start a chat, send a message, generate a commit message or resolve conflicts with it. What the agent reads and sends is up to the agent and its provider.

### Do I have to approve every edit?

No. By default the agent may edit and read files inside the project without asking, and asks before running commands. You review the result in the Commit panel and roll back what you don't want. You can change this with the policy link in the chat header; the choice applies to all chats. See [AI agents](../guides/ai-agents.md#what-the-agent-may-do-without-asking).

### Can I keep using the agent in a terminal?

Yes. Run `claude` or `codex` in any terminal tab. `⌘`-click on `path:line` in its output opens the file, and `⇧↵` inserts a new line in its prompt.

## Projects

### Can I open several projects in one window?

Yes. Use "Add Project to Workspace…" (`⇧⌘O`). Search, `⌘P`, terminals, chats and run configurations all work across the attached projects. See [Open projects](../how-to/open-projects.md#add-a-second-project-to-the-window).

### Can I open a project in a new window?

Yes. In Recent Projects (`⌘O`), select the project and press `⌘↵`.

### Why does a folder open in Restricted Mode?

Because you haven't trusted it yet. Restricted Mode stops the folder's code from running on its own until you decide you trust it. See [Restricted Mode](../guides/restricted-mode.md).

### Does Flint index my project?

No. `⌘P` and search use ripgrep directly, so there is no indexing step and nothing to wait for.

## Features

### Which languages have code intelligence?

Go, Rust, TypeScript/JavaScript and Python, through their language servers. Install the server for your language; see [Troubleshooting](troubleshooting.md).

### Which languages can I debug?

Go, through Delve. Tests run for Go and JavaScript/TypeScript; other test runners report only success or failure.

### Which databases are supported?

PostgreSQL and ClickHouse.

### Where are my database passwords?

In the macOS Keychain, never in files. See [Files and data](files-and-data.md#keychain).

### Can Flint change my current Kubernetes context?

Only when you click ★ in the Kubernetes panel. Everything else passes an explicit context and namespace and leaves your kubeconfig alone.

### Are my HTTP client tokens safe?

Keep them in `http-client.private.env.json` and add that file to `.gitignore`. Flint doesn't write headers or variable values to its log, and keeps responses in memory only.

## Settings

### Can I change keyboard shortcuts?

Yes, in Settings ▸ Keymap. Shortcuts handled by the editor itself (such as `⌘F` and `⌘S`) and the macOS menu keys can't be changed. See [Customize Flint](../how-to/customize-flint.md#rebind-a-keyboard-shortcut).

### Can I sync my settings between Macs?

Copy `~/Library/Application Support/flint/settings.json` to the other Mac. It contains no secrets.
