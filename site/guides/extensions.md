# Extensions

An extension is a separate program that adds commands, a sidebar panel, a Settings page, status bar items and tools for agents to Flint. It can be written in any language. Some extensions are only data: color themes, or a programming language with its language server, debugger and test runner (**Debug File** and **Run Tests in File** in the command palette use them). Flint draws everything an extension shows with its own components, from data the extension sends: an extension never runs code inside Flint's window.

There is no marketplace. **Settings ▸ Extensions** lists every extension of the [flint-ide/extensions](https://github.com/flint-ide/extensions) repository, and you can also install one from a folder on your disk. Either way, Flint shows you what it is and what it asks for first. To write one, see [Write an extension](../extensions/write-an-extension.md).

## Install an extension

### From the repository

**Settings ▸ Extensions ▸ Repository** shows the extensions of `flint-ide/extensions`, each marked *Not installed*, *Installed* or *Update available*. Flint reads the repository with `git` and your own credentials (credential helper or SSH key) into its cache folder, the first time you open the page; **Refresh Repository** fetches it again. Set `FLINT_EXTENSIONS_REPO` to use a fork, a mirror or a local clone instead.

**Install…** (or **Update…**) builds an extension that is a Go program on your machine with `go build` (Go must be installed), then shows the same confirmation as installing from a folder. A theme-only extension needs no build. **Uninstall…** removes it.

### From a folder

1. Get the extension's folder: it contains `flint-extension.json` (the manifest) and the extension's program.
2. Run **Install Extension from Folder…** from the command palette, or click **Install from Folder…** in **Settings ▸ Extensions**, and pick the folder.
3. Flint shows a confirmation with the extension's name, version, publisher, id, the program it runs, when it starts, the permissions it declares and the tools it offers to agents. Click **Install** only if you trust it.

Flint copies the folder to `extensions/<id>/` in its [configuration folder](../reference/files-and-data.md). Installing an extension with the same id again replaces it (the old version is stopped first) and keeps its settings.

Nothing starts when you install: the extension's process starts the first time you use one of its commands, open its panel, or an agent calls one of its tools. An extension whose manifest says `onStartup` starts once Flint's window is up, and the confirmation says so.

## Settings ▸ Extensions

For each installed extension:

- **The checkbox** turns it on or off. Off stops its process and removes its commands, panel, Settings page and status items. It stays installed.
- **The state**: *Not running*, *Starting…*, *Running*, *Crashed* (it ended on its own; the next use starts it again), *Stopped after repeated crashes* (three crashes in ten minutes; click **Restart** to try again), *Off*, or *Broken* (the manifest is wrong; the reason is shown).
- **Program** and **Permissions**, as declared in its manifest.
- **Agent tools**: the names agents see (for example `stats_summary`) and **Offer to agents**, which you can untick to keep the tools away from agents.
- **Show Log**: what the extension wrote to its standard error, its log messages, and when Flint started and stopped it. The log is kept in memory (the last 256 KiB) until Flint quits.
- **Uninstall…** stops the extension and deletes its folder after you confirm. Its settings stay in `settings.json` and any secrets it stored stay in the system keychain, so installing it again restores them.

**Built-in** lists the parts of Flint you can switch off, such as Database. A switched-off one isn't started, and the change takes effect after a restart (see [features](../reference/settings.md#features)).

**Reload Installed** reads the extensions folder again, for example after you rebuilt an extension you are writing.

An extension with settings gets its own page in Settings, after Flint's pages, generated from its manifest. Values are checked against the manifest before they are saved, under `extensions` in [settings.json](../reference/settings.md#extensions).

## Shortcuts

An extension's command can come with a default shortcut. Flint never lets it replace one that already exists: when the keys are taken by Flint or by your keymap, the command gets no shortcut and Settings ▸ Extensions says why. You can bind any extension command in **Settings ▸ Keymap**, like Flint's own.

## What an extension can and can't do

An extension is a program running with your user's permissions. Flint can't confine what that program does on its own, so install only extensions you trust, as you would any program.

What it asks **Flint** for is checked:

- It sees only [trusted](restricted-mode.md) project folders. Folders in Restricted Mode are never passed to it, and it can't read, open or attach files in them.
- Reading files through Flint (`workspace.read`), keeping secrets in the keychain (`secrets`) and writing an agent chat draft (`agent.ask`) need permissions declared in its manifest. Flint refuses the request otherwise.
- Files inside `.git` and credential files such as `.env`, `.netrc` or private keys are never returned to it.
- Links it opens are limited to `http` and `https`, and Flint asks you the first time it opens a link to a given site.
- Its secrets are stored in the system keychain under its own name and are never in `settings.json`; it can't read another extension's.
- An agent chat draft is only a draft: you send it.
- Its tools for agents are read-only from Flint's point of view, are offered only to agents in trusted folders while Flint's MCP server is on (**Settings ▸ Agent Context**), and can be turned off per extension.
- Its process gets a minimal environment: `PATH`, `HOME`, `USER`, `LOGNAME`, `SHELL`, `TMPDIR`, `TZ`, the locale and proxy variables, plus `FLINT_EXTENSION_ID`, `FLINT_EXTENSION_DIR`, `FLINT_EXTENSION_DATA` (its own data folder) and `FLINT_VERSION`. Other variables, such as `KUBECONFIG`, reach it only when its manifest names them in `run.env`; the confirmation lists them. Tokens and keys in Flint's own environment (such as `GITHUB_TOKEN`) are not passed unless named there.
- Opening terminal tabs (`terminal`) also needs a declared permission. A terminal it opens is a normal tab in a trusted folder, so you see what runs and can stop it.
- Network hosts declared in its manifest (`network:<host>`) are shown so you know where it talks to, but Flint does not enforce them.

Flint itself sends nothing about extensions anywhere. See [Privacy and security](../reference/privacy-and-security.md#extensions).

Project-local extensions (in a repository's own folder) are not supported: every extension is one you installed yourself.
