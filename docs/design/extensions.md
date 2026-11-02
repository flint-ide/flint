# Extensions: feature modules (level 1) and external extensions (level 2)

Status: level 1 implemented (Docker, TODO list and the release check migrated). Level 2 implemented in its first version (contract 1): see [what is implemented and the decisions taken](#level-2-implemented-v1); the parts of this design left for later are listed there.

Flint should be easy to extend through an explicit contract. Two levels:

1. **Feature modules** (level 1): features inside Flint's repository. A feature is one Go package plus one frontend directory, listed once on each side. No edits to `settings.go`, `settings.ts`, `SettingsView`, `main.go`'s service list, the tool table or the panel list.
2. **External extensions** (level 2): separate processes in any language, installed explicitly by the user, talking JSON-RPC 2.0 over stdio and described by a manifest. Flint renders their UI from data (no extension JavaScript or HTML in the WebView). Level 2 is built on level 1: an extension's contributions land in the same registries as a feature module's.

Contents: [Goals](#goals) · [Level 1](#level-1-feature-modules) · [Level 2](#level-2-external-extensions) · [Remaining migration](#what-remains-to-migrate) · [Open questions](#open-questions)

## Goals

- **One unit per feature.** Unrelated features never touch the same file. Before this change, the Docker settings and GitHub settings pull requests conflicted in `settings.go`, `settings.ts`, `SettingsView.tsx` and the settings reference although they had nothing in common.
- **Typed and simple.** Go generics and TypeScript declaration merging give each feature its own typed settings without a central type listing them. Contributions are plain data.
- **No user-visible change.** `settings.json` keeps one flat object with the same keys and shapes; `state.json` keeps its keys; command ids, shortcuts, panel ids and the order of the UI stay as they were.
- **No startup cost.** Registration is in-memory work before the first window, a few microseconds. Nothing new runs before launch: no I/O, goroutines or backend calls. Panels, Settings pages and tab views stay lazy components.
- **Ready for level 2.** Every contribution point takes data with ids and orders (not positions in a hand-written list) and remembers who contributed it, so a contribution can come from a module or from an extension, and an extension's can be withdrawn as a whole.

Non-goals: a marketplace; running extension code inside the WebView; changing how existing, non-migrated features work.

---

## Level 1: feature modules

### Layout

```text
internal/features/<id>/        Go: module.go (the Module), api.go (the service), settings.go, tests
frontend/src/features/<id>/    TS: index.ts (the FeatureModule), components, store, pure logic, settings.ts, tests
site/reference/_settings/<key>.md   the settings reference for each key the feature owns
features.go                    the Go list: one line per feature
frontend/src/features/index.ts the frontend list: one line per feature
```

Contract code: `internal/feature` (Go), `frontend/src/lib/features.ts`, `lib/contributions.ts` and `lib/settingsSections.ts` (TypeScript), and the generic section support in `internal/settings/section.go`.

The migrated features are working examples: `docker` (service, settings section, tool, panel, Settings page), `todo` (service, a settings key holding an array, panel, Settings page) and `update` (service, a boolean key, a `state.json` key, a Start hook, a status bar item, a command, an activate hook).

### Go contract

```go
package feature

type Module struct {
	ID       string                 // "docker"
	Settings []settings.AnySection  // settings.json keys it owns
	Tools    []tools.Tool           // external programs, for the tool check and the bug report
	State    []string               // state.json keys it keeps (Host.LoadState / SaveState)
	New      func(Host) Instance    // nil when there is nothing to run
}

type Instance struct {
	Services   []application.Service      // application.NewService(&API{…}), written out in the module
	AgentTools func() []mcpserver.Tool    // tools on Flint's MCP server, read on every request
	Start      func(ctx context.Context)  // after launch; must return quickly
	Close      func()                     // at quit, in parallel with everything else
}

type Host interface {
	Context() context.Context              // ends at quit; nil before launch
	Settings() settings.Settings           // a copy; sections via Section.Get
	Roots() []string                       // open project folders, primary first
	Emit(event string, data ...any)
	OpenURL(link string) error
	Version() string
	LoadState(key string, v any) error
	SaveState(key string, v any) error
	StartStream(termID, title string, st terminal.Stream) error // a terminal tab agents can read too
}
```

The list in `features.go`:

```go
var features = []feature.Module{
	docker.Module,
	todo.Module,
	update.Module,
}
```

`Host` is the only way a feature reaches the app; `App` implements it in `features.go` (`featureHost`). A feature that needs something new from the app adds a method there, which is a deliberate, reviewable widening of the contract. Tests use `internal/feature/featuretest`, an in-memory `Host`.

#### Lifecycle

| When | What | Where |
|---|---|---|
| `NewApp`, before `settings.Load` and `state.json` are read | `feature.Register`: checks ids and state keys, registers settings sections and tools (once per process) | `registerFeatures` in `features.go` |
| `NewApp`, before the window exists | `feature.New`: calls every `Module.New`. Construction only: no I/O, no goroutines | `app.go` |
| `application.New` | the modules' services are appended after Flint's own | `main.go` (`flint.featureServices()`) |
| launch (`lifecycle.ServiceStartup`) | `Start` hooks, with the app context | `App.startup` |
| quit (`ServiceShutdown`) | `Close` hooks, in parallel with the other stops | `App.shutdown` |

The update check shows the pattern: its background loop used to be started from `App.startup`; now its module's `Start` does `go api.watch(ctx)`, with the same 5-second delay.

#### Settings sections

```go
var Section = &settings.Section[Settings]{
	Key:       "docker",
	Default:   func() Settings { return Settings{LogTailLines: 500} },
	Normalize: normalize, // optional: invalid parts → defaults (on load)
	Check:     Check,     // optional: why a value would not be saved
	Clone:     nil,       // needed when the value holds slices or maps
}

host := docker.Section.Get(h.Settings()).Host       // read
s = update.Enabled.With(s, false)                   // immutable update (tests, migrations)
```

`settings.Settings` keeps its built-in fields and carries the registered sections' values next to them (an unexported map). Its JSON methods make the document one flat object:

- `MarshalJSON` writes the built-in keys, then each registered section in registration order (its default when the value is missing).
- `UnmarshalJSON` overlays each section's raw JSON on its current value (or default), so missing fields inside a section keep their defaults, exactly like built-in keys. A value of the wrong type loses only that value, and loading tolerates it.
- `Default`, `Normalize`, `Validate` (Save) and `Clone` include every section.
- Keys must be unique and must not be built-in keys (`Register` panics, since it is a programming error). Unknown keys are still ignored on load and dropped on save, as before.

One consequence: because `Settings` now has `MarshalJSON`, the Wails generator types it as `any` in TypeScript (`settings$0.Settings = any`), and the generated `Settings` class and its nested model classes are gone. The frontend never relied on them. `coerceSettings` has always treated the backend's answer as untrusted JSON, and `store/settings.ts` now passes the plain object to `SettingsAPI.Save`. Field-level Go types that other methods use (`AgentSettings`, `CustomAgent`, `LanguageServerSettings`) are still generated.

A key can move between `Settings` and a section without a migration. The tests in `features_test.go` and in each feature package decode the old JSON shapes and check the defaults.

#### Tools

`tools.Register` adds a feature's tools to the table behind the tool check and the bug report. `tools.Install(mac, linux)` picks the install command, `Tool.Find(bin, versionArgs...)` adds a binary, and `Order` places a tool within its group (Docker 10, kubectl 20, AWS CLI 30 keep the infra group's order).

#### State

`Module.State` declares `state.json` keys. `appState` keeps them as raw JSON (`appState.features`, written after its own keys), so other parts of Flint that save `state.json` don't drop them. `Host.LoadState` / `SaveState` refuse undeclared keys. The release check keeps `"update"` there, with the shape it always had.

#### Agent tools

`Instance.AgentTools` are appended to the MCP server's tool list (`api_mcp.go`). The server's gates still apply: the server must be on and the root trusted. A feature decides from its own settings which of its tools to offer. No migrated feature uses this yet; Kubernetes' `get_kubernetes_status` is the natural first one (see the migration list).

#### Bindings

The Wails bindings generator finds services by their `application.NewService(&T{})` call, so a module writes that call out with its concrete type (a generic helper would hide the type). Bindings land in `frontend/bindings/flint/internal/features/<id>/api.ts`, and models in the same directory (`docker.Overview`). Regenerate with `wails3 task bindings` as usual.

### Frontend contract

```ts
interface FeatureModule {
  id: string                                  // same as the Go module
  settings?: readonly SettingsSection[]       // settings.json keys it owns
  panels?: readonly PanelSpec[]               // sidebar panels (+ their "Show …" commands)
  commands?: readonly Command[]               // palette commands and shortcuts
  settingsPages?: readonly SettingsPage[]     // { id, label, order, component: ComponentType<{ active }> }
  statusItems?: readonly StatusItem[]         // { id, order, component }
  tabKinds?: readonly TabKind[]               // { id, component: ComponentType<{ tab, active }>, icon? }
  fileMenu?: readonly FileMenuItem[]          // { id, label, when?(paths), run(paths) }
  activate?: () => void | (() => void)        // after the first render: event subscriptions
}
```

Example (`features/docker/index.ts`):

```ts
const DockerPanel = lazy(() => import('./DockerPanel').then((m) => ({ default: m.DockerPanel })))
const DockerSection = lazy(() => import('./DockerSection').then((m) => ({ default: m.DockerSection })))

export default defineFeature({
  id: 'docker',
  settings: [dockerSettings],
  panels: [{ id: 'docker', title: 'Docker', icon: 'ship', order: 80, component: DockerPanel,
             command: { title: 'Docker', keywords: 'containers compose logs' } }],
  settingsPages: [{ id: 'docker', label: 'Docker', order: 110, component: DockerSection }],
})
```

Components are only types in the contract, so `React.lazy` works and so does `lazyPreload` from pull request #48. A lazy component created when the feature module is evaluated is queued for background preloading like any other.

#### Installation and activation

- `installFeatures()` runs in `main.tsx` before the first render. It is data only: it checks the modules (`checkFeatures`: unique ids per kind), registers settings sections and hands panels, Settings pages, status items, tab kinds and file menu entries to the contribution registry under the source `feature:<id>`. The first paint is complete: no panel pops in later.
- `activateFeatures()` runs in App's first effect, right after Flint's own commands are registered. It registers each module's commands, plus a "Show …" command per panel, then runs `activate`. Commands and subscriptions are wired exactly when they were before.

#### Settings sections (frontend)

```ts
declare module '../../lib/settingsSections' {
  interface FeatureSettings { docker: DockerSettings }   // types settings.docker everywhere
}
export const dockerSettings = defineSection('docker', DEFAULT_DOCKER, coerceDocker)
```

`AppSettings = CoreSettings & FeatureSettings`. `coerceSettings` coerces the core keys and every registered section. `store/settings.ts` reads the localStorage cache when it loads, before features are installed. It keeps that raw JSON and fills in sections as they register (`onSectionsChanged`), so a section's value is never lost or replaced by its default. A Go test (`features_test.go`) checks that the keys passed to `defineSection` in `features/*/settings.ts` are the Go sections' keys.

#### Contribution points

| Point | Registry | How the shell uses it |
|---|---|---|
| Panels | `lib/contributions.ts` → `panels/index.ts` `usePanels()` / `currentPanels()` | merged with Flint's panels by `order` (`mergePanels`; a duplicate id throws) |
| Commands | `lib/commands.ts` (existing) | unchanged; registered by `activateFeatures` |
| Settings pages | `contributions.settingsPages` | `SettingsView` merges them with its own pages by `order` (Flint's pages are 10…160 in steps of 10) and renders them in `Suspense` |
| Status items | `contributions.statusItems` | rendered after Flint's own items, by `order` |
| Tab kinds | `contributions.tabKinds` + `useEditor().openContributed(view, key, title, data)` | a `kind: 'contrib'` tab with JSON `data`; `EditorArea` renders the kind's component, or "not available" when its source is gone; not saved in sessions |
| File tree menu | `contributions.fileMenu` | appended after "Add to Agent Chat" for the right-clicked paths; failures become a toast |
| Settings sections | `lib/settingsSections.ts` | see above |
| Hand to agent | `lib/askAgent.ts` (existing) | features call `askAgent({ prompt, attachments })`; no separate registry is needed |

`contribute(source, parts)` returns a function that withdraws everything the source added, which is what an extension needs when it stops or is uninstalled. Ids are unique per kind across sources; a colliding contribution is refused as a whole.

### Documentation

The settings reference no longer lists feature keys by hand. Each key has a partial, `site/reference/_settings/<key>.md`: its first line is the key's row in the Keys table, and the rest, if any, is its section. `cmd/docsite` pulls partials in with `<!-- include-rows _settings/*.md -->` and `<!-- include-sections _settings/*.md -->`, and treats any directory starting with `_` as partials, not pages. Links in partials are relative to the including page. A Go test fails when a registered key has no partial, so the docs can't fall behind, and two features never edit the same lines of `settings.md`.

### Verification

- Go: `internal/settings/section_test.go` (registration rules, JSON round trip, wrong types, cloning, decoding into a zero value as the binding does), `internal/feature/feature_test.go`, the feature packages' tests (the old JSON shapes, the update check against a fake host and a local release server), `internal/tools` (ordering, duplicates), `cmd/docsite/include_test.go`, and `features_test.go` (`state.json` keys survive other saves; Go and frontend keys match; every key is documented).
- Frontend: `lib/settingsSections.test.ts`, `lib/contributions.test.ts` (sources, ordering, withdrawal, duplicates, file menu), `store/editor.test.ts` (contributed tabs), plus the moved tests of the migrated features.
- Server mode, with a fresh config: the activity bar, the Settings page list and the palette entries are unchanged. A pre-written `settings.json` (`docker.logTailLines: 321`, `todoExclude: ["vendorish"]`, `checkForUpdates: false`) loads into the pages and the TODO scan. Edits save back under the same keys, and Docker is still in the tool check's infra group, before kubectl.

### What changed for the migrated features

| Feature | Go | Frontend |
|---|---|---|
| Docker | `api_docker.go` → `internal/features/docker` (`API`, `Overview`); `settings.Docker` → `docker.Section`; the tool entry → `Module.Tools`; terminal streams through `Host.StartStream` | panel, Settings page, store, tree logic and settings moved to `features/docker`; `TerminalView` imports the new binding |
| TODO list | `api_todo.go` → `internal/features/todo`; `Settings.TodoExclude` → `todo.Exclude` (`settings.CleanExcludes` / `CheckExcludes` stay shared with `watchExclude`) | panel, store and logic moved; the Settings page left `SettingsView` |
| Release check | `api_update.go` → `internal/features/update`; `Settings.CheckForUpdates` → `update.Enabled`; `appState.Update` → the state key `update`; the daily loop → a Start hook | widget left `StatusBar` (status item); page left `SettingsView`; `help.checkForUpdates` and the Help menu event moved to the module |

The bindings changed paths (`bindings/flint/dockerapi.ts` → `bindings/flint/internal/features/docker/api.ts` and so on). Command ids, event names, settings keys and the `state.json` key did not change.

---

## Level 2: external extensions

### Model

An **extension** is a directory with a manifest (`flint-extension.json`) and a program. Flint starts the program as a child process and talks JSON-RPC 2.0 to it over stdin/stdout. The program may be written in any language. Everything an extension shows is described as data and rendered by Flint's own components. An extension cannot run JavaScript, inject HTML or reach the WebView.

Inside Flint, level 2 is one level-1 feature, `extensions`:

- Go: `internal/features/extensions`, with `internal/extensions/{manifest,host,rpc}`. It discovers manifests, owns the processes and exposes one service (`ExtensionsAPI`) to the frontend.
- Frontend: `features/extensions`. It installs each extension's contributions with `contribute('extension:<id>', …)` and renders declarative views.

### Installation and discovery

- **Global extensions** live in `<config>/extensions/<id>/` (macOS `~/Library/Application Support/flint/extensions/`). The user installs one explicitly: **Settings ▸ Extensions ▸ Install from Folder…** copies a directory after showing its manifest and permissions, and the CLI equivalent is `flint --install-extension <dir>`. There is no marketplace and no automatic download in v1.
- **Project extensions** live in `<project>/.flint/extensions/<id>/`. They are only discovered in trusted folders (Restricted Mode, `trust.go`), and the user approves each one once per folder, like a custom agent's command line.
- Discovery reads manifests only, without starting any process: at startup, on a background goroutine after launch, and again when a project folder is opened. Uninstalling removes the directory. The extension's settings stay in `settings.json`, so reinstalling restores them.

### Manifest

```json
{
  "contract": 1,
  "id": "acme.jira",
  "name": "Jira",
  "version": "0.3.0",
  "publisher": "Acme",
  "description": "Your Jira issues next to your code.",
  "run": {
    "command": "bin/flint-jira",
    "args": ["--stdio"],
    "darwin": { "command": "bin/flint-jira-darwin" },
    "linux": { "command": "bin/flint-jira-linux" }
  },
  "activation": ["onCommand:acme.jira.refresh", "onView:acme.jira.issues", "onAgentTool:jira_issue"],
  "permissions": ["network:acme.atlassian.net", "secrets", "workspace.read", "agent.ask"],
  "contributes": {
    "commands": [{ "id": "acme.jira.refresh", "title": "Refresh Issues", "category": "Jira", "keys": "Mod+Alt+J" }],
    "panels": [{ "id": "acme.jira.issues", "title": "Jira", "icon": "checkbox", "order": 120, "view": "tree" }],
    "settings": {
      "title": "Jira",
      "properties": {
        "site": { "type": "string", "format": "hostname", "default": "", "description": "Your Atlassian site." },
        "jql": { "type": "string", "default": "assignee = currentUser() AND resolution = Unresolved", "maxLength": 500 },
        "pollMinutes": { "type": "integer", "minimum": 1, "maximum": 60, "default": 10 }
      }
    },
    "statusItems": [{ "id": "acme.jira.count", "order": 10 }],
    "fileMenu": [{ "id": "acme.jira.link", "label": "Link to Jira Issue…", "command": "acme.jira.link" }],
    "tabViews": [{ "id": "acme.jira.issue", "view": "form" }],
    "agentTools": [{
      "name": "jira_issue",
      "title": "Jira issue",
      "description": "Summary, status and description of a Jira issue by key (ABC-123).",
      "inputSchema": { "type": "object", "properties": { "key": { "type": "string" } }, "required": ["key"] }
    }]
  }
}
```

Rules, checked at discovery, where a violation disables the extension and is shown in Settings ▸ Extensions:

- `contract` is the integer major version of this protocol (below).
- `id` is `publisher.name` with lower-case letters, digits and dashes, unique across installed extensions. Every contributed id must start with `<id>.`, so an extension can't shadow Flint's own ids or another extension's (`git.push`, `view.docker`).
- `run.command` is a path inside the extension directory. It is never resolved through `PATH` or run through a shell. Arguments are a fixed list.
- Shortcuts are allowed but never override Flint's defaults or the user's keymap: on a conflict the extension's shortcut is dropped and the conflict listed. Users rebind extension commands in Settings ▸ Keymap like any command.
- Icons come from Flint's own icon set (`components/ui/icons.ts`); extensions cannot ship SVG.

### Mapping onto level 1

| Manifest | Level-1 point | Notes |
|---|---|---|
| `commands` | `registerCommand` | `run` sends `command/execute`, which activates the extension first |
| `panels` (`view`: tree, list, table, form) | `contribute(...panels)` | the component is Flint's `DeclarativeView` for that view kind; data from `view/resolve` |
| `settings` | one level-1 section, `extensions` (see below), plus a generated `SettingsPage` per extension | the page is rendered from the schema by Flint |
| `statusItems` | `contribute(...statusItems)` | text, tooltip and command come from `status/update` notifications; hidden until the first one |
| `tabViews` | `contribute(...tabKinds)` | opened by the extension with `window/openTab`; same view kinds as panels |
| `fileMenu` | `contribute(...fileMenu)` | runs the named command with `{ paths }` |
| `agentTools` | `Instance.AgentTools` of the `extensions` feature | offered on Flint's MCP server as `<short>_<name>` (see below) |
| notifications | `showToast`, OS notification when in the background (`notifyInBackground`) | `window/showMessage` |
| hand to agent | `askAgent` | `agent/ask`, needs the `agent.ask` permission, always ends in a draft the user sends |

All of them are contributed under the source `extension:<id>`. Stopping or uninstalling an extension withdraws them. Open tabs of its views then show "not available".

### Settings

Extension settings live in one section of `settings.json`, owned by the `extensions` feature:

```json
"extensions": {
  "enabled": { "acme.jira": true },
  "acme.jira": { "site": "acme.atlassian.net", "jql": "…", "pollMinutes": 10 }
}
```

- The Go section validates each extension's object against its manifest schema on save, with the same rules as `Check`. Normalization replaces invalid values with schema defaults.
- Objects of extensions that aren't installed are kept as raw JSON, so uninstalling and reinstalling keeps them. This is the one place where unknown keys are preserved.
- The supported JSON Schema subset is: `type` (string, integer, number, boolean, array of strings, enum), `default`, `minimum`, `maximum`, `maxLength`, `pattern`, `format` (`hostname`, `uri`), `description` and `title`. Anything else is a manifest error. The Settings page is generated from the schema with the same controls as Flint's own pages (`components/settings/fields.tsx`).
- Secrets are never settings (see Security).
- The documentation of an extension's settings is its manifest. Settings ▸ Extensions shows each setting's description. Flint's settings reference documents the `extensions` key once.

### Protocol

Transport: the process's stdin and stdout carry JSON-RPC 2.0 messages, one per line (UTF-8 JSON without raw newlines), as in MCP's stdio transport. stderr goes to Flint's log, rate-limited and truncated. A message larger than 4 MiB is a protocol error that stops the process.

Both sides send requests, responses and notifications. Ids are integers or strings, as in JSON-RPC.

#### Lifecycle

| Direction | Method | Params → result |
|---|---|---|
| Flint → ext | `initialize` (request) | `{ contract: 1, flintVersion, extension: { id, version, dir }, settings, roots: [{ path, trusted }], platform: "darwin" \| "linux", capabilities: { views: ["tree","list","table","form"], secrets: true, agent: true } }` → `{ capabilities: { … } }` |
| Flint → ext | `initialized` (notification) | `{}` |
| Flint → ext | `shutdown` (request) | `{}` → `null`; then Flint closes stdin and, after 2 s, kills the process group |

#### Flint → extension

| Method | Kind | Params → result |
|---|---|---|
| `command/execute` | request | `{ id, args? }` (args: `{ paths }` from the file menu) → `null` |
| `view/resolve` | request | `{ view, parent? }` → a view model (below); `parent` lazily expands a tree node |
| `view/action` | request | `{ view, item, action }` → `null` (context menu or button on an item) |
| `form/submit` | request | `{ view, values }` → `{ errors?: { field: message } }` |
| `agentTool/call` | request | `{ name, root, arguments }` → `{ text }` (shown to the agent; errors become failed tool calls) |
| `settings/changed` | notification | `{ settings }` (the extension's object, already validated) |
| `workspace/rootsChanged` | notification | `{ roots: [{ path, trusted }] }` |
| `view/visible` | notification | `{ view, visible }`; the extension may poll only while visible |

#### Extension → Flint

| Method | Kind | Params → result | Permission |
|---|---|---|---|
| `view/refresh` | notification | `{ view, item? }`; Flint calls `view/resolve` again | none |
| `status/update` | notification | `{ id, text, tooltip?, command?, severity? }`, where `text` is at most 40 characters | none |
| `window/showMessage` | notification | `{ type: "info" \| "warning" \| "error", message }` | none |
| `window/showMessageRequest` | request | `{ type, message, actions: [title] }` → `{ action? }` | none |
| `window/openTab` | request | `{ view, key, title, data }` → `null` | none |
| `window/openFile` | request | `{ path, line? }` → `null` (the path must be inside a root) | `workspace.read` |
| `workspace/readFile` | request | `{ path }` → `{ text }` (inside a root; 4 MiB at most; never `.git/`, never ignored secrets files such as `.env`) | `workspace.read` |
| `secrets/get`, `secrets/set`, `secrets/delete` | request | `{ key, value? }` → `{ value? }` | `secrets` |
| `agent/ask` | request | `{ prompt, attachments?: [{ path, from?, to? }] }` → `null` (fills a chat draft; never sends) | `agent.ask` |
| `log` | notification | `{ level, message }` | none |

There is no `exec` and no generic HTTP proxy. The extension is its own process and does its own network I/O, under the `network:` permission it declared (see Security).

#### View models

```jsonc
// tree: nodes are expanded lazily with view/resolve { parent: id }
{ "kind": "tree", "items": [{ "id": "ABC-1", "label": "Fix login", "description": "In progress",
  "icon": "checkbox", "hasChildren": false, "actions": [{ "id": "open", "label": "Open in Browser" }],
  "command": { "id": "acme.jira.open", "args": { "key": "ABC-1" } } }] }
// list: like tree, without children
// table
{ "kind": "table", "columns": [{ "id": "key", "title": "Key" }], "rows": [{ "id": "ABC-1", "cells": { "key": "ABC-1" } }] }
// form: rendered with Settings' field components
{ "kind": "form", "fields": [{ "id": "comment", "type": "string", "title": "Comment", "multiline": true }], "submit": "Add Comment" }
// any kind may instead be a message
{ "kind": "message", "text": "Sign in to Jira first.", "actions": [{ "id": "signin", "label": "Sign In" }] }
```

Sizes are bounded: 5,000 items per response and 2,000 characters per label. Text is always text, never HTML or Markdown, except that a form may set `"markdown": true` on a read-only field, which Flint renders with its sanitizing preview.

### Activation and limits

- **Lazy.** A process starts on its first activation event: a command run, a panel or tab view shown, an agent tool called, or `onStartup`. `onStartup` needs to be declared and is shown as "runs at startup" when installing. Even then the process starts after launch, never before the window.
- **Idle stop.** With no request for 5 minutes, no visible view and no pending request, Flint sends `shutdown`. The next event starts the process again. Status items keep their last text, and the next start refreshes them.
- **Timeouts.** `initialize`: 10 s. `view/resolve`: 5 s, after which the view shows "not responding". Other requests: 30 s, or 120 s for `agentTool/call`. A request the user cancels sends `$/cancelRequest`, as in LSP.
- **Resources.** One process per extension. Processes are killed as a process group (`internal/proctree`). Stdout throughput above 8 MiB/s for 5 s stops the process. Memory and CPU are not limited by the OS in v1 (see open questions). Settings ▸ Extensions shows each process's state and RSS.
- **Crashes.** A crash ends the extension's pending requests with an error and withdraws nothing; the next event restarts the process. After 3 crashes in 10 minutes the extension is disabled until the user re-enables it, and a toast says so once.

### Security model

- **Trust is explicit.** Installing is a user action that shows the manifest, its permissions and its program path. Flint never installs, updates or enables an extension on its own.
- **Restricted Mode.** Project extensions are never discovered in untrusted folders. Global extensions run, but untrusted roots are passed as `trusted: false`; `workspace/readFile`, `window/openFile` and `agent/ask` refuse paths in them; and an extension's agent tools are not offered for them (the MCP server already refuses untrusted roots).
- **Permissions** gate what Flint does on an extension's behalf. Flint enforces them; a request without the permission fails with JSON-RPC error `-32001`. The permissions are `workspace.read`, `secrets`, `agent.ask` and `network:<host>[,<host>…]`. Network is declarative only: it is shown at install and in Settings, but an extension is a native process that Flint can't confine. This is said plainly in the install dialog ("runs with your user's permissions"). Sandboxing is an open question.
- **Secrets** are stored only in the OS keyring through Flint, under service `flint-extensions` and account `<id>:<key>`. Settings and `state.json` never hold them, and an extension can't read another extension's keys.
- **No telemetry.** Flint sends nothing about extensions anywhere, and the protocol has no telemetry channel. Extensions whose description or permissions suggest analytics endpoints are the user's call; the install dialog shows every declared network host.
- **No WebView code.** Contributions are data rendered by Flint's components, so an extension cannot run script in the UI or read other features' state.
- **Agent tools.** Extension tools are read-only from Flint's point of view: Flint passes the arguments and returns the text. The tool names carry the extension's short name (`jira_jira_issue` → `mcp__flint__jira_jira_issue`). MCP limits tool names to 64 characters of `[A-Za-z0-9_-]`, so names are shortened and deduplicated. Each extension's tools can be turned off in Settings ▸ Agent Context.

### Versioning

- `contract` is a major version. Flint supports the current major and refuses others with a clear message ("built for Flint's extension contract 2; this Flint supports 1").
- Within a major version, changes are additive only: new methods, new optional fields and new view kinds, advertised in `initialize`'s `capabilities` on both sides. A method the other side doesn't support answers `-32601` (method not found), and callers must handle that.
- Level 1 has no compatibility promise: modules live in the repository and change with it. Level 2's manifest and protocol are the public surface. The level-1 points they map onto can change as long as the mapping keeps working.

### Example: a minimal extension (Python)

```python
#!/usr/bin/env python3
# flint-extension.json: {"contract":1,"id":"me.hello","name":"Hello","version":"0.1.0",
#   "run":{"command":"hello.py"},"activation":["onView:me.hello.list","onCommand:me.hello.greet"],
#   "contributes":{"commands":[{"id":"me.hello.greet","title":"Say Hello"}],
#                  "panels":[{"id":"me.hello.list","title":"Hello","icon":"files","order":200,"view":"list"}]}}
import json, sys

def send(msg):
    sys.stdout.write(json.dumps(msg) + "\n"); sys.stdout.flush()

for line in sys.stdin:
    msg = json.loads(line)
    method, mid = msg.get("method"), msg.get("id")
    if method == "initialize":
        send({"jsonrpc": "2.0", "id": mid, "result": {"capabilities": {}}})
    elif method == "view/resolve":
        send({"jsonrpc": "2.0", "id": mid, "result": {"kind": "list", "items": [{"id": "1", "label": "Hello from Python"}]}})
    elif method == "command/execute":
        send({"jsonrpc": "2.0", "method": "window/showMessage", "params": {"type": "info", "message": "Hello!"}})
        send({"jsonrpc": "2.0", "id": mid, "result": None})
    elif method == "shutdown":
        send({"jsonrpc": "2.0", "id": mid, "result": None}); break
```

### Implementation plan (level 2)

1. `internal/extensions/manifest`: parsing, validation, the schema subset and id rules, with table tests.
2. `internal/extensions/rpc`: newline-delimited JSON-RPC over pipes, with bounded messages, cancellation and timeouts, fuzzed.
3. `internal/extensions/host`: discovery, lazy start, idle stop, crash policy, permissions and keyring access, tested with an extension written in Go under `testdata`.
4. `internal/features/extensions`: the level-1 module, with the `extensions` settings section, agent tools, a Start hook (discovery after launch) and a Close hook (stop all).
5. `frontend/src/features/extensions`: `DeclarativeView` (tree, list, table, form, message), the Settings ▸ Extensions page (installed list, permissions, state, enable and disable), and contribution install and withdrawal per extension.
6. Docs: a user guide page and a protocol reference generated from the Go types.

### Level 2: implemented (v1)

Code: `internal/extensions/{manifest,rpc,host}`, the module `internal/features/extensions`, `frontend/src/features/extensions`, the example `examples/extensions/stats` (Go, standard library). Docs: [site/guides/extensions.md](../../site/guides/extensions.md) (users), [site/extensions/write-an-extension.md](../../site/extensions/write-an-extension.md) and [site/extensions/protocol.md](../../site/extensions/protocol.md) (authors; the reference is written by hand, not generated). Tests: manifest table tests, the RPC transport (with a fuzz target), the host against `host/testdata/testext` (lazy start, permissions, crashes, idle stop, timeouts, process-group kill, environment, tools, settings) and a conformance test that builds the example and drives every contribution point it uses; frontend tests for the translation into contributions, the settings section and the schema checks.

**Implemented as designed:** the manifest and its rules (ids namespaced by `<id>.`, `run.command` inside the folder, no shell or `PATH`), the lifecycle (`initialize` with contract check, `initialized`, `shutdown`, then stdin closed and the process group killed after 2 s), lazy activation, idle stop after 5 minutes, the timeouts, the 4 MiB message cap and the 8 MiB/s × 5 s flood limit, 3 crashes in 10 minutes stop the extension, `$/cancelRequest`, the permissions and `-32001`, the keyring layout, the `extensions` settings section (objects of extensions that aren't installed are kept), agent tools named `<short>_<name>`, contributions under `extension:<id>` withdrawn when an extension is turned off or uninstalled.

**Decisions where this design was open or is narrowed in v1:**

- **Project-local extensions are not supported.** Only user-installed extensions in `<config>/extensions/<id>/`. Project ones wait for sandboxing (open question 1); a repository can't make Flint run anything.
- **Restricted Mode is stricter than designed:** untrusted roots are not passed at all (instead of `trusted: false`), so an extension learns only of folders the user trusts. `roots` keeps the `trusted` field for compatibility; it is always `true`.
- **Installation** is the palette command and Settings ▸ Extensions ▸ Install from Folder…; the folder picker is the frontend's, the confirmation is Go's native dialog (in the dev-only server build it says yes, like every native confirmation there). No `flint --install-extension` CLI yet. A copy skips `.git`, follows symbolic links only inside the folder, and is limited to 10,000 files and 512 MiB; the old version is stopped before it is replaced.
- **Discovery** happens on the frontend's first `List` after the first render (and on install, uninstall, **Reload**), not on a goroutine at launch; folders aren't watched. `onStartup` extensions start after that first `List`.
- **Contribution points in v1:** commands, panels (`tree`, `list`, plus `message`), the settings page, status items, toasts, `window/showMessageRequest` (a native question with up to three buttons), files and links, chat drafts and agent tools. `table` and `form` views, `tabViews` / `window/openTab` and `fileMenu` are not implemented: a manifest with a `table` or `form` panel is refused, and unknown contribution points are ignored with a warning in Settings.
- **`window/openUrl`** (not in the original tables) opens `http(s)` links only, after a native confirmation once per extension and origin per session. The extension could open a browser itself, so this is about not surprising the user, not containment.
- **Settings are checked when they are written and when they are used,** not when `settings.json` is saved: Go's save check is structural (ids, sizes). The generated page refuses invalid values, and the extension always receives schema-valid values (invalid ones become defaults). This way a newer manifest with a stricter schema can never make `settings.json` unsavable. `enabled` and `agentTools` are maps where a missing id means on.
- **Crash policy details:** the next start waits 1 s after the first crash and 4 s after the second; an extension with a panel on screen or `onStartup` is restarted on its own after that delay, others on their next use. "Failed" lasts for the session; **Restart** (or turning it off and on) clears it. Contributions stay while an extension is crashed or failed; their use reports why.
- **Idle stop** doesn't apply to `onStartup` extensions: the user saw "starts with Flint" when installing, so they run for the session.
- **Agent tools are read-only only** (open question 4 stays open): there is no approval path for acting tools. They are listed from manifests (no process starts for `tools/list`), offered only for trusted roots while the MCP server is on, and can be turned off per extension in Settings ▸ Extensions (`extensions.agentTools`), not in Settings ▸ Agent Context.
- **Environment:** `PATH` (with the login shell's additions), `HOME`, `USER`, `LOGNAME`, `SHELL`, `TMPDIR`, `TZ`, `LANG`, `LC_ALL`, `LC_CTYPE`, proxy variables, and `FLINT_EXTENSION_ID`, `FLINT_EXTENSION_DIR`, `FLINT_VERSION`. The working directory is the extension's folder.
- **Logs** are in memory (the last 256 KiB per extension, stderr lines cut at 2,000 characters), shown in Settings ▸ Extensions; they are not written to Flint's log file. RSS is not shown yet.
- **Uninstall** deletes the folder and keeps the settings and keychain secrets (the keyring API can't list an extension's keys).
- **Level-1 contract additions** this needed: `Host.Trusted`, `ConfigDir`, `Confirm`, `Choose`, and `Instance.SettingsChanged`, `RootsChanged`.
- **Panels appear after the first paint.** Extensions are discovered after the window is up, so their activity bar buttons arrive a moment later; a stored sidebar choice of an extension panel falls back to Files on the next start.

---

## What remains to migrate

Each line moves to `internal/features/<id>` plus `frontend/src/features/<id>`, typically in one pull request per feature. Order by value: features that other pull requests touch often go first.

| Feature | Go today | Frontend today | Contribution points it will use |
|---|---|---|---|
| Kubernetes | `api_kube.go`, `mcptools/kube.go`, `refresh.kubernetesSeconds`, `mcpServer.tools.kubernetes` | `KubePanel`, `store/kube.ts` | service, tool (kubectl), agent tool (`get_kubernetes_status`), panel; the refresh key would move into a `kubernetes` section with a fallback read of `refresh` |
| AWS profiles | `api_aws.go`, `refresh.awsSeconds` | `AwsPanel`, `store/aws.ts` | service, tool (aws), panel |
| GitHub, accounts and CI | `api_github.go`, `api_accounts.go`, `api_ci.go`, `github` and `ci` keys (reworked by #47, now merged) | pull request panel and tabs, `CIWidget`, `AccountsSection`, `CISection` | services, `github` and `ci` sections, panel, tab kinds (`pr`, `prdiff`, `cilog`), status item |
| Usage | `api_usage.go`, `usage_state.go`, `usage` key | `UsagePanel`, `UsageSection`, the agent usage status widget | service, section, panel, page, status item, Start/Close (`openUsage` / `closeUsage`) |
| HTTP client | `api_http.go` | `httpClient` store, `HttpResponseView` (`http` tab), commands in `featureCommands.ts` | service, tab kind, commands |
| Profiling | `api_profile.go` | `ProfileView` (`profile` tab), commands | service, tab kind, Close (`profiles.closeAll`) |
| Databases and scratchpads | `api_database.go`, `api_scratch.go` | panels, SQL consoles | services, panels, commands; scratch tabs are core enough to stay |
| Inline completions and Edit with Agent | `api_completions.go`, `completions` and `inlineEdit` keys | `EditorAiSection`, `CompletionsWidget` | service, sections, page, status item |
| Agent tasks, prompts, agent review | `api_tasks.go`, `api_prompts.go`, `api_agent_review.go`, their keys | panels, pages, commands | services, sections, panels, pages; they depend on agent chat, which stays core |
| Preview | `api_preview.go` | preview store and views | service, commands |

Stays in core: workspace, files, search, git, editor, terminals, run and debug, LSP, agent chat, settings, keymap, appearance and the MCP server.

Points not yet in the contract, to add when a migration needs them:

- **Native menu items.** The Help ▸ Check for Updates… item is still built in `menu.go` and emits an event the update module listens for.
- **Terminal stream sources.** Docker log and shell tabs are terminal tabs with a `docker` source that `TerminalView` knows about.
- **Ordering among status items.** Flint's own status items are not in the registry; contributed items come after them.
- **Session restore for contributed tabs.** Their `data` is JSON, so saving them is a small step, but a view must be able to rebuild itself from it.

## Open questions

1. **Sandboxing extension processes.** On macOS, `sandbox-exec` is deprecated but works, and an App Sandbox helper is heavy. On Linux, Landlock or seccomp would need cgo or a helper. v1 relies on explicit installation and honest disclosure. *Decided for v1:* project-local extensions are disabled entirely until sandboxing exists.
2. **Memory and CPU limits.** `setrlimit` on Linux is easy; macOS has no reliable per-process memory cap. Should Flint watch RSS and stop a runaway extension instead?
3. **Distribution.** "Install from folder" leaves signing and updates to the user. A signed index (minisign) could come later without changing the protocol.
4. **Agent tools that act.** v1 extension tools are read-only, like Flint's own MCP tools. Tools that change state would need the agent approval policy (`agentsession.Policy`) to apply to them.
5. **Shared views between extensions.** A tab kind from one extension used by another is out of scope; ids are namespaced per extension.
6. **The Wails `Settings` type is `any` in TypeScript.** If typed bindings for settings are wanted back, `SettingsAPI` could return a wrapper type, at the cost of a second representation. Today the frontend's own `AppSettings` is the typed view and nothing was lost.
7. **Contract checks in CI.** Level 1 is checked by tests (Go and frontend keys, documentation partials, duplicate ids). Level 2 has `host/testdata/testext` and the conformance test of `examples/extensions/stats`, which build with `go` and are skipped without it; they run wherever `go test ./...` runs, which includes CI's macOS and Linux jobs.
