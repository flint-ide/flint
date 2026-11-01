# Benchmark: startup and memory

The numbers in the README come from `cmd/bench`. This page says what they measure, how they were taken and how to repeat them.

## Results

Measured on 2026-11-01 on an Apple M4 Pro with 24 GB of memory, macOS 27.0.1. The project is the Go standard library source (`$(go env GOROOT)/src`, 12,053 files). Each editor ran once to warm up and then seven times (`-runs 7`); the table shows the medians.

| Editor | Version | First window | Memory after 30 s idle |
|---|---|---|---|
| Flint | build of `main` | 0.44 s | 135 MB |
| Visual Studio Code | 1.115.0 | 0.56 s | 599 MB |
| Zed | 1.27.0 (Preview) | 0.34 s | 160 MB |

Flint before it showed its window early (see [Startup](ARCHITECTURE.md#startup)), measured in the same session: 0.56 s and 136 MB. Zed's memory varied most between runs (134 to 169 MB).

Cursor wasn't installed on that machine; the program measures it too when it is.

## What is measured

- **First window:** the time from starting the editor's binary until it has an on-screen window of a real size (at least 300 × 200 points). The program polls the window list through CoreGraphics, which needs no Screen Recording permission: the main process every 2 ms, its children every 100 ms (listing them runs `ps`). This is when a window appears, not when everything in it has finished loading: VS Code keeps loading its workbench after the window shows, and Flint's window shows only its title bar and background until its page has loaded.
- **Memory:** the physical footprint, what Activity Monitor shows as Memory, read with `footprint` for each process 30 seconds after the window appeared, summed over all of the editor's processes (a helper that exits meanwhile is skipped):
  - the process the program started and all its descendants (VS Code's helpers, language servers);
  - for Flint, also WebKit's processes (web content, networking, GPU) that appeared during the run. A WKWebView renders in XPC services that launchd starts, so they are not Flint's children, but they are part of what Flint costs.

## How

- Every run gets a new, empty profile directory, so the user's extensions, settings, caches and restored windows play no part:
  - Flint: `FLINT_CONFIG_DIR`. Its state file marks the one-time tool check as shown, because that dialog appears only on the very first launch.
  - VS Code: `--user-data-dir` and `--extensions-dir`.
  - Zed: `--user-data-dir`.
- The editor opens the project folder. No file is opened and nothing is typed, so this is the cost of an idle editor on a large project: indexing, file watching and, for VS Code, its built-in extensions.
- After the measurement the editor's process group, and every process found in its tree, gets SIGTERM, then SIGKILL after 10 seconds.

## Repeat it

Build Flint, quit the editors you will measure, then:

```sh
make build
go run ./cmd/bench -flint build/bin/flint.app
```

Flags:

- `-project <dir>`: open another project.
- `-runs N`: the number of measured runs (default 5).
- `-settle 30s`: how long to wait before reading memory.
- `-only flint,zed`: measure only some editors.

The table is printed on stdout and each run on stderr.

## Caveats

- Numbers depend on the machine, the macOS version and what else is running. Compare editors measured in the same session, not numbers from different machines.
- Another app that opens a web view during a Flint run would be counted towards Flint.
- Fresh profiles show each editor's first-launch screens (welcome pages, Restricted Mode), which is part of what a new user sees.
