package testrun

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"

	"flint/internal/extensions/vars"
	"flint/internal/runconfig"
)

// LangExt is a Request for an extension's test runner: Dirs[0] is the
// project root (${workspaceFolder} and the working directory) and File the
// file whose tests run, picked by its language.
const LangExt = "ext"

// ExtensionRunner is a test runner an installed, enabled extension adds
// (manifest contributes.testRunners). Flint runs Go and JavaScript/TypeScript
// tests itself, so an extension's runner for those is left out.
type ExtensionRunner struct {
	ID    string
	Label string
	// Languages are the language ids whose files it tests.
	Languages []string
	// Command is the argument list, its program already resolved (a name for
	// PATH, or an absolute path inside the extension), variables still to
	// substitute.
	Command []string
	// Format is FormatTAP or FormatPlain.
	Format string
	// Source names the extension, for messages.
	Source string
}

// builtinTestLanguages are tested by Flint itself (go test, node --test).
var builtinTestLanguages = []string{"go", "typescript"}

var runners struct {
	sync.RWMutex
	byLanguage map[string]ExtensionRunner
}

// SetExtensionRunners replaces the extensions' test runners; between
// extensions the first one listed wins a language.
func SetExtensionRunners(list []ExtensionRunner) {
	byLanguage := map[string]ExtensionRunner{}
	for _, r := range list {
		for _, l := range r.Languages {
			if _, taken := byLanguage[l]; !taken && !slices.Contains(builtinTestLanguages, l) {
				byLanguage[l] = r
			}
		}
	}
	runners.Lock()
	runners.byLanguage = byLanguage
	runners.Unlock()
}

// ExtensionRunnerFor is the extension test runner for a language.
func ExtensionRunnerFor(language string) (ExtensionRunner, bool) {
	runners.RLock()
	defer runners.RUnlock()
	r, ok := runners.byLanguage[language]
	return r, ok
}

// lookPath finds a runner's program; injectable for tests.
var lookPath = exec.LookPath

// PlanExtension plans a LangExt request with runner r: its command with the
// file's variables and the selected tests, run in the project root.
func PlanExtension(req Request, r ExtensionRunner) (Plan, error) {
	if len(req.Dirs) == 0 || req.File == "" {
		return Plan{}, errors.New("no project folder or file given")
	}
	root, file := req.Dirs[0], req.File
	if !filepath.IsAbs(root) || !filepath.IsAbs(file) {
		return Plan{}, fmt.Errorf("path %q or %q is not absolute", root, file)
	}
	if !within(file, root) {
		return Plan{}, fmt.Errorf("%s is not inside the project folder %s", filepath.Base(file), root)
	}
	if len(r.Command) == 0 {
		return Plan{}, fmt.Errorf("%s has no command", r.Label)
	}
	if _, err := lookPath(r.Command[0]); err != nil {
		return Plan{}, fmt.Errorf("Can't run the tests of %s: %s needs the program %q, which isn't installed or isn't on your PATH. Install it, then try again.",
			filepath.Base(file), r.Label, filepath.Base(r.Command[0]))
	}
	args := vars.ExpandArgs(r.Command[1:], vars.ForFile(root, file), req.Tests)
	title := filepath.Base(file)
	switch {
	case len(req.Tests) == 1:
		title = req.Tests[0]
	case len(req.Tests) > 1:
		title = fmt.Sprintf("%d tests", len(req.Tests))
	}
	format := FormatPlain
	if r.Format == FormatTAP {
		format = FormatTAP
	}
	return Plan{
		Title:  title,
		Steps:  []runconfig.Command{runconfig.NewCommand(r.Command[0], args...)},
		Cwd:    root,
		Root:   root,
		Format: format,
	}, nil
}
