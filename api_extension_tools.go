package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"flint/internal/dap"
	"flint/internal/extensions/vars"
	"flint/internal/lsp"
	"flint/internal/terminal"
	"flint/internal/testrun"
)

// Debuggers and test runners from extensions (manifest contributes.debuggers
// and testRunners). They are programs Flint doesn't know that run the
// project's code, so, like an extension's language server, they start only
// in trusted folders. The messages here reach the user as they are: each
// names the file and says what to do next.

// maxLaunchDepth bounds the nesting of a launch configuration (the manifest
// refuses deeper ones already).
const maxLaunchDepth = 8

// trustHint is how to leave Restricted Mode, in the UI's words.
const trustHint = "Run Trust Folder from the command palette (or click Trust Folder in the Restricted Mode banner)"

// fileLanguage is the language of a file with a debugger or test runner
// (verb: "debug", "run the tests of"), or a message saying none is known.
func fileLanguage(verb, path string) (language, label string, err error) {
	language, label, ok := lsp.LanguageOf(path)
	if !ok {
		return "", "", fmt.Errorf("Can't %s %s: Flint doesn't know its language. An extension can add it: see Settings ▸ Extensions ▸ Repository.",
			verb, filepath.Base(path))
	}
	return language, label, nil
}

// fileInRoot checks the project folder and that path lies inside it.
func fileInRoot(root, path string) error {
	if err := checkRoot(root); err != nil {
		return err
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("file %q is not absolute", path)
	}
	if rel, err := filepath.Rel(root, path); err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
		return fmt.Errorf("%s is not inside the project folder %s", filepath.Base(path), root)
	}
	return nil
}

// extensionAdapter picks the extension debugger for path in root and fills
// in its adapter and launch arguments.
func (a *App) extensionAdapter(root, path string) (dap.Adapter, json.RawMessage, string, error) {
	if err := fileInRoot(root, path); err != nil {
		return dap.Adapter{}, nil, "", err
	}
	name := filepath.Base(path)
	language, label, err := fileLanguage("debug", path)
	if err != nil {
		return dap.Adapter{}, nil, "", err
	}
	if language == "go" {
		return dap.Adapter{}, nil, "", fmt.Errorf("Flint debugs Go itself: to debug %s, pick a Go run configuration in the toolbar and click Debug, or Shift-click a test's marker in the editor gutter.", name)
	}
	d, ok := dap.ExtensionDebuggerFor(language)
	if !ok {
		return dap.Adapter{}, nil, "", fmt.Errorf("Can't debug %s: no extension that debugs %s files is installed and on. Add one from Settings ▸ Extensions ▸ Repository, or turn it on in Settings ▸ Extensions.", name, label)
	}
	if !a.isTrusted(root) {
		return dap.Adapter{}, nil, "", fmt.Errorf("Debugging %s is off in Restricted Mode, because %s (from the extension %s) runs this project's code. %s, then try again.", name, d.Label, d.Source, trustHint)
	}
	argv, err := firstInstalled(d.Candidates)
	if err != nil {
		return dap.Adapter{}, nil, "", fmt.Errorf("Can't debug %s: %s", name, missingProgram(d.Label, d.Candidates, d.Install))
	}
	v := vars.ForFile(root, path)
	launch := json.RawMessage("{}")
	if len(d.Launch) > 0 {
		if launch, err = vars.ExpandJSON(d.Launch, v, maxLaunchDepth); err != nil {
			return dap.Adapter{}, nil, "", fmt.Errorf("Can't debug %s: the launch settings of %s (from the extension %s) are broken (%v). Update or reinstall the extension.", name, d.Label, d.Source, err)
		}
	}
	argv = append(argv[:1:1], vars.ExpandArgs(argv[1:], v, nil)...)
	return dap.Adapter{Argv: argv, Env: terminal.Environ(), Dir: root, ID: d.Type}, launch, d.Label, nil
}

// firstInstalled is the first command line whose program exists (an
// absolute path inside the extension, or a name on PATH), program resolved.
func firstInstalled(candidates [][]string) ([]string, error) {
	for _, c := range candidates {
		if p, err := exec.LookPath(c[0]); err == nil {
			return append([]string{p}, c[1:]...), nil
		}
	}
	return nil, exec.ErrNotFound
}

// missingProgram says which program to install, and how.
func missingProgram(label string, candidates [][]string, install string) string {
	names := make([]string, 0, len(candidates))
	for _, c := range candidates {
		names = append(names, filepath.Base(c[0]))
	}
	what := "its program"
	if len(names) > 0 {
		what = strings.Join(names, " or ")
	}
	how := "Install it"
	if install != "" {
		how = "Install it with `" + install + "`"
	}
	return fmt.Sprintf("%s needs %s, which isn't installed or isn't on your PATH. %s, then try again.", label, what, how)
}

// planExtensionTests plans a testrun.LangExt request: the extension test
// runner for the file's language, in a trusted project folder.
func (a *App) planExtensionTests(req testrun.Request) (testrun.Plan, error) {
	if len(req.Dirs) == 0 || req.File == "" {
		return testrun.Plan{}, fmt.Errorf("no project folder or file given")
	}
	root, path := req.Dirs[0], req.File
	if err := fileInRoot(root, path); err != nil {
		return testrun.Plan{}, err
	}
	name := filepath.Base(path)
	language, label, err := fileLanguage("run the tests of", path)
	if err != nil {
		return testrun.Plan{}, err
	}
	r, ok := testrun.ExtensionRunnerFor(language)
	if !ok {
		return testrun.Plan{}, fmt.Errorf("Can't run the tests of %s: no extension that runs %s tests is installed and on. Add one from Settings ▸ Extensions ▸ Repository, or turn it on in Settings ▸ Extensions.", name, label)
	}
	if !a.isTrusted(root) {
		return testrun.Plan{}, fmt.Errorf("Running the tests of %s is off in Restricted Mode, because %s (from the extension %s) runs this project's code. %s, then try again.", name, r.Label, r.Source, trustHint)
	}
	return testrun.PlanExtension(req, r)
}
