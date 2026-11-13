# Write an extension

This guide builds a small extension step by step: a command, a sidebar panel, a setting, a status bar item and a tool for agents. Every message is described in the [protocol reference](protocol.md).

An extension is a folder with two things:

- `flint-extension.json`, the **manifest**: who the extension is, the program Flint runs, what it adds to Flint and which permissions it needs.
- **A program** in any language. Flint starts it when it is first needed and talks to it with [JSON-RPC 2.0](https://www.jsonrpc.org/specification), one JSON message per line, over the program's standard input and output. Whatever the program writes to standard error goes to its log in **Settings ▸ Extensions**.

A complete example in Go, using only the standard library, is in Flint's repository: [`examples/extensions/stats`](https://github.com/flint-ide/flint/tree/main/examples/extensions/stats). It counts the files of the open projects by type. [`examples/extensions/todos`](https://github.com/flint-ide/flint/tree/main/examples/extensions/todos) marks TODO lines in the editor, with a quick fix.

## 1. The manifest

Make a folder `hello` with this `flint-extension.json`:

```json
{
  "contract": 1,
  "id": "me.hello",
  "name": "Hello",
  "version": "0.1.0",
  "publisher": "Me",
  "description": "Says hello.",
  "run": { "command": "hello.py" },
  "contributes": {
    "commands": [{ "id": "me.hello.greet", "title": "Say Hello", "category": "Hello" }],
    "panels": [{ "id": "me.hello.list", "title": "Hello", "icon": "files", "order": 10, "view": "list" }]
  }
}
```

- `contract` is the version of the extension contract; this Flint speaks `1`.
- `id` is `publisher.name` in lower-case letters, digits and dashes. Every id the extension contributes starts with it (`me.hello.greet`), so it can't collide with Flint's own or another extension's.
- `run.command` is a path inside the folder. Flint runs it directly, never through a shell or `PATH`, so a script needs a `#!` line and the executable bit.

## 2. The program

`hello.py` (make it executable with `chmod +x hello.py`):

```python
#!/usr/bin/env python3
import json, sys

def send(msg):
    sys.stdout.write(json.dumps(msg) + "\n")
    sys.stdout.flush()

def reply(id, result):
    send({"jsonrpc": "2.0", "id": id, "result": result})

for line in sys.stdin:
    msg = json.loads(line)
    method, id = msg.get("method"), msg.get("id")
    if method == "initialize":
        reply(id, {"contract": 1, "capabilities": {}})
    elif method == "view/resolve":
        reply(id, {"kind": "list", "items": [{"id": "1", "label": "Hello from Python", "icon": "files"}]})
    elif method == "command/execute":
        send({"jsonrpc": "2.0", "method": "window/showMessage", "params": {"type": "info", "message": "Hello!"}})
        reply(id, None)
    elif method == "shutdown":
        reply(id, None)
    elif id is not None:
        send({"jsonrpc": "2.0", "id": id, "error": {"code": -32601, "message": "method not found"}})
```

Rules worth knowing from the start:

- **Standard output carries only protocol messages.** A stray `print` there is a protocol error and Flint stops the process. Log to standard error.
- Answer every request (a message with an `id`), with a `result` or an `error`. Answer methods you don't know with error `-32601`.
- After `shutdown`, Flint closes your standard input; exit when it does.

## 3. Install and try it

Run **Install Extension from Folder…** from the command palette and pick the `hello` folder. Flint shows the confirmation and copies the folder to its extensions folder. Then:

- **Say Hello** is in the command palette (under *Hello*). Running it starts the program, sends `initialize`, then `command/execute`, and shows the toast.
- The **Hello** panel is on the activity bar. Opening it sends `view/resolve`.
- **Settings ▸ Extensions ▸ Show Log** shows when Flint started the program and everything it wrote to standard error.

After you change the program, reinstall the folder (Flint replaces the old copy), or edit the installed copy and click **Reload Installed** in Settings ▸ Extensions. Five idle minutes after its last request, Flint stops the program; the next use starts it again.

## 4. A tree, with actions

Change the panel's `view` to `tree` and return items with `hasChildren`. Flint asks for a node's children with `view/resolve` and `parent` when the user expands it:

```json
{ "kind": "tree", "items": [
  { "id": "greetings", "label": "Greetings", "hasChildren": true },
  { "id": "readme", "label": "README.md", "icon": "files",
    "command": { "id": "me.hello.open", "args": { "path": "/path/to/README.md" } },
    "actions": [{ "id": "open", "label": "Open" }] }
] }
```

- `command` runs when the item is clicked: Flint sends `command/execute` with its `args`. It must be one of your own commands.
- `actions` are the item's context menu: Flint sends `view/action` with the item and action ids.
- `icon` is a name from Flint's icon set (`files`, `search`, `branch`, `database`, `checkbox`, `terminal`, `sum`, `puzzle` and a few others); an unknown name shows no icon.
- When the content changes, send the `view/refresh` notification and Flint asks again.

## 5. A setting

Add a settings schema to `contributes`:

```json
"settings": {
  "title": "Hello",
  "properties": {
    "name": { "type": "string", "default": "world", "maxLength": 40, "description": "Who to greet." },
    "times": { "type": "integer", "minimum": 1, "maximum": 5, "default": 1 }
  }
}
```

Flint adds a **Hello** page to Settings with a field for each property, checks values before saving them, and keeps them in `settings.json` under `extensions` → `me.hello`. Your program receives them in `initialize` (`settings`, with every default filled in) and again in the `settings/changed` notification whenever the user changes one.

## 6. A status bar item

Declare it, then set its text whenever you like:

```json
"statusItems": [{ "id": "me.hello.count", "order": 10 }]
```

```json
{ "jsonrpc": "2.0", "method": "status/update",
  "params": { "id": "me.hello.count", "text": "3 greetings", "tooltip": "Click to greet", "command": "me.hello.greet" } }
```

The item is hidden until its first `status/update`. Text is cut at 40 characters.

## 7. A tool for agents

Agents that Flint starts (Claude Code, Codex, custom ACP agents) can call your tool through Flint's MCP server:

```json
"agentTools": [{
  "name": "greeting",
  "title": "Greeting",
  "description": "A greeting for a person.",
  "inputSchema": { "type": "object", "properties": { "who": { "type": "string" } } }
}]
```

The agent sees it as `hello_greeting` (your id's name part, then the tool's name). A call arrives as `agentTool/call` with the tool's `name`, the agent's project folder (`root`) and the `arguments`; answer with `{ "text": "…" }`. Tools are read-only by default: return information, don't change things. A tool that does change something declares `"acts": true`, and Flint asks the user before each call.

## 8. Ask Flint for more

Requests your program can send to Flint, each needing a permission declared in the manifest's `permissions`:

| Request | Permission | What it does |
|---|---|---|
| `workspace/readFile` | `workspace.read` | Reads a file in a trusted project folder |
| `window/openFile` | `workspace.read` | Opens a file in Flint's editor |
| `secrets/get`, `secrets/set`, `secrets/delete` | `secrets` | Keeps a token or password in the system keychain |
| `agent/ask` | `agent.ask` | Writes a draft in the agent chat for the user to send |
| `window/openUrl` | none | Opens an `http(s)` link in the browser (Flint asks the user the first time per site) |
| `window/openTerminal` | `terminal` | Opens a terminal tab in a trusted project folder that runs a command you give |

Your program also gets a private folder for its own data in `FLINT_EXTENSION_DATA`, and the variables of the user's environment it names in the manifest's `run.env` (for example `["KUBECONFIG"]`). For a panel or tab drawn by your own HTML, see [Webviews](protocol.md#webviews) and the [`notes`](https://github.com/flint-ide/flint/tree/main/examples/extensions/notes) example.

A request without its permission fails with error `-32001`. The user sees the permissions when installing, so ask only for what you need. If your program talks to the network, declare `network:<host>` so users know where; Flint shows it but can't enforce it.

## 9. Marks in the editor

A linter, a coverage tool or an issue tracker marks lines in open files. Declare a **decoration set** and the files it is for; it needs the `workspace.read` permission:

```json
"permissions": ["workspace.read"],
"contributes": {
  "decorationSets": [{ "id": "me.hello.todos", "title": "TODO Marks", "globs": ["*"], "unsavedText": true }]
}
```

`languages` (`go`, `typescript`, `python`, `rust`, or an extension's language), `extensions` (`.md`) and `globs` (patterns for the file's name) say which files; `"default": false` leaves the set off until the user turns it on in Settings ▸ Extensions.

When a matching file in a trusted folder is opened in an editor, Flint starts your program and sends `editor/opened`, then `editor/changed` (at most every 300 ms while the user types) and `editor/closed`:

```json
{ "jsonrpc": "2.0", "method": "editor/opened",
  "params": { "path": "/path/to/main.go", "language": "go", "version": 1, "sets": ["me.hello.todos"], "text": "…" } }
```

`text` is there because the set says `unsavedText` (the install dialog tells the user); without it, read the saved file with `workspace/readFile`. Answer with the set's decorations for that version of the document:

```json
{ "jsonrpc": "2.0", "method": "editor/setDecorations", "params": {
  "set": "me.hello.todos", "path": "/path/to/main.go", "version": 1, "decorations": [
    { "kind": "diagnostic", "line": 3, "from": 3, "to": 7, "severity": "info", "message": "To do: tidy up",
      "actions": [{ "id": "remove", "label": "Remove TODO marker" }] }
  ] } }
```

- Kinds are `diagnostic` (underline, gutter marker, message on hover), `gutter` (a marker with a hover), `inline` (dim text after the line, clickable with a `command`) and `highlight` (a background). Colors come from `severity` (`info`, `ok`, `warning`, `error`).
- Lines are 1-based; columns count UTF-16 code units, as in LSP.
- Always send the `version` you computed for: Flint drops a publish for a version the editor has moved past, and moves what is shown with the user's edits until your next publish.
- When the user picks an action, Flint sends the `editor/action` request with the set, path, version, action id and the decoration's current place. Answer `{ "edits": [{ "line": 3, "from": 3, "to": 9, "text": "" }] }`; Flint applies the edits as one undo step, and only if the document is still at that version. An action with a `command` runs that command instead.
- While a document you decorate is open your program is not stopped for being idle. If it stops or crashes, its decorations are removed.

The complete example is [`todos`](https://github.com/flint-ide/flint/tree/main/examples/extensions/todos); every field and limit is in the [protocol reference](protocol.md#editor-decorations).

## 10. A color theme

A theme is data, so it needs no program: an extension with only `themes` (and settings) leaves out `run` and never runs code.

```json
"themes": [{
  "id": "me.hello.dusk",
  "label": "Dusk",
  "base": "dark",
  "colors": { "--bg-editor": "#1e1f29", "--fg": "#e3e3ea", "--accent": "#ff9e64", "--syn-keyword": "#bb9af7" },
  "terminal": { "background": "#1e1f29", "red": "#f7768e", "green": "#9ece6a", "brightBlue": "#7aa2f7" }
}]
```

`colors` takes the CSS variables of a custom theme and `terminal` the terminal colors (`background`, `foreground`, `cursor`, `cursorAccent`, `selectionBackground`, the 16 ANSI colors); see [customTheme](../reference/settings.md#customtheme) for the full lists. Values are hex colors. Any other name or value refuses the whole manifest, with the reason shown in Settings ▸ Extensions. The user picks a theme under Settings ▸ Appearance ▸ Color theme, or with **Use …** on your extension; Flint copies it into their custom theme.

## 11. A programming language

An extension can teach Flint a language it doesn't know: which files are in it, its comments, its language server and its syntax highlighting. Like a theme this is data, so a language-only extension needs no program of its own.

```json
"languages": [{
  "id": "ruby",
  "label": "Ruby",
  "extensions": [".rb", ".rake"],
  "filenames": ["Gemfile", "Rakefile"],
  "lineComment": "#",
  "blockComment": ["=begin", "=end"],
  "server": {
    "candidates": [["ruby-lsp"], ["solargraph", "stdio"]],
    "install": "gem install ruby-lsp"
  },
  "grammar": { "states": {
    "start": [
      { "regex": "#.*", "token": "comment" },
      { "regex": "\"", "token": "string", "push": "string" },
      { "regex": "\\b(?:def|end|class|module|if|elsif|else|unless|do|return)\\b", "token": "keyword" },
      { "regex": "\\d+(?:\\.\\d+)?", "token": "number" }
    ],
    "string": [
      { "regex": "\\\\.", "token": "string" },
      { "regex": "\"", "token": "string", "pop": true },
      { "regex": "[^\"\\\\]+", "token": "string" }
    ]
  } }
}]
```

- **`id`** names the language (lower-case) and is the LSP `languageId` unless `languageId` says otherwise. **`extensions`** (with the dot) and **`filenames`** (exact names) pick its files.
- **`lineComment`** and **`blockComment`** drive **Toggle Comment**.
- **`server`**: Flint's own LSP client runs the first candidate it finds: completion, hover, go to definition, references and diagnostics, as for the built-in languages. A candidate's program is a name looked up on `PATH`, or a path inside your extension's folder starting with `./`. **`install`** is shown when none is installed. A server from an extension starts only in [trusted folders](../guides/restricted-mode.md), and the install confirmation lists it.
- **`grammar`**: CodeMirror's simple mode as data. States hold rules tried in order. Each rule has a `regex` (JavaScript syntax, at most 500 characters) and a `token`. It can also `push` a state, `pop` back, switch to a state with `next`, or match only at the start of a line with `sol`. Tokens: `keyword`, `atom`, `number`, `def`, `variable`, `variable-2`, `variable-3`, `type`, `builtin`, `string`, `string-2`, `comment`, `meta`, `operator`, `punctuation`, `bracket`, `tag`, `attribute`, `property`, `qualifier`, `header`, `link`, `error`, `invalidchar`. A regex that repeats a repeated group (`(a+)+`) is refused, because it can freeze the editor. One that matches an empty string, or doesn't compile, turns highlighting off for the language.

Flint's own languages (Go, TypeScript and JavaScript, Python, Rust, and the formats it highlights) always win: an extension can't take over their files.

## 12. A debugger and a test runner

An extension can debug and test files of a language: yours from section 10, or another one, such as Python. Both are data that name programs to run, so they need no program of their own either.

```json
"debuggers": [{
  "type": "ruby-rdbg",
  "label": "Ruby (rdbg)",
  "languages": ["ruby"],
  "adapter": {
    "candidates": [["rdbg", "--open", "--command"], ["./bin/adapter"]],
    "install": "gem install debug"
  },
  "launch": { "type": "rdbg", "request": "launch", "script": "${file}", "cwd": "${workspaceFolder}" }
}],
"testRunners": [{
  "id": "rspec",
  "label": "RSpec",
  "languages": ["ruby"],
  "command": ["bundle", "exec", "rspec", "${file}"],
  "format": "plain"
}]
```

- **`debuggers`**: a debug adapter that speaks the [Debug Adapter Protocol](https://microsoft.github.io/debug-adapter-protocol/) on its standard input and output. Flint runs the first of the **`adapter.candidates`** it finds (a name on `PATH`, or a `./` path inside your extension's folder, as for language servers) and shows **`install`** when none is installed. It sends `initialize` with `type` as the adapter id, then `launch` with **`launch`** as its arguments, then the breakpoints and `configurationDone`. **`launch`** is a JSON object of at most 16 KiB. Flint's debugger works as it does for Go: breakpoints, stepping, the call stack, variables and the debug console.
- **`testRunners`**: **`command`** is the program and its arguments. Flint runs it in the project folder, without a shell. **`format`** is `tap` (Flint reads the results of each test from [TAP](https://testanything.org/) on standard output) or `plain` (only the exit code counts: 0 passed).
- **Variables** in launch strings and command arguments: `${file}` (the file's absolute path), `${fileBasename}`, `${fileDirname}` and `${workspaceFolder}` (the project folder). In a command, an argument that is exactly `${tests}` becomes the names of the tests to run, one argument each, or nothing when the whole file runs (a TAP runner's results can be run again one by one). Any other `${…}` refuses the manifest.
- **`languages`** are language ids, the `id` of a language from section 10 or one of Flint's own (`python`, `rust`). Flint debugs Go and runs Go and JavaScript/TypeScript tests itself, so a debugger for `go`, or a test runner for `go` or `typescript`, is ignored. When two extensions offer one for the same language, the first by extension id is used.

The user runs them with **Debug File** and **Run Tests in File** in the command palette, the run configuration menu in the toolbar, or a file's context menu in the sidebar. Both run the project's code, so they start only in [trusted folders](../guides/restricted-mode.md), and the install confirmation lists the programs they start.

## Checklist before you share an extension

- The manifest has `contract`, a `publisher.name` id, `name` and a `major.minor.patch` version.
- The program starts in under 10 seconds and answers `initialize`; `view/resolve` answers in under 5 seconds; other requests in under 30 (agent tools: 120).
- Nothing but protocol messages on standard output; one message is at most 4 MiB.
- The program exits when its standard input closes.
- Build the program for both macOS and Linux if you can, using `run.darwin` and `run.linux` for separate binaries.
- Run the contract checker from a Flint checkout: `go run ./cmd/flint-extcheck path/to/hello`. It checks the manifest and webview files with Flint's own code, then starts the program, sends `initialize`, resolves every declarative panel and lists the agent tools. The [flint-ide/extensions](https://github.com/flint-ide/extensions) repository runs it in CI; a Go extension there is built by Flint on the user's machine when they install it from Settings ▸ Extensions.
