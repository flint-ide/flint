# Extension protocol reference

Everything an extension and Flint say to each other, for extension contract **1**. For a walk-through, see [Write an extension](write-an-extension.md).

## Transport

- Flint starts the extension's program and talks [JSON-RPC 2.0](https://www.jsonrpc.org/specification) over its standard input and output: one message per line, UTF-8 JSON without raw newlines (as in MCP's stdio transport). Batches are not used.
- Both sides send requests, responses and notifications. Ids are numbers or strings; Flint's are integers.
- Standard output carries only protocol messages: a line that isn't a JSON-RPC 2.0 message stops the process. Standard error goes to the extension's log (lines cut at 2,000 characters; the last 256 KiB kept).
- A message is at most 4 MiB; a larger one stops the process. So does more than 8 MiB/s of output for 5 seconds.
- At most 16 requests from the extension are handled at once; more are answered with error `-32603`.
- A request either side gives up on is cancelled with the `$/cancelRequest` notification, `{ "id": <request id> }`, as in LSP.

### Error codes

| Code | Meaning |
|---|---|
| `-32700`, `-32600` | Parse error, invalid request |
| `-32601` | Method not found. Answer it for methods you don't implement; Flint does the same |
| `-32602` | Invalid params |
| `-32603` | Internal error |
| `-32001` | Permission denied: the manifest doesn't declare the permission the method needs, or the path is outside the trusted project folders |
| `-32800` | Cancelled (for example, the user said no to opening a link) |

## Manifest

`flint-extension.json` at the root of the extension's folder, at most 256 KiB. A manifest that breaks a rule is refused as a whole, and Settings ▸ Extensions shows why.

| Field | Type | Rules |
|---|---|---|
| `contract` | integer | Required. The major version of the contract; this Flint supports `1` and refuses others ("built for Flint's extension contract 2; this Flint supports 1") |
| `id` | string | Required. `publisher.name`: lower-case letters, digits and dashes, each part at most 31 characters. The installed folder is named after it |
| `name` | string | Required, at most 60 characters |
| `version` | string | Required, `major.minor.patch` with an optional `-suffix` |
| `publisher`, `description` | string | At most 60 and 500 characters |
| `run` | object | Required. `command`: a clean relative path inside the folder to an executable file (symbolic links must stay inside the folder); `args`: up to 32 fixed arguments. `darwin` and `linux` (each `{ command, args }`) replace both on that platform. Never run through a shell or `PATH` |
| `activation` | string[] | `onStartup` starts the process once Flint's window is up. `onCommand:<id>`, `onView:<id>`, `onAgentTool:<name>` must name the extension's own contributions; using any contribution starts the extension anyway |
| `permissions` | string[] | `workspace.read`, `secrets`, `agent.ask`, `terminal`, `network:<host>[,<host>…]` (shown, not enforced) |
| `contributes` | object | Below. Contribution points this Flint doesn't know (`fileMenu`) are ignored with a warning in Settings |

Every contributed id must start with `<id>.` followed by letters, digits, dots, dashes or underscores.

### contributes.commands

At most 100. `{ "id", "title" (≤ 100), "category"? (≤ 40, default: the extension's name), "keys"? }`. `keys` is a default shortcut such as `Mod+Alt+J` (`Mod` is ⌘ on macOS and Ctrl on Linux). It is dropped when Flint or the user's keymap already uses those keys.

### contributes.panels

At most 10. `{ "id", "title" (≤ 40), "icon", "order", "view" }`. `icon` is a name from Flint's icon set (`files`, `search`, `branch`, `fork`, `updown`, `database`, `pencil`, `checkbox`, `ship`, `cloud`, `helm`, `terminal`, `sum`, `puzzle`); an unknown name shows `puzzle`. `order` places the panel among other extensions' panels, after Flint's. `view` is `tree`, `list` or `webview`; a `webview` panel also has `"webview": { "html": "<path>" }` (see [Webviews](#webviews)). Each panel also gets a **Show …** command, `view.<panel id>`.

