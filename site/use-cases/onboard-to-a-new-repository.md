# Onboard to a new repository

You just cloned a repository you don't know: a new job, an open-source project, a vendor's code. You want to understand how it is put together before you run anything. This walkthrough keeps the folder in Restricted Mode while you read, uses search, Git history and an agent to learn the architecture, and trusts the folder once you are ready.

## 1. Open it in Restricted Mode

1. Clone the repository in Terminal.
2. Open it in Flint: `flint path/to/repo`, drag the folder onto the window, or use `⌘O` ▸ **Browse…**.

The **Restricted Mode** banner appears. Leave it for now. Until you trust the folder, Flint:

- doesn't start a terminal automatically;
- doesn't start the Rust or Python language servers, which run project code;
- runs agents without the project's own agent settings and hooks;
- runs Git without hooks.

Reading, searching, `⌘P`, diffs, blame and the Go and TypeScript language servers work normally. See [Restricted Mode](../guides/restricted-mode.md).

## 2. Read the README

1. Press `⌘P`, type `readme` and press `↵`.
2. Press `⇧⌘V` to show the rendered preview, or switch to **Split** to see source and preview side by side.

Relative links in the preview open the linked files in Flint. Remote images (badges, screenshots) load only when you click **Load remote images**, so opening a file never reports back to anyone.

Look for other docs the same way: `CONTRIBUTING`, `docs/`, `ARCHITECTURE`.

## 3. Get the lay of the land

- Browse the file tree. Right-click a folder and choose **Find in Folder** to search only inside it.
- Press `⇧⌘F` and search for entry points: `func main`, `createServer`, `app.listen`, `if __name__`.
- Use **File filters** in the Search panel to exclude tests or vendored code (`*_test.go, vendor/**`).
- In Go or TypeScript code, `⌘`-click or `⌘B` jumps to definitions and `⌥F7` finds usages.

## 4. Check the TODO list

1. Click `☑` on the activity bar, or run "TODO List".
2. Scan the `TODO`, `FIXME`, `HACK`, `XXX` and `BUG` markers, grouped by file, with authors from `TODO(name)`.
3. Filter by marker. `FIXME` and `BUG` usually point at known weak spots.

If generated or legacy folders clutter the list, exclude them in Settings ▸ TODO List.

## 5. Learn from history

- Click **Blame** in the status bar on an important file. Hover a line to see who changed it, when, and the commit message; click it to open that commit's change.
- Right-click a file and choose **Show History** to read its commits (renames included) with the diff of each.
- Open the Pull Requests view (`⇅`) to see what is being worked on right now (sign in with GitHub first).

## 6. Ask the agent to explain the architecture

1. Click `+` ▸ **claude chat** (or codex).
2. Attach the most relevant files: open them and press `⌘L`, or right-click them in the tree ▸ **Add to Agent Chat**.
3. Ask for an overview, and keep it read-only:

   ```text
   I'm new to this repository. Don't change anything.
   Explain the main components, how a request flows through them,
   where configuration is loaded, and how the tests are organized.
   Point me to the files I should read first.
   ```

4. Follow up on anything unclear, and open the files it mentions with `⌘P`.

In Restricted Mode the agent ignores the repository's own agent settings and hooks, so a malicious `.claude` configuration in the repository can't take effect. It still asks before running commands.

> **Tip:** Copy the parts of the explanation you want to keep into a Markdown note ("New Markdown Note" in the palette) and extend it as you learn.

## 7. Trust the folder when you are ready

When you have read enough to trust the code and its authors:

1. Click **Trust Folder** in the banner, or run "Trust Folder" from the palette.

The held-back language servers start right away, and Git hooks and the project's agent settings now apply. You can now install dependencies and run the project, for example with a detected run configuration in the ▶ selector (`⌃R`) or the tests from the gutter.

Changed your mind? "Revoke Folder Trust (Restricted Mode)" turns the restrictions back on.

## Tips

- **Trust is per folder.** Trusting a folder also trusts its subfolders, but not sibling projects in the same window.
- **You can open a terminal yourself** in Restricted Mode (`⇧⌘T`). Remember that your shell's prompt hooks may run tools such as `git status` in the folder.
- **Several repositories at once.** Add related repositories to the window with `⇧⌘O` and search across all of them.
