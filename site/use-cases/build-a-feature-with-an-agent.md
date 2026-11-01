# Build a feature with an agent

You have a feature to add, and you want an agent to write most of the code while you stay in control: you decide the plan, give it the right context, review every change, run the tests, and ship it as a pull request.

This walkthrough uses Claude Code, but Codex works the same way.

## Before you start

- Install the `claude` (or `codex`) CLI and sign in to it in Terminal.
- Open the project and, if you see the Restricted Mode banner, trust it. In Restricted Mode the agent runs without your project's agent settings and hooks.
- Make sure your working tree is clean, or at least that you know which changes are yours. The Commit panel (`⌘K`) shows everything.

## 1. Plan with the agent

1. Click `+` in the tab bar and choose **claude chat**.
2. Describe the feature and ask for a plan first, before any code:

   ```text
   We need rate limiting on the public API: 100 requests per minute per API key,
   with a 429 response and a Retry-After header. Don't write code yet.
   Read the router and middleware code and propose a plan.
   ```

3. Watch the chat. You see the files the agent reads and, when it makes one, its plan.
4. Discuss the plan until you agree with it.

> **Tip:** Tick **thinking** in the chat header if you want to see the agent's reasoning.

## 2. Give it the right context

The agent can search the project, but pointing it at the right code saves time and tokens.

- Select the relevant part of a file and press `⌘L` to attach it.
- Press `⌘L` with nothing selected to attach the whole current file, or click **＋ current file**.
- Right-click a file in the tree and choose **Add to Agent Chat**.
- Use `⌘P` and `⇧⌘F` to find the code you want to show it.

Attachments appear as chips above the message box. Then ask it to implement the plan.

## 3. Let it work, and decide what it may do

By default the agent edits and reads files inside the project without asking, and asks before running commands. When it wants to run something (for example `go build` or `npm test`), a permission card appears; choose the option you want.

Check the policy link in the chat header (for example **auto: edits**) to see or change these defaults. See [What the agent may do without asking](../guides/ai-agents.md#what-the-agent-may-do-without-asking).

While it works:

- Open files reload in place as the agent edits them, and the gutter marks the changed lines.
- Each tool call in the chat shows a mini diff of the edit.
- Press `Esc` or **■ Stop** if it goes off track, then correct it in your next message.

## 4. Review the changes

When the turn ends, the chat lists the changed files under **Changed:**.

1. Press `⌘K` to open the Commit panel.
2. Click each changed file to open a side-by-side diff.
3. Read the diffs like you would review a colleague's pull request.
4. If a file shouldn't have changed, click **↶ Rollback** on it.
5. If something needs work, select the code and press `⌘L`, then tell the agent what to change.

There is no "accept diff" step. The edits are already in your files, and Git is your safety net.

## 5. Run the tests

1. Open a test file and click ▶ in the gutter next to a test, or next to `package` to run the whole file. `⌥`-click runs the whole package.
2. Results appear in the **Tests** tab, and each test shows ✓ or ✗ in the gutter.

You can also run your own run configuration with `⌃R` (see [Run and test](../how-to/run-and-test.md)).

## 6. Fix failing tests with the agent

1. In the Tests tab, click **Fix with agent**. The failures and their output go into the chat draft.
2. Add any hint you have and press `↵`.
3. When the agent is done, click **Rerun failed tests**.

Compiler errors and warnings in the editor work the same way: hover the problem and click **Fix with agent**, or press `⌥⇧⌘L` to send every problem in the file.

## 7. Commit with a generated message

1. In the Commit panel, tick the files that belong to this change.
2. Click **✦ Generate message**. The agent reads the diff of the ticked files and your recent commit messages, and writes a message in the same style.
3. Edit the message.
4. Click **Commit and Push** (`⌥⌘↵`), or **Commit** (`⌘↵`) and push later with `⇧⌘K`.

## 8. Open and review the pull request

Flint shows and reviews pull requests through `gh`, but you create them the usual way: ask the agent to run `gh pr create` (it will ask for permission), run it yourself in a terminal (`⌘J`), or use GitHub in your browser.

Then:

1. Open the Pull Requests view (`⇅` on the activity bar) and click your pull request.
2. Watch the CI checks in the **Checks** list.
3. When reviewers comment, open the file diffs to read and reply to their threads inline.

See [Review a pull request](review-a-pull-request.md) for the review side.

## Tips

- **Keep chats focused.** Start a new chat for an unrelated task; the context bar in the header shows how full the current conversation is.
- **Work in parallel with agent tasks.** Run "New Agent Task…" (`⇧⌘P`), describe the task, pick the agent and the base branch, and click **Start**: Flint creates a branch and a worktree, opens it in the window and hands the description to the agent in a new chat. The **Agent Tasks** panel shows every task's state (working, waiting for you, finished, failed), its changes, and Open, Show diff, Merge, Create PR and Remove. See [Agent tasks](../guides/ai-agents.md#agent-tasks-agents-in-parallel). You can still create a worktree by hand from the branch widget and open a terminal there.
- **Watch the cost.** The chat header shows tokens and, for Claude, the cost; the status bar shows the total for the project.
- **Let it finish in the background.** Switch to another app; Flint notifies you when the agent's turn is done.
