## Highlights

### Agent definitions, model tiers and a backlog delegated to agents

#### Defined agents

Describe an agent in a Markdown file and use it wherever Flint asks for an agent: the chat's agent picker, New Agent Task… and Compare agents, the default agent, the review agent, and the `start_agent` tool. The text of the file is the agent's standing instructions for the whole conversation; the front matter can add an `engine` (`claude` or `codex`) and a `model`:

```markdown
---
description: Reviews a change for bugs; reports, never edits
model: strong
---
You are the reviewer of this project. Read the diff and the code around it.
Report bugs with file and line. Do not change files.
```

+ Personal agents live in `agents/` in Flint's configuration folder. Project agents live in `.flint/agents/` of a repository, so your team gets them when you commit them; they are loaded only in trusted folders, and a personal agent wins over a project agent with the same name.
+ **New Agent…** in Settings ▸ Agents ▸ Defined agents, or "New Agent Definition…" in the command palette, creates one from a template. Settings ▸ Agents lists your agents, and any file Flint couldn't use with the reason.
+ Without `engine`, the agent runs on your default agent (Claude Code or Codex), so one file works for everyone on a team whichever program they use.
+ Set a defined agent as the review agent and Review Changes with Agent reviews by its instructions.
+ A new or changed file applies to the next conversation; there is nothing to restart.

See [Defined agents](https://flint-ide.github.io/flint/guides/ai-agents.html#defined-agents).

#### Model tiers

A defined agent's `model` can be a tier, `fast`, `balanced` or `strong`, instead of a model name, so an agent shared with a team asks for a kind of model whichever program each person runs it on. Settings ▸ Agents has a **Fast model**, **Balanced model** and **Strong model** field for each program. For Claude Code they default to `haiku`, `sonnet` and `opus`. Codex has no defaults: until you set them, a tier runs on the Codex model from Settings. In `settings.json` they are `agents.claude.tiers` and `agents.codex.tiers`.

See [Model tiers](https://flint-ide.github.io/flint/guides/ai-agents.html#model-tiers).

#### Backlog

A backlog holds work that is planned but not started: a folder with `flint-backlog.json`, which names your repositories by their git remote URL, and one Markdown file per item in `items/`, with a title, status, priority, agent and the repositories it is for. Open the **Backlog** panel from the activity bar or with "Show Backlog" in the command palette; **New Backlog…** and **Open Backlog…** are in its `⋯` menu, and **Show Board** shows the items as cards in To do, Doing, Review and Done columns that you can drag between.

+ **Delegate** on an item starts one agent task per repository it names, with the agent it names, in your checkout of each repository. Flint finds checkouts by remote URL, or you choose one once with **Locate…**. Delegate walks you through anything missing first, and creates all the tasks or none.
+ The item's status follows its agent tasks: `doing` when you delegate it, `review` when every task finished, `done` when every task is merged or has a pull request. You can still set any status yourself. Statuses are written to the item's file, so your team sees them through git.
+ Agent chats can read your backlogs with the read-only `get_backlog` tool, so you can ask what is planned or what to do next.
+ A backlog in a folder you haven't trusted is left out of `get_backlog`, and Delegate from it asks first and shows what the agent will get.
+ Backlog is a built-in feature; turn it off in Settings ▸ Extensions ▸ Built-in.

See the [Backlog guide](https://flint-ide.github.io/flint/guides/backlog.html).

Pull request: [#83](https://github.com/flint-ide/flint/pull/83)

## Upgrade notes

+ `defaultAgent` and the review agent (`agentReview.agent`) now accept the name of a defined agent. A name that isn't available (the file is gone, its folder isn't trusted, its program isn't installed) falls back to the first installed agent, as a missing custom agent does.
+ A prompt limited to `claude` or `codex` is now also offered in a chat with a defined agent that runs on that program, and a `start_agent` role meant for `claude` or `codex` also fits such an agent.
+ When a New Agent Task description is too long, the message now says the limit in bytes: 8,000 bytes, which is what Flint always counted. Text outside ASCII takes two to four bytes per character. Backlog items have the same limit.

Pull request: [#83](https://github.com/flint-ide/flint/pull/83)

## All changes

### Features

* Agent definitions, model tiers and a backlog delegated to agents by @ODudek in https://github.com/flint-ide/flint/pull/83

**Full Changelog**: https://github.com/flint-ide/flint/compare/v0.6.0...v0.7.0
