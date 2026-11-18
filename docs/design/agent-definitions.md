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
| `model` | Optional. A model tier (`fast`, `balanced`, `strong`; see [Model tiers](#model-tiers)), with or without `engine`. Any other value is a model name, only with `engine`: model names belong to an engine, so a model name without an engine is a problem. Replaces the engine's model from settings for this agent; checked with `agent.CheckModel`. |
| Body | The instructions. Required, at most 16,000 characters. |

Unknown keys are ignored, so newer files still load. A file that breaks a rule is skipped and reported as a problem (file and reason), like a prompt file.

Limits, fixed because they protect Flint from a hostile repository: 64 KiB per file, 50 definitions per folder.

## Model tiers

A model name ties a role to one engine (`opus` means nothing to Codex). A tier abstracts from it: a reviewer that says `model: strong` and no engine works on whichever engine each teammate has.

- Three tier names, reserved words (`agent.Tiers`): `fast`, `balanced`, `strong`, matched exactly (lowercase). In a definition, a `model` that is a tier name is a tier; any other value is a model name as before.
- Settings map a tier to a model per native engine: `agents.claude.tiers` and `agents.codex.tiers`, an object `{ "fast": "…", "balanced": "…", "strong": "…" }` next to `model` (`settings.AgentSettings.Tiers`). Each value is checked with `agent.CheckModel`; an unknown tier key or an invalid value is refused by `Validate` (and by `SettingsAPI.CheckAgent`) and dropped by `Normalize` when the file is loaded, so the tier falls back to its default. A value of the wrong type is dropped the same way.
- Defaults (`agent.DefaultTiers`): Claude Code `fast: haiku`, `balanced: sonnet`, `strong: opus`, its own stable aliases. Codex has none, because its model names change.
- A tier missing from settings uses the default; a tier set to `""` has no model, not even the default. A tier without a model is no override: the agent runs on the engine's `model` from settings (or the CLI's default), as if the definition named none. `""` is how a user clears a Claude Code default; the Settings field can set a model or return to the default (empty), but not write `""`, which stays a `settings.json` edit.
- **Resolution** happens when a conversation starts, on the engine it resolves to, from the settings of that moment: `settings.AgentConfig` fills `agent.Config.Tiers` (settings over defaults, tiers without a model left out) and `definedConfig` replaces the model with the tier's (`definedModel`). Drivers read their settings on every start and resume, so an unpinned `strong` reviewer gets `opus` on Claude Code and the mapped model (or the engine's model) on Codex, and a resumed conversation resolves the tier again on its recorded engine. Headless runs (review, commit message, inline edit) resolve it the same way in `agentView.native`.
- Same as for a model name, a changed mapping applies to the next conversation; running ones keep their process.

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

A defined agent runs on a driver of its engine, with two differences:

- **Model**: the definition's model, when set, replaces the model of the engine's settings: a model name on the engine it pins, a tier's model (from settings or the defaults) on whichever engine runs it. Path and extra arguments still come from the engine's settings (`agents.claude`, `agents.codex`).
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
- A chat starts on exactly the definition its own call read. Each read builds (or keeps, when unchanged) the definition's drivers, one per engine; the call that opens or resumes a chat resolves the engine once and hands that engine's driver to the session manager (`Manager.StartWith`, `ResumeWith`). Nothing is registered by name, so a read made by another call in between (a file edited or removed, a folder untrusted) cannot change what this chat runs; it applies to the next one.
- Wherever Flint needs the *program* behind an agent name (a headless run for a commit message, a terminal command to resolve conflicts, per-engine settings, engine-specific UI such as slash commands), it uses the definition's engine (resolved as for a new conversation). No code may cast a defined agent's name to `agent.Kind`. The conflict terminal runs the plain engine (its command line goes through a shell, where the instructions must not).
- Headless runs use the engine with the definition's model (a tier resolved on that engine). A **review** (Review Changes with Agent, `AgentAPI.ReviewChanges`) also gets the instructions: a reviewer's rules are the point of setting it as the review agent, and Flint's prompt fixes only the form of the answer, which Flint parses and checks. Runs whose answer has a fixed mechanical form (a commit message, Edit with Agent's replacement text) leave them out. A review passes them like a chat, as one argument: Claude Code `--append-system-prompt=…`; Codex `-c developer_instructions=…`, the config key behind `thread/start`'s developer instructions, as a TOML basic string with every quote, backslash and control character escaped (Codex parses `-c` values as TOML). The pull request reviews (Request Code Review, Address review comments) and ✦ Fix it go to a chat, which has the instructions when it runs a defined agent.
- One call reads the files once and passes what it read along (`agentView`: the definitions, the native CLIs found and the default agent).
- An orchestration role (`.flint/roles`, `internal/orchestrate`) whose `agent` is an engine (`claude`, `codex`) is for that program: it fits a defined agent that runs on it (the engine resolved for this chat). A role whose `agent` names a defined or custom agent fits that agent only. The prompt library's `agent` key follows the same rule in the UI.
- `defaultAgent` (and `agentReview.agent`) may name a defined agent; settings accept any name of the custom-agent-id form there, since definitions are files that come and go. When that agent is not available (file removed, folder untrusted, engine missing), Flint falls back as it does for a missing custom agent.

## API and UI

- A service next to the prompt library's (`api_prompts.go` is the model): list the definitions of the workspace roots with their source, engine, model, description, path and the problems; with each, the engine and model a new conversation would use now (`resolvedEngine`; `resolvedModel`: a tier's model on that engine, the model name itself, or `""` for none, an unmapped tier or no engine installed), while `model` stays as the file writes it; create a definition from a template in the personal folder or a trusted root's `.flint/agents` and return its path, which the UI opens in the editor. Nothing else: a definition is edited as a file.
- `AgentChatAPI.Agents()` lists defined agents after the native and custom ones.
- Pickers show a defined agent by name with its description, engine and model; a tier as `strong (opus)`, or `strong` when it has no model on the resolved engine.
- **Settings ▸ Agents** gets a list of the defined agents (name, engine, model, source), the problems, **New Agent…** and **Open**. The command palette gets **New Agent Definition…**. Each native engine's block (Claude Code, Codex) gets a field per tier (Fast, Balanced, Strong model) with the default as placeholder. The template New Agent… writes mentions tiers in its comment.

## Security

- A definition can only choose an engine, a model and text. It cannot add arguments, tools, permissions or environment, so it cannot widen what an agent may do; the engine's settings and the approval policy stay the user's.
- A tier adds nothing that can be injected: it resolves only to a model string from the user's own settings (validated with `agent.CheckModel`) or a built-in default, so a project file can name a tier but not decide what it means.
- The instructions are passed as one argument or one protocol field, never through a shell, and never parsed as flags.
- Project definitions follow workspace trust (above). Trusting a folder already lets its `CLAUDE.md` and `AGENTS.md` instruct agents; a definition adds no new kind of power.

## Not in version 1

- Custom ACP agents as engines (pinned or as the default engine).
- Running one role on a chosen engine from a picker (for Compare agents across engines).
- Per-agent permissions and tool sets, a scope (repositories or paths), budgets and turn limits, and the machine an agent runs on.
- The background session host (`hostmode`) registers native drivers only, as it does for custom agents; defined agents come with its settings sync.
- Workflows between agents (run the reviewer when a task finishes).
