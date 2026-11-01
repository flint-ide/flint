# Architectural principles

These five principles decide trade-offs in Flint. When a change conflicts with one of them, the pull request says so and why the trade-off is worth it. Reviews check new code against them; [the latest audit](audits/2026-10-principles.md) records where the code stands.

## 1. As few dependencies as possible

Every dependency is code that runs with the user's privileges, on their source code and credentials. Fewer dependencies mean a smaller attack surface, fewer supply-chain risks and fewer updates to review.

- Prefer the Go standard library and a small amount of our own code over a module that does more than we need.
- Prefer driving an existing CLI (`git`, `kubectl`, `docker`, `aws`, `rg`, `dlv`) over embedding its SDK: the user already has and trusts it, and its auth works unchanged. GitHub is the exception: its API is plain HTTPS and JSON, so Flint calls it with the standard library (`internal/github`) instead of requiring `gh`, and imports `gh`'s token in one click for those who have it.
- Runtime npm packages are limited to the editor, terminal and React. A new one needs a reason in the issue, and is installed with `npm ci --ignore-scripts`. Build tooling stays in Go (`cmd/webbuild` uses esbuild's Go API; no Node toolchain at runtime, no `npx`).
- Pin what we depend on: Go modules via `go.sum`, npm via `package-lock.json`, GitHub Actions by commit SHA. Dependabot proposes updates; `govulncheck` and `npm audit` gate CI.

**In practice:** before adding a module, check its transitive tree (`go mod graph`) and whether 50 lines of our own would do.

## 2. Simple, without needless complexity

The simplest design that works is the easiest to secure, review and change.

- Small, focused files and functions; one way to do a thing. Plain functions and data before frameworks, layers and generic abstractions.
- Pure logic lives in testable units (`frontend/src/lib`, small Go packages); stores and components wire it up.
- Fix the cause, not the symptom; delete code that no longer earns its place.
- Performance is part of simplicity: no polling where an event exists, lazy-load what isn't needed at startup, bound every cache and buffer.

**In practice:** if a reviewer needs a diagram to follow a change, it is probably too clever.

## 3. Extensible

Adding a language, an agent, a panel, a database or a run type should mean adding one unit, not editing a dozen files.

- Features register themselves in a registry (commands and shortcuts in `lib/commands.ts`, sidebar panels in `panels/index.ts`, agent drivers, LSP server specs, run-configuration detectors) instead of being listed in several places.
- Backends talk to the UI through Wails services and events with stable, typed contracts (generated bindings).
- A new external tool is a package under `internal/` with a narrow API, validated inputs and its own tests.
- What differs per operating system lives behind one small API with a file per system (`_darwin.go`, `_linux.go`, and an `_other.go` fallback that says honestly what isn't supported) and, in the UI, in `lib/platform.ts`; feature code doesn't branch on the system or hard-code ⌘ (see [Platforms](ARCHITECTURE.md#platforms)).

**In practice:** a contributor should be able to add a feature by reading one existing feature of the same kind.

## 4. AI-first

Flint is built around coding agents (Claude Code, Codex): they write and fix code; Flint gives them context and gives the user control.

- Wherever something fails (a test, a build, a crashing pod, a query, a request, a merge conflict), there is a one-step way to hand it to an agent with the relevant context.
- One path builds agent context: gather the facts, redact secrets (`internal/redact`), and put the request in the chat draft for the user to review. Nothing goes to an AI provider without the user seeing it.
- Agents investigate read-only first; changes to live systems (clusters, databases) need an explicit request. Project-level agent settings are ignored in Restricted Mode.
- Everything the user can do, an agent can do through the same CLIs and files; Flint doesn't keep state an agent can't see.

**In practice:** a new failure view without "Ask the agent" is incomplete.

## 5. Everything configurable

People work differently; Flint shouldn't force one way.

- User preferences live in `settings.json` (validated in Go, edited in Settings, with defaults for anything missing); project settings live in the repository (`.flint/run.json`, `.http` environments).
- Values a user would reasonably want to change (tool paths and arguments, language servers, agent models, shells, intervals, limits, excludes, keybindings, theme) are settings, not constants. Security limits that protect the user stay constants.
- Every setting has a sensible default, so Flint works with an empty configuration.

**In practice:** when you write a constant, ask whether someone would want a different value; if yes, make it a setting with this value as the default.
