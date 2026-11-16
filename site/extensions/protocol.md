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

Editor decorations add no error code: `editor/setDecorations` and `editor/clearDecorations` are notifications, so a publish Flint refuses or drops is a line in the extension's log, not an error answer.

## Manifest

`flint-extension.json` at the root of the extension's folder, at most 256 KiB. A manifest that breaks a rule is refused as a whole, and Settings ▸ Extensions shows why.

| Field | Type | Rules |
|---|---|---|
| `contract` | integer | Required. The major version of the contract; this Flint supports `1` and refuses others ("built for Flint's extension contract 2; this Flint supports 1") |
| `id` | string | Required. `publisher.name`: lower-case letters, digits and dashes, each part at most 31 characters. The installed folder is named after it |
| `name` | string | Required, at most 60 characters |
| `version` | string | Required, `major.minor.patch` with an optional `-suffix` |
| `publisher`, `description` | string | At most 60 and 500 characters |
| `run` | object | Required. `command`: a clean relative path inside the folder to an executable file (symbolic links must stay inside the folder); `args`: up to 32 fixed arguments. `darwin` and `linux` (each `{ command, args }`) replace both on that platform. Never run through a shell or `PATH`. `env`: see [Environment](#environment). `sandbox`: see [run.sandbox](#runsandbox) |
| `activation` | string[] | `onStartup` starts the process once Flint's window is up. `onCommand:<id>`, `onView:<id>`, `onAgentTool:<name>` must name the extension's own contributions and `onLanguage:<language>` a language one of its decoration sets lists; using any contribution starts the extension anyway |
| `permissions` | string[] | `workspace.read`, `secrets`, `agent.ask`, `terminal`, `network:<host>[,<host>…]` (the hosts are shown, not enforced; in a [sandbox](#runsandbox) the permission opens the network) |
| `contributes` | object | Below. Contribution points this Flint doesn't know (`fileMenu`) are ignored with a warning in Settings |

Every contributed id must start with `<id>.` followed by letters, digits, dots, dashes or underscores.

### run.sandbox

What the program needs from the machine. Flint confines a program that declares this object with the operating system's sandbox: Landlock and seccomp on Linux, `sandbox-exec` on macOS.

```json
"run": {
  "command": "bin/ext",
  "env": ["KUBECONFIG"],
  "sandbox": { "read": ["~/.kube", "${env:KUBECONFIG}"], "write": ["~/.cache/tool"], "network": true, "exec": true }
}
```

| Field | Type | Meaning |
|---|---|---|
| `mode` | string | `"strict"` (the default) or `"none"`: the program can't work confined and runs with the user's permissions. With `"none"`, no other field is allowed |
| `read` | string[] | More files and folders the program may read, at most 32 |
| `write` | string[] | Files and folders it may read and change, at most 32 |
| `network` | boolean | It talks to the Internet (IPv4 and IPv6). A `network:<host>` permission implies it. Hosts are not filtered |
| `localSockets` | boolean | It connects to services on this machine through Unix sockets (the Docker socket, the desktop's message bus) |
| `exec` | boolean | It starts other programs. They are confined exactly like the program itself |

An unknown field is a manifest error. `"sandbox": {}` is the **strict profile**:

- **Read:** the extension's own folder; the system's programs, libraries, certificates, time zones and `/etc`; the trusted project folders, only with the `workspace.read` permission.
- **Write:** `FLINT_EXTENSION_DATA` and a private temporary folder (`TMPDIR`), made for each run and deleted when the process ends. The extension's own folder is read-only.
- **Network:** none. No socket of any kind can be created.
- **Programs:** only files in the extension's folder, plus what the system needs to start its program (the dynamic loader; a script's `#!` interpreter and, for `#!/usr/bin/env name`, the `name` found on `PATH`). With `exec`, anything it can read, and the folders of `PATH` become readable.
- **Everything else is unreadable:** the rest of the home folder (`~/.ssh`, `~/.gnupg`, `~/.aws`, `~/.kube`, `~/.docker`, `~/.config/gcloud`, browser profiles, keyrings and Keychains, shell history), Flint's configuration, other extensions' folders and data, other programs' temporary files.
- **Limits:** no core dumps, at most 4,096 open files. Memory and CPU are not limited.

Paths in `read` and `write`:

| Form | Meaning |
|---|---|
| `/abs/path` | An absolute path |
| `~/path` | Inside the user's home folder |
| `${XDG_CONFIG_HOME}/path`, also `XDG_DATA_HOME`, `XDG_CACHE_HOME`, `XDG_STATE_HOME`, `XDG_RUNTIME_DIR` | Inside that folder (`~/.config`, `~/.local/share`, `~/.cache`, `~/.local/state` when the variable is unset; `XDG_RUNTIME_DIR` has no default) |
| `${env:NAME}` | The path, or the paths separated by `:`, in a variable listed in `run.env`: `${env:KUBECONFIG}` |

Paths are written clean: no `..` or `.`, no doubled or trailing slash, no wildcards, at most 512 bytes; the root and the home folder themselves can't be named. A folder grants everything in it. When the program starts, Flint does not grant, and says so in Settings ▸ Extensions and in the log:

- a path that doesn't exist;
- anything inside Flint's own configuration folder other than the extension's folders;
- a path that contains a protected location, such as `~/.config` (browser profiles and Flint's settings are in it): name the folders you need;
- a protected location reached through a symbolic link. Protected locations (`~/.ssh`, `~/.kube`, …) are granted only when the manifest names them as written, so the user saw them when installing.

**A manifest without `run.sandbox` runs unconfined,** as extensions did before the sandbox existed, and Flint says so in the install dialog, in Settings ▸ Extensions and in the log. Users can refuse to run such extensions (`extensions.requireSandbox`), so declare the object, even as `{}`.

Where the system has no sandbox (Linux without Landlock, that is before 5.13 or with Landlock left out of the kernel; macOS without `/usr/bin/sandbox-exec`), a program that declares one runs unconfined and Flint shows "Not sandboxed: not available on this system". On an older Linux kernel some protections are missing, and Flint lists them: readable files can be truncated before 6.2, signals reach other processes before 6.12. On macOS, `network` and `localSockets` are not told apart: declaring one allows both.

What a sandboxed program should expect:

- Opening a file it wasn't granted fails with "permission denied" (`EACCES`); creating a socket without `network` or `localSockets` fails the same way. Starting a program without `exec` does too.
- `HOME` still names the home folder, but nothing in it is readable: keep state in `FLINT_EXTENSION_DATA`, scratch files in `TMPDIR`.
- The trusted project folders are readable directly only with `workspace.read`. When they change, Flint stops a sandboxed program and starts it again (a sandbox can't be widened while the process runs) instead of sending `workspace/rootsChanged`.
- `secrets/*`, `workspace/readFile`, `window/openUrl` and the other requests work as before: Flint does that work, outside the sandbox.

### contributes.commands

At most 100. `{ "id", "title" (≤ 100), "category"? (≤ 40, default: the extension's name), "keys"? }`. `keys` is a default shortcut such as `Mod+Alt+J` (`Mod` is ⌘ on macOS and Ctrl on Linux). It is dropped when Flint or the user's keymap already uses those keys.

### contributes.panels

At most 10. `{ "id", "title" (≤ 40), "icon", "order", "view" }`. `icon` is a name from Flint's icon set (`files`, `search`, `branch`, `fork`, `updown`, `database`, `pencil`, `checkbox`, `ship`, `cloud`, `helm`, `terminal`, `sum`, `puzzle`); an unknown name shows `puzzle`. `order` places the panel among other extensions' panels, after Flint's. `view` is what the panel shows: `tree`, `list`, `table`, `form` or `chart`, drawn by Flint from the answer to [`view/resolve`](#viewresolve), or `webview`; a `webview` panel also has `"webview": { "html": "<path>" }` (see [Webviews](#webviews)) and no other panel may have that key. Any other `view` (`markdown`, `cards`, `stack`) refuses the manifest: this Flint doesn't draw it. Each panel also gets a **Show …** command, `view.<panel id>`.

### contributes.tabViews

At most 50. `{ "id", "title" (≤ 60), "icon"?, "view", "webview"?, "restore"? }`. Kinds of editor tabs the extension opens with `window/openTab` (or `flint.openTab` from a page). `view` is `table`, `form` or `chart` (drawn by Flint from [`view/resolve`](#viewresolve), which gets the tab's `key` and `data`), or `webview` with `"webview": { "html" }`; `tree` and `list` are for panels only. With `restore: true`, open tabs are saved with the session (`{ view, key, title, data }`, `data` ≤ 64 KiB) and reopened at the next start; a restored tab is resolved when it is first shown.

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

### contributes.decorationSets

At most 20. A decoration set is a group of marks the extension draws in editors (see [Editor decorations](#editor-decorations)); the user turns each set on or off. Declaring one needs the `workspace.read` permission.

```json
"decorationSets": [{ "id": "acme.lint.issues", "title": "Lint Issues", "languages": ["go"], "extensions": [".md"], "globs": ["*.test.ts"], "default": true, "unsavedText": false }]
```

| Field | Rules |
|---|---|
| `id` | Required, starts with `<id>.` |
| `title` | Required, ≤ 60 characters: its name in Settings ▸ Extensions and in the command **Toggle … in Editors** |
| `languages` | Flint's language ids: `go`, `typescript` (JavaScript too), `python`, `rust`, or the id of a language an extension [contributes](write-an-extension.md) |
| `extensions` | File extensions with the dot (`.md`), compared without case |
| `globs` | Patterns for the file's **name**, without folders: `*`, `?` and `[a-z]` classes (`Dockerfile*`, `*.test.ts`, `*` for every file), ≤ 80 characters each |
| `default` | `false` leaves the set off until the user turns it on. Default `true` |
| `unsavedText` | `true`: `editor/opened` and `editor/changed` carry the document's text as it is in the editor, unsaved edits included. The install dialog says so |

A set needs at least one of `languages`, `extensions` and `globs` (at most 50 together); a file matching any of them is the set's.

## Lifecycle

1. **Start.** On the first use of a contribution, when a file one of its decoration sets is for is opened in an editor (or after the window is up, for `onStartup`), Flint starts the program in the extension's folder, in its own process group, with the environment below, inside its [sandbox](#runsandbox) when the manifest declares one.
2. **`initialize`** (request, Flint → extension, 10 s). The extension answers with its contract version.
3. **`initialized`** (notification, Flint → extension). Requests follow.
4. **Idle stop.** After 5 minutes without a request in either direction, with none of its panels on screen and no document it decorates open in an editor, Flint stops it (`shutdown`). Extensions with `onStartup` are not stopped for being idle. Status items keep their last text; the next use starts the program again.
5. **`shutdown`** (request, Flint → extension, 2 s). Flint then closes standard input and, if the process is still there 2 seconds later, kills its process group. The same happens when the user turns the extension off, uninstalls or replaces it, or quits Flint.
6. **Crashes.** When the process ends on its own, its pending requests fail. The next use starts it again, after 1 s and then 4 s; an extension whose panel is on screen, that decorates an open document or that runs at startup is started again on its own. After 3 crashes in 10 minutes it stays stopped until the user clicks **Restart** (or turns it off and on), and a toast says so once.

### Environment

The process gets only `PATH` (with the login shell's additions), `HOME`, `USER`, `LOGNAME`, `SHELL`, `TMPDIR`, `TZ`, `LANG`, `LC_ALL`, `LC_CTYPE`, the `HTTP(S)_PROXY` / `NO_PROXY` variables when set, and:

| Variable | Value |
|---|---|
| `FLINT_EXTENSION_ID` | The extension's id |
| `FLINT_EXTENSION_DIR` | Its installed folder (also the working directory) |
| `FLINT_EXTENSION_DATA` | A private folder for its own data (`<config>/extension-data/<id>`, mode 0700), created before it starts and kept when it is uninstalled |
| `FLINT_VERSION` | Flint's version |
| `FLINT_EXTENSION_SANDBOX` | `1` when the process runs in a [sandbox](#runsandbox); not set otherwise |
| `TMPDIR` | In a sandbox: a private temporary folder for this run, the only one the process can write, deleted when it ends. Otherwise the user's |

Other variables of the user's environment reach the program only when the manifest names them in `run.env` (at most 30 upper-case names, e.g. `["KUBECONFIG", "AWS_CONFIG_FILE", "DOCKER_HOST"]`): Flint copies their values from its own environment, which includes what the login shell sets. `PATH`, `HOME`, `SHELL`, `USER`, `LOGNAME`, `TMPDIR`, loader variables and `FLINT_*` can't be requested. The install dialog lists the names.

### Timeouts

| Request | Timeout |
|---|---|
| `initialize` | 10 s |
| `view/resolve` | 5 s (the panel or tab shows "not responding" and **Try again**) |
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
  "sandbox": { "active": true, "network": false, "localSockets": false, "exec": false },
  "capabilities": { "views": ["tree", "list", "table", "form", "chart", "message"], "charts": ["line", "bar", "area"],
                    "secrets": true, "agent": true, "openUrl": true,
                    "decorations": ["diagnostic", "gutter", "inline", "highlight"], "quickFixes": true }
} }
```

`capabilities.views` lists the view kinds this Flint draws and `charts` the chart types. A Flint without `table`, `form` or `chart` there refuses a manifest that declares such a panel, so an extension that wants to run on both declares what the older one knows.

`capabilities.decorations` lists the decoration kinds this Flint draws and `quickFixes` says it sends `editor/action`; a Flint without them has no editor decorations (and ignores `contributes.decorationSets` with a warning).

`roots` are the open project folders the user trusts; folders in Restricted Mode are left out. `sandbox.active` says whether the process is confined ([run.sandbox](#runsandbox)); the other three fields are present when it is. Answer:

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

`parent` is present when the user expands a tree node. For a tab of a [tab view](#contributestabviews), `key` is the tab's key and `data` the JSON saved with it (≤ 64 KiB; left out when there is none):

```json
{ "jsonrpc": "2.0", "id": 8, "method": "view/resolve", "params": { "view": "acme.jira.board", "key": "sprint-42", "data": { "team": "core" } } }
```

Answer with a view model of the kind the panel or tab view declares: a tree or list (below), a [table](#table-views), a [form](#form-views), a [chart](#chart-views), or a message. A tree:

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
| `kind` | The panel's or tab view's `view`, or `message`. Another kind is shown as a message saying so |
| `items[].id` | Required, unique in the answer, ≤ 256 characters |
| `label`, `description`, `tooltip` | Plain text (never HTML or Markdown), ≤ 2,000 characters; control characters are removed |
| `icon` | An icon name, else none |
| `hasChildren` | Trees only |
| `actions` | ≤ 10, each `{ id (≤ 64), label (≤ 60) }`: the item's context menu, sent back as `view/action` |
| `command` | One of the extension's commands, run on click with `args` (≤ 16 KiB) |

At most 5,000 items per answer; Flint shows the first 5,000 and says so. Instead of its content, any panel or tab may show a message with buttons:

```json
{ "kind": "message", "text": "Sign in to Jira first.", "actions": [{ "id": "signin", "label": "Sign In" }] }
```

Everything in a view model is checked and cut to its limits by Flint before it is drawn. Text is plain text everywhere: never HTML or Markdown, control characters removed. An extension can't set a color: badges and chart series take theirs from the theme.

#### Table views

```json
{ "kind": "table",
  "columns": [{ "id": "key", "title": "Key", "type": "text" },
              { "id": "points", "title": "Points", "type": "number", "align": "end" },
              { "id": "updated", "title": "Updated", "type": "date" },
              { "id": "status", "title": "Status", "type": "badge", "badges": { "Done": "ok", "Blocked": "error" } }],
  "rows": [{ "id": "ABC-1",
             "cells": { "key": "ABC-1", "points": 3, "updated": "2026-11-01T09:30:00Z", "status": "Done" },
             "command": { "id": "acme.jira.open", "args": { "key": "ABC-1" } },
             "actions": [{ "id": "close", "label": "Close Issue" }] }],
  "sort": { "column": "updated", "descending": true },
  "actions": [{ "id": "assign", "label": "Assign to Me" }] }
```

| Field | Rules |
|---|---|
| `columns` | 1–30 (more are cut; none shows a message). `id` required, unique, ≤ 64 characters; `title` ≤ 100 (default: the id) |
| `columns[].type` | `text` (default), `number`, `date` or `badge`. An unknown type is `text` |
| `columns[].align` | `start`, `center` or `end`. Default: `end` for numbers, `start` otherwise |
| `columns[].badges` | For `badge` columns: value → `info`, `ok`, `warning` or `error`, ≤ 50 entries. A value without a severity is a neutral badge |
| `rows` | ≤ 5,000; Flint shows the first 5,000 and says so. `id` required, unique, ≤ 256 characters; a row without one is left out |
| `rows[].cells` | An object by column id. `text` and `badge`: a string ≤ 2,000 characters (a number or boolean is shown as written); `number`: a JSON number; `date`: an ISO 8601 date (`2026-11-01`) or date and time (`2026-11-01T09:30:00Z`; without a zone it is UTC). A missing value, or one of another type, is an empty cell. Keys that aren't columns are ignored |
| `rows[].command` | One of the extension's commands, run with `args` (≤ 16 KiB) on Enter or a double click |
| `actions`, `rows[].actions` | ≤ 10 each, `{ id (≤ 64), label (≤ 60) }`: the row's context menu, sent as [`view/action`](#viewaction) with the row's id as `item`. A row's own `actions` replace the table's for that row |
| `sort` | `{ "column", "descending"? }`: the order the table is first shown in. Without it, the order of `rows` |

Flint sorts the rows itself when the user clicks a column header (ascending, descending, then the extension's order again): numbers by value, dates by time, text without case and with the numbers in it by value, empty cells last. The extension is not asked again. Rows are drawn as they scroll into view, so 5,000 rows cost no more than 50. There is no paging: an extension with more rows filters or summarizes them.

#### Form views

```json
{ "kind": "form", "id": "comment", "description": "Add a comment to ABC-1.", "submit": "Add Comment",
  "fields": [{ "id": "comment", "type": "string", "title": "Comment", "multiline": true, "maxLength": 4000, "required": true },
             { "id": "notify", "type": "boolean", "title": "Notify watchers", "default": true },
             { "id": "priority", "type": "string", "enum": ["low", "normal", "high"], "value": "normal" },
             { "id": "token", "type": "string", "title": "API token", "secret": true }] }
```

A field is a property of the [settings schema](#contributessettings), with the same keywords and the same checks (`type`, `enum`, `default`, `minimum`, `maximum`, `maxLength`, `pattern`, `format`, `items`, `title`, `description`), plus:

| Keyword | Meaning |
|---|---|
| `id` | Required: letters, digits, dashes and underscores (≤ 40), unique in the form. The key of the value in `form/submit` |
| `value` | The value to show. One the field's rules refuse is replaced with `default` |
| `required` | Text and lists may not be empty |
| `multiline` | A text field of several lines (strings without `enum`) |
| `secret` | A password field (strings without `enum`). Flint never shows a value in it: `value` and `default` are dropped, so the field always starts empty, and what the user types is sent with the submit only. Flint stores it nowhere; keep it with [`secrets/set`](#requests) |
| `placeholder` | Dim text in an empty field, ≤ 100 characters |

At most 50 fields. `maxLength` above 4,096 is lowered to 4,096, the longest text a field holds. `submit` is the button's label (≤ 60, default "Submit"); `description` is plain text above the fields (≤ 2,000). `id` (≤ 64) is for the extension's own use. Keywords Flint doesn't know are ignored; a field it can't draw (an unknown `type`, a `default` its own rules refuse, a title with control characters) makes the whole form a message that names the field, so it is noticed while developing. The read-only Markdown field and the `select` type of the design are not part of the contract: use `enum` for a menu.

Submitting sends [`form/submit`](#formsubmit).

#### Chart views

```json
{ "kind": "chart", "type": "line", "title": "Open issues",
  "x": { "type": "date", "label": "Week" }, "y": { "label": "Issues", "min": 0 },
  "series": [{ "id": "open", "label": "Open", "points": [["2026-10-01", 14], ["2026-10-08", 11]] },
             { "id": "blocked", "label": "Blocked", "points": [["2026-10-01", 2], ["2026-10-08", 3]] }] }
```

| Field | Rules |
|---|---|
| `type` | `line`, `bar` or `area` (`capabilities.charts`). Another type shows a message |
| `title` | ≤ 100 characters |
| `x.type` | `number`, `date` (ISO 8601, as in a table) or `category` (text ≤ 200 characters; the default). `x.label` ≤ 100 |
| `y` | `label` ≤ 100; `min` and `max` fix an end of the axis (values beyond it are drawn at the edge). Without them the axis covers the values, from zero for bars and areas |
| `series` | ≤ 10, each `{ "id" (≤ 64, unique), "label" (≤ 100, default: the id), "points" }` |
| `points` | ≤ 2,000 per series, each `[x, y]` with `y` a JSON number. A point whose `x` doesn't fit the axis or whose `y` isn't a number is left out |

More series or points than that are cut, and the chart says so. Lines and areas over numbers or dates are drawn in the order of `x`, whatever the order of `points`; categories keep the order in which they first appear. Bars are grouped by `x`, one bar per series, and at most 500 different `x` values are drawn. Series get their colors from the theme, in order; the chart has a legend and lists its values as a table under **Values**.

### view/action

```json
{ "jsonrpc": "2.0", "id": 9, "method": "view/action", "params": { "view": "acme.jira.issues", "item": "ABC-1", "action": "open" } }
```

`item` is the id of a tree or list item or of a table's row, and `""` for a message's button. For a tab of a tab view, `key` names the tab. Answer `null`.

### form/submit

The user pressed a [form](#form-views)'s button. Request, 30 s:

```json
{ "jsonrpc": "2.0", "id": 11, "method": "form/submit", "params": {
  "view": "acme.jira.comment", "key": "ABC-1",
  "values": { "comment": "Fixed in 1.4.", "notify": true, "priority": "normal", "token": "…" } } }
```

`values` holds every field of the form Flint last drew for that panel or tab, by field id, and nothing else: Flint checks each value against the field's rules first (and `required`), shows the reasons under the fields, and sends the request only when all of them pass. A field the page didn't send has its `default`. `key` is present for a tab. Answer with nothing to accept the values, or say what is wrong:

```json
{ "errors": { "comment": "mentions a user who can't see this issue" }, "error": "The issue is locked.", "message": "" }
```

| Field | Meaning |
|---|---|
| `errors` | Field id → why its value is refused, shown under the field (≤ 500 characters; ids that aren't fields are dropped) |
| `error` | Why the form as a whole is refused, shown above the button (≤ 500) |
| `message` | Shown next to the button after an accepted submit (≤ 500; default "Done.") |

After an accepted submit (no `errors`, no `error`) Flint resolves the view again; if the form comes back unchanged, what the user typed stays, except in secret fields. An error answer (a JSON-RPC error) is shown above the button. If the form was never drawn, or its fields no longer are what Flint last drew (more than 64 forms of one extension open at once), the submit fails without reaching the extension and the user is told to refresh.

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

If the extension refreshes a view on a timer, it should do so only while the view is visible. For a tab view (declarative or webview), `key` names the tab.

### webview/request

A page's `flint.request(method, params)`. The result (any JSON, ≤ 4 MiB) resolves the page's promise; an error rejects it with the error's message. Timeout: 30 s.

```json
{ "jsonrpc": "2.0", "id": 9, "method": "webview/request",
  "params": { "view": "example.notes.note", "key": "2", "method": "save", "params": { "title": "Plan", "text": "…" } } }
```

### editor/opened, editor/changed, editor/closed

Notifications about the documents open in editors, sent only to an extension with a [decoration set](#contributesdecorationsets) that is on and for that file. See [Editor decorations](#editor-decorations) for which files and when.

```json
{ "jsonrpc": "2.0", "method": "editor/opened", "params": {
  "path": "/Users/me/src/app/main.go", "language": "go", "version": 1, "sets": ["acme.lint.issues"], "text": "package main\n…" } }
{ "jsonrpc": "2.0", "method": "editor/changed", "params": { "path": "/Users/me/src/app/main.go", "version": 4, "text": "…" } }
{ "jsonrpc": "2.0", "method": "editor/closed", "params": { "path": "/Users/me/src/app/main.go" } }
```

| Field | Meaning |
|---|---|
| `path` | The file, absolute. Use it as given in `editor/setDecorations` |
| `language` | Flint's language id, `""` when the file has none |
| `version` | The document's version: 1 when it is opened, one more with every change to its text. `editor/changed` is sent at most every 300 ms per file while the user types, so versions may skip |
| `sets` | The extension's sets that are on and for this file. `editor/opened` is sent again for an open document when this list changes (the user turned a set on or off), and after the extension restarts |
| `text` | Only for a set with `unsavedText`, and only when the text is at most 1 MiB: the document as it is in the editor. Without it, read the saved file with `workspace/readFile` |

### editor/action

The user chose a quick fix (an entry of a decoration's `actions` without a `command`). Request, 30 s:

```json
{ "jsonrpc": "2.0", "id": 12, "method": "editor/action", "params": {
  "set": "acme.lint.issues", "path": "/Users/me/src/app/main.go", "version": 4,
  "action": "rename", "decoration": "d1", "line": 12, "endLine": 12, "from": 4, "to": 9 } }
```

`decoration` is the decoration's `id` (left out when it has none); `line`, `endLine`, `from` and `to` are where the decoration is now, after the user's edits since the publish. Answer with edits for the document **at `version`**, or with none:

```json
{ "edits": [{ "line": 12, "from": 4, "to": 9, "text": "err2" }, { "line": 20, "from": 0, "text": "// renamed\n" }] }
```

| Field | Rules |
|---|---|
| `line`, `endLine` | 1-based; `endLine` defaults to `line` |
| `from`, `to` | Columns in UTF-16 code units from the start of `line` and of `endLine`; `to` defaults to `from` (an insertion) |
| `text` | The replacement, `""` to delete; `\n` or `\r\n` between lines |

At most 1,000 edits and 1 MiB of text. Flint applies the edits as one change (one undo step) and only if the document is still at `version`; otherwise the user is told to try again. An edit outside the document, or two edits that overlap, refuse the whole answer: edits are never clamped. If you don't know the text of that version, answer without edits. The extension may do anything else in the handler; to run one of its commands instead, give the action a `command`.

## Extension → Flint

### Notifications

| Method | Params | Notes |
|---|---|---|
| `status/update` | `{ "id", "text", "tooltip"?, "command"?, "severity"? }` | `id` is a declared status item; `text` ≤ 40 characters (`""` hides the item); `command` one of the extension's commands, run on click; `severity` `info`, `warning` or `error` |
| `view/refresh` | `{ "view", "key"?, "item"? }` | Flint resolves the panel again, with the nodes the user expanded. For a table, form or chart tab view, `key` refreshes one tab and no `key` every open tab of the view. A view that isn't in the manifest, or a webview, is a line in the log |
| `window/showMessage` | `{ "type": "info" \| "warning" \| "error", "message" }` | A toast, prefixed with the extension's name; ≤ 1,000 characters; at most 5 per 10 seconds |
| `webview/post` | `{ "view", "key"?, "message" }` | `message` (any JSON, ≤ 4 MiB) reaches `flint.onMessage` in the view's frames: the tab with that key, or every frame of the view without one |
| `editor/setDecorations` | `{ "set", "path", "version"?, "decorations" }` | Replaces what a decoration set shows in one open document; needs `workspace.read`. See [Editor decorations](#editor-decorations) |
| `editor/clearDecorations` | `{ "set", "path"? }` | Removes a set's decorations from one document, or from all |
| `log` | `{ "level", "message" }` | A line in the extension's log |

### Requests

| Method | Params → result | Permission |
|---|---|---|
| `window/showMessageRequest` | `{ "type", "message", "actions": [title, …] }` (1–3 actions) → `{ "action"? }` | none |
| `window/openFile` | `{ "path", "line"? }` → `null` | `workspace.read` |
| `window/openUrl` | `{ "url" }` → `null` | none |
| `window/openTab` | `{ "view", "key", "title"?, "data"? }` → `null`; opens or focuses a tab of a declared tab view (`data` ≤ 64 KiB). For a table, form or chart tab view Flint then sends `view/resolve` with that `key` and `data` | none |
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

## Editor decorations

An extension with a [decoration set](#contributesdecorationsets) marks lines in the files open in editors. Flint tells it which documents are open (`editor/opened`, `editor/changed`, `editor/closed`), and it publishes the set's decorations for a document:

```json
{ "jsonrpc": "2.0", "method": "editor/setDecorations", "params": {
  "set": "acme.lint.issues", "path": "/Users/me/src/app/main.go", "version": 4, "decorations": [
    { "kind": "diagnostic", "id": "d1", "line": 12, "from": 4, "to": 9, "severity": "warning", "message": "err is shadowed",
      "actions": [{ "id": "rename", "label": "Rename to err2" }, { "id": "docs", "label": "Open the Rule", "command": { "id": "acme.lint.rule", "args": { "rule": "shadow" } } }] },
    { "kind": "gutter", "line": 30, "icon": "check", "severity": "ok", "hover": "Covered by 3 tests" },
    { "kind": "inline", "line": 41, "text": "ABC-123 · In progress", "command": { "id": "acme.lint.open", "args": { "key": "ABC-123" } } },
    { "kind": "highlight", "line": 50, "endLine": 58, "severity": "info" }
  ] } }
```

Each publish replaces everything the set showed in that document; an empty list removes it.

| Kind | Draws |
|---|---|
| `diagnostic` | A wavy underline under the range, a marker in the gutter, and `message` (required) on hover |
| `gutter` | A marker in the gutter next to the line, with `hover` as its tooltip. `icon` is `dot` (default), `check`, `cross`, `alert`, `info` or `arrow` |
| `inline` | `text` (required, ≤ 200 characters, one line) in dim type after the end of the line; with `command`, a click runs that command of the extension |
| `highlight` | A background over the range, or over whole lines when no columns are given |

| Field | Rules |
|---|---|
| `line` | Required, 1-based. A decoration without it is dropped |
| `endLine` | The last line of a range; default `line` |
| `from`, `to` | Columns in UTF-16 code units (as in LSP) from the start of `line` and of `endLine`. Without them the decoration is about the whole line(s) |
| `severity` | `info` (default), `ok`, `warning` or `error`: the color, taken from the theme. An extension can't set a color |
| `message`, `hover` | Plain text for the hover, ≤ 2,000 characters; line breaks are kept. Both names work on every kind |
| `id` | ≤ 256 characters; sent back as `decoration` in `editor/action` |
| `actions` | ≤ 10 quick fixes, each `{ "id" (≤ 64), "label" (≤ 60), "command"? }`. Without `command`, choosing it sends [`editor/action`](#editoraction); with one, Flint runs that command of the extension (`command/execute` with its `args`, ≤ 16 KiB) |

How Flint treats a publish:

- **Which files.** Only a document the extension was told about with `editor/opened` and not yet `editor/closed`, for a set in that notification's `sets`. Flint announces a file only when it is inside a trusted project folder (symbolic links resolved), not inside `.git` and not a credential file (the list under `workspace/readFile`): exactly the files `workspace/readFile` would return. A publish for any other file, for a set that is off or for a set of another extension is dropped, with a line in the log.
- **Versions.** Give the `version` the decorations were computed for. A publish for a version older than the editor's is dropped, so a slow extension never paints marks on text that has changed; the next `editor/changed` is the cue to publish again. Without `version` the publish is drawn as is.
- **Positions** beyond the document are clamped to it: a line past the end lands on the last line, a column past the end of the line at its end.
- **Edits.** Until the next publish, decorations move with the user's edits; a decoration whose text is deleted goes.
- **Limits.** 5,000 decorations per set and document (more are cut), 4 MiB per message as everywhere. Publishes for one editor are applied together, at most every 50 ms.
- **Removal.** `editor/clearDecorations { set, path? }` removes a set's decorations from one document or from all. Flint removes them itself when the user turns the set or the extension off, when the extension is uninstalled, and when its process stops or crashes (it is started again while a document it decorates is open, and gets `editor/opened` for each).
- **Quick fixes** appear as buttons in the decoration's hover, in a list when the gutter marker is clicked, and with **Show Extension Quick Fixes** (`⌥⇧↵` / `Alt+Shift+Enter`) for the cursor's line.

## Versioning

- `contract` is a major version. Flint supports one major version at a time and says so when a manifest asks for another.
- Within a major version, changes are additive only: new methods, new optional fields, new view kinds, new contribution points, announced in `initialize`'s `capabilities`. A side that doesn't know a method answers `-32601`, and callers must cope with that. Unknown fields are ignored.
