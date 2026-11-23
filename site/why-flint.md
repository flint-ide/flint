# Why Flint

There are already good editors. This page explains why Flint exists, what it does differently, and when another tool is the better choice. In short: Flint is a place to get work done with AI agents under your control. The editor and the other tools are there to check and correct what the agents did.

## The problem

Coding agents now write a large share of the code. Claude Code, Codex and others edit files, run commands and fix their own mistakes. Most of the work around them is no longer typing code. It is:

- deciding what to hand to an agent, and giving it the context it needs;
- making sure it follows a process: a plan, the tests, an independent review, your approval;
- keeping several agents busy at once without them stepping on each other;
- reading and reviewing what they changed;
- running, testing and debugging the result;
- looking at what broke in the database, the API, the cluster or CI, and handing it back.

Today's editors were built for the person typing. AI was added later as a side panel, an extension or a separate product. So working with an agent usually means juggling a terminal for the agent, an editor to read the code, a git client to review, a database client, a Kubernetes dashboard and a browser tab for CI. You copy context between them by hand, and the process (did it plan, did it test, who approved) lives in your head.

Flint is built the other way round. The agents do the work, under rules you write down and decisions only you take. Flint is also the rest of what you need to check that work, in one window.

## What Flint does differently

### Workflows: a process the agent cannot skip

A [workflow](guides/workflows.md) is one Markdown file of steps that agents, scripts and you take in order: plan, implement, run the tests, have a second agent review, wait for your approval. A failed test or review sends the work back to the step that can fix it, with the reason, a bounded number of times. A step that waits for you is a gate, and only you can pass it: an agent can start and steer a run if you allow it, but it cannot approve a gate or continue past a failed step, in the app or from the `flint` command. Each run is an agent task with its own branch and worktree, and every step is an ordinary chat or run tab you can open and take over.

### A board for the work you will hand over

A [project](guides/projects.md) is a folder of work items, plain files you share through git, for one or more repositories. **Delegate** starts an agent task, or a workflow run, in each repository an item names. The item's status follows the work, and items can wait for others to be done. The same things are available from a terminal or a script through the [`flint` command](reference/cli.md).

### Bring your own agent, no lock-in

