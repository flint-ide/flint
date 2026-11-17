# Agent definitions: agents you define

Status: version 1 in progress. This document is the contract for the implementation; where the code and this text disagree, fix one of them in the same pull request.

## Problem

An "agent" in Flint is a program: Claude Code, Codex or a custom ACP command (`customAgents`). There is no way to say *who* an agent is: a reviewer that never edits, a backend developer on a stronger model, a planner. The prompt library fills a draft once; it does not make a lasting agent that tasks and other agents can be handed to.

Agent definitions add that: a named agent made of an engine (one of the programs), a model and standing instructions. A defined agent is an agent everywhere an agent name is used today: the chat's agent picker, New Agent Task and Compare agents, `defaultAgent`, the orchestration tool `start_agent`, and the usage breakdown.

This is the first step toward planning work in Flint (projects above repositories, a backlog, handing tasks to agents): tasks will point at defined agents.

## A definition

One Markdown file per agent, in the format of the prompt library (`internal/prompts`): optional comments, simple `key: value` front matter, then the body.

```markdown
---
description: Reviews a change for bugs; reports, never edits
engine: claude
model: opus
---
You are the reviewer of this project. Read the diff and the code around it.
Report bugs with file and line. Do not change files.
```

| Part | Rule |
|---|---|
| Name | The file name without `.md`. Lowercase letters, digits and dashes, starting with a letter, at most 32 characters (the rule for a custom agent's id), because the name is used as an agent name and in task branch names. |
| `description` | One line for pickers; at most 200 characters; optional. |
| `engine` | Optional. Without it the agent runs on the user's default engine: `defaultAgent` when that is a native agent that is installed, otherwise the first native agent installed. A definition is then a role that works for everyone on a team, whichever CLI each has. Set it (`claude` or `codex` in version 1) only to pin the role to one program. |
| `model` | Optional, and only with `engine`: model names belong to an engine, so a model without an engine is a problem. Replaces the engine's model from settings for this agent; checked with `agent.CheckModel`. |
| Body | The instructions. Required, at most 16,000 characters. |

Unknown keys are ignored, so newer files still load. A file that breaks a rule is skipped and reported as a problem (file and reason), like a prompt file.

Limits, fixed because they protect Flint from a hostile repository: 64 KiB per file, 50 definitions per folder.

## Where definitions live

| Source | Folder | Loaded |
|---|---|---|
| Personal | `agents/` in Flint's config directory | always |
| Project | `.flint/agents/` in each workspace root | only when the root is trusted |

- A project definition in a folder in Restricted Mode is **not loaded at all**. Its instructions and model would steer an agent with text from a repository the user has not trusted. The list reports how many were ignored and where.
- A name that is a built-in agent (`claude`, `codex`) or a custom agent's id is refused (a problem).
- When a personal and a project definition share a name, **the personal one wins** and the project one is reported as shadowed: a repository must not replace an agent the user relies on. Two roots with the same name: the first root wins, the other is reported.
- Folders are resolved like prompt folders: a symbolic link that leaves the root is refused.

## How a defined agent runs

A defined agent is registered with the session manager (`agentsession.Manager`) under its name, as a driver of its engine with two differences:

- **Model**: the definition's model, when set, replaces the model of the engine's settings. Path and extra arguments still come from the engine's settings (`agents.claude`, `agents.codex`).
- **Instructions**: given to the agent as standing instructions for the whole conversation, on start and on resume.
  - Claude Code: `--append-system-prompt`.
  - Codex: the developer instructions of `thread/start` (and of resume).
  - If an engine has no such channel, the fallback is to put the instructions in front of the first prompt of a new conversation, invisibly to the chat's transcript. Version 1 should not need it.

Everything else is the engine's: permissions and the approval policy, Flint's MCP server and its tools, Restricted Mode handling of the working folder, usage and cost.

Rules that follow:

- A defined agent is listed (and can start) only when its engine is installed: the pinned one, or any native agent when none is pinned.
- An agent without a pinned engine resolves its engine when a conversation starts, so changing `defaultAgent` applies to the next conversation. A conversation remembers the engine it started on and resumes on it, whatever the default is by then. The engine is `SessionInfo.engine`; the UI saves it with the chat and hands it back to `AgentChatAPI.Resume` (a chat saved without it resumes on the engine resolved now).
- Changing or adding a file applies to the next conversation that starts, without a restart. Conversations that are running keep what they started with (the manager already keeps a retired driver for its sessions).
- A conversation reopened after a restart finds its agent by name. When the definition is gone, resuming fails with a message that names it, as for a removed custom agent.
- Wherever Flint needs the *program* behind an agent name (a headless run for a commit message, a terminal command to resolve conflicts, per-engine settings, engine-specific UI such as slash commands), it uses the definition's engine (resolved as for a new conversation). No code may cast a defined agent's name to `agent.Kind`. A headless run uses the engine with the definition's model but not its instructions, since Flint's prompt there asks for an answer in a fixed format; the conflict terminal runs the plain engine (its command line goes through a shell, where the instructions must not).
- `defaultAgent` (and `agentReview.agent`) may name a defined agent; settings accept any name of the custom-agent-id form there, since definitions are files that come and go. When that agent is not available (file removed, folder untrusted, engine missing), Flint falls back as it does for a missing custom agent.

## API and UI

- A service next to the prompt library's (`api_prompts.go` is the model): list the definitions of the workspace roots with their source, engine, model, description, path and the problems; create a definition from a template in the personal folder or a trusted root's `.flint/agents` and return its path, which the UI opens in the editor. Nothing else: a definition is edited as a file.
- `AgentChatAPI.Agents()` lists defined agents after the native and custom ones.
- Pickers show a defined agent by name with its description and engine.
- **Settings ▸ Agents** gets a list of the defined agents (name, engine, model, source), the problems, **New Agent…** and **Open**. The command palette gets **New Agent Definition…**.

## Security

- A definition can only choose an engine, a model and text. It cannot add arguments, tools, permissions or environment, so it cannot widen what an agent may do; the engine's settings and the approval policy stay the user's.
- The instructions are passed as one argument or one protocol field, never through a shell, and never parsed as flags.
- Project definitions follow workspace trust (above). Trusting a folder already lets its `CLAUDE.md` and `AGENTS.md` instruct agents; a definition adds no new kind of power.

## Not in version 1

- Custom ACP agents as engines (pinned or as the default engine).
- Model tiers that are not tied to an engine (`model: strong`, mapped to each engine's model in settings), and running one role on a chosen engine from a picker (for Compare agents across engines).
- Per-agent permissions and tool sets, a scope (repositories or paths), budgets and turn limits, and the machine an agent runs on.
- The background session host (`hostmode`) registers native drivers only, as it does for custom agents; defined agents come with its settings sync.
- Workflows between agents (run the reviewer when a task finishes).
