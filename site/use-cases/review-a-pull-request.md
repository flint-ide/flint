# Review a pull request

A teammate asked for your review. This walkthrough takes you from the pull request list through CI, the diff and inline threads to submitting your review, and checking the branch out when you want to run it, all without leaving Flint.

## Before you start

- Install the GitHub CLI (`brew install gh`) and sign in with `gh auth login`. Flint uses `gh` for everything, so GitHub Enterprise and `GH_TOKEN` work as they do in your terminal.
- Open the repository in Flint. It needs a GitHub remote.

## 1. Find the pull request

1. Click `⇅` on the activity bar, or run "Pull Requests…" from the command palette.
2. Set the filters at the top: **Review requested** and **Open** show what is waiting for you.
3. Scan the list. Each entry shows:
   - CI state: ✓ passed, ✗ failed, ● running (hover for the counts);
   - the review decision: Approved, Changes requested, Review required, Draft or Merged;
   - when it was last updated.

## 2. Open it and check CI

1. Click the pull request. It opens in a tab with the title, branches, author and description.
2. Expand **Checks** to see each CI check. Follow a check's link to its logs on GitHub.
3. Look at **Files changed** (with added and removed lines and the number of open threads per file), **Commits** and earlier **Reviews**.

> **Tip:** If CI failed, read why before reviewing the code; the fix may change the diff.

## 3. Read the diff

1. Click a file under **Files changed**. A side-by-side diff opens, against the merge base, the same way GitHub shows it.
2. Existing review threads appear under their lines. Outdated threads and file-level comments are listed above the diff.
3. Use `⌘P` or `⇧⌘F` to look at surrounding code in your checkout whenever you need more context.

## 4. Reply to and resolve threads

In a thread under a line:

1. Type your reply and press `⌘↵`.
2. Click **Resolve** when the discussion is settled, or **Unresolve** to reopen it.

## 5. Add your own comments

1. Hover a line inside a changed hunk and click `+` in the gutter.
2. Write your comment.
3. Choose how to post it:
   - **Add single comment** posts it right away;
   - **Start a review** (or **Add review comment** once you have started) adds it to a pending review that only you can see until you submit it.

Pending comments are marked as such in the diff.

## 6. Submit the review

The review bar appears in the pull request tab and in each diff.

1. Write a summary.
2. Click **Comment**, **Approve** or **Request changes**.

While you have a pending review, the bar shows how many comments it holds. **Discard** throws the pending review away.

## 7. Check out the branch to try it

Sometimes you need to run the code.

1. Make sure you have no uncommitted changes to tracked files. Check out refuses otherwise, to protect your work.
2. Click **Check out** in the pull request tab. Flint runs `gh pr checkout`.
3. Run the tests from the gutter or start a run configuration with `⌃R`.

To understand a tricky part, select it and press `⌘L` to ask the agent about it.

## 8. When CI is red

If a check failed and the author asks for help:

1. Expand **Checks** in the pull request tab and open the failing check's link to read its log on GitHub.
2. Check out the branch (step 7) and run the failing test from the gutter ▶.
3. In the **Tests** tab, click **Fix with agent** to have the agent investigate the failure, or select the relevant output and press `⌘L` to ask about it.
4. Share what you found as a review comment, or push a fix if you agreed to with the author.

When you are done, switch back to your own branch from the branch widget in the status bar.

## Tips

- **Refresh when you need it.** Nothing reloads in the background. The list and the open pull request reload when you come back to the Flint window, and ↻ reloads them on demand.
- **Open on GitHub** is there for anything Flint doesn't show, such as merging.
- **Use a worktree** to review without disturbing your current branch: create one from the branch widget, open it in a new window, and check the pull request out there.
