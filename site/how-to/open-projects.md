# Open projects

Recipes for opening folders, switching between recent projects and working with several projects in one window.

## Open a folder

1. Press `⌘O`, or choose **File ▸ Open Folder…**.
2. In Recent Projects, choose **Browse…**.
3. Select the folder and confirm.

With no project open, the welcome screen also has an **Open folder…** button.

## Open a recent project

1. Press `⌘O`.
2. Type part of the project name to filter the list.
3. Press `↵` to open it in this window, or `⌘↵` to open it in a new window.

You can also use **File ▸ Open Recent**. **Clear Recently Opened** at the bottom of that menu empties the list, and the ✕ next to an entry in Recent Projects removes just that entry.

## Add a second project to the window

1. Press `⇧⌘O`, or choose **File ▸ Add Project to Workspace…**.
2. Select the folder.

Alternatively, in Recent Projects (`⌘O`), select a project and press `⌥↵` or click ＋ next to it.

The file tree now has a section per project. Search, `⌘P`, the Commit panel, terminals, chats and run configurations work with all of them. See [Several projects in one window](../guides/editor-and-navigation.md#several-projects-in-one-window).

## Remove a project from the window

1. In the file tree, find the project's section.
2. Choose **Remove from Workspace**.

Or run "Remove `<project>` from Workspace" from the command palette. The folder itself is not touched.

## Open a folder or files by dragging

- Drag a folder from Finder onto the Flint window. It opens as the project, or is added to the workspace when a project is already open.
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

## Trust a project

When the Restricted Mode banner appears and you trust the folder's authors:

1. Click **Trust Folder** in the banner, or run "Trust Folder" from the command palette.

See [Restricted Mode](../guides/restricted-mode.md) for what changes.