### contributes.tabViews

At most 50. `{ "id", "title" (≤ 60), "icon"?, "view": "webview", "webview": { "html" }, "restore"? }`. Kinds of editor tabs the extension opens with `window/openTab` (or `flint.openTab` from a page). With `restore: true`, open tabs are saved with the session (`{ view, key, title, data }`, `data` ≤ 64 KiB) and reopened at the next start.

### Webviews

A `webview` panel or tab view shows the extension's own UI: **one self-contained HTML file** in the extension's folder (≤ 32 MiB; inline every script, style, font and image, e.g. with a single-file build). Flint loads it into a sandboxed frame with an opaque origin and this policy, put first in `<head>`:

```
default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data: blob:;
font-src data:; media-src data: blob:; connect-src 'none'; frame-src 'none'; form-action 'none'; base-uri 'none'
```

The page has no network and nothing to load: it talks to its process through `window.flint`, which Flint defines before the page's scripts run:

| | |
|---|---|
| `flint.request(method, params?)` → `Promise` | sends `webview/request { view, key?, method, params }` to the process (starting it); resolves with the result, rejects with the error message; 30 s, 4 MiB |
| `flint.onMessage(fn)` → unsubscribe | the `message` of each `webview/post` the process sends to this view (and key) |
| `flint.view`, `flint.key`, `flint.data` | the panel or tab (`key` is `""` for a panel; `data` is the tab's saved data) |
| `flint.theme`, `flint.onTheme(fn)` | Flint's theme: CSS variables (`--bg-editor`, `--fg`, `--accent`, `--font-mono`, …), also set on `:root`, and `dark`; `<html data-theme>` is `dark` or `light` |
| `flint.openTab(view, key, title, data?)` | opens or focuses one of the extension's tab views |
| `flint.setTitle(title)`, `flint.setDirty(bool)`, `flint.setData(json)` | the tab's title, unsaved dot and saved data |
| `flint.runCommand(id, args?)`, `flint.copy(text)`, `flint.showMessage(type, text)` | one of its own commands; the clipboard; a toast |
| `flint.openFile(path, line?)` | opens a file in a project; needs `workspace.read` |

Shortcuts with ⌘/Ctrl/Alt, F-keys and Escape that the page doesn't `preventDefault()` reach Flint's keymap; ⌘A/C/V/X/Z/Y stay with the page. The example `examples/extensions/notes` uses all of this.

### contributes.statusItems

At most 10. `{ "id", "order" }`. Hidden until the first `status/update`.

### contributes.settings

`{ "title"?, "properties": { "<key>": <property>, … } }`, at most 50 properties, shown on the extension's Settings page in this order. Keys are letters, digits, dashes and underscores. A property supports exactly these keywords (anything else is a manifest error):

| Keyword | For | Meaning |
|---|---|---|
| `type` | all | `string`, `integer`, `number`, `boolean` or `array` (of strings) |
| `items` | array | Required: `{ "type": "string" }` |
| `enum` | string | Allowed values (a menu); the first is the default |
| `default` | all | Must be valid; otherwise `""`, `0` (or the nearest bound), `false`, `[]` |
| `minimum`, `maximum` | integer, number | Bounds |
| `maxLength` | string, array | Characters per value (default and limit 4,096) |
| `pattern` | string, array | A regular expression the whole value must match |
| `format` | string, array | `hostname` or `uri` (http or https); empty is allowed |
| `title`, `description` | all | The field's label and hint |

Values are stored in `settings.json` under `extensions` → `<id>`. The extension always receives every property: a stored value that doesn't fit the schema is replaced with the default.

### contributes.agentTools

At most 20. `{ "name", "title"?, "description", "inputSchema"?, "acts"? }`. `name` is lower-case letters, digits and underscores (≤ 40). `inputSchema` is a JSON Schema with `"type": "object"` (≤ 16 KiB), passed to agents as is. Agents see the tool as `<name part of the id>_<name>`, with dashes turned into underscores, cut to 64 characters and numbered if two collide (`acme.jira-cloud` + `issue` → `jira_cloud_issue`).

A tool is read-only unless it declares `"acts": true`: set it for every tool that changes something (creates, sends, starts, writes, deletes). Flint then asks the user before each call, showing the tool and the arguments (in the agent chat when the agent asks there, otherwise in a dialog of Flint's own); no approval policy and no "always allow" answers for them, and `agentTool/call` only arrives after a yes. The install confirmation and Settings ▸ Extensions mark such tools. A tool without `acts` must not change anything.

## Lifecycle

1. **Start.** On the first use of a contribution (or after the window is up, for `onStartup`), Flint starts the program in the extension's folder, in its own process group, with the environment below.
2. **`initialize`** (request, Flint → extension, 10 s). The extension answers with its contract version.
3. **`initialized`** (notification, Flint → extension). Requests follow.
4. **Idle stop.** After 5 minutes without a request in either direction, with none of its panels on screen, Flint stops it (`shutdown`). Extensions with `onStartup` are not stopped for being idle. Status items keep their last text; the next use starts the program again.
5. **`shutdown`** (request, Flint → extension, 2 s). Flint then closes standard input and, if the process is still there 2 seconds later, kills its process group. The same happens when the user turns the extension off, uninstalls or replaces it, or quits Flint.
6. **Crashes.** When the process ends on its own, its pending requests fail. The next use starts it again, after 1 s and then 4 s; an extension whose panel is on screen or that runs at startup is started again on its own. After 3 crashes in 10 minutes it stays stopped until the user clicks **Restart** (or turns it off and on), and a toast says so once.

### Environment

The process gets only `PATH` (with the login shell's additions), `HOME`, `USER`, `LOGNAME`, `SHELL`, `TMPDIR`, `TZ`, `LANG`, `LC_ALL`, `LC_CTYPE`, the `HTTP(S)_PROXY` / `NO_PROXY` variables when set, and:

| Variable | Value |
|---|---|
| `FLINT_EXTENSION_ID` | The extension's id |
| `FLINT_EXTENSION_DIR` | Its installed folder (also the working directory) |
| `FLINT_EXTENSION_DATA` | A private folder for its own data (`<config>/extension-data/<id>`, mode 0700), created before it starts and kept when it is uninstalled |
| `FLINT_VERSION` | Flint's version |

Other variables of the user's environment reach the program only when the manifest names them in `run.env` (at most 30 upper-case names, e.g. `["KUBECONFIG", "AWS_CONFIG_FILE", "DOCKER_HOST"]`): Flint copies their values from its own environment, which includes what the login shell sets. `PATH`, `HOME`, `SHELL`, `USER`, `LOGNAME`, `TMPDIR`, loader variables and `FLINT_*` can't be requested. The install dialog lists the names.

### Timeouts

| Request | Timeout |
|---|---|
| `initialize` | 10 s |
| `view/resolve` | 5 s (the panel shows "not responding" and **Try again**) |
| `agentTool/call` | 120 s |
| everything else | 30 s |

A request that times out is cancelled with `$/cancelRequest`; the process keeps running.

## Flint → extension

### initialize

```json
{ "jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {
  "contract": 1,
  "flintVersion": "v1.4.0",
  "extension": { "id": "acme.jira", "version": "0.3.0", "dir": "/Users/me/Library/Application Support/flint/extensions/acme.jira" },
  "settings": { "site": "acme.atlassian.net", "pollMinutes": 10 },
  "roots": [{ "path": "/Users/me/src/app", "trusted": true }],
  "platform": "darwin",
  "capabilities": { "views": ["tree", "list", "message"], "secrets": true, "agent": true, "openUrl": true }
} }
```

`roots` are the open project folders the user trusts; folders in Restricted Mode are left out. Answer:

```json
{ "jsonrpc": "2.0", "id": 1, "result": { "contract": 1, "capabilities": {} } }
```

`contract` is optional; any value other than `1` makes Flint stop the process.

### initialized

Notification, `{}`, after Flint accepted the answer to `initialize`.

### shutdown

Request, `{}`. Answer `null`; Flint then closes standard input.

### command/execute

```json
{ "jsonrpc": "2.0", "id": 7, "method": "command/execute", "params": { "id": "acme.jira.refresh", "args": { "key": "ABC-1" } } }
```

`args` is present when an item's `command` has them. Answer `null` once done (errors are shown to the user as a toast).

### view/resolve

```json
{ "jsonrpc": "2.0", "id": 8, "method": "view/resolve", "params": { "view": "acme.jira.issues", "parent": "ABC-1" } }
```

`parent` is present when the user expands a tree node. Answer with a view model:

```json
{ "kind": "tree", "items": [{
  "id": "ABC-1", "label": "Fix login", "description": "In progress", "tooltip": "ABC-1 · Fix login",
  "icon": "checkbox", "hasChildren": false,
  "actions": [{ "id": "open", "label": "Open in Browser" }],
  "command": { "id": "acme.jira.open", "args": { "key": "ABC-1" } }
}] }
```

| Field | Rules |
|---|---|
| `kind` | The panel's `view` (`tree` or `list`), or `message` |
| `items[].id` | Required, unique in the answer, ≤ 256 characters |
| `label`, `description`, `tooltip` | Plain text (never HTML or Markdown), ≤ 2,000 characters; control characters are removed |
| `icon` | An icon name, else none |
| `hasChildren` | Trees only |
| `actions` | ≤ 10, each `{ id (≤ 64), label (≤ 60) }`: the item's context menu, sent back as `view/action` |
| `command` | One of the extension's commands, run on click with `args` (≤ 16 KiB) |

At most 5,000 items per answer; Flint shows the first 5,000 and says so. Instead of items, any panel may show a message with buttons:

```json
{ "kind": "message", "text": "Sign in to Jira first.", "actions": [{ "id": "signin", "label": "Sign In" }] }
```

### view/action

```json
{ "jsonrpc": "2.0", "id": 9, "method": "view/action", "params": { "view": "acme.jira.issues", "item": "ABC-1", "action": "open" } }
```

`item` is `""` for a message's button. Answer `null`.

### agentTool/call

```json
{ "jsonrpc": "2.0", "id": 10, "method": "agentTool/call", "params": { "name": "jira_issue", "root": "/Users/me/src/app", "arguments": { "key": "ABC-1" } } }
```

`root` is the agent's project folder, always a trusted one. Answer `{ "text": "…" }`; an error becomes a failed tool call for the agent. Flint's MCP server cuts long answers.

### settings/changed

```json
{ "jsonrpc": "2.0", "method": "settings/changed", "params": { "settings": { "site": "acme.atlassian.net", "pollMinutes": 5 } } }
```

All properties, with defaults filled in.

### workspace/rootsChanged

```json
{ "jsonrpc": "2.0", "method": "workspace/rootsChanged", "params": { "roots": [{ "path": "/Users/me/src/app", "trusted": true }] } }
```

Sent when a folder is opened, attached, removed, trusted or untrusted.

### view/visible

```json
{ "jsonrpc": "2.0", "method": "view/visible", "params": { "view": "acme.jira.issues", "visible": true } }
```

If the extension refreshes a view on a timer, it should do so only while the view is visible. For a tab view, `key` names the tab.

### webview/request

A page's `flint.request(method, params)`. The result (any JSON, ≤ 4 MiB) resolves the page's promise; an error rejects it with the error's message. Timeout: 30 s.

```json
{ "jsonrpc": "2.0", "id": 9, "method": "webview/request",
  "params": { "view": "example.notes.note", "key": "2", "method": "save", "params": { "title": "Plan", "text": "…" } } }
```

## Extension → Flint

### Notifications

| Method | Params | Notes |
|---|---|---|
| `status/update` | `{ "id", "text", "tooltip"?, "command"?, "severity"? }` | `id` is a declared status item; `text` ≤ 40 characters (`""` hides the item); `command` one of the extension's commands, run on click; `severity` `info`, `warning` or `error` |
| `view/refresh` | `{ "view", "item"? }` | Flint resolves the panel again, with the nodes the user expanded |
| `window/showMessage` | `{ "type": "info" \| "warning" \| "error", "message" }` | A toast, prefixed with the extension's name; ≤ 1,000 characters; at most 5 per 10 seconds |
| `webview/post` | `{ "view", "key"?, "message" }` | `message` (any JSON, ≤ 4 MiB) reaches `flint.onMessage` in the view's frames: the tab with that key, or every frame of the view without one |
| `log` | `{ "level", "message" }` | A line in the extension's log |

### Requests

| Method | Params → result | Permission |
|---|---|---|
| `window/showMessageRequest` | `{ "type", "message", "actions": [title, …] }` (1–3 actions) → `{ "action"? }` | none |
| `window/openFile` | `{ "path", "line"? }` → `null` | `workspace.read` |
| `window/openUrl` | `{ "url" }` → `null` | none |
| `window/openTab` | `{ "view", "key", "title"?, "data"? }` → `null`; opens or focuses a tab of a declared tab view (`data` ≤ 64 KiB) | none |
| `workspace/readFile` | `{ "path" }` → `{ "text" }` | `workspace.read` |
| `secrets/get` | `{ "key" }` → `{ "value"? }` | `secrets` |
| `secrets/set` | `{ "key", "value" }` → `{}` | `secrets` |
| `secrets/delete` | `{ "key" }` → `{}` | `secrets` |
| `agent/ask` | `{ "prompt", "attachments"?: [{ "path" }] }` → `null` | `agent.ask` |
| `window/openTerminal` | `{ "title"?, "cwd"?, "env"?, "command"?, "tag"? }` → `null`; a terminal tab in `cwd` (a folder in a trusted project; default: the active project) that types `command` (argv, quoted by Flint) | `terminal` |

- **Paths** are absolute and must be inside a trusted project folder after symbolic links are resolved; otherwise the request fails with `-32001`.
- **`workspace/readFile`** returns UTF-8 text up to 4 MiB. Files inside `.git` and credential files (`.env`, `.env.*`, `.envrc`, `.netrc`, `.npmrc`, `.pypirc`, `.pgpass`, `id_rsa` and other SSH keys, `*.pem`, `*.key`, `*.p12`, `*.pfx`, `*.keystore`, `*.jks`) are refused.
- **`window/openUrl`** accepts `http` and `https` only. The first link to a site asks the user; after that, links to the same site open without asking until Flint quits. A refusal is error `-32800`.
- **`window/showMessageRequest`** shows a native question with the actions and Cancel; `action` is missing when the user cancels.
- **Secrets** are kept in the system keychain (service `flint-extensions`, account `<id>:<key>`). Keys are 1–128 letters, digits, dots, dashes or underscores; values at most 64 KiB. An extension can't reach another's secrets.
- **`window/openTerminal`** opens a normal terminal tab, so the user sees what runs and can stop it. `env` (≤ 50 variables) may not set `PATH`, `HOME`, `SHELL`, the shell's startup variables, loader variables or `FLINT_*`; arguments (≤ 64) and values are at most 4,096 characters without control characters.
- **`agent/ask`** puts the prompt (≤ 20,000 characters) and the attached files in the agent chat's draft. It never sends.

There is no method to run commands and no proxy for network requests: the extension is its own program and does its own I/O.

## Versioning

- `contract` is a major version. Flint supports one major version at a time and says so when a manifest asks for another.
- Within a major version, changes are additive only: new methods, new optional fields, new view kinds, new contribution points, announced in `initialize`'s `capabilities`. A side that doesn't know a method answers `-32601`, and callers must cope with that. Unknown fields are ignored.
