# Search and replace

Recipes for finding text across the project and replacing it safely. For how search works, see [Editor and navigation](../guides/editor-and-navigation.md#search-and-replace).

## Find text in the project

1. Press `⇧⌘F`.
2. Type your query. Results appear as they are found, grouped by file.
3. Click a result to open the file at that line.

Turn on **Match case**, **Whole word** or **Regular expression** with the buttons next to the field.

## Limit the search to some files

1. In the Search panel, click **File filters**.
2. Enter include globs (for example `*.go, src/**`) and exclude globs (for example `*_test.go, vendor/**`).

## Search inside one folder

1. Right-click the folder in the file tree.
2. Choose **Find in Folder**.

With several projects open, the panel searches the whole workspace by default; this narrows it down.

## Replace across the project

1. Press `⇧⌘R`, or click the replace button (⇄) in the Search panel.
2. Type the search text and the replacement.
3. Review the preview: each match shows the old text struck through and the new text next to it.
4. Untick files or single lines you want to leave alone.
5. Click the replace button, or press `⌘↵`, to replace every ticked match.

Files that changed since the search, binary files, very large files and files with unsaved changes in the editor are skipped.

## Replace with regular expression groups

1. Turn on **Regular expression**.
2. Use groups in the search, for example `func (\w+)Handler`.
3. Refer to them in the replacement with `$1`, or with `${name}` for named groups (`(?P<name>…)`).

The preview shows the exact result before anything is written.

## Undo the last replace

- Click **Undo** in the Search panel, or
- run "Undo Last Replace in Files" from the command palette.

## Make search faster

Install ripgrep with `brew install ripgrep`. Without it, Flint uses a slower built-in search.
