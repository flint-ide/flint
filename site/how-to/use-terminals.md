# Use terminals

Recipes for opening terminals, pointing them at an AWS profile or a Kubernetes context, and sending their output to the agent. For background, see [Terminals](../guides/terminals.md).

## Open a terminal

- Press `⌘J` to jump to a terminal (Flint opens one if there is none). Press `⌘J` again to go back to your last file.
- Press `⇧⌘T`, or click `+` in the tab bar and choose **Terminal**, to open a new one.

With several projects in the window, the new terminal starts in the active tab's project. To pick a different project, use the `+` menu, which has a section per project.

## Open a terminal in a specific folder

1. Right-click the folder in the file tree.
2. Choose **Open in Terminal**.

## Open a terminal as an AWS profile

1. Open the AWS Profiles panel (`☁` on the activity bar).
2. Double-click the profile, or click `❯_` on its row.

The terminal has `AWS_PROFILE` and `AWS_REGION` set, and its tab is named `aws:<profile> · …`. It keeps the profile after a restart.

## Open a terminal in a Kubernetes context

1. Open the Kubernetes panel (`☸` on the activity bar).
2. Choose the context and namespace at the top.
3. Click `❯_`.

`kubectl` in this terminal uses that context and namespace. Your kubeconfig's current context doesn't change. The tab is named `k8s:<cluster>/<namespace>`.

## Open a file from terminal output

1. Hold `⌘` and click a `path:line` in the output.

The file opens next to the terminal at that line. Press `⌘[` to go back to the terminal.

## Send terminal output to the agent

1. Select the output.
2. Press `⌘L`, or right-click and choose **Send to agent**.
3. In the chat, add your question below the code block and press `↵`.

To send right away without editing the draft, `⌥`-click **Send to agent**.

## Clear the screen

Press `⌘K` while the terminal has focus.

## Write a multi-line prompt for claude or codex

When running `claude` or `codex` in a terminal, press `⇧↵` to insert a new line instead of sending.

## Change the terminal font

1. Press `⌘,` and open **Terminal**.
2. Set the font family, size and line height.

Open terminals update immediately.
