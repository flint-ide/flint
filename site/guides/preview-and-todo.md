# Preview and TODO list

This guide covers the Markdown and image previews and the TODO list, which collects `TODO`-style comments from the whole workspace.

## Markdown preview

Every `.md` file has an **Editor / Split / Preview** switch. `⇧⌘V` (or "Toggle Markdown Preview" in the palette) toggles the preview, and "Open Markdown Preview to the Side" opens the split view.

- The preview updates live as you type.
- In Split mode, the preview follows the heading visible in the editor.
- GitHub-flavored Markdown is supported: tables, task lists, strikethrough and autolinks.
- Relative links open files in the editor; external links open in your browser.
- Relative images load only from folders that are open in Flint.

### Remote images

Images from the internet are not loaded until you click **Load remote images**. This prevents tracking pixels from reporting that you opened a file.

### Safety

The preview drops raw HTML and `javascript:` links, so a Markdown file can't run scripts in Flint.

## Image preview

Images (`png`, `jpg`, `gif`, `webp`, `svg`, `ico`, `bmp`) open in a preview tab.

- The image fits the window. Zoom with `⌘+`, `⌘-` and `⌘0`, or `⌘`+scroll.
- Transparent areas show a checkerboard.
- The tab shows the image's dimensions and file size.
- SVG files can be switched to their source.

Images larger than 25 MB are not previewed.

## TODO list

The TODO panel (`☑` on the activity bar, or "TODO List" in the palette) collects markers from every folder in the workspace.

### Markers

`TODO`, `FIXME`, `HACK`, `XXX` and `BUG`, as whole words in upper case. A name in parentheses, as in `TODO(ada)`, is shown as the author.

### Using the panel

- Entries are grouped by file. Click one to open the file at that line.
- Filter by marker and by text ("Filter notes").
- The list refreshes after files change, but only while the panel is open. **Rescan** refreshes it by hand.

The scan uses the same search as `⇧⌘F` (ripgrep, honoring `.gitignore`), so installing `ripgrep` makes it faster.

### What is skipped

The TODO list always skips:

- files ignored by `.gitignore`;
- dependency, cache and build folders at any depth (Settings ▸ TODO List shows the exact list);
- Python environments of any name (a folder with `pyvenv.cfg` or `conda-meta`);
- folders tagged with `CACHEDIR.TAG`, such as Cargo's `target`;
- `target`, `build` or `_build` next to `Cargo.toml`, `pom.xml`, `build.gradle` or `mix.exs`.

To skip more, add folders or globs in Settings ▸ TODO List ▸ **Excluded folders**, one per line, as in `.gitignore` (for example `docs/legacy`, `/generated` or `*.min.js`). Patterns can't start with `!` or contain commas. See [Customize Flint](../how-to/customize-flint.md#exclude-folders-from-the-todo-list).

## Related

- [Onboard to a new repository](../use-cases/onboard-to-a-new-repository.md)
