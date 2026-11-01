# Restricted Mode

Restricted Mode lets you open, read and search a folder you don't fully trust yet, without Flint running any of its code on its own. This guide explains what is restricted and why, how to trust and revoke a folder, and which protections apply everywhere.

## Why it exists

A downloaded archive or a fresh clone can carry configuration that runs programs: Git hooks, shell prompt hooks, language server build scripts, agent hooks. In a normal IDE, simply opening such a folder can run that code. Restricted Mode makes sure nothing in the folder runs until you decide you trust it.

## When it applies

A folder you open for the first time is untrusted. A **Restricted Mode** banner appears above the tabs with a **Trust Folder** button. Closing the banner with ✕ only hides it; the folder stays restricted.

Trust covers the folder and all its subfolders. With several projects in the window, each one is trusted separately.

A worktree you create from the branch widget is a new folder and starts untrusted, even when its repository is trusted. The worktree of an [agent task](ai-agents.md#agent-tasks-agents-in-parallel) is the exception: it is trusted when the checkout the task started from is trusted.

## What is restricted

In an untrusted folder, Flint:

| Feature | In Restricted Mode | Why |
|---|---|---|
| Terminal | Doesn't open one automatically (you can still open one yourself) | Shell prompts run `git status` and version-manager hooks that read the folder |
| Rust language server | Doesn't start | rust-analyzer runs build scripts, proc macros and tools from `.cargo/config` |
| Python language servers | Don't start | They run the virtualenv interpreter the project points at |
| Agents (chat and commit messages) | Run without the project's agent settings and hooks, and without your extra agent arguments from Settings | Claude Code gets `--setting-sources user`, because its non-interactive mode skips its own trust prompt; an argument such as `--mcp-config` with a relative path would load the project's files |
| Custom ACP agents | Don't start; Flint asks you to trust the folder | Flint can't tell an arbitrary agent to ignore the project's configuration (its settings, MCP servers, hooks) |
| Language servers set in Settings | A server Flint doesn't know doesn't start; extra arguments are left out | Flint can't tell what another server runs |
| Git | Runs without hooks, even on commit and push | Hooks are programs from the repository |

Everything else works: browsing, editing, `⌘P`, search, diffs, blame, the Go and TypeScript language servers, and the Markdown preview.

## Trusting a folder

Do one of these:

- Click **Trust Folder** in the banner.
- Run "Trust Folder" from the command palette (`⇧⌘P`).

The language servers that were held back start right away, and if the project has no terminal yet, Flint opens one in the folder.

> **Tip:** Trust a folder when you trust its authors, not just because you want a feature. If you only need to read the code, staying in Restricted Mode costs you little.

## Revoking trust

Run "Revoke Folder Trust (Restricted Mode)" from the command palette. Restricted Mode is back on for that folder.

Trusted folders are remembered in `state.json` in Flint's data folder (see [Files and data](../reference/files-and-data.md)).

## Protections that always apply

These apply whether or not you trust the folder:

- **Git filters.** Flint's own background Git calls (status, blame, diff) never run clean/smudge filters, textconv drivers or fsmonitor defined by the repository. Filters from your global Git configuration, such as Git LFS, still work.
- **TypeScript.** typescript-language-server starts only when it can use its own TypeScript, never the project's `node_modules/typescript`. vtsls uses its bundled TypeScript.
- **HTTP client.** A `.http` request can send body files (`< file`) only from inside the open project.
- **Markdown preview.** Remote images aren't loaded until you click **Load remote images**, so no tracking pixels. Raw HTML and `javascript:` links are dropped.
- **Databases.** TLS connections verify the server certificate and host name. "Don't verify certificate (insecure)" is an explicit opt-out per connection.
- **Terminal sessions.** Credentials are removed from terminal output before a session is saved.

## Related

- [Onboard to a new repository](../use-cases/onboard-to-a-new-repository.md)
- [Privacy and security](../reference/privacy-and-security.md)
