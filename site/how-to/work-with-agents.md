# Work with agents

Recipes for the agent chat. For how agents fit into Flint, see [AI agents](../guides/ai-agents.md).

## Pick your default agent

1. Press `⌘,` and open **Agents**.
2. Choose **Default agent**: claude or codex.

The default agent is used for new chats, generated commit messages and conflict resolution. Agents that aren't installed are marked "not installed".

## Start a chat

1. Click `+` in the tab bar.
2. Choose **claude chat** or **codex chat**.

Or run "New Chat with claude" / "New Chat with codex" from the command palette. The chat starts in the active tab's project.

## Attach context

- **A selection:** select code in the editor and press `⌘L`.
- **The current file:** press `⌘L` with nothing selected, or click **＋ current file** in the chat.
- **Any file:** right-click it in the file tree and choose **Add to Agent Chat**.
- **Terminal output:** select it and press `⌘L`, or right-click ▸ **Send to agent**.

Attachments appear as chips above the message box. Click ✕ on a chip to remove it.

## Fix a problem with the agent

1. Hover an error or warning in the editor.
2. Click **Fix with agent**, or **Fix all N in file** (`⌥⇧⌘L`) for every problem in the file.
3. Add any extra instructions to the draft and press `↵`.

`⌥`-click sends the request immediately.

## Fix failing tests with the agent

1. Run the tests (see [Run and test](run-and-test.md)).
2. In the **Tests** tab, click **Fix with agent**, or run "Fix Failed Tests with Agent" from the palette.
3. Review the draft and send it.

## Change what the agent may do without asking

1. In the chat header, click the policy link (for example **auto: edits**).
2. Tick or untick:
   - **Edit files in the project** (on by default);
   - **Read and search the project** (on by default);
   - **Do everything (commands too)** (off by default).

The policy applies to all chats. When the agent needs permission, a card appears in the chat; choose one of its options to continue.

## Stop the agent

Press `Esc` in the message box, or click **■ Stop**.

## Resume a chat after a restart

1. Open the project again. Its chats come back with their transcripts.
2. Type your next message and press `↵`.

The agent picks the conversation up where it left off. If it can't, Flint starts a new conversation and tells you.

## Review what the agent changed

1. After a turn, check the **Changed:** list under the conversation and click a file to open it.
2. Press `⌘K` to open the Commit panel and click each file for a side-by-side diff.
3. Use **↶ Rollback** on any file you want to undo.

## Let the agent write the commit message

1. In the Commit panel (`⌘K`), tick the files to commit.
2. Click **✦ Generate message**.
3. Edit the message if needed and click **Commit**.
