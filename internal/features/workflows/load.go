package workflows

import (
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"

	"flint/internal/prompts"
)

// The workflows shipped with Flint. A personal or project workflow with the
// same name replaces one.
//
//go:embed builtin/*.md
var builtinFS embed.FS

// ProjectDir is where a repository keeps its workflows.
func ProjectDir(root string) string { return filepath.Join(root, ".flint", "workflows") }

// PersonalDir is where the user keeps their own workflows.
func PersonalDir(configDir string) string { return filepath.Join(configDir, "workflows") }

// Builtins parses the shipped workflows; problems would be a bug in Flint.
func Builtins() (list []Workflow, problems []string) {
	files, _ := fs.Glob(builtinFS, "builtin/*.md")
	for _, f := range files {
		data, err := builtinFS.ReadFile(f)
		if err == nil {
			name := strings.TrimSuffix(strings.TrimPrefix(f, "builtin/"), ".md")
			var w Workflow
			if w, err = Parse(name, data); err == nil {
				w.Source = SourceBuiltin
				list = append(list, w)
				continue
			}
		}
		problems = append(problems, fmt.Sprintf("built-in %s: %v", f, err))
	}
	return list, problems
}

// LoadDir reads the workflows of one folder. within, when set, is the
// project root the folder and every file must stay inside (prompts.ReadDir
// checks symlinks and sizes). A file that is wrong is a problem
// ("name.md: reason") and is not in list.
func LoadDir(dir, within string, source Source) (list []Workflow, problems []string) {
	files, problems := prompts.ReadDir(dir, within, MaxWorkflows)
	for _, f := range files {
		err := f.Err
		if err == nil {
			var w Workflow
			if w, err = Parse(f.Name, f.Data); err == nil {
				w.Source, w.Path = source, f.Path
				list = append(list, w)
				continue
			}
		}
		problems = append(problems, fmt.Sprintf("%s.md: %v", f.Name, err))
	}
	return list, problems
}

// Merge lays the workflows over each other by name: project over personal
// over built-in. The result is sorted by name.
func Merge(builtin, personal, project []Workflow) []Workflow {
	byName := map[string]Workflow{}
	for _, list := range [][]Workflow{builtin, personal, project} {
		for _, w := range list {
			byName[w.Name] = w
		}
	}
	out := make([]Workflow, 0, len(byName))
	for _, w := range byName {
		out = append(out, w)
	}
	slices.SortFunc(out, func(a, b Workflow) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// Find returns the lookup of a list by name, as Resolve wants it.
func Find(list []Workflow) func(name string) (Workflow, bool) {
	return func(name string) (Workflow, bool) {
		i := slices.IndexFunc(list, func(w Workflow) bool { return w.Name == name })
		if i < 0 {
			return Workflow{}, false
		}
		return list[i], true
	}
}
