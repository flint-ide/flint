# Settings file

Flint stores your preferences in `settings.json`. The [Settings tab](../guides/settings.md) (`⌘,`) edits this file for you, so you rarely need to open it. This page documents every key for when you want to read, copy or edit it by hand.

## Location

```text
~/Library/Application Support/flint/settings.json
```

The file is readable only by your user. It never contains secrets: database passwords live in the macOS Keychain, and other credentials stay with the tools that own them.

> **Note:** Flint reads `settings.json` when it starts. If you edit the file by hand, restart Flint to apply your changes. Changes made in the Settings tab apply immediately.

## How the file is read

- Missing keys get their default value.
- Unknown keys are ignored.
- A value of the wrong type or out of range is replaced with its default. A hand-edited file never stops Flint from starting.
- When Flint saves the file, it checks every value strictly and writes the whole document.

## Example

```json
{
  "version": 1,
  "theme": "system",
  "editor": {
    "fontFamily": "'JetBrains Mono', 'SF Mono', Menlo, Consolas, monospace",
    "fontSize": 14,
    "lineHeight": 1.55
  },
  "terminal": {
    "fontFamily": "\"JetBrains Mono\", \"SF Mono\", Menlo, monospace",
    "fontSize": 13,
    "lineHeight": 1.2
  },
  "defaultAgent": "claude",
  "autoSave": true,
  "notifyInBackground": true,
  "keymap": {
    "terminal.new": "Mod+Alt+T",
    "git.push": ""
  },
  "todoExclude": ["docs/legacy", "*.min.js"]
}
```

## Keys

| Key | Type | Default | Meaning |
|---|---|---|---|
| `version` | number | `1` | Schema version. Written by Flint; you don't need to change it. |
| `theme` | string | `"dark"` | `"dark"`, `"light"` or `"system"` (follows the macOS appearance). |
| `editor` | object | see below | Font for all editors: files, diffs and scratchpads. |
| `terminal` | object | see below | Font for terminals and run consoles. |
| `defaultAgent` | string | `""` | Agent for new chats, commit messages and conflict resolution: `"claude"` or `"codex"`. Empty means the first installed agent. |
| `autoSave` | boolean | `false` | Save modified files when the Flint window loses focus. |
| `notifyInBackground` | boolean | `true` | Show a macOS notification and a Dock badge when a run, a test run or an agent finishes while Flint is in the background. |
| `keymap` | object | `{}` | Shortcut overrides: command id → shortcut. See [keymap](#keymap). |
| `todoExclude` | array of strings | `[]` | Extra folders or globs the TODO list skips. See [todoExclude](#todoexclude). |

### Font objects

`editor` and `terminal` have the same three keys:

| Key | Type | Range | Editor default | Terminal default |
|---|---|---|---|---|
| `fontFamily` | string | up to 200 characters | `'JetBrains Mono', 'SF Mono', Menlo, Consolas, monospace` | `"JetBrains Mono", "SF Mono", Menlo, monospace` |
| `fontSize` | integer (px) | 8–32 | `13` | `13` |
| `lineHeight` | number (multiple of the font size) | 1.0–2.5 | `1.55` | `1.2` |

`fontFamily` is a CSS font list; the first installed font is used. It may not contain `;`, `{`, `}`, `<`, `>` or `\`.

### keymap

Each entry maps a command id to a shortcut:

- The shortcut is written as modifiers joined with `+`, followed by one key, for example `"Mod+Shift+P"`, `"Alt+F7"` or `"Ctrl+R"`. `Mod` is `⌘`. Accepted modifier names are `Mod`, `Cmd`, `Meta`, `Ctrl`, `Control`, `Alt`, `Option` and `Shift`.
- An empty string removes the command's shortcut.
- Commands that aren't listed keep their default shortcuts.

The easiest way to find a command id is Settings ▸ Keymap: hover a command's name to see its id. Some common ids:

| Command | Id |
|---|---|
| Command palette | `app.commandPalette` |
| Go to File | `nav.goToFile` |
| Find in Files | `nav.findInFiles` |
| Commit | `git.commit` |
| Push | `git.push` |
| Toggle Terminal | `terminal.toggle` |
| New Terminal | `terminal.new` |
| Send Selection to Agent | `agent.sendSelection` |
| Run | `run.selected` |
| Debug | `debug.selected` |

### todoExclude

A list of patterns in `.gitignore` style, such as `"docs/legacy"`, `"/generated"` or `"*.min.js"`. These are added to the folders the TODO list always skips. Each pattern:

- can't be empty, start with `!` or contain a comma;
- is at most 200 characters long.

Up to 100 patterns are kept; duplicates are removed.

## Related settings stored elsewhere

Some preferences are not in `settings.json`:

- Recent folders, the agent approval policy and trusted folders are in `state.json`.
- The sidebar width is kept by the app's web view.
- The selected HTTP environment is remembered only until Flint quits.

See [Files and data](files-and-data.md).
