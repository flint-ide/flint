# Open folders

Recipes for opening folders, switching between recent folders and working with several folders in one window (a workspace). A [Flint project](../guides/projects.md), a folder of planned work items, is something else.

## Open a folder

1. Press `⌘O`, or choose **File ▸ Open Folder…**.
2. In Recent Folders, choose **Browse…**.
3. Select the folder and confirm.

With no folder open, the welcome screen also has an **Open folder…** button.

## Open a recent folder

1. Press `⌘O`.
2. Type part of the folder's name to filter the list.
3. Press `↵` to open it in this window, or `⌘↵` to open it in a new window.

You can also use **File ▸ Open Recent**. **Clear Recently Opened** at the bottom of that menu empties the list, and the ✕ next to an entry in Recent Folders removes just that entry.

## Add a second folder to the window

1. Press `⇧⌘O`, or choose **File ▸ Add Folder to Workspace…**.
2. Select the folder.

Alternatively, in Recent Folders (`⌘O`), select a folder and press `⌥↵` or click ＋ next to it.

The file tree now has a section per folder. Search, `⌘P`, the Commit panel, terminals, chats and run configurations work with all of them. See [Several folders in one window](../guides/editor-and-navigation.md#several-folders-in-one-window).

## Remove a folder from the window

1. In the file tree, find the folder's section.
2. Choose **Remove from Workspace**.

Or run "Remove `<folder>` from Workspace" from the command palette. The folder itself is not touched.

## Open a folder or files by dragging

- Drag a folder from Finder onto the Flint window. It opens in the window, or is added to the workspace when a folder is already open.
- Drag files onto the window to open them in tabs (up to 20 at a time).

While you drag, the window shows what will happen when you drop.

## Open a folder from the terminal

1. Install the `flint` command (see [Installation](../installation.md#the-flint-command)).
2. In a terminal, run:

   ```bash
   flint .                # the current directory
   flint ~/src/my-api     # any folder
   ```

If Flint is already running, it takes the folder over and comes to the front.

## Trust a folder

When the Restricted Mode banner appears and you trust the folder's authors:

1. Click **Trust Folder** in the banner, or run "Trust Folder" from the command palette.

See [Restricted Mode](../guides/restricted-mode.md) for what changes.
