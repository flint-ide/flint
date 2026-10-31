# Run, debug and test

This guide covers run configurations, running tests from the gutter, the Go debugger and Go profiling with a flame graph.

## Run configurations

A run configuration describes how to start something in your project: a server, a script, a build. Configurations live in `.flint/run.json` in the project. The file is meant to be committed, like `.idea/runConfigurations` or `.vscode/launch.json`.

### Types

| Type | Runs | Key fields |
|---|---|---|
| `command` | A program with arguments, or a whole shell line when there are no arguments | `command`, `args` |
| `go` | Builds a Go package or file, then runs the binary | `package`, `buildFlags`, `args` |
| `npm` | A `package.json` script, with the package manager chosen from your lockfile | `script`, `args` |
| `make` | A Makefile target (empty means the default target) | `target` |

Every type also accepts:

- `cwd`: the working directory, relative to the project root;
- `env`: environment variables;
- `before`: a shell line run first (for example a build); the run stops if it fails;
- `profileURL`: a pprof endpoint for [profiling](#profiling-go-programs);
- `debug`: debugger options for `go` configurations (`adapter`, `stopOnEntry`, `args`).

### Example

```json
{
  "version": 1,
  "configurations": [
    {
      "id": "api",
      "name": "API server",
      "type": "go",
      "package": "./cmd/api",
      "args": ["--port", "8080"],
      "env": { "LOG_LEVEL": "debug" },
      "profileURL": "http://localhost:6060/debug/pprof/profile?seconds=10"
    },
    { "id": "web", "name": "Web dev server", "type": "npm", "script": "dev", "cwd": "web" },
    { "id": "lint", "name": "Lint", "type": "make", "target": "lint" },
    { "id": "seed", "name": "Seed database", "type": "command", "command": "./scripts/seed.sh --reset", "before": "make build" }
  ]
}
```

Flint validates the file and reports unknown fields or wrong types as errors. You don't have to write it by hand: "Add Configuration…" and "Edit Configurations…" in the command palette open an editor for it.

### Detected configurations

Without a `run.json`, Flint detects Go `main` packages, `Makefile` targets and `package.json` scripts on its own. They are written to `run.json` only after you edit one.

### Running

The selector in the title bar shows the configurations of the active tab's project. Next to it:

- **▶ Run** (`⌃R`) starts the selected configuration, or reruns it if it is already running;
- **↻ Rerun** starts it again;
- **■ Stop** (`⌘F2`) stops it.

Output appears in a tab, and `path:line` locations in it are clickable. The process runs in its own process group, so Stop also ends child processes (for example the program started by `go run` or `npm run`): Flint sends SIGINT, then SIGKILL after 3 seconds.

With several projects in the window, each project remembers its own selected configuration.

## Tests

### Running tests from the gutter

A ▶ appears in the gutter next to:

- Go: `func TestXxx` and `t.Run("…")` subtests;
- JavaScript and TypeScript: `describe`, `it` and `test` in `*.test.ts(x)` and `*.spec.*` files.

Click ▶ to run that test. The ▶ next to `package` (or on line 1 of a JavaScript file) runs the whole file; `⌥`-click it to run the whole package. `⌃⇧R` runs the test under the cursor.

After a run, each test shows ✓ (passed), ✗ (failed) or ↷ (skipped) next to it.

### The Tests tab

Results open in the **Tests** tab: a tree of packages and tests with status and duration, failure output with clickable locations, **Rerun failed tests**, rerun of a single test, and **Fix with agent**, which sends the failures to the agent chat.

Command palette entries: "Run Tests in File", "Run Package Tests", "Rerun Failed Tests" and "Fix Failed Tests with Agent".

### Supported runners

| Language | How tests run | Per-test results |
|---|---|---|
| Go | `go test -json` | Yes |
| JavaScript / TypeScript | `node --test` (also through the repository's `test` script) | Yes |
| Other runners (jest, vitest) | Through the `test` script | Exit code only |

## Debugger (Go)

Flint debugs Go programs and tests with Delve.

### Requirements

- `dlv`: `go install github.com/go-delve/delve/cmd/dlv@latest`
- macOS developer mode: `sudo DevToolsSecurity -enable`

### Starting a debug session

- The bug button next to ▶ in the title bar, or `⌃D`, debugs the selected `go` configuration.
- `⇧`-click the gutter ▶ next to a test to debug that test.
- "Debug Tests in File" in the palette, or **Debug Tests** in the file tree menu.

### Breakpoints

Click the gutter of a `.go` file, or press `⌘F8`, to toggle a breakpoint on the current line. Breakpoints move with your edits and are saved with the session. "Remove All Breakpoints" clears them.

### When the program stops

The editor highlights the current line. The **Debug** tab shows the stack frames, the variables (expanded on demand) and a console with the program output, where you can also evaluate expressions.

| Shortcut | Action |
|---|---|
| `F9` | Resume |
| `F8` | Step over |
| `F7` | Step into |
| `⇧F8` | Step out |
| `⌘F2` | Stop |

"Pause Program" in the palette pauses a running program.

## Profiling Go programs

### Profiling tests

This is the easiest path. Choose "Profile Tests in File" or "Profile Package Tests" from the ▶ menu in the title bar, the palette, the file tree menu (**Profile Tests**), or by right-clicking a gutter ▶ (also for a single test). Flint runs the tests with CPU and memory profiling in the Tests tab, then opens a profile tab.

### Profiling a running program

Go programs don't profile themselves, so a configuration needs a `profileURL` that points at the program's `net/http/pprof` endpoint, for example `http://localhost:6060/debug/pprof/profile?seconds=10`. Without `seconds`, Flint records for 10 seconds. Only localhost URLs are allowed.

Choose "Profile" in the palette. Flint starts the configuration if it isn't running, waits until it is listening, and fetches the profile. Without `profileURL`, it opens the configuration editor instead. "Cancel Profiling" stops a recording.

You can also open any `.pprof` file with "Open Profile File…".

### Reading the flame graph

The profile tab shows CPU and memory (allocated space by default; you can switch the sample type).

- Hover a frame for the function, `file:line`, flat and cumulative values and percentage.
- Click a frame to zoom in; `Esc` or **Reset Zoom** goes back.
- `⌘`-click or double-click a frame to open its source.
- Search highlights matching functions and shows their share.
- The **Top** view is a sortable table of functions (flat, cum, %).
- **Raw .pprof** copies the profile's path or reveals it in Finder, for example to use with `go tool pprof`.

## Notifications

When a run or test run finishes while Flint isn't the active app, you get a macOS notification and a badge on the Dock icon. Turn this off in Settings ▸ Agents.

## Related

- [Run and test](../how-to/run-and-test.md)
- [Debug a Go service](../use-cases/debug-a-go-service.md)
