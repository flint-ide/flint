# Customize Flint

Recipes for changing how Flint looks and behaves. All of these are in the Settings tab (`⌘,`), and every change applies immediately. For a description of each option, see [Settings](../guides/settings.md).

## Import your settings from VS Code

If you are coming from Visual Studio Code, Cursor or VSCodium, Flint can take over your fonts, auto-save, keyboard shortcuts, color theme and Vim extension.

1. Run "Import from VS Code…" from the command palette (`⇧⌘P`), or press `⌘,`, open **Import** and click **Import from VS Code…**.
2. If you have more than one of these editors, choose which one to import from.
3. Review the preview. It lists everything Flint would change and, under **Skipped**, everything it leaves out and why (for example a setting Flint has no equivalent for, a shortcut with a `when` condition, or a theme built into VS Code). A shortcut that another Flint command already uses is marked with that command's name.
4. Click **Import**. Nothing is written before you do.

What is imported:

| From VS Code | To Flint |
|---|---|
| `editor.fontFamily`, `editor.fontSize`, `editor.lineHeight` | Editor font. A line height in pixels is converted to a multiple of the font size. |
| `terminal.integrated.fontFamily`, `fontSize`, `lineHeight` | Terminal font. |
| `files.autoSave` (`onFocusChange`, `onWindowChange` or `off`) | **Save files when Flint loses focus**. Flint has no "after delay" mode. |
| `workbench.colorTheme` | A **Custom** theme with the theme's colors, terminal colors included, when the theme comes from an installed extension. For a theme built into VS Code, which Flint can't read, the Dark or Light theme. |
| `window.autoDetectColorScheme` | The **System** theme. |
| The Vim extension (`vscodevim.vim`) installed | **Vim mode**. |
| `keybindings.json` | Shortcuts of the Flint commands that do the same thing, for example Go to File, Find in Files, Toggle Terminal, Push or Step Over. |

Skipped shortcuts include multi-key chords (`⌘K ⌘S`), shortcuts with a `when` clause or arguments, removals of VS Code defaults (`-command`), and commands Flint doesn't have. Flint keeps one shortcut per command; when your file binds several, the last one wins. Everything you import can be changed afterwards in Settings.

Flint reads the editor's `settings.json` and `keybindings.json` in `~/Library/Application Support/Code/User` (or `Cursor/User`, `VSCodium/User`) and the themes of installed extensions (`~/.vscode/extensions`, `~/.cursor/extensions`, `~/.vscode-oss/extensions`). It never changes those files.

## Change the theme

1. Press `⌘,` and open **Appearance**.
2. Choose **Dark**, **Light** or **System**, or **Custom** after you imported a theme or chose one from an extension.

System follows the macOS appearance and switches with it. Custom lays the theme's colors over the dark or light palette, whichever the theme was made for; colors the theme doesn't set keep Flint's. Terminals take the theme's terminal colors when it has them (ANSI colors included), otherwise its background, text, cursor and selection colors.

## Use a theme from an extension

An extension can bring color themes: colors for the interface, the editor and the terminal. It is a folder with a `flint-extension.json` manifest, and it needs no program.

1. Run "Show Extensions" from the command palette (or press `⌘,` and open **Extensions**), or run "Install Extension from Folder…".
2. For a theme from the [flint-ide/extensions](https://github.com/flint-ide/extensions) repository, click **Install…** next to it under **Repository**; a theme needs no build. Otherwise choose **Install from Folder…** and pick the extension's folder. Flint shows its name, what it adds and, if it has one, its program and permissions. Choose **Install** to copy it into Flint's extensions folder (`~/Library/Application Support/flint/extensions/<id>`).
3. Choose one of its themes, either with **Use …** on the extension or under **Color theme** in **Appearance**.

The theme becomes your **Custom** theme, so it stays even if you uninstall the extension, and choosing another theme replaces it. Flint checks an extension's colors with the same rules as an imported theme: only the names Flint knows and plain hex colors. A manifest with anything else isn't installed, and Settings ▸ Extensions says why.

A minimal theme extension:

```json
{
  "contract": 1,
  "id": "me.nord",
  "name": "Nord",
  "version": "1.0.0",
  "contributes": {
    "themes": [{
      "id": "me.nord.dark",
      "label": "Nord",
      "base": "dark",
      "colors": { "--bg-editor": "#2e3440", "--fg": "#d8dee9", "--accent": "#88c0d0", "--syn-keyword": "#81a1c1" },
      "terminal": { "background": "#2e3440", "red": "#bf616a", "green": "#a3be8c", "blue": "#81a1c1" }
    }]
  }
}
```

The color names are those of [customTheme](../reference/settings.md#customtheme). Without `run`, the extension has no program and runs no code. Extensions can also be programs that add commands and panels: see [Extensions](../guides/extensions.md) and [Write an extension](../extensions/write-an-extension.md).

## Use Vim key bindings

1. Press `⌘,` and open **Editor**.
2. Turn on **Vim mode**, or run "Toggle Vim Mode" from the command palette.

Every open editor switches right away. The status bar shows the mode of the focused editor (`-- NORMAL --`, `-- INSERT --`, `-- VISUAL --`). `:w` saves, `:q` closes the tab, and `:wq` or `:x` do both.

While a Vim editor has the focus, it keeps the `⌃` keys Vim uses (`⌃` with a letter, `[` or `]` and no other modifier), so `⌃R` is redo, `⌃D`/`⌃U` scroll, `⌃V` starts a block selection, `⌃O`/`⌃I` jump and `⌃A`/`⌃X` change numbers. Flint's `⌃R` (Run) and `⌃D` (Debug) still work everywhere else, and from the command palette. All other Flint shortcuts, such as `⌘P` or `⌃⇧R`, keep working in a Vim editor.

`:w` and `:q` work in file and scratchpad editors; diffs and other read-only views show a note instead.

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
