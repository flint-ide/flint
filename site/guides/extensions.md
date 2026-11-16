# Extensions

An extension is a separate program that adds commands, sidebar panels and editor tabs (trees, tables, forms and charts), a Settings page, status bar items, marks in the editor and tools for agents to Flint. It can be written in any language. Some extensions are only data: color themes, or a programming language with its language server, debugger and test runner (**Debug File** and **Run Tests in File** in the command palette use them). Flint draws everything an extension shows with its own components, from data the extension sends: an extension never runs code inside Flint's window.

There is no marketplace. **Settings ▸ Extensions** (**Show Extensions** in the command palette, or `⌘,` ▸ **Extensions**) offers the extensions of the [flint-ide/extensions](https://github.com/flint-ide/extensions) repository, prebuilt from its signed [extension index](#from-the-extension-index) or built from its sources, and you can also install one from a folder on your disk. Either way, Flint shows you what it is and what it asks for first. To write one, see [Write an extension](../extensions/write-an-extension.md).

## Install an extension

### From the extension index

**Settings ▸ Extensions ▸ Extension index** installs prebuilt extensions, so you need neither `git` nor Go. The index is one signed list published by `flint-ide/extensions`; it is not a marketplace, and nobody else can add to it.

1. Click **Browse Extensions** (or run **Browse Extensions** from the command palette). Flint downloads the index and its signature and checks the signature against the public key built into Flint. Nothing is shown from an index that fails the check.
2. Each extension shows its version, kind, download size, permissions and agent tools. Click **Install…**.
3. Flint downloads the archive for your Mac or Linux machine, checks that it is exactly the file the index describes (size and SHA-256), unpacks it, and checks that it holds the extension and version the index lists. Then it shows the same confirmation as for a folder. Click **Install** only if you trust it.

**Check for Updates** (or **Check for Extension Updates** in the command palette) downloads the index again. An installed extension with a newer version in it shows **Update to x.y.z…**, on its card under **Installed** too. The confirmation of an update starts with what changes: the version, and every permission and agent tool the new version adds.

Flint contacts the index only when you click one of these buttons or run one of these commands, never on its own, and it never updates anything by itself. Extensions you installed from a folder are left alone unless you click **Update** on them.

The official index's public key is not built into Flint yet. Until it is, this section says the index is not configured and requests nothing; use the repository below or a folder.

To use an index of your own (for example your company's), set **Index address** and **Index public key** below the list; see [The extension index](../extensions/index-format.md#your-own-index).

### From the repository

**Settings ▸ Extensions ▸ Repository** shows the extensions of `flint-ide/extensions`, each marked *Not installed*, *Installed* or *Update available*. Flint reads the repository with `git` and your own credentials (credential helper or SSH key) into its cache folder, the first time you open the page; **Refresh Repository** fetches it again. Set `FLINT_EXTENSIONS_REPO` to use a fork, a mirror or a local clone instead.

**Install…** (or **Update…**) builds an extension that is a Go program on your machine with `go build` (Go must be installed), then shows the same confirmation as installing from a folder. A theme-only extension needs no build. **Uninstall…** removes it.

### From a folder

1. Get the extension's folder: it contains `flint-extension.json` (the manifest) and the extension's program.
2. Run **Install Extension from Folder…** from the command palette, or click **Install from Folder…** in **Settings ▸ Extensions**, and pick the folder. From a terminal, `flint --install-extension <folder>` does the same: Flint opens (or the running Flint comes to the front) and shows the confirmation. The flag never installs without it.
3. Flint shows a confirmation with the extension's name, version, publisher, id, the program it runs, when it starts, the permissions it declares, the tools it offers to agents and its sandbox: what the program may read, write and connect to, or that it is not sandboxed. Click **Install** only if you trust it.

Flint copies the folder to `extensions/<id>/` in its [configuration folder](../reference/files-and-data.md). Installing an extension with the same id again replaces it (the old version is stopped first) and keeps its settings; the confirmation then starts with what changes from the installed version.

Nothing starts when you install: the extension's process starts the first time you use one of its commands, open its panel, open a file it marks in the editor, or an agent calls one of its tools. An extension whose manifest says `onStartup` starts once Flint's window is up, and the confirmation says so.

## Settings ▸ Extensions

For each installed extension:

- **The checkbox** turns it on or off. Off stops its process and removes its commands, panel, Settings page and status items. It stays installed.
- **The state**: *Not running*, *Starting…*, *Running*, *Crashed* (it ended on its own; the next use starts it again), *Stopped after repeated crashes* (three crashes in ten minutes; click **Restart** to try again), *Off*, or *Broken* (the manifest is wrong; the reason is shown).
- **Program** and **Permissions**, as declared in its manifest.
- **Editor decorations**: a checkbox per group of marks the extension draws in editors, with the files it is for. See [Marks in the editor](#marks-in-the-editor).
- **Sandbox**: *Sandboxed*, with what the program may read, write and connect to, what your system can't enforce and anything it asked for that Flint did not grant; or *Not sandboxed*, with the reason (see [The sandbox](#the-sandbox)).
- **Agent tools**: the names agents see (for example `stats_summary`) and **Offer to agents**, which you can untick to keep the tools away from agents.
- **Show Log**: what the extension wrote to its standard error, its log messages, and when Flint started and stopped it. The log is kept in memory (the last 256 KiB) until Flint quits.
- **Uninstall…** stops the extension and deletes its folder after you confirm. Its settings stay in `settings.json` and any secrets it stored stay in the system keychain, so installing it again restores them.

**Only run sandboxed extensions** (off by default, [`extensions.requireSandbox`](../reference/settings.md#extensions)) keeps Flint from starting any extension whose program would run unconfined. Such an extension stays installed, its card says why it isn't started, and using one of its commands or panels shows the same reason.

**Built-in** lists the parts of Flint you can switch off, such as Database. A switched-off one isn't started, and the change takes effect after a restart (see [features](../reference/settings.md#features)).

**Reload Installed** reads the extensions folder again, for example after you rebuilt an extension you are writing.

An extension with settings gets its own page in Settings, after Flint's pages, generated from its manifest. Values are checked against the manifest before they are saved, under `extensions` in [settings.json](../reference/settings.md#extensions).

## Panels and tabs

What an extension shows in a sidebar panel or an editor tab is a tree, a list, a table, a form or a chart that Flint draws itself from the extension's data, in your theme and with Flint's keys. (Some extensions show a page of their own instead, in a sandboxed frame; those look and behave as their authors made them.)

- **Refresh** (`↻` in the panel's or tab's header) asks the extension again. Extensions also refresh on their own.
- **Tables.** Click a column header to sort by it: once for ascending, again for descending, a third time for the extension's own order. Flint sorts the rows it has; the extension isn't asked again. With the table focused, `↑` `↓` `Page Up` `Page Down` `Home` and `End` move the selection, `↵` or a double click runs the row's command if it has one, a right click (or `Shift+F10`) opens the row's actions, and `⌘C` copies the row. **Copy** under the table copies all shown rows, tab-separated. A table shows at most 5,000 rows and says so when the extension sent more.
- **Forms.** Fill in the fields and press the button (or `↵` in a one-line field). Flint checks the values first and shows what is wrong under each field; the extension can refuse values too, with its own reasons. A password field is always empty when the form opens: Flint passes what you type to the extension and keeps no copy.
- **Charts** are line, bar or area charts in your theme's colors. Hover a bar or a point for its value, or open **Values** under the chart for all of them as a table.
- An extension's tabs can come back with your session, like file tabs; such a tab loads its content when you first switch to it.

## Marks in the editor

An extension such as a linter or a coverage tool can mark lines in the files you have open: a marker in the gutter, a wavy underline with a message when you hover it, dim text after the end of a line, or a highlighted range. The colors are your theme's. Each extension groups its marks into one or more sets, for the files it names (a language, a file extension, a name pattern).

- **Turn a set on or off** with its checkbox under **Editor decorations** in Settings ▸ Extensions, or with **Toggle … in Editors** in the command palette. Turning it off removes its marks at once. A set can be off until you turn it on, if its extension says so.
- **Hide all marks in one editor** with **Toggle Extension Decorations in This Editor**; the same command shows them again.
- **Quick fixes.** A mark can offer fixes: click its marker in the gutter, use the buttons in its hover, or press `⌥⇧↵` (**Show Extension Quick Fixes**) for a list of the fixes on the cursor's line (arrow keys and `↵` choose, `Esc` closes). A fix changes the file in one step that `⌘Z` undoes, and only if the file hasn't changed since the extension looked at it; otherwise Flint says so and you try again.
- The marks follow your typing until the extension updates them. They disappear when the extension stops, crashes, is turned off or is uninstalled.

An extension with a set that is on starts when you open a file the set is for, and keeps running while such a file is open. Flint tells it only about files in [trusted](restricted-mode.md) folders, never about files inside `.git` or credential files such as `.env`. It gets the file's path and reads the saved file through Flint; it sees what you have typed but not saved only if its set asks for that, which the install confirmation and Settings ▸ Extensions say ("sees their text as you type").

## Shortcuts

An extension's command can come with a default shortcut. Flint never lets it replace one that already exists: when the keys are taken by Flint or by your keymap, the command gets no shortcut and Settings ▸ Extensions says why. You can bind any extension command in **Settings ▸ Keymap**, like Flint's own.

## What an extension can and can't do

An extension is a program on your machine. One that declares a [sandbox](#the-sandbox) is confined by the operating system to the files and network access the confirmation showed. One that doesn't runs with your user's permissions, and Flint can't confine what it does on its own. Either way, install only extensions you trust, as you would any program.

What it asks **Flint** for is checked:

- It sees only [trusted](restricted-mode.md) project folders. Folders in Restricted Mode are never passed to it, and it can't read, open or attach files in them.
- Reading files through Flint (`workspace.read`), keeping secrets in the keychain (`secrets`) and writing an agent chat draft (`agent.ask`) need permissions declared in its manifest. Flint refuses the request otherwise.
- Files inside `.git` and credential files such as `.env`, `.netrc` or private keys are never returned to it, and it is never told that one is open in an editor.
- Links it opens are limited to `http` and `https`, and Flint asks you the first time it opens a link to a given site.
- Its secrets are stored in the system keychain under its own name and are never in `settings.json`; it can't read another extension's.
- An agent chat draft is only a draft: you send it.
- Its tools for agents are read-only from Flint's point of view, are offered only to agents in trusted folders while Flint's MCP server is on (**Settings ▸ Agent Context**), and can be turned off per extension.
- Its process gets a minimal environment: `PATH`, `HOME`, `USER`, `LOGNAME`, `SHELL`, `TMPDIR`, `TZ`, the locale and proxy variables, plus `FLINT_EXTENSION_ID`, `FLINT_EXTENSION_DIR`, `FLINT_EXTENSION_DATA` (its own data folder) and `FLINT_VERSION`. Other variables, such as `KUBECONFIG`, reach it only when its manifest names them in `run.env`; the confirmation lists them. Tokens and keys in Flint's own environment (such as `GITHUB_TOKEN`) are not passed unless named there.
- Opening terminal tabs (`terminal`) also needs a declared permission. A terminal it opens is a normal tab in a trusted folder, so you see what runs and can stop it.
- Network hosts declared in its manifest (`network:<host>`) are shown so you know where it talks to, but Flint does not enforce them: the sandbox blocks the network as a whole or not at all.

Flint itself sends nothing about extensions anywhere. See [Privacy and security](../reference/privacy-and-security.md#extensions).

## The sandbox

An extension's manifest can ask Flint to confine its program. Flint then starts it inside the operating system's sandbox: Landlock and seccomp on Linux, `sandbox-exec` on macOS. Settings ▸ Extensions and the install confirmation show one of:

| Shown | Meaning |
|---|---|
| **Sandboxed** | The program is confined. The lines below say what it may read, write and connect to |
| **Not sandboxed (declared by the extension)** | The extension says it can't work confined, usually because it runs command-line tools that use your credentials (`kubectl`, `aws`) |
| **Not sandboxed: the extension doesn't declare a sandbox** | It was written before Flint had a sandbox, or not updated for it |
| **Not sandboxed: not available on this system (…)** | The extension asks to be confined, but your system can't: Linux without Landlock (before 5.13, or switched off in the kernel), or macOS without `sandbox-exec` |

A sandboxed program, unless its manifest declares more:

- **reads** only its own folder, the system's programs and libraries, and, with the `workspace.read` permission, your trusted project folders;
- **writes** only its own data folder and a private temporary folder;
- has **no network**: it can't open a connection of any kind;
- **starts no other program** than its own;
- can't read the rest of your home folder: `~/.ssh`, `~/.gnupg`, `~/.aws`, `~/.kube`, `~/.docker`, cloud SDK folders, browser profiles, keyrings and Keychains, shell history, Flint's own settings and other extensions' files.

What a manifest adds is listed in the confirmation and on the card: more folders to read or write, **Network: allowed**, **Local sockets: allowed** (services on your machine, such as Docker), **Other programs** (which then run in the same sandbox). Flint refuses some requests even when declared, and says so on the card: anything in its own configuration folder, and folders that contain protected ones, such as your whole home folder or `~/.config`.

What the sandbox does not do:

- It doesn't limit what the program does with what it may reach. An extension with `workspace.read` reads every file of a trusted project folder directly, `.env` files included; one with network access can send what it reads to any host.
- With local sockets allowed, on Linux the program can reach every local service you can, including the desktop's message bus.
- It doesn't limit memory or CPU. A sandboxed program can't write core dumps and has at most 4,096 open files.
- On an older Linux kernel some protections are missing; the card lists them. On macOS, network access and local sockets can only be allowed together, and `sandbox-exec` is a tool Apple calls deprecated, though macOS still ships and uses it.
- An extension that is not sandboxed is not limited at all.

Project-local extensions (in a repository's own folder) are not supported: every extension is one you installed yourself.
