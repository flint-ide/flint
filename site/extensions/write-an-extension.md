# Write an extension

This guide builds a small extension step by step: a command, a sidebar panel, a setting, a status bar item and a tool for agents. Every message is described in the [protocol reference](protocol.md).

An extension is a folder with two things:

- `flint-extension.json`, the **manifest**: who the extension is, the program Flint runs, what it adds to Flint and which permissions it needs.
- **A program** in any language. Flint starts it when it is first needed and talks to it with [JSON-RPC 2.0](https://www.jsonrpc.org/specification), one JSON message per line, over the program's standard input and output. Whatever the program writes to standard error goes to its log in **Settings ▸ Extensions**.

A complete example in Go, using only the standard library, is in Flint's repository: [`examples/extensions/stats`](https://github.com/flint-ide/flint/tree/main/examples/extensions/stats). It counts the files of the open projects by type.

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

After you change the program, reinstall the folder (Flint replaces the old copy), or edit the installed copy and click **Reload**. Five idle minutes after its last request, Flint stops the program; the next use starts it again.

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

The agent sees it as `hello_greeting` (your id's name part, then the tool's name). A call arrives as `agentTool/call` with the tool's `name`, the agent's project folder (`root`) and the `arguments`; answer with `{ "text": "…" }`. Tools are read-only: return information, don't change things.

## 8. Ask Flint for more

Requests your program can send to Flint, each needing a permission declared in the manifest's `permissions`:

| Request | Permission | What it does |
|---|---|---|
| `workspace/readFile` | `workspace.read` | Reads a file in a trusted project folder |
| `window/openFile` | `workspace.read` | Opens a file in Flint's editor |
| `secrets/get`, `secrets/set`, `secrets/delete` | `secrets` | Keeps a token or password in the system keychain |
| `agent/ask` | `agent.ask` | Writes a draft in the agent chat for the user to send |
| `window/openUrl` | none | Opens an `http(s)` link in the browser (Flint asks the user the first time per site) |

A request without its permission fails with error `-32001`. The user sees the permissions when installing, so ask only for what you need. If your program talks to the network, declare `network:<host>` so users know where; Flint shows it but can't enforce it.

## 9. A color theme

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

## 10. A programming language

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

## Checklist before you share an extension

- The manifest has `contract`, a `publisher.name` id, `name` and a `major.minor.patch` version.
- The program starts in under 10 seconds and answers `initialize`; `view/resolve` answers in under 5 seconds; other requests in under 30 (agent tools: 120).
- Nothing but protocol messages on standard output; one message is at most 4 MiB.
- The program exits when its standard input closes.
- Build the program for both macOS and Linux if you can, using `run.darwin` and `run.linux` for separate binaries.
