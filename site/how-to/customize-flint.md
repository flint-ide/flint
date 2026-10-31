# Customize Flint

Recipes for changing how Flint looks and behaves. All of these are in the Settings tab (`⌘,`), and every change applies immediately. For a description of each option, see [Settings](../guides/settings.md).

## Change the theme

1. Press `⌘,` and open **Appearance**.
2. Choose **Dark**, **Light** or **System**.

System follows the macOS appearance and switches with it.

## Change the editor font

1. Press `⌘,` and open **Editor**.
2. Set **Font family** (a CSS font list, for example `'Fira Code', Menlo, monospace`), **Font size** and **Line height**.

The change applies to file editors, diffs and scratchpads. **Reset to Defaults** restores the original font.

## Change the terminal font

1. Press `⌘,` and open **Terminal**.
2. Set the font family, size and line height.

## Rebind a keyboard shortcut

1. Press `⌘,` and open **Keymap**.
2. Search for the command by name (for example "push") or by its current shortcut (for example `⌘K`).
3. Click the command's shortcut and press the new key combination. Press `Esc` to cancel.
4. If the keys are already used by another command, Flint tells you which one. Change that one too, or pick other keys.

To restore a single default, click `↺`. To remove a shortcut, click `✕`. **Reset All** restores every default.

> **Note:** Shortcuts that belong to the editor (such as `⌘F`, `⌘S` and `⌘↵`) and the macOS menu keys (`⌘Q`, `⌘C`, `⌘V`, `⌘O`, `⌘,`) can't be changed.

## Save files automatically

1. Press `⌘,` and open **Editor**.
2. Turn on **Save files when Flint loses focus**.

Modified files are written whenever you switch to another app.

## Turn background notifications on or off

1. Press `⌘,` and open **Agents**.
2. Toggle **Tell me when work finishes in the background**.

When on, you get a macOS notification and a Dock badge when a run, a test run or an agent finishes while Flint isn't the active app.

## Use another agent model or executable

1. Press `⌘,` and open **Agents**.
2. Under **Claude Code** or **Codex**, set **Model** (for example `opus`), **Path** (for example `/opt/homebrew/bin/claude`) or **Extra arguments**, one per line.
3. Click outside the field to save. If Flint can't accept a value, the reason appears under the field.

New chats, commit messages and conflict resolution use the new settings; running chats keep theirs.

## Choose or turn off a language server

1. Press `⌘,` and open **Language Servers**.
2. Under the language, clear **Enabled** to turn its server off, or type a server in **Server** (for example `pylsp`, or an absolute path).

The language's server restarts right away. A server Flint doesn't know starts only in trusted folders.

## Change how often panels refresh

1. Press `⌘,` and open **Refresh**.
2. Set the interval of the **Kubernetes panel** or the **AWS panel**, in seconds.

## Exclude folders from the TODO list

1. Press `⌘,` and open **TODO List**.
2. In **Excluded folders**, add one pattern per line, as in `.gitignore`, for example:

   ```text
   docs/legacy
   /generated
   *.min.js
   ```

3. Click outside the field to save.

Patterns can't start with `!` or contain commas. Dependency, cache and build folders are always skipped; the section lists them.

## Hide the sidebar

Press `⌘B` (when the focus isn't in an editor), or click the active icon on the activity bar.

## Edit settings by hand

Your settings are in `~/Library/Application Support/flint/settings.json`. See the [settings file reference](../reference/settings.md) for every key. Restart Flint after editing the file by hand.
