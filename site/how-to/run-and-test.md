# Run and test

Recipes for run configurations and tests. For all options, see [Run, debug and test](../guides/run-debug-test.md).

## Create a run configuration

1. Run "Add Configuration…" from the command palette (`⇧⌘P`).
2. Choose a type: `command`, `go`, `npm` or `make`.
3. Fill in the fields for that type (for example the package for `go`, or the script for `npm`), plus the working directory, environment variables and an optional "before launch" step.
4. Save.

Flint writes the configuration to `.flint/run.json` in the project. Commit that file to share it with your team.

> **Tip:** Even without a `run.json`, Flint offers detected configurations for Go `main` packages, `Makefile` targets and `package.json` scripts. Editing one saves it to the file.

## Edit a run configuration

1. Select it in the title bar selector.
2. Run "Edit Configurations…" from the palette.

You can also edit `.flint/run.json` directly; Flint reports invalid fields.

## Run, rerun and stop

1. Pick the configuration in the title bar selector.
2. Click ▶ **Run**, or press `⌃R`. Pressing `⌃R` again while it runs restarts it.
3. Click ■ **Stop**, or press `⌘F2`, to stop it and its child processes.

## Run a test from the gutter

1. Open a test file (Go `_test.go`, or `*.test.ts(x)` / `*.spec.*`).
2. Click ▶ in the gutter next to a test, subtest, `describe`, `it` or `test`.

To run all tests in the file, click the ▶ next to `package` (or on line 1 of a JavaScript file). `⌥`-click it to run the whole package. `⌃⇧R` runs the test under the cursor.

Each test then shows ✓, ✗ or ↷ next to it.

## Rerun failed tests

1. Open the **Tests** tab.
2. Click **Rerun failed tests**, or run "Rerun Failed Tests" from the palette.

You can also rerun a single test from its row in the tree.

## Debug a test (Go)

1. Set a breakpoint: click the gutter of a `.go` file, or press `⌘F8`.
2. `⇧`-click the ▶ next to the test.
3. When the debugger stops, use `F8` (step over), `F7` (step into), `⇧F8` (step out) and `F9` (resume).
4. Press `⌘F2` to stop.

To debug every test in the file, run "Debug Tests in File" from the palette. Debugging needs `dlv` and macOS developer mode; see [Debugger](../guides/run-debug-test.md#debugger-go).

## Profile tests (Go)

1. Right-click a gutter ▶, or open the ▶ menu in the title bar.
2. Choose "Profile Tests in File" or "Profile Package Tests".

A flame graph opens when the tests finish.
