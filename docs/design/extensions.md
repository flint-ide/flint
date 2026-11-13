# Extensions: feature modules (level 1) and external extensions (level 2)

Status: level 1 implemented (Docker, TODO list and the release check migrated; Docker, Kubernetes and AWS Profiles have since moved on to level-2 extensions, see [Separate packages](#separate-packages-database-kubernetes-docker-aws)). Level 2 implemented in its first version (contract 1), color themes included: see [what is implemented and the decisions taken](#level-2-implemented-v1). Editor decorations and quick fixes are implemented ([decisions](#editor-decorations-implemented)). Of the component catalog, `table`, `form` and `chart` are implemented, for panels and tab views ([decisions](#table-form-and-chart-implemented)); `markdown`, `cards`, `stack`, the file menu and file icons are designed, not implemented ([plan](#implementation-plan-level-2)).

Flint should be easy to extend through an explicit contract. Two levels:

1. **Feature modules** (level 1): features inside Flint's repository. A feature is one Go package plus one frontend directory, listed once on each side. No edits to `settings.go`, `settings.ts`, `SettingsView`, `main.go`'s service list, the tool table or the panel list.
2. **External extensions** (level 2): separate processes in any language, installed explicitly by the user, talking JSON-RPC 2.0 over stdio and described by a manifest. Flint renders their UI from data (no extension JavaScript or HTML in the WebView). Level 2 is built on level 1: an extension's contributions land in the same registries as a feature module's.

Contents: [Goals](#goals) · [Level 1](#level-1-feature-modules) · [Level 2](#level-2-external-extensions) ([themes](#themes-and-file-icons) · [components](#component-catalog) ([implemented](#table-form-and-chart-implemented)) · [tab views](#tab-views) · [editor decorations](#editor-decorations) · [implemented](#level-2-implemented-v1)) · [Remaining migration](#what-remains-to-migrate) · [Open questions](#open-questions)

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

The migrated features are working examples: `todo` (service, a settings key holding an array, panel, Settings page) and `update` (service, a boolean key, a `state.json` key, a Start hook, a status bar item, a command, an activate hook).

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
	Trusted(path string) bool                  // Restricted Mode
	ConfigDir() string
	Confirm(title, message, ok string) bool    // native question the web view can't answer
	Choose(title, message string, buttons []string) string
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

`tools.Register` adds a feature's tools to the table behind the tool check and the bug report. `tools.Install(mac, linux)` picks the install command, `Tool.Find(bin, versionArgs...)` adds a binary, and `Order` places a tool within its group. No built-in feature registers a tool now (Docker and kubectl went with their extensions).

#### State

`Module.State` declares `state.json` keys. `appState` keeps them as raw JSON (`appState.features`, written after its own keys), so other parts of Flint that save `state.json` don't drop them. `Host.LoadState` / `SaveState` refuse undeclared keys. The release check keeps `"update"` there, with the shape it always had.

#### Agent tools

`Instance.AgentTools` are appended to the MCP server's tool list (`api_mcp.go`). The server's gates still apply: the server must be on and the root trusted, and a tool with `Acts` set runs only after the user approved that call. A feature decides from its own settings which of its tools to offer. The extension host uses it to offer the extensions' tools; Kubernetes' `get_kubernetes_status` became the Kubernetes extension's tool.

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
  decorationSets?: readonly DecorationSetContribution[] // { id, title, group, enabled?, action?, command? }
  activate?: () => void | (() => void)        // after the first render: event subscriptions
}
```

Example (the former `features/docker/index.ts`, from before Docker became an extension):

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
| Decoration sets | `contributions.decorationSets` + `lib/editorDecorations.ts` | a contributed set that is on may publish marks for open documents (`publishDecorations`), hears of them with `watchDocuments`, and answers quick fixes ([Editor decorations](#editor-decorations)) |
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
| Docker (since removed: now the `flint.docker` extension) | `api_docker.go` → `internal/features/docker` (`API`, `Overview`); `settings.Docker` → `docker.Section`; the tool entry → `Module.Tools`; terminal streams through `Host.StartStream` | panel, Settings page, store, tree logic and settings moved to `features/docker`; `TerminalView` imports the new binding |
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

- **Global extensions** live in `<config>/extensions/<id>/` (macOS `~/Library/Application Support/flint/extensions/`). The user installs one explicitly: **Settings ▸ Extensions ▸ Install from Folder…** copies a directory after showing its manifest and permissions, and the CLI equivalent is `flint --install-extension <dir>`. There is no marketplace and no automatic download; prebuilt extensions come from one signed index, fetched only when the user asks ([Distribution](#distribution-the-signed-index)).
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
| `decorationSets` | `contribute(...decorationSets)` (implemented) | gutter markers, inline hints, warnings and highlights in editors ([Editor decorations](#editor-decorations)) |
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

`tree`, `list`, `table`, `form`, `chart` and `message` are implemented; where the code differs from this section, [Table, form and chart: implemented](#table-form-and-chart-implemented) below says so and the [protocol reference](../../site/extensions/protocol.md#table-views) is authoritative.

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

#### Table, form and chart: implemented

Code: `internal/extensions/manifest/views.go`, `internal/extensions/host/richviews.go`, `frontend/src/lib/{viewTable,chartScale}.ts`, `frontend/src/features/extensions/{DeclarativeView,ViewTable,ViewForm,ViewChart}.tsx` and `formModel.ts`, the example `examples/extensions/stats` (a table panel and a chart tab view). The reference is [site/extensions/protocol.md](../../site/extensions/protocol.md#table-views). What was built follows the section above, with these decisions where it was open or is changed:

- **Where.** A panel's `view` may be `tree`, `list`, `table`, `form`, `chart` or `webview`; a tab view's `table`, `form`, `chart` or `webview`. `tree` and `list` stay panel-only (their component is the sidebar's row list). The views take no options in the manifest: everything else is in the view model. Any other kind (`markdown`, `cards`, `stack`) still refuses the manifest, as `table` and `form` did before, so an extension never installs with a view that would show nothing. `"webview": {…}` on a declarative view is an error.
- **One Go type for the page.** `host.View` gained `table`, `form` and `chart` next to `items`; the extension's flat answer (`{ "kind": "table", "columns": … }`) is decoded per kind and rebuilt, never passed through. A kind other than the declared one, an answer Flint can't draw (no columns, an unknown chart type, a bad field) becomes a `message` view that says why. `initialize` lists `views` (now with `table`, `form`, `chart`) and `charts` (`line`, `bar`, `area`).
- **Cells are typed in Go.** Rows carry `cells` as an object by column id, as designed; the page gets them as an array in column order, each a string, a number or null. A value that isn't of its column's type is an empty cell, not text. Dates are ISO 8601 dates or date-times (a time without a zone is UTC); the page shows local time to the minute.
- **Sorting is Flint's, client-side.** A header click cycles ascending, descending, the extension's order; `sort` in the view model is only the first order. There is no multi-column sort and no filter. **No paging:** the design's `view/resolve { parent: "<cursor>" }` beyond 5,000 rows is not implemented; more rows are cut and the table says so. Up to 30 columns.
- **Row actions.** The table's `actions` are every row's context menu, as designed; a row may carry its own `actions`, which replace them for that row (a stopped and a running container want different entries). A row's `command` runs on Enter or a double click, not on a single click, which selects. Selection is one row; it is not reported to the extension.
- **Forms reuse the settings schema.** A field is a settings property (`manifest.ParseProperty`, the same Go checks; `schema.ts`'s `checkValue` in the page) with `id`, `value`, `required`, `multiline`, `secret` and `placeholder`. `enum` is the menu: there is no separate `select` type, and the read-only Markdown field waits for the `markdown` view. `maxLength` above the settings limit of 4,096 is lowered to it (the design's example asks for 5,000). Unknown keywords are ignored, but a field Flint can't draw turns the form into a message that names it, rather than a form with a field missing. Up to 50 fields. The form's controls are its own small component (stacked labels fit a sidebar; Settings' rows are two columns and save on blur), with Settings' input styles, `Select` and validation.
- **Submit is checked twice.** The page checks the values and shows the reasons; Go checks them again against the fields it last sent for that panel or tab (kept per extension, 64 forms) and only then sends `form/submit { view, key?, values }`. Values for keys that aren't fields are dropped, missing ones get the default. The answer is `{ errors?, error?, message? }`: the design had only `errors`; `error` is for the form as a whole and `message` a note after success. After an accepted submit the view is resolved again. `form` (a form inside a stack) is not sent, since there are no stacks.
- **Secret fields.** The design says secrets are never settings and live in the keyring through `secrets/*`; it doesn't say how one is typed. A field with `"secret": true` is a password input. Go drops its `value` and `default`, so no secret travels from the extension to the page, and what the user types goes to the extension with the submit and is cleared from the page afterwards. Flint doesn't store it: the extension calls `secrets/set`. Form values are not written to the extension's log.
- **Buttons on a form** other than submit (`view/action` with `values`) are not implemented.
- **Charts.** `line`, `bar` and `area`; x is `number`, `date` or `category`, sent to the page as a number (a date as milliseconds) or text. Go drops points that don't fit the axis and cuts to 10 series of 2,000 points, flagged as truncated. The page draws plain SVG (`lib/chartScale.ts`: round ticks, bands, paths), with series colors from ten theme variables in a fixed order, a legend, and the values again as an HTML table under **Values** for keyboards and screen readers. Bars over numbers or dates are grouped per x value like categories; at most 500 bands are drawn. No stacking, no second axis, no zoom.
- **Tab views.** A declarative tab view is a contributed tab kind like a webview's: opened with `window/openTab`, resolved with `{ view, key, data }` when first shown (a restored tab too), `view/visible` and `view/action` with `key`. `view/refresh { view, key }` reloads one tab and without `key` every tab of the view; it is now accepted for tab views as well as panels. The command shortcut of the design (`"opens": { view, key }` on a command) is not implemented: a command calls `window/openTab`.
- **Timeouts and limits** are those of `view/resolve` (5 s) and other requests (30 s for `form/submit`); labels are 2,000 characters, titles 100, form messages 500.
- **Checks**: manifest table tests (`views_test.go`); `host/richviews_test.go` covers the cleaning of each kind, the bounds, and resolve, action, visibility, refresh and submit against `testdata/testext`; the conformance test drives the `stats` example's table and chart; `flint-extcheck` resolves every declarative panel and tab view (key `extcheck`), fails on an answer Flint can't draw or has to cut, and submits every form with the values it shows; `lib/viewTable.test.ts`, `lib/chartScale.test.ts` and `formModel.test.ts` cover sorting, formatting, ticks, geometry and validation.

### Tab views

Declarative tab views (`table`, `form`, `chart`) and webview tab views are implemented, with session restore; `"opens"` on a command is not ([decisions](#table-form-and-chart-implemented)).

Panels are narrow; reports, dashboards and long forms want an editor tab. A tab view is declared in `contributes.tabViews` and behaves like a panel with a key:

- **Opening.** The extension calls `window/openTab { view, key, title, data? }` (from a command, a button, a tree item) and Flint opens the tab, or focuses it when one with the same view and key is open. A command can also open a tab without a round trip by listing it: `{ "id": "acme.jira.board", "title": "Jira Board", "opens": { "view": "acme.jira.issue", "key": "board" } }`.
- **Content.** Flint calls `view/resolve { view, key, data }` and draws the answer with the component catalog. `view/refresh { view, key }` refreshes one tab; `view/visible { view, key, visible }` tells the extension when the tab is shown, so it may poll only then.
- **Sessions.** Unlike today's contributed tabs, extension tabs are saved with the project's session: `{ view, key, title, data }`, with `data` at most 64 KiB of JSON. On restore the tab shows its title right away and resolves when it is first shown, which activates the extension (`onView`). A tab whose extension is gone shows "not available" with the extension's name.
- **Level 1.** This adds session restore to contributed tab kinds (`TabKind.restore: true`), the open item in "Points not yet in the contract".

### Webviews

Some extensions are applications in their own right: a database client with an SQL editor, an editable result grid and query plans, or a Kubernetes dashboard. Describing those as data would move their components into Flint's core. A **webview** is the exception to "everything is data": the extension ships its own UI as **one self-contained HTML file**, and Flint shows it in an isolated frame. Database, Kubernetes and Docker are packaged this way (see [Separate packages](#separate-packages-database-kubernetes-docker)).

```json
"panels":   [{ "id": "flint.database.tree", "title": "Databases", "icon": "database", "order": 50,
               "view": "webview", "webview": { "html": "ui/panel.html" } }],
"tabViews": [{ "id": "flint.database.console", "title": "SQL Console", "icon": "database",
               "view": "webview", "webview": { "html": "ui/console.html" }, "restore": true }]
```

**Isolation.** The file is read by Go (a clean relative path inside the extension folder, at most 32 MiB, valid UTF-8) and loaded as the `srcdoc` of an `<iframe sandbox="allow-scripts allow-forms allow-popups-to-escape-sandbox">` without `allow-same-origin`. Its origin is opaque, so it cannot reach Flint's page, storage, bindings or cookies. Flint inserts a Content Security Policy as the first element of `<head>`:

```
default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data: blob:;
font-src data:; media-src data: blob:; connect-src 'none'; frame-src 'none'; form-action 'none'; base-uri 'none'
```

The page cannot load anything, from the extension folder or the network: scripts, styles, fonts and images are inlined (Vite with a single-file build does this). `connect-src 'none'` means it has no network at all, so everything goes through its own process, which declared its hosts (`network:` permission) like any extension. A second policy in the file can only narrow this one. The frame gets `allow="clipboard-write"` and nothing else.

**Bridge.** Flint inserts a small script before the extension's own code. It defines `window.flint`:

| API | Does |
|---|---|
| `flint.request(method, params)` → Promise | `webview/request { view, key?, method, params }` to the extension's process (activating it), the result or a rejected error; 30 s timeout, 4 MiB each way |
| `flint.onMessage(fn)` | receives what the process sends with `webview/post { view, key?, message }` |
| `flint.view`, `flint.key`, `flint.data` | which panel or tab this is (a tab's `data` is its saved JSON) |
| `flint.theme` + `flint.onTheme(fn)` | Flint's theme variables (`--bg-editor`, `--fg`, `--accent`, `--font-mono`, …), also set on `:root`, and `dark` / `light` on `<html data-theme>`; updated live |
| `flint.openTab(view, key, title, data?)` | opens or focuses one of the extension's own tab views |
| `flint.setTitle(title)`, `flint.setDirty(dirty)` | the tab's title and unsaved dot |
| `flint.setData(data)` | the JSON saved with the tab for session restore (64 KiB) |
| `flint.runCommand(id, args?)` | runs one of the extension's own commands |
| `flint.copy(text)` | writes the clipboard (1 MiB) |
| `flint.showMessage(type, text)` | a toast, rate-limited like `window/showMessage` |
| `flint.openFile(path, line?)` | opens a file in a root; needs `workspace.read` |

Messages from the frame are accepted only from that frame's window, are size-checked, and can only address the extension that owns the view; ids of other extensions or of Flint are refused. Shortcuts: the bridge forwards key presses with ⌘/Ctrl/Alt that the page didn't `preventDefault()` to Flint, so the command palette, tab switching and the user's keymap keep working while a webview has focus. Escape is forwarded too.

**Process side.** `webview/request` is a request from Flint to the process (`{ view, key?, method, params }` → any JSON) and `webview/post` a notification from the process (`{ view, key?, message }`, at most 4 MiB). A webview without a process (no `run`) can only use the bridge's local calls. `window/openTab` from the process opens a tab view as well.

**Lifecycle.** The frame is created when the panel or tab is first shown and kept while it is open (hidden, not destroyed, when another tab or panel is shown); `view/visible` is sent as for other views. Uninstalling or turning the extension off replaces it with "not available". `restore: true` tab views are saved with the session (`{ view, key, title, data }`) and recreated on start.

**What this changes in the security model.** A webview runs the extension's JavaScript inside Flint's window, but in a frame with an opaque origin, no network and no access to Flint except the bridge above. The bridge offers nothing the extension's process couldn't already do through the protocol. The install dialog lists the webviews ("shows its own UI in Flint").

### Editor decorations

Implemented; where the code differs from this section (the manifest key and fields, quick fixes that return edits, what happens when the extension stops), [Editor decorations: implemented](#editor-decorations-implemented) below says so and the [protocol reference](../../site/extensions/protocol.md#editor-decorations) is authoritative.

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

#### Editor decorations: implemented

Code: `internal/extensions/manifest/decorations.go`, `internal/extensions/host/decorations.go`, `frontend/src/lib/{decorationModel,editorDecorations}.ts`, `frontend/src/features/extensions/documents.ts`, the example `examples/extensions/todos`. The reference is [site/extensions/protocol.md](../../site/extensions/protocol.md#editor-decorations). What was built follows the section above, with these decisions where it was open or is changed:

- **The manifest key is `decorationSets`**, the name of the level-1 point, not `decorations`; a set has `title` (not `label`), and selects files by `languages`, `extensions` and `globs` (patterns for the file's name), at least one of them. `"default": false` leaves a set off until the user turns it on. At most 20 sets. An older Flint lists the key as an unsupported contribution point, like any it doesn't know.
- **Declaring a set needs `workspace.read`** (a manifest error otherwise): the `editor/*` notifications tell the extension which files are open, and it reads them. No new permission.
- **An extension hears only of files `workspace/readFile` would return**: inside a trusted folder with symbolic links resolved, not in `.git`, not a credential file. The check is made in Go for every document and repeated when folders, settings or extensions change.
- **Languages** are Flint's server languages (`go`, `typescript` for JavaScript too, `python`, `rust`) and the languages extensions contribute; files of other types are selected by extension or glob. Go decides the language (`lsp.LanguageOf`), so the frontend can't claim one.
- **Positions**: lines are 1-based, columns UTF-16 code units, as in LSP and CodeMirror.
- **`editor/opened` carries `sets`**, the extension's sets that are on and match, and is sent again when that list changes, so turning a set on needs no other signal. There is no `editor/visible`: the three notifications of the design are enough.
- **Unsaved text** is sent only for a set with `unsavedText`, up to 1 MiB; a larger document is announced without text.
- **Versions**: a publish must be for exactly the editor's version (Go drops older ones against the version last reported, the editor checks the exact one); a publish without a version is drawn as is. Positions are clamped; marks are mapped through edits until the next publish.
- **Diagnostics are not merged into the lint gutter and the Problems list.** They are drawn by `lib/editorDecorations.ts` (underline, gutter marker, hover), so the language server client stays untouched. Merging is a later step.
- **Gutter icons** are six glyphs (`dot`, `check`, `cross`, `alert`, `info`, `arrow`), not Flint's icon set, whose icons are panel-sized line drawings.
- **Quick fixes return edits**, unlike the design ("edits are not part of the protocol"): choosing an action sends the new request `editor/action { set, path, version, action, decoration?, line, endLine, from, to }` (not `view/action`), and the answer's `edits` are applied as one transaction only if the document is still at that version. Edits outside the document or overlapping are refused as a whole, never clamped. An action with `command` runs one of the extension's commands instead. Actions are allowed on every kind, not only diagnostics.
- **Where quick fixes show**: buttons in the hover, a list on a click on the gutter marker, and the command *Show Extension Quick Fixes* (`Alt+Shift+Enter`). `⌥↩` / `⌘.` belong to the language server's code actions; `extensionQuickFixes(view, pos)` in `lib/editorDecorations.ts` returns `{ title, run }[]` for a shared menu later.
- **Lifecycle differs from the design in one point:** decorations are removed when the extension's process ends for any reason (stopped, crashed, turned off, uninstalled), instead of staying until the file changes, so the editor never shows marks nobody maintains. To make that rare, a document an extension decorates keeps it from the idle stop, like a panel on screen, and a crashed extension with such a document is started again after its backoff and gets `editor/opened` for each. Opening a file starts an extension only when one of its sets is on and matches the file.
- **Toggles** are `extensions.decorations` in `settings.json` (`{ "<set id>": bool }`, only where it differs from the manifest's default), a checkbox per set in Settings ▸ Extensions and a *Toggle … in Editors* command per set. *Toggle Extension Decorations in This Editor* hides them in one editor (not saved).
- **Cost**: `editor/changed` at most every 300 ms per file; publishes for one editor applied together at most every 50 ms; 5,000 decorations per set and file; 500 tracked documents. A file open in two editors is decorated in the first.
- **Checks**: `flint-extcheck` opens a file for every decoration set, sends `editor/action` and fails on a publish Flint refuses; `host/decorations_test.go` covers trust, credential files, versions, bounds, toggles, stops and the quick-fix round trip against `testdata/testext`; `decorations_conformance_test.go` drives the `todos` example.

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
- **No WebView code, except in webviews.** Contributions are data rendered by Flint's components. The one exception, [webviews](#webviews), runs the extension's script in a sandboxed frame with an opaque origin and no network, which can't read Flint's or other extensions' state.
- **Agent tools.** Extension tools are read-only from Flint's point of view unless the manifest says `"acts": true` (then the user approves each call): Flint passes the arguments and returns the text. The tool names carry the extension's short name (`jira_jira_issue` → `mcp__flint__jira_jira_issue`). MCP limits tool names to 64 characters of `[A-Za-z0-9_-]`, so names are shortened and deduplicated. Each extension's tools can be turned off in Settings ▸ Agent Context.

### Distribution: the signed index

Open question 3, decided. "Install from Folder…" left building, integrity and updates to the user; the repository view (a git clone, built with Go on the user's machine) needs `git` and Go. The index adds prebuilt, verified installs and updates without becoming a marketplace: **one index, curated in one repository, trusted through one key.** The protocol and the manifest are unchanged. Format reference: [site/extensions/index-format.md](../../site/extensions/index-format.md). Code: `internal/extensions/index` (format, signature, download, archives, building), `internal/features/extensions/index.go` (the store and the bound methods), `cmd/flint-extindex`.

- **Index.** A JSON document: `format` (1), `generatedAt`, and per extension the manifest's id, name, description, publisher, version, contract, permissions and agent tool names (for display before download), a `kind` (`program`, `themes`, `languages`, `data`) and archives by platform (`darwin-arm64`, `darwin-amd64`, `linux-amd64`, `linux-arm64`, or `any` for an extension without a program), each with URL, SHA-256 and size. What the index says about an extension is only a preview: the manifest inside the archive is what is checked and confirmed.
- **Signature.** A detached Ed25519 signature over the exact bytes of the index, in a file next to it (`index.json.sig`: the signature in base64, one line). Keys are base64 too (public: 32 bytes; secret: the 64-byte private key). *Not minisign:* its file format needs BLAKE2b, which is in neither Go's standard library nor `go.mod`, and principle 1 (few dependencies) outweighs compatibility with a tool nobody here uses; `crypto/ed25519` is all this needs. The signed bytes are the file as served, so there is no canonical-JSON question.
- **Key.** The official public key is a constant (`index.OfficialPublicKey`), **empty until the maintainer generates the pair** (`flint-extindex keygen`), like `github.clientId`: with an empty key the official index is "not configured", Settings says how a maintainer sets it up, and no request is made. No key was generated for this; a key someone else made must never be shipped as the official one. The secret key lives only in the extensions repository's signing secret.
- **Settings.** `extensions.indexUrl` (empty: the release asset `https://github.com/flint-ide/extensions/releases/latest/download/index.json`) and `extensions.indexPublicKey` (empty: the built-in key), so a company can run its own index with its own key. They are user settings: `settings.json` is per user and Flint has no project-level settings, so a repository can't point Flint at another index. Both are validated on save (https, or http for loopback; a 32-byte base64 key).
- **Fetching.** Only on a user action: **Browse Extensions** / **Check for Updates** in Settings ▸ Extensions and the palette commands of the same names. No timer, nothing at startup: the release check stays the only request Flint makes on its own. Two anonymous GETs (index, then `<url>.sig`), `User-Agent: Flint/<version>`, no cookie jar, https only (http for loopback, for tests), 20 s and 4 MiB at most. Redirects are followed (GitHub serves release assets from another host) to addresses that pass the same check, never from https to http. The signature is verified before the document is parsed, and nothing is shown otherwise. The verified pair is cached in `<config>/extension-index/` with the address it came from and verified again whenever it is read, so "Update to x.y.z" survives a restart without a request; a changed address or key drops it.
- **Install and update.** For the entry the user chose: download this platform's archive (exactly the index's size, at most 256 MiB; SHA-256 must match), unpack it into a temporary folder (tar.gz with the standard library; folders and regular files only, written through an `os.Root`; absolute paths, `..`, links, devices, duplicate names, more than 10,000 entries, depth over 32 or more than 512 MiB fail the whole archive), then `host.InstallExpected`: the manifest is loaded with the usual rules, its id and version must equal the entry's, and the **same native confirmation and copy** as Install from Folder follow. The confirmation says where the download came from; when an extension with that id is installed (from the index or from a folder) it starts with what changes: the version, every added permission and agent tool, a tool that now acts, a program where there was none. That list is shown for every replacing install, also from a folder.
- **Updates are never automatic.** After a fetch, an installed extension with a higher version in the index shows **Update to x.y.z…** (on its card and in the index list). Extensions installed from a folder are untouched unless the user clicks it. An entry for another contract or without an archive for this platform is listed with the reason and can't be installed.
- **CLI.** `flint --install-extension <dir>` (also `--install-extension=<dir>`, and through `scripts/flint`) is the command-line form of Install from Folder. The flag is parsed in Go (`extensions.InstallArg`: the value must be an existing folder; it is not taken for a project to open). A Flint started with it hands the folder to the frontend once the window is up (`PendingInstall`); a second launch hands it to the running instance (`extensions:install-request`). Either way the frontend calls the same `Install` binding as the folder picker, so the native confirmation can't be skipped. There is no headless install.
- **Tooling.** `cmd/flint-extindex`: `keygen`, `build` (packs built folders or takes archives, checks manifests with the manifest package and each platform's program, writes reproducible archives and `index.json`), `sign` (secret key from an environment variable or a file, never an argument; refuses without a key and refuses an index Flint would refuse), `verify` (signature, content and, with `-archives`, the files). The release workflow of flint-ide/extensions runs it from a Flint checkout, as its `contract` job runs `flint-extcheck`.
- **What this does not do.** It does not make an extension safe: a signature says the index came from the key's holder, not that a program is harmless, and the install confirmation stays the trust decision. There is no revocation list beyond publishing a new index (Flint only ever uses the last one fetched), no key rotation scheme (a new key ships with a new Flint, or goes into settings), no rollback protection against a server replaying an older signed index, and no ratings, search or accounts.

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

1. **Done (v1).** Manifest, RPC, host, the `extensions` module and settings section, commands, `tree`/`list` panels, status items, agent tools, docs and the conformance test ([implemented](#level-2-implemented-v1)).
2. **Done.** Color themes, with no program needed, and terminal colors in custom themes ([Themes](#themes-and-file-icons)).
3. **File icons.** `fileIcons` in the manifest, a `fileIcons` contribution point and its use in `FileIcon.tsx`. Data only, like themes.
4. **Declarative views, part 1.** **Done:** `table` and `form` in `DeclarativeView`, for panels and tab views ([implemented](#table-form-and-chart-implemented)). Open: the file menu.
5. **Declarative views, part 2.** **Done:** `chart`, and declarative tab views with session restore (`TabKind.restore`, which webviews brought). Open: `markdown`, `cards` and `stack` (manifests with them are refused).
6. **Done.** Editor decorations: the `decorationSets` point and `lib/editorDecorations.ts`, the `editor/*` notifications, quick-fix actions, per-set toggles ([implemented](#editor-decorations-implemented)).
7. **Done:** `flint --install-extension <dir>` and the signed index ([Distribution](#distribution-the-signed-index)). Later: project extensions (after sandboxing), a protocol reference generated from the Go types.

### Level 2: implemented (v1)

Code: `internal/extensions/{manifest,rpc,host}`, the module `internal/features/extensions`, `frontend/src/features/extensions`, the example `examples/extensions/stats` (Go, standard library). Docs: [site/guides/extensions.md](../../site/guides/extensions.md) (users), [site/extensions/write-an-extension.md](../../site/extensions/write-an-extension.md) and [site/extensions/protocol.md](../../site/extensions/protocol.md) (authors; the reference is written by hand, not generated). Tests: manifest table tests, the RPC transport (with a fuzz target), the host against `host/testdata/testext` (lazy start, permissions, crashes, idle stop, timeouts, process-group kill, environment, tools, settings) and a conformance test that builds the example and drives every contribution point it uses; frontend tests for the translation into contributions, the settings section and the schema checks.

**Implemented as designed:** the manifest and its rules (ids namespaced by `<id>.`, `run.command` inside the folder, no shell or `PATH`), the lifecycle (`initialize` with contract check, `initialized`, `shutdown`, then stdin closed and the process group killed after 2 s), lazy activation, idle stop after 5 minutes, the timeouts, the 4 MiB message cap and the 8 MiB/s × 5 s flood limit, 3 crashes in 10 minutes stop the extension, `$/cancelRequest`, the permissions and `-32001`, the keyring layout, the `extensions` settings section (objects of extensions that aren't installed are kept), agent tools named `<short>_<name>`, contributions under `extension:<id>` withdrawn when an extension is turned off or uninstalled.

**Decisions where this design was open or is narrowed in v1:**

- **Project-local extensions are not supported.** Only user-installed extensions in `<config>/extensions/<id>/`. Project ones wait for sandboxing (open question 1); a repository can't make Flint run anything.
- **Restricted Mode is stricter than designed:** untrusted roots are not passed at all (instead of `trusted: false`), so an extension learns only of folders the user trusts. `roots` keeps the `trusted` field for compatibility; it is always `true`.
- **Installation** is the palette command and Settings ▸ Extensions ▸ Install from Folder…; the folder picker is the frontend's, the confirmation is Go's native dialog (in the dev-only server build it says yes, like every native confirmation there). `flint --install-extension <dir>` came later ([Distribution](#distribution-the-signed-index)). A copy skips `.git`, follows symbolic links only inside the folder, and is limited to 10,000 files and 512 MiB; the old version is stopped before it is replaced.
- **Discovery** happens on the frontend's first `List` after the first render (and on install, uninstall, **Reload**), not on a goroutine at launch; folders aren't watched. `onStartup` extensions start after that first `List`.
- **Contribution points in v1:** commands, panels (`tree`, `list`, plus `message`), the settings page, status items, toasts, `window/showMessageRequest` (a native question with up to three buttons), files and links, chat drafts and agent tools. `table` and `form` views, `tabViews` / `window/openTab` and `fileMenu` were not part of v1 (tab views came with webviews; `table`, `form` and `chart` [later](#table-form-and-chart-implemented)): a manifest with a view kind Flint doesn't draw is refused, and unknown contribution points are ignored with a warning in Settings.
- **`window/openUrl`** (not in the original tables) opens `http(s)` links only, after a native confirmation once per extension and origin per session. The extension could open a browser itself, so this is about not surprising the user, not containment.
- **Settings are checked when they are written and when they are used,** not when `settings.json` is saved: Go's save check is structural (ids, sizes). The generated page refuses invalid values, and the extension always receives schema-valid values (invalid ones become defaults). This way a newer manifest with a stricter schema can never make `settings.json` unsavable. `enabled` and `agentTools` are maps where a missing id means on.
- **Crash policy details:** the next start waits 1 s after the first crash and 4 s after the second; an extension with a panel on screen or `onStartup` is restarted on its own after that delay, others on their next use. "Failed" lasts for the session; **Restart** (or turning it off and on) clears it. Contributions stay while an extension is crashed or failed; their use reports why.
- **Idle stop** doesn't apply to `onStartup` extensions: the user saw "starts with Flint" when installing, so they run for the session.
- **Agent tools were read-only only in v1**; acting tools came later with a per-call approval (open question 4). They are listed from manifests (no process starts for `tools/list`), offered only for trusted roots while the MCP server is on, and can be turned off per extension in Settings ▸ Extensions (`extensions.agentTools`), not in Settings ▸ Agent Context.
- **Environment:** `PATH` (with the login shell's additions), `HOME`, `USER`, `LOGNAME`, `SHELL`, `TMPDIR`, `TZ`, `LANG`, `LC_ALL`, `LC_CTYPE`, proxy variables, and `FLINT_EXTENSION_ID`, `FLINT_EXTENSION_DIR`, `FLINT_VERSION`. The working directory is the extension's folder.
- **Logs** are in memory (the last 256 KiB per extension, stderr lines cut at 2,000 characters), shown in Settings ▸ Extensions; they are not written to Flint's log file. RSS is not shown yet.
- **Uninstall** deletes the folder and keeps the settings and keychain secrets (the keyring API can't list an extension's keys).
- **Level-1 contract additions** this needed: `Host.Trusted`, `ConfigDir`, `Confirm`, `Choose`, and `Instance.SettingsChanged`, `RootsChanged`.
- **Color themes** (`contributes.themes`, added after v1) are data: an extension with only themes (and settings) needs no `run` and never starts a program; Settings ▸ Extensions says "runs no code" and the install dialog shows no program. Themes are checked with `settings.ValidateCustomTheme` when the manifest is read, offered through the level-1 `themes` contribution point (Settings ▸ Appearance ▸ Color theme, and **Use …** on the extension's card), and copied into `customTheme` (with `source` and the new `terminal` colors) when chosen, so saving checks them again.
- **Installing, hardened after review:** the copy reads through an `os.Root`, checks each opened file against what the walk listed, opens non-blocking (a FIFO can't hang it), counts folders, limits depth to 32 and enforces the byte limit while copying; a manifest that changed between the confirmation and the copy is refused; `run.command` and arguments may not hold control characters (the dialog shows them), and the dialog shows the command for this platform.
- **Panels appear after the first paint.** Extensions are discovered after the window is up, so their activity bar buttons arrive a moment later; a stored sidebar choice of an extension panel falls back to Files on the next start.

---

## Separate packages: Database, Kubernetes, Docker, AWS

These four leave Flint's core and become level-2 extensions, installed separately (**Settings ▸ Extensions ▸ Install from Folder…**, or from the packages that ship with Flint and are listed under **Available**). Each package is a folder with `flint-extension.json`, a Go program built from the packages that are Flint's today (`internal/db`, `internal/kube`, `internal/docker`, `internal/awsprof`), and single-file webview UIs built from today's React code. The sources live in the [flint-ide/extensions](https://github.com/flint-ide/extensions) repository, one folder per extension (`database/`, `kubernetes/`, `docker/`, `aws/`), next to `stats` and the webview example `notes`. Each is its own Go module; the Go packages they need from Flint (`internal/db`, `internal/kube`, `internal/docker`, `internal/awsprof`) move there with them. Flint keeps copies of `stats` and `notes` under `examples/extensions` for its conformance tests.

| Package | Process | Webviews | Other contributions | Needs from the contract |
|---|---|---|---|---|
| `flint.database` | connections, keychain passwords (same keyring service, so saved passwords keep working), queries, schema, EXPLAIN, row edits, history; data in `FLINT_EXTENSION_DATA` | panel: connections and schema tree with its menus; tab: SQL console (editor with completions from the schema, results grid with local sort and filters, editing, EXPLAIN) | commands (New SQL Console, New Connection, Run, Explain, History) | webviews, tab views with restore, per-extension data folder, a one-time import of `connections.json` and `history/` |
| `flint.kubernetes` (**done**) | kubectl calls, kubeconfig overrides for terminals | panel: contexts, namespaces, apps, workloads | agent tool `kubernetes_status`, command to open a terminal in a context | webviews, **terminal tabs** (`window/openTerminal { title, cwd?, env, command? }`), `run.env` (`KUBECONFIG`, AWS variables) |
| `flint.docker` (**done**) | Engine API client | panel: containers, Compose projects | Compose actions; logs and shells are terminal tabs running `docker logs -f` / `docker exec -it` | webviews, terminal tabs, `run.env` (`DOCKER_HOST` and the TLS variables) |
| `flint.aws` (**done**) | AWS CLI profiles, credential status, login and refresh | panel: profiles and their expiry | Log In and Open Terminal as a profile as terminal tabs | webviews, terminal tabs, `run.env` (`AWS_CONFIG_FILE`, `AWS_SHARED_CREDENTIALS_FILE` and the like) |

Order, one pull request each, each leaving Flint shippable:

1. **Done.** Database and Kubernetes moved into level-1 modules (`internal/features/{database,kubernetes}`, `frontend/src/features/{database,kubernetes}`), so their code has one home; optional modules can be switched off (`features.enabled`).
2. **Webviews and tab views** in the contract (above), with `examples/extensions/webview` and a conformance test.
3. **`flint.database`**: the package, the data import, then removing `internal/features/database` and its frontend from core. SQL consoles become the extension's tab views; existing SQL scratchpads are offered for import.
4. **Done.** **Terminal tabs** and `run.env` in the contract; **`flint.kubernetes`**, **`flint.docker`** and **`flint.aws`** moved to flint-ide/extensions and their built-ins (`internal/features/{docker,kubernetes}`, `internal/{docker,kube,awsprof}`, `api_aws.go`, `mcptools/kube.go`, the `refresh` key and `mcpServer.tools.kubernetes`) left core. `get_kubernetes_status` became the extension's tool.
5. **Not needed.** Stream tabs were planned for Docker's logs and shells; the Docker extension runs the docker CLI in terminal tabs instead, so the contract has no `window/openStream`. `feature.Host.StartStream` and `internal/terminal`'s stream sessions remain without a built-in user.
6. **Available packages**: Settings ▸ Extensions lists the packages bundled with Flint (in the app's resources) with an **Install** button that copies them like Install from Folder.

## What remains to migrate

Each line moves to `internal/features/<id>` plus `frontend/src/features/<id>`, typically in one pull request per feature. Order by value: features that other pull requests touch often go first.

| Feature | Go today | Frontend today | Contribution points it will use |
|---|---|---|---|
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
- **Ordering among status items.** Flint's own status items are not in the registry; contributed items come after them.
- **Session restore for contributed tabs.** Their `data` is JSON, so saving them is a small step, but a view must be able to rebuild itself from it. Needed by [tab views](#tab-views).
- **File icons.** A new point for [file icons](#themes-and-file-icons), useful to feature modules too. ([Decoration sets](#editor-decorations) exist; no feature module uses them yet.)

## Open questions

1. **Sandboxing extension processes.** On macOS, `sandbox-exec` is deprecated but works, and an App Sandbox helper is heavy. On Linux, Landlock or seccomp would need cgo or a helper. v1 relies on explicit installation and honest disclosure. *Decided for v1:* project-local extensions are disabled entirely until sandboxing exists.
2. **Memory and CPU limits.** `setrlimit` on Linux is easy; macOS has no reliable per-process memory cap. Should Flint watch RSS and stop a runaway extension instead?
3. **Distribution. Decided:** a signed index of prebuilt archives, fetched only when the user asks, with Ed25519 signatures in a small format of Flint's own instead of minisign (see [Distribution](#distribution-the-signed-index)). Still open: key rotation and protection against replaying an older signed index.
4. **Agent tools that act. Done.** A tool on Flint's MCP server can be marked as acting (`mcpserver.Tool.Acts`; in a manifest `"acts": true` on a `contributes.agentTools` entry). The server runs such a call only after the user said yes to it: the answer to the agent's own permission request in the chat when there is one (kept out of `agentsession.Policy`'s auto-approvals and of "always allow"), otherwise a native dialog. Flint's own acting tools (`run_tests`, `run_configuration`, `send_http_request`, `run_sql_query`) are off by default (`mcpServer.actions`); an extension's acting tools follow its "Offer to agents" switch and are marked in the install confirmation and Settings ▸ Extensions. An extension tool has no plan step: the user sees its name and arguments, not what it will do.
5. **Shared views between extensions.** A tab kind from one extension used by another is out of scope; ids are namespaced per extension.
6. **The Wails `Settings` type is `any` in TypeScript.** If typed bindings for settings are wanted back, `SettingsAPI` could return a wrapper type, at the cost of a second representation. Today the frontend's own `AppSettings` is the typed view and nothing was lost.
7. **Contract checks in CI.** Level 1 is checked by tests (Go and frontend keys, documentation partials, duplicate ids). Level 2 has `host/testdata/testext` and the conformance test of `examples/extensions/stats`, which build with `go` and are skipped without it; they run wherever `go test ./...` runs, which includes CI's macOS and Linux jobs.
