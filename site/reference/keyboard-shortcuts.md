# Keyboard shortcuts

These are Flint's default shortcuts. All of them, except the ones that belong to the editor itself, can be changed in Settings ▸ Keymap (see [Customize Flint](../how-to/customize-flint.md#rebind-a-keyboard-shortcut)). The command palette (`⇧⌘P`) always shows the current shortcut of each command.

The keys below are the macOS ones. On Linux, read Ctrl for ⌘ and Alt for ⌥ (`Ctrl+Shift+P`, `Ctrl+P`), with these exceptions: Run is `Shift+F10`, Run Test at Cursor `Ctrl+Shift+F10` and Debug `Shift+F9`; in a terminal, `Ctrl` with a letter goes to the shell, and copy and paste are `Ctrl+Shift+C` and `Ctrl+Shift+V` (see [Installation](../installation.md#differences-from-macos)).

## General and navigation

| Shortcut | Action |
|---|---|
| `⇧⌘P` | Command palette |
| `⌘,` | Settings (including the keymap) |
| `⌘[` / `⌘]` | Back / forward |
| `⌘O` | Open folder (Recent Projects) |
| `⇧⌘O` | Add project to workspace |
| `⌘P` | Go to file |
| `⇧⌘F` | Find in files |
| `⇧⌘R` | Replace in files |
| `⌘B` | Toggle sidebar (in an editor: go to definition) |
| `⌘W` | Close tab |
| `⌘S` | Save |

### In Recent Projects

| Shortcut | Action |
|---|---|
| `↵` | Open the project in this window |
| `⌘↵` | Open the project in a new window |
| `⌥↵` | Add the project to the current workspace |

## File tree

| Shortcut | Action |
|---|---|
| `F2` / `↵` | Rename |
| `⌘⌫` | Move to Trash |

## Editor and language servers

| Shortcut | Action |
|---|---|
| `⌘F` | Find in file |
| `⌥⌘G` | Go to line |
| `⌘B` / `F12` / `⌘`-click | Go to definition |
| `⌥F7` / `⇧F12` | Find usages |
| `⇧⌘M` | Problems list |
| `F8` | Next problem (when not debugging) |
| `⇧⌘V` | Markdown preview |

### Vim mode

With [Vim mode](../how-to/customize-flint.md#use-vim-key-bindings) on, editors take Vim's keys. A focused Vim editor also keeps `⌃` with a letter, `[` or `]` (so `⌃R` is redo there, not Run, and `⌃D` scrolls instead of starting the debugger); Flint's other shortcuts come first.

| Command | Action |
|---|---|
| `:w` | Save |
| `:q` | Close the tab |
| `:wq` / `:x` | Save and close the tab |

## Image preview

| Shortcut | Action |
|---|---|
| `⌘+` / `⌘-` | Zoom in / out |
| `⌘0` | Fit to window |
| `⌘`+scroll | Zoom |

## Agents

| Shortcut | Action |
|---|---|
| `⌘L` | Send to agent (selection in the editor or terminal) |
| `⌥⇧⌘L` | Fix all problems in file with the agent |
| `⌘I` | Edit the selection (or the current line) with the agent; `Esc` closes the prompt or stops the agent |
| `⇥` | Accept an inline completion (when one is shown; otherwise indent) |
| `Esc` | Dismiss an inline completion |
| `↵` | Send the message (in the chat) |
| `⇧↵` | New line (in the chat) |
| `Esc` | Stop the agent while it works |

## Git

| Shortcut | Action |
|---|---|
| `⌘K` | Commit (in a terminal: clear the screen) |
| `⇧⌘K` | Push |
| `⌘↵` | Commit (in the commit message field) |
| `⌥⌘↵` | Commit and push (in the commit message field) |
| `⌘↵` | Post a reply (in a pull request review thread) |

### In the merge editor

| Shortcut | Action |
|---|---|
| `⌥⌘N` / `⌥⌘P` | Next / previous conflict |
| `⌥⌘1` | Accept current |
| `⌥⌘2` | Accept incoming |
| `⌥⌘3` | Accept both, current first |
| `⌥⌘4` | Accept both, incoming first |
| `⌘S` | Save the result without staging it |
| `⇧⌘↵` | Mark as resolved (save and `git add`) |

The accept actions work on the conflict at the cursor, or the next one. "Ask Agent about Conflict" has no default shortcut; like the others, it can get one in Settings ▸ Keymap.

## Terminal and scratchpads

| Shortcut | Action |
|---|---|
| `⌘J` | Toggle terminal / last file |
| `⇧⌘T` | New terminal |
| `⌘N` | New SQL console |
| `⌘↵` | Run query (SQL) / send request (`.http`) |
| `⇧↵` | New line in `claude` / `codex` prompts (in a terminal) |

## Run, test and debug

| Shortcut | Action |
|---|---|
| `⌃R` | Run the selected configuration (rerun if running) |
| `⌃⇧R` | Run the test under the cursor (outside a test file: the selected configuration) |
| `⌃D` | Debug |
| `⌘F2` | Stop |
| `⌘F8` | Toggle breakpoint |
| `F9` | Resume (while debugging) |
| `F8` | Step over (while debugging) |
| `F7` | Step into (while debugging) |
| `⇧F8` | Step out (while debugging) |

### Mouse actions in the gutter

| Action | Effect |
|---|---|
| Click ▶ | Run the test, or the file when next to `package` |
| `⌥`-click ▶ | Run the whole package |
| `⇧`-click ▶ | Debug the test (Go) |
| Right-click ▶ | Profile menu (Go) |

## Databases

| Shortcut | Action |
|---|---|
| `⇧⌘E` | EXPLAIN |
| `⇧⌘A` | EXPLAIN ANALYZE (ClickHouse: PIPELINE) |
| `⇧⌘H` | Query history |

### In query history

| Shortcut | Action |
|---|---|
| `↵` | Insert into the editor |
| `⌘↵` | Run again |
| `⌘C` | Copy |

### In the results table (PostgreSQL)

| Shortcut | Action |
|---|---|
| `↵` / `F2` / double-click | Edit the cell |
| `⌘⌫` | Mark the row for deletion |

## Profiles

| Shortcut | Action |
|---|---|
| `Esc` | Reset the flame graph zoom |
| `⌘`-click / double-click | Open the source of a frame |
