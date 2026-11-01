# Settings

Flint's settings open in a tab. Every change applies immediately, without a restart, and saves itself. This guide describes each section. For the file format and every key, see the [settings reference](../reference/settings.md).

## Opening Settings

- `⌘,`
- **flint ▸ Settings…** in the menu bar
- "Settings…" in the command palette

The tab has ten sections: Appearance, Editor, Terminal, Agents, Language Servers, Refresh, TODO List, Keymap, Updates and Import.

## Appearance

**Theme**: Dark, Light or System, and Custom once you have [imported a theme from VS Code](../how-to/customize-flint.md#import-your-settings-from-vs-code). System follows the macOS appearance and switches when macOS does. Custom lays the imported colors over the dark or light palette; the hint under the label names the theme. The title bar and window buttons follow the theme, and Flint starts in the theme you used last, so a light theme never flashes dark on launch.

## Editor

- **Font family**: a CSS font list; the first installed font is used. The default is JetBrains Mono, then SF Mono, Menlo and Consolas.
- **Font size**: 8 to 32 px (default 13).
- **Line height**: 1.0 to 2.5 times the font size (default 1.55).
- **Save files when Flint loses focus**: writes modified files when you switch to another app. Off by default.
- **Vim mode**: Vim key bindings in every editor, with the mode in the status bar; `:w` saves and `:q` closes the tab. A focused Vim editor keeps the `⌃` keys Vim uses (`⌃R` redo instead of Run, `⌃D` instead of Debug); other Flint shortcuts keep working. Off by default. See [Use Vim key bindings](../how-to/customize-flint.md#use-vim-key-bindings).

The font settings apply to all editors: files, diffs and scratchpads. **Reset to Defaults** restores the font settings.

## Terminal

Font family, size (default 13) and line height (default 1.2) for terminals and run consoles. Run consoles use a size one point smaller. Terminals resize immediately when you change these.

## Agents

- **Default agent**: the agent used for new chats, generated commit messages and conflict resolution. Agents that aren't installed are marked. When none is chosen, Flint uses the first installed agent.
- **Claude Code** and **Codex**, each with:
  - **Path**: the executable. Empty looks the agent up on your login shell's `PATH`; otherwise a command name or an absolute path.
  - **Model**: empty uses the CLI's default; for Claude Code an alias such as `sonnet` or `opus`, or a full model name.
  - **Extra arguments**: one per line, for example `--add-dir` and `/Users/me/notes` on two lines. They come before Flint's own flags, and arguments that would change a flag Flint sets (output format, permission prompt, setting sources, sandbox, approvals, model) are refused with the reason shown under the field. They are ignored in [Restricted Mode](restricted-mode.md).
  - **Reset to Defaults** appears once something is set.

  Agent settings apply to chats, commit messages and conflict resolution started afterwards; running chats keep theirs.
- **Tell me when work finishes in the background**: shows a macOS notification and a count on the Dock icon when a run, a test run or an agent turn finishes while Flint isn't the active app. On by default.

## Language Servers

For Go, TypeScript / JavaScript, Python and Rust:

- **Enabled**: turn the language's server off or on.
- **Server**: empty tries the known servers in the order shown. Pick one by name (the field suggests them), give an absolute path to one, or name another server. A server Flint doesn't know starts only in trusted folders.
- **Extra arguments**: one per line, added after the server's own arguments. Ignored in Restricted Mode.

A change restarts that language's servers for the open files. Rust and Python servers run the project's code and start only in trusted folders, whatever you choose here.

## Refresh

How often panels that poll re-read their data while they are shown:

- **Kubernetes panel**: 2 to 600 seconds (default 5).
- **AWS panel**: 5 to 3600 seconds (default 30).

Git status, Docker and pull requests update on their own, without polling.

## TODO List

- **Excluded folders**: extra folders or globs the [TODO list](preview-and-todo.md#todo-list) skips, one per line, as in `.gitignore`. Saved when you leave the field. Patterns can't start with `!` or contain commas.
- **Always skipped**: the folders Flint skips on its own.

## Keymap

A searchable list of all commands. Search by name or by shortcut (for example `⌘K`).

- Click a shortcut and press a new key combination to change it; `Esc` cancels. If the keys are already bound to another command, Flint tells you which.
- `↺` restores the default shortcut; `✕` removes it. **Reset All** restores every default.
- Shortcuts that belong to the editor itself (such as `⌘F`, `⌘S` or `⌘↵`) are marked "editor" and can't be changed.
- Keys of the macOS menu (`⌘Q`, `⌘C`, `⌘V`, `⌘O`, `⌘,`) stay with the menu.

The command palette uses your shortcuts too.

## Updates

- **Check for new releases**: once a day Flint asks GitHub which release is the latest and shows a notice in the status bar when there is a newer one, with **Download** (opens the release page in your browser), **Skip This Version** and **✕**. On by default. When it is off, Flint makes no request on its own.
- **Check Now**: checks right away, even when the daily check is off, and tells you if you are up to date. **Help ▸ Check for Updates…** does the same.

Flint never downloads or installs updates itself. See [Update check](../reference/privacy-and-security.md#update-check) for exactly what is sent.

## Import

**Import from VS Code…** takes your fonts, auto-save, keyboard shortcuts, color theme and Vim extension from Visual Studio Code, Cursor or VSCodium, after showing what would change and what is skipped. See [Import your settings from VS Code](../how-to/customize-flint.md#import-your-settings-from-vs-code).

## Trusted folders

Folder trust isn't managed in the Settings tab. Use the Restricted Mode banner and the "Trust Folder" / "Revoke Folder Trust (Restricted Mode)" commands. See [Restricted Mode](restricted-mode.md).

## Related

- [Customize Flint](../how-to/customize-flint.md)
- [Settings file reference](../reference/settings.md)