Flint has no model and no AI subscription of its own. It drives the agents you already pay for and trust: Claude Code, Codex, or any agent with an [Agent Client Protocol](https://agentclientprotocol.com) mode (Gemini CLI, OpenCode, …), added in Settings with no code. Your logins, your models, your limits. Switching agents means picking another one in a menu, not changing editors.

### Agents see what Flint sees

An agent in a terminal doesn't know what your language server reports, what you have selected, or which test just failed. Flint gives its agents that context through its own read-only MCP server: language server problems, open files and the selection, the last test results, and, if you allow it, terminal output and cluster status. The agent stops guessing. See [AI agents](guides/ai-agents.md).

### Every failure is one step from the agent

When something fails, Flint offers a one-step hand-off to the agent with the relevant context already gathered, redacted and put in the chat draft for you to review. That covers:

- a test or a build;
- a language server error;
- a crashing pod;
- a slow or failing query;
- a merge conflict.

### Built for reviewing, not just writing

The editor and the Commit panel are there for this. The Commit panel, side-by-side diffs, blame, history and pull request review with inline threads work the way you expect from IntelliJ. [Agent tasks](guides/ai-agents.md) give each agent its own branch and worktree, so several work in parallel and you review and merge each one separately. Edits from agents show up live in the files you have open. Nothing has to be accepted chunk by chunk: you review the result as you would a colleague's.

### The tools to check the work, in one window

Beyond the agents, Flint drives the CLIs you already use, with the same `PATH` and logins as your terminal:

- **Git and GitHub**: commits, diffs and pull request review (`git`, and GitHub's API with your account).
- **Run, test, debug and profile**: run configurations, a test tree, the Go debugger (Delve) and flame graphs.
- **Databases**: PostgreSQL and ClickHouse with a visual EXPLAIN.
- **An HTTP client** for `.http` files.
- **Infrastructure**, as extensions you install: Docker and Compose, Kubernetes (apps by Flux, Argo CD and Helm, with *Diagnose with AI*), AWS profiles.
- **Real terminals.**

### Fast and light

Flint is a single native process using the system WebView, not Electron. On an Apple M4 Pro, with a fresh profile on a 12,000-file repository, its window appears after 0.29 s and it uses about 120 MB after settling. VS Code uses about 590 MB in the same test. See the [benchmark](https://github.com/flint-ide/flint/blob/main/docs/BENCHMARK.md) for the method and how to repeat it.

### Safe with code you don't trust yet

[Restricted Mode](guides/restricted-mode.md) keeps an untrusted folder from running its own code: no automatic terminal, no language servers that execute the folder's code, and agents ignore the folder's hooks and settings.

Elsewhere:

- secrets are redacted before anything reaches an agent;
- database passwords and API keys live in the system keyring (the macOS Keychain, or the Secret Service on Linux);
- there is no telemetry;
- the scripts of a workflow can run in the operating system's sandbox, which stops them reading your keys or writing outside their worktree (agent steps are not sandboxed by it).

### Open and simple

Flint is MIT-licensed and keeps its dependencies few: the Go standard library, the editor, the terminal and React. Every user preference is in one `settings.json`. You can import your settings, shortcuts and color theme from VS Code, Cursor or VSCodium, and there is a Vim mode.

## How it compares

A rough guide, from Flint's point of view. Every tool here is good at what it was built for.

| | Strength | Where Flint differs |
|---|---|---|
| **VS Code + an AI extension** | Huge extension ecosystem, every language | Flint is agent-first rather than an extension on top, gives agents the IDE's context, and uses a fraction of the memory. |
| **Cursor, Windsurf** | Polished AI editing and Tab completion | They come with their own AI subscription and models; Flint drives the agents you already use and adds the work around them (workflows with approval gates, the project board, review, tasks, infrastructure). |
| **Zed** | Very fast native editor with its own agent panel and ACP support | Flint focuses on everything around the agent: Git review the IntelliJ way, databases, HTTP, Kubernetes, AWS, debugging, failures handed to the agent. |
| **JetBrains IDEs** | Deep refactoring and language intelligence | Flint is lighter and starts in a fraction of the time, and it is built around coding agents. |
| **An agent in a terminal** | Nothing to install, works anywhere | Flint gives the same agent the IDE's context and gives you written workflows with approval gates, a project board, and the review, run and infrastructure tools around it. |

## When Flint isn't the right choice (yet)

- **You rely on a particular extension.** Flint has no extension marketplace: its [extensions](guides/extensions.md) come from one repository, [flint-ide/extensions](https://github.com/flint-ide/extensions), or from a folder, and there are only a few of them so far.
- **You work on Windows.** Flint runs on macOS and Linux (x86_64 and arm64, with GTK 4 and WebKitGTK 6.0); Windows is planned.
- **Your language isn't Go, TypeScript/JavaScript, Python or Rust.** Other languages get editing and search, but no language server out of the box.
- **You need workflows that run steps in parallel or across several repositories.** A run takes its steps one after the other, in one repository, with a pass or fail result per step. Parallel steps, and a run across repositories, are not there yet; a [project item](guides/projects.md) can start one run per repository.
- **You need agent steps confined.** The sandbox covers a workflow's script steps. Agents run with the approval policy of any chat, not in the sandbox.
- **You want AI completion as you type, above all.** Flint has optional ghost-text completion from a local model, but its focus is agents, not completion.
- **You develop on a remote machine over SSH, or edit together in real time.** Neither is supported yet.

## Next steps

- [Install Flint](installation.md)
- [First steps](first-steps.md): a 10-minute tour
- [Workflows](guides/workflows.md): your first run in ten minutes
- [Build a feature with an agent](use-cases/build-a-feature-with-an-agent.md)
