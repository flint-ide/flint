# Why Flint

There are already good editors. This page explains why Flint exists, what it does differently, and when another tool is the better choice.

## The problem

Coding agents now write a large share of the code. Claude Code, Codex and others edit files, run commands and fix their own mistakes. Most of the work around them is no longer typing code. It is:

- telling the agent what to do, and giving it the context it needs;
- reading and reviewing what it changed;
- running, testing and debugging the result;
- looking at what broke in the database, the API, the cluster or CI, and handing it back.

Today's editors were built for the person typing. AI was added later as a side panel, an extension or a separate product. So working with an agent usually means juggling a terminal for the agent, an editor to read the code, a git client to review, a database client, a Kubernetes dashboard and a browser tab for CI. You copy context between them by hand.

Flint is built the other way round. The agent writes the code, and Flint is everything around it, in one window.

## What Flint does differently

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

The Commit panel, side-by-side diffs, blame, history and pull request review with inline threads work the way you expect from IntelliJ. [Agent tasks](guides/ai-agents.md) give each agent its own branch and worktree, so several work in parallel and you review and merge each one separately. Edits from agents show up live in the files you have open. Nothing has to be accepted chunk by chunk: you review the result as you would a colleague's.

### Everything in one window

Flint drives the CLIs you already use, with the same `PATH` and logins as your terminal:

- **Git and GitHub**: commits, diffs and pull request review (`git`, and GitHub's API with your account).
- **Run, test, debug and profile**: run configurations, a test tree, the Go debugger (Delve) and flame graphs.
- **Databases**: PostgreSQL and ClickHouse with a visual EXPLAIN.
- **An HTTP client** for `.http` files.
- **Infrastructure**: Docker and Compose, Kubernetes (apps by Flux, Argo CD and Helm, with *Diagnose with AI*), AWS profiles.
- **Real terminals.**

### Fast and light

Flint is a single native process using the system WebView, not Electron. On an Apple M4 Pro, with a fresh profile on a 12,000-file project, its window appears after 0.29 s and it uses about 120 MB after settling. VS Code uses about 590 MB in the same test. See the [benchmark](https://github.com/flint-ide/flint/blob/main/docs/BENCHMARK.md) for the method and how to repeat it.

### Safe with code you don't trust yet

[Restricted Mode](guides/restricted-mode.md) keeps an untrusted folder from running its own code: no automatic terminal, no language servers that execute project code, and agents ignore the project's hooks and settings.

Elsewhere:

- secrets are redacted before anything reaches an agent;
- database passwords and API keys live in the macOS Keychain;
- there is no telemetry.

### Open and simple

Flint is MIT-licensed and keeps its dependencies few: the Go standard library, the editor, the terminal and React. Every user preference is in one `settings.json`. You can import your settings, shortcuts and color theme from VS Code, Cursor or VSCodium, and there is a Vim mode.

## How it compares

A rough guide, from Flint's point of view. Every tool here is good at what it was built for.

| | Strength | Where Flint differs |
|---|---|---|
| **VS Code + an AI extension** | Huge extension ecosystem, every language | Flint is agent-first rather than an extension on top, gives agents the IDE's context, and uses a fraction of the memory. |
| **Cursor, Windsurf** | Polished AI editing and Tab completion | They come with their own AI subscription and models; Flint drives the agents you already use and adds the work around them (review, tasks, infrastructure). |
| **Zed** | Very fast native editor with its own agent panel and ACP support | Flint focuses on everything around the agent: Git review the IntelliJ way, databases, HTTP, Kubernetes, AWS, debugging, failures handed to the agent. |
| **JetBrains IDEs** | Deep refactoring and language intelligence | Flint is lighter and starts in a fraction of the time, and it is built around coding agents. |
| **An agent in a terminal** | Nothing to install, works anywhere | Flint gives the same agent the IDE's context and gives you the review, run and infrastructure tools around it. |

## When Flint isn't the right choice (yet)

- **You rely on a particular extension.** Flint has no extension marketplace. New languages, agents and panels are added to Flint itself.
- **You work on Windows or Linux.** Flint runs on macOS; Linux is in progress and Windows is planned.
- **Your language isn't Go, TypeScript/JavaScript, Python or Rust.** Other languages get editing and search, but no language server out of the box.
- **You want AI completion as you type, above all.** Flint has optional ghost-text completion from a local model, but its focus is agents, not completion.
- **You develop on a remote machine over SSH, or edit together in real time.** Neither is supported yet.

## Next steps

- [Install Flint](installation.md)
- [First steps](first-steps.md): a 10-minute tour
- [Build a feature with an agent](use-cases/build-a-feature-with-an-agent.md)
