# Extensions: feature modules (level 1) and external extensions (level 2)

Status: level 1 implemented (Docker, TODO list and the release check migrated). Level 2: the manifest, installing from a folder and color themes are implemented ([what exists today](#level-2-what-exists-today)); extension processes, declarative views, tab views and editor decorations are designed, not implemented.

Flint should be easy to extend through an explicit contract. Two levels:

1. **Feature modules** (level 1): features inside Flint's repository. A feature is one Go package plus one frontend directory, listed once on each side. No edits to `settings.go`, `settings.ts`, `SettingsView`, `main.go`'s service list, the tool table or the panel list.
2. **External extensions** (level 2): separate processes in any language, installed explicitly by the user, talking JSON-RPC 2.0 over stdio and described by a manifest. Flint renders their UI from data (no extension JavaScript or HTML in the WebView). Level 2 is built on level 1: an extension's contributions land in the same registries as a feature module's.

Contents: [Goals](#goals) · [Level 1](#level-1-feature-modules) · [Level 2](#level-2-external-extensions) ([themes](#themes-and-file-icons) · [components](#component-catalog) · [tab views](#tab-views) · [editor decorations](#editor-decorations) · [today](#level-2-what-exists-today)) · [Remaining migration](#what-remains-to-migrate) · [Open questions](#open-questions)

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
	PickFolder(title string) (string, error)  // native folder picker; "" when cancelled
	Ask(title, message, ok string) bool        // native question the web view can't answer
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
  themes?: readonly ThemeContribution[]       // { id, label, group, base, colors, terminal }
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

- `installFeatures()` runs in `main.tsx` before the first render. It is data only: it checks the modules (`checkFeatures`: unique ids per kind), registers settings sections and hands panels, Settings pages, status items, tab kinds, file menu entries and themes to the contribution registry under the source `feature:<id>`. The first paint is complete: no panel pops in later.
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
| Color themes | `contributions.themes` | Settings ▸ Appearance ▸ Color theme (shown when there is one); choosing one copies it into `customTheme` (`lib/contributedThemes.ts`) |
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
    "tabViews": [{ "id": "acme.jira.issue", "view": "stack" }],
    "agentTools": [{
      "name": "jira_issue",
      "title": "Jira issue",
      "description": "Summary, status and description of a Jira issue by key (ABC-123).",
      "inputSchema": { "type": "object", "properties": { "key": { "type": "string" } }, "required": ["key"] }
    }],
    "themes": [{
      "id": "acme.jira.dark", "label": "Jira Dark", "base": "dark",
      "colors": { "--bg-editor": "#1d2125", "--accent": "#579dff", "--syn-keyword": "#9f8fef" },
      "terminal": { "background": "#1d2125", "red": "#f87168", "brightBlue": "#85b8ff" }
    }],
    "fileIcons": {
      "names": { "jira.yml": { "label": "JI", "bg": "#0c66e4" } },
      "extensions": { "jql": { "label": "JQL", "bg": "#0c66e4" } }
    },
    "decorations": [{ "id": "acme.jira.refs", "label": "Jira issue references", "languages": ["go", "typescript"] }]
  }
}
```

Rules, checked at discovery, where a violation disables the extension and is shown in Settings ▸ Extensions:

- `contract` is the integer major version of this protocol (below).
- `id` is `publisher.name` with lower-case letters, digits and dashes, unique across installed extensions. Every contributed id must start with `<id>.`, so an extension can't shadow Flint's own ids or another extension's (`git.push`, `view.docker`).
- `run.command` is a path inside the extension directory, written as a clean relative path with forward slashes. It is never resolved through `PATH` or run through a shell. Arguments are a fixed list. `run` may be left out when the extension contributes only data (themes, file icons, settings): such an extension never runs code.
- `activation` events must name something the manifest contributes (`onCommand:<command id>`, `onView:<panel or tab view id>`, `onAgentTool:<name>`, `onLanguage:<language>` for a decoration set's language) or be `onStartup`.
- Every list is bounded (100 entries per kind, 50 themes) and every string has a maximum length; text may not contain control characters. The manifest is at most 1 MiB.
- Settings may use only the schema subset below. An unknown keyword is an error rather than ignored, so an extension never relies on a check Flint would skip. Unknown top-level fields are ignored, so a newer manifest with a new optional field still installs (additive changes within a contract).
- Shortcuts are allowed but never override Flint's defaults or the user's keymap: on a conflict the extension's shortcut is dropped and the conflict listed. Users rebind extension commands in Settings ▸ Keymap like any command.
- Icons come from Flint's own icon set (`components/ui/icons.ts`); extensions cannot ship SVG.

### Mapping onto level 1

| Manifest | Level-1 point | Notes |
|---|---|---|
| `commands` | `registerCommand` | `run` sends `command/execute`, which activates the extension first |
| `panels` (`view`: any kind of the [component catalog](#component-catalog)) | `contribute(...panels)` | the component is Flint's `DeclarativeView`; data from `view/resolve` |
| `settings` | one level-1 section, `extensions` (see below), plus a generated `SettingsPage` per extension | the page is rendered from the schema by Flint |
| `statusItems` | `contribute(...statusItems)` | text, tooltip and command come from `status/update` notifications; hidden until the first one |
| `tabViews` | `contribute(...tabKinds)` | see [Tab views](#tab-views); same view kinds as panels |
| `fileMenu` | `contribute(...fileMenu)` | runs the named command with `{ paths }` |
| `themes` | `contribute(...themes)` (implemented) | offered in Settings ▸ Appearance; choosing one copies it into `customTheme` ([Themes](#themes-and-file-icons)) |
| `fileIcons` | `contribute(...fileIcons)` (new level-1 point) | badges in the file tree and tab strip, merged after Flint's own |
| `decorations` | `contribute(...decorationSets)` (new level-1 point) | gutter markers, inline hints, warnings and highlights in editors ([Editor decorations](#editor-decorations)) |
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
| Flint → ext | `initialize` (request) | `{ contract: 1, flintVersion, extension: { id, version, dir }, settings, roots: [{ path, trusted }], platform: "darwin" \| "linux", capabilities: { views: ["tree","list","table","form","markdown","chart","cards","stack"], charts: ["line","bar","area"], decorations: ["gutter","inline","diagnostic","highlight"], secrets: true, agent: true } }` → `{ capabilities: { … } }` |
| Flint → ext | `initialized` (notification) | `{}` |
| Flint → ext | `shutdown` (request) | `{}` → `null`; then Flint closes stdin and, after 2 s, kills the process group |

#### Flint → extension

| Method | Kind | Params → result |
|---|---|---|
| `command/execute` | request | `{ id, args? }` (args: `{ paths }` from the file menu) → `null` |
| `view/resolve` | request | `{ view, key?, data?, parent? }` → a view model ([component catalog](#component-catalog)); `parent` lazily expands a tree node; `key` and `data` identify a [tab view](#tab-views)'s tab |
| `view/action` | request | `{ view, key?, item, action, values? }` → `null` (a context menu entry, or a button on an item, a card or a form) |
| `form/submit` | request | `{ view, key?, form?, values }` → `{ errors?: { field: message } }` (`form` names a form inside a stack) |
| `editor/opened` | notification | `{ path, language, version }`; only for files in trusted roots whose language a decoration set lists |
| `editor/changed` | notification | `{ path, version }`, at most every 300 ms per file while typing; the text is read with `workspace/readFile` (unsaved text: see [Editor decorations](#editor-decorations)) |
| `editor/closed` | notification | `{ path }` |
| `agentTool/call` | request | `{ name, root, arguments }` → `{ text }` (shown to the agent; errors become failed tool calls) |
| `settings/changed` | notification | `{ settings }` (the extension's object, already validated) |
| `workspace/rootsChanged` | notification | `{ roots: [{ path, trusted }] }` |
| `view/visible` | notification | `{ view, key?, visible }`; the extension may poll only while visible |

#### Extension → Flint

| Method | Kind | Params → result | Permission |
|---|---|---|---|
| `view/refresh` | notification | `{ view, key?, item? }`; Flint calls `view/resolve` again (one tab when `key` is given) | none |
| `editor/setDecorations` | notification | `{ set, path, version?, decorations }`: replaces the set's decorations in one file ([Editor decorations](#editor-decorations)) | `workspace.read` |
| `editor/clearDecorations` | notification | `{ set, path? }`: one file, or every file | none |
| `status/update` | notification | `{ id, text, tooltip?, command?, severity? }`, where `text` is at most 40 characters | none |
| `window/showMessage` | notification | `{ type: "info" \| "warning" \| "error", message }` | none |
| `window/showMessageRequest` | request | `{ type, message, actions: [title] }` → `{ action? }` | none |
| `window/openTab` | request | `{ view, key, title, data? }` → `null`; opens or focuses the tab ([Tab views](#tab-views)) | none |
| `window/openFile` | request | `{ path, line? }` → `null` (the path must be inside a root) | `workspace.read` |
| `workspace/readFile` | request | `{ path }` → `{ text }` (inside a root; 4 MiB at most; never `.git/`, never ignored secrets files such as `.env`) | `workspace.read` |
| `secrets/get`, `secrets/set`, `secrets/delete` | request | `{ key, value? }` → `{ value? }` | `secrets` |
| `agent/ask` | request | `{ prompt, attachments?: [{ path, from?, to? }] }` → `null` (fills a chat draft; never sends) | `agent.ask` |
| `log` | notification | `{ level, message }` | none |

There is no `exec` and no generic HTTP proxy. The extension is its own process and does its own network I/O, under the `network:` permission it declared (see Security).

### Component catalog

An extension describes what to show; Flint draws it with its own components, in its theme, with its keyboard handling and virtualization. Most extension UIs are dashboards, lists of results and reports, and these components cover them while keeping Flint's look and its speed. The catalog is the `kind` of a view model, the answer to `view/resolve`. The same kinds work in a sidebar panel and in an editor tab.

| Kind | Draws | Built on |
|---|---|---|
| `tree` | nodes with icon, label, description, lazy children, context actions | the file tree's row and keyboard model |
| `list` | a tree without children | same |
| `table` | typed, sortable columns; row actions; selection; virtualized rows | the database results grid |
| `form` | fields with validation and a submit button | Settings' field components (`components/settings/fields.tsx`) |
| `markdown` | a document: headings, lists, code blocks with highlighting, tables, links | the Markdown preview, which renders no raw HTML |
| `chart` | line, bar or area chart of up to 10 series | a small SVG chart in Flint's colors (the Usage panel's bars, generalized) |
| `cards` | tiles with a title, a value, a trend or status, and buttons | new, in the style of the account cards |
| `stack` | several of the above, one under another, each with an optional heading | a column layout; one level of nesting |
| `message` | a sentence and buttons, for empty, error and sign-in states | the panels' empty state |

```jsonc
// tree: nodes are expanded lazily with view/resolve { parent: id }
{ "kind": "tree", "items": [{ "id": "ABC-1", "label": "Fix login", "description": "In progress",
  "icon": "checkbox", "hasChildren": false, "actions": [{ "id": "open", "label": "Open in Browser" }],
  "command": { "id": "acme.jira.open", "args": { "key": "ABC-1" } } }] }

// table: cell values are typed by their column; "badge" maps values to a severity
{ "kind": "table",
  "columns": [{ "id": "key", "title": "Key", "type": "text" },
              { "id": "points", "title": "Points", "type": "number", "align": "end" },
              { "id": "updated", "title": "Updated", "type": "date" },
              { "id": "status", "title": "Status", "type": "badge", "badges": { "Done": "ok", "Blocked": "error" } }],
  "rows": [{ "id": "ABC-1", "cells": { "key": "ABC-1", "points": 3, "updated": "2026-11-01T09:30:00Z", "status": "Done" },
             "command": { "id": "acme.jira.open", "args": { "key": "ABC-1" } } }],
  "sort": { "column": "updated", "descending": true },
  "actions": [{ "id": "assign", "label": "Assign to Me" }] }

// form: fields use the settings schema's types, plus multiline, select and a read-only markdown field
{ "kind": "form", "id": "comment",
  "fields": [{ "id": "comment", "type": "string", "title": "Comment", "multiline": true, "maxLength": 5000 },
             { "id": "notify", "type": "boolean", "title": "Notify watchers", "default": true }],
  "submit": "Add Comment" }

// markdown: CommonMark and tables; links open http(s) URLs in the browser, files in a root in the editor,
// and "command:<id>" runs one of the extension's own commands; images only from https and inline data URLs
{ "kind": "markdown", "text": "## Release 0.3\n\n- 12 issues closed\n- [Open the board](https://acme.atlassian.net/…)" }

// chart: x is a number, an ISO date or a category; colors come from Flint's chart palette, never from the extension
{ "kind": "chart", "type": "line", "title": "Open issues",
  "x": { "type": "date" }, "y": { "label": "Issues", "min": 0 },
  "series": [{ "id": "open", "label": "Open", "points": [["2026-10-01", 14], ["2026-10-08", 11]] },
             { "id": "blocked", "label": "Blocked", "points": [["2026-10-01", 2], ["2026-10-08", 3]] }] }

// cards: a value with an optional trend and severity, a short body and buttons
{ "kind": "cards", "items": [
  { "id": "sprint", "title": "Sprint 42", "value": "18 / 25", "subtitle": "points done",
    "trend": { "direction": "up", "text": "+5 this week" }, "severity": "ok", "icon": "checkbox",
    "actions": [{ "id": "open-board", "label": "Open Board" }] }] }

// stack: a dashboard; children are any kind but stack and message
{ "kind": "stack", "children": [
  { "title": "This sprint", "view": { "kind": "cards", "items": [ … ] } },
  { "title": "Trend", "view": { "kind": "chart", … } },
  { "title": "My issues", "view": { "kind": "table", … } }] }

// any kind may instead be a message
{ "kind": "message", "text": "Sign in to Jira first.", "actions": [{ "id": "signin", "label": "Sign In" }] }
```

Rules:

- **Text is text.** Labels, cells and card values are plain text. Only `markdown` views and read-only form fields marked `"markdown": true` are Markdown, rendered without raw HTML and with `javascript:` and other schemes dropped.
- **Colors are Flint's.** Severities (`info`, `ok`, `warning`, `error`) and chart series map to theme variables, so every view follows the theme, a custom one included. An extension cannot set a color in a view; its theme is the place for colors.
- **Icons are Flint's** (`components/ui/icons.ts`), by name.
- **Buttons and menu entries** send `view/action { view, key?, item, action }`, or run a command when they carry `command`. A form's submit sends `form/submit` with the values, and the field errors in the answer appear under the fields.
- **Bounds.** A response holds at most 5,000 rows or items in total (tables page with `view/resolve { parent: "<cursor>" }` beyond that), 10 chart series of 2,000 points, 50 cards, 10 children in a stack, 200 KiB of Markdown and 2,000 characters per label. Larger answers are cut, and the view says so.
- **Unknown kinds or fields** within the contract are ignored by an older Flint, which shows a message for a kind it doesn't know. That is why `initialize` lists the supported kinds and chart types: an extension can fall back to a table.

### Tab views

Panels are narrow; reports, dashboards and long forms want an editor tab. A tab view is declared in `contributes.tabViews` and behaves like a panel with a key:

- **Opening.** The extension calls `window/openTab { view, key, title, data? }` (from a command, a button, a tree item) and Flint opens the tab, or focuses it when one with the same view and key is open. A command can also open a tab without a round trip by listing it: `{ "id": "acme.jira.board", "title": "Jira Board", "opens": { "view": "acme.jira.issue", "key": "board" } }`.
- **Content.** Flint calls `view/resolve { view, key, data }` and draws the answer with the component catalog. `view/refresh { view, key }` refreshes one tab; `view/visible { view, key, visible }` tells the extension when the tab is shown, so it may poll only then.
- **Sessions.** Unlike today's contributed tabs, extension tabs are saved with the project's session: `{ view, key, title, data }`, with `data` at most 64 KiB of JSON. On restore the tab shows its title right away and resolves when it is first shown, which activates the extension (`onView`). A tab whose extension is gone shows "not available" with the extension's name.
- **Level 1.** This adds session restore to contributed tab kinds (`TabKind.restore: true`), the open item in "Points not yet in the contract".

### Editor decorations

Linters, test runners, coverage tools and issue trackers want to mark lines in the editor. An extension declares a decoration set and then publishes decorations for files:

```json
"decorations": [{ "id": "acme.lint.issues", "label": "Acme Lint", "languages": ["go"] }]
```

```jsonc
// editor/setDecorations: replaces everything the set shows in this file
{ "set": "acme.lint.issues", "path": "/Users/me/app/main.go", "version": 7, "decorations": [
  { "kind": "diagnostic", "line": 12, "from": 4, "to": 9, "severity": "warning", "message": "err is shadowed",
    "actions": [{ "id": "fix", "label": "Rename to err2" }] },
  { "kind": "gutter", "line": 30, "icon": "check", "severity": "ok", "hover": "Covered by 3 tests" },
  { "kind": "inline", "line": 41, "text": "ABC-123 · In progress", "command": { "id": "acme.jira.open", "args": { "key": "ABC-123" } } },
  { "kind": "highlight", "line": 50, "endLine": 58, "severity": "info" }] }
```

| Kind | Shows | Built on |
|---|---|---|
| `diagnostic` | an underline with a message, in the lint gutter and the Problems list, merged with the language server's | `@codemirror/lint`, as LSP diagnostics are today; the source is the extension's name |
| `gutter` | an icon in a gutter column of its own, with a hover | the git and blame gutters |
| `inline` | dim text after the end of a line (an inlay hint), clickable when it has a command | the ghost text widget |
| `highlight` | a background over lines or a range, in a severity's color | `Decoration.mark` / line decorations with theme classes |

How it works:

- **Which files.** Flint sends `editor/opened`, `editor/changed` and `editor/closed` only for files in trusted roots whose language one of the extension's sets lists, and only while the extension is enabled. A set's language is also an activation event (`onLanguage:go`). The notifications carry the path and a version, not the text: the extension reads saved files with `workspace/readFile`, so publishing decorations needs `workspace.read`. Unsaved text is passed only to extensions that declare `"unsavedText": true` on the set, which the install dialog shows.
- **Versions.** A `version` older than the editor's current one is dropped, so slow extensions never paint stale marks. Positions are clamped to the document, and Flint maps published decorations through later edits until the next update arrives.
- **Cost.** Updates for one file are applied in one transaction, at most every 50 ms; files not open in an editor are not decorated. Each set may hold 5,000 decorations per file; text is plain and at most 200 characters (`inline`) or 2,000 (`message`, `hover`).
- **Control.** Settings ▸ Extensions turns each set on or off, and a palette command hides all extension decorations in the current editor. `editor/clearDecorations` removes a set's decorations from one file or all. When the extension stops, its decorations stay until the file changes; when it is disabled or uninstalled they are removed.
- **Actions.** A diagnostic's actions appear as quick fixes (`⌥↩`) and send `view/action { view: set, item: "<path>:<line>", action }`. Edits are not part of the protocol: an extension that fixes code does so through the agent (`agent/ask`) or by writing the file itself, which the editor then reloads.
- **Level 1.** A new contribution point, `decorationSets`, with a registry in `lib/editorDecorations.ts` that a feature module can use too (coverage from `go test -cover` is a natural first one).

### Themes and file icons

A theme is data and runs no code, so it needs neither a program nor a permission.

```json
"themes": [{ "id": "me.nord.dark", "label": "Nord", "base": "dark",
  "colors": { "--bg-editor": "#2e3440", "--fg": "#d8dee9", "--accent": "#88c0d0" },
  "terminal": { "background": "#2e3440", "red": "#bf616a", "brightBlue": "#81a1c1" } }]
```

- `colors` are the CSS variables of a custom theme (`settings.ThemeVariables`) and `terminal` the xterm colors (`settings.TerminalColors`: background, foreground, cursor, cursor accent, selection and the 16 ANSI colors), all hex. The manifest is checked with `settings.ValidateCustomTheme`, the same function that checks `customTheme` on save and an imported VS Code theme, so a theme with an unknown name or a non-hex value is refused at install time and can't reach the page's style.
- Choosing a theme (Settings ▸ Appearance ▸ Color theme, or **Use …** in Settings ▸ Extensions) copies it into `customTheme` with `source` set to its id, and sets `theme` to `custom`. Saving checks it again in Go. The boot script needs nothing new: the first paint already uses the cached custom theme, and uninstalling the extension leaves the copy in place.
- `customTheme.terminal` is new and also filled by the VS Code import (`terminal.ansi*`, `terminal.background` and so on). Without it, terminals keep taking the background, text, accent and selection from `colors` and the ANSI colors from the base palette.
- **File icons** follow the same idea. Flint's file icons are a badge (a label of up to 4 characters, a background and a text color) on an outlined page (`components/icons/FileIcon.tsx`). An extension contributes badges by file name and extension, `{ "label": "JQL", "bg": "#0c66e4", "fg": "#ffffff" }`, checked as hex colors and short plain text. Flint's own mappings win for names it knows; extensions fill the gaps. No SVG and no images, so icons stay crisp, consistent and safe.

### Level 2: what exists today

Implemented in this step, with no extension process yet:

- `internal/extensions/manifest`: parsing and every rule above for `contract`, `id`, `name`, `version`, `run`, `activation`, `permissions` and `contributes` (commands, panels, settings and its schema subset, status items, file menu, tab views, agent tools, themes), with table tests and a fuzz test. `fileIcons`, `decorations` and `onLanguage:` activation are not accepted yet (unknown fields are ignored; `onLanguage:` is refused) and come with their features.
- `internal/extensions/installed`: discovery of `<config>/extensions/<id>/` (manifests only), install by copying a folder (regular files only, no symlinks, at most 5,000 files and 256 MiB, `.git` left out, replacing an older version atomically, refusing if the manifest changed after the user approved it) and removal.
- `internal/features/extensions`: the level-1 module with `ExtensionsAPI.List`, `InstallFromFolder` and `Uninstall`. Install and uninstall ask through `Host.Ask`, a native dialog that code in the web view can't answer; the install dialog lists the contributions, the program and the permissions. `feature.Host` gained `PickFolder` and `Ask` for this.
- `frontend/src/features/extensions`: Settings ▸ Extensions (install, list with errors, contributions, permissions, themes with **Use …**, uninstall), the palette command "Install Extension from Folder…" and the contribution of each extension's themes under `extension:<id>`. Level 1 gained the `themes` contribution point, which Settings ▸ Appearance reads.
- `settings.CustomTheme` gained `terminal` and `source`, checked and normalized like `colors`.

Extensions with a program can be installed and are listed with what they would add, and the install dialog says that this version doesn't start programs.

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

Each step is one pull request that leaves Flint shippable.

1. **Done.** `internal/extensions/manifest`, `internal/extensions/installed`, the `extensions` module, Settings ▸ Extensions, theme contributions and terminal colors in custom themes ([what exists today](#level-2-what-exists-today)).
2. **File icons.** `fileIcons` in the manifest, a `fileIcons` contribution point and its use in `FileIcon.tsx`. Data only, like themes.
3. `internal/extensions/rpc`: newline-delimited JSON-RPC over pipes, with bounded messages, cancellation and timeouts, fuzzed.
4. `internal/extensions/host`: lazy start, idle stop, crash policy, permissions and keyring access, tested with an extension written in Go under `testdata`; the `extensions` settings section (enabled flags and per-extension values); project extensions in trusted folders; `flint --install-extension <dir>`.
5. **Declarative views, part 1.** `DeclarativeView` with `tree`, `list`, `table`, `form` and `message`, panels, commands, status items and the file menu. This is the first step where extension programs run.
6. **Declarative views, part 2.** `markdown`, `chart`, `cards` and `stack`, and tab views with session restore (`TabKind.restore`).
7. **Editor decorations.** The `decorationSets` point and `lib/editorDecorations.ts`, the `editor/*` notifications, quick-fix actions, per-set toggles.
8. **Agent tools** on Flint's MCP server, with their toggle in Settings ▸ Agent Context.
9. Docs: a guide for extension authors and a protocol reference generated from the Go types; a conformance extension in `testdata` run in CI.

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
- **Session restore for contributed tabs.** Their `data` is JSON, so saving them is a small step, but a view must be able to rebuild itself from it. Needed by [tab views](#tab-views).
- **File icons and editor decorations.** New points for [file icons](#themes-and-file-icons) and [decoration sets](#editor-decorations); both are useful to feature modules too.

## Open questions

1. **Sandboxing extension processes.** On macOS, `sandbox-exec` is deprecated but works, and an App Sandbox helper is heavy. On Linux, Landlock or seccomp would need cgo or a helper. v1 relies on explicit installation and honest disclosure. Is that enough for project-local extensions, or should they be disabled entirely until sandboxing exists?
2. **Memory and CPU limits.** `setrlimit` on Linux is easy; macOS has no reliable per-process memory cap. Should Flint watch RSS and stop a runaway extension instead?
3. **Distribution.** "Install from folder" leaves signing and updates to the user. A signed index (minisign) could come later without changing the protocol.
4. **Agent tools that act.** v1 extension tools are read-only, like Flint's own MCP tools. Tools that change state would need the agent approval policy (`agentsession.Policy`) to apply to them.
5. **Shared views between extensions.** A tab kind from one extension used by another is out of scope; ids are namespaced per extension.
6. **The Wails `Settings` type is `any` in TypeScript.** If typed bindings for settings are wanted back, `SettingsAPI` could return a wrapper type, at the cost of a second representation. Today the frontend's own `AppSettings` is the typed view and nothing was lost.
7. **Contract checks in CI.** Level 1 is checked by tests (Go and frontend keys, documentation partials, duplicate ids). Level 2 will need a conformance test extension in `testdata` that runs in CI on both platforms.
