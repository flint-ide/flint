# Debug a Go service

Your Go HTTP service returns the wrong result for one endpoint, and it is slow under load. This walkthrough sets up a run configuration, calls the endpoint with the HTTP client, steps through the handler with the debugger, and finds the hot spot with a CPU profile.

## Before you start

- Install Delve: `go install github.com/go-delve/delve/cmd/dlv@latest`.
- Enable macOS developer mode once: `sudo DevToolsSecurity -enable`.
- Install `gopls` for code navigation: `go install golang.org/x/tools/gopls@latest`.
- For profiling a running service, expose `net/http/pprof` on localhost, for example:

  ```go
  import _ "net/http/pprof"

  go func() { _ = http.ListenAndServe("localhost:6060", nil) }()
  ```

## 1. Create a run configuration

Flint already detects Go `main` packages, so you may find your service in the ▶ selector in the title bar. To save settings such as arguments and environment, create a configuration:

1. Run "Add Configuration…" from the command palette.
2. Set the type to **go**, the package to `./cmd/api`, and add arguments and environment variables.
3. Add the pprof endpoint as the profile URL: `http://localhost:6060/debug/pprof/profile?seconds=10`.
4. Save.

The result in `.flint/run.json` looks like this:

```json
{
  "version": 1,
  "configurations": [
    {
      "id": "api",
      "name": "API",
      "type": "go",
      "package": "./cmd/api",
      "args": ["-port", "8080"],
      "env": { "DATABASE_URL": "postgres://localhost:5432/app" },
      "profileURL": "http://localhost:6060/debug/pprof/profile?seconds=10"
    }
  ]
}
```

## 2. Run the service

1. Select **API** in the title bar and press `⌃R`.
2. The output opens in a tab. `path:line` locations in it are clickable.

`⌃R` while it runs restarts it; `⌘F2` stops it, together with any child processes.

## 3. Reproduce the bug with the HTTP client

1. Run "New HTTP Request File" from the palette, or create `api.http`.
2. Write the request:

   ```http
   ### Get order
   GET http://localhost:8080/orders/42
   Accept: application/json
   ```

3. Press `⌘↵` with the cursor in the request.

The response opens in a tab with the status, time, headers and a formatted JSON body. Keep this file; you will send the request again.

## 4. Set breakpoints

1. Press `⌘P` and open the handler.
2. Click the gutter next to the line where you want to stop, or press `⌘F8`.

Breakpoints move with your edits and are kept with the session.

## 5. Start the debugger

1. Stop the running service (`⌘F2`).
2. Click the bug button next to ▶ in the title bar, or press `⌃D`.
3. Send the request again from the `.http` file (`⌘↵`).

The debugger stops at your breakpoint. The editor highlights the current line, and the **Debug** tab shows:

- the stack frames;
- the variables, which you can expand;
- a console with the program output, where you can evaluate expressions.

## 6. Step through the code

| Key | Action |
|---|---|
| `F8` | Step over |
| `F7` | Step into |
| `⇧F8` | Step out |
| `F9` | Resume |
| `⌘F2` | Stop |

Use `⌘B` or `⌘`-click to jump to a definition, and `⌥F7` to find usages, while you are stopped.

> **Tip:** To debug a single test instead of the whole service, `⇧`-click the ▶ next to the test in the gutter.

## 7. Fix and verify

1. Fix the code, or select the faulty part, press `⌘L` and ask the agent.
2. Run the related tests from the gutter.
3. Restart the service with `⌃R` and send the request again. Compare the new response with the previous one.

## 8. Profile the slow endpoint

1. With the service running, run "Profile" from the command palette (or use the ▶ menu in the title bar).
2. While Flint records (10 seconds with the URL above), send the slow request a few times from the `.http` file.
3. A profile tab opens with a flame graph of CPU time.

Read the flame graph:

- Hover a frame for its function, `file:line`, and flat and cumulative time.
- Click a frame to zoom in; `Esc` resets the zoom.
- `⌘`-click or double-click to open the source.
- Search for a function name to highlight it and see its share.
- Switch to **Top** for a sortable table of the most expensive functions.

> **Tip:** If the slow path is covered by a test, profiling the test is quicker: right-click the gutter ▶ next to it and choose a profile action. No pprof endpoint is needed.

## Tips

- **Notifications.** Long runs and test runs notify you when they finish while you are in another app.
- **Raw profile.** **Raw .pprof** in the profile tab copies the path or reveals the file, for use with `go tool pprof`.
