package agentdefs

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"

	"flint/internal/agent"
	"flint/internal/prompts"
)

// PersonalDir is the user's own definition folder in Flint's config directory.
func PersonalDir(configDir string) string { return filepath.Join(configDir, "agents") }

// ProjectDir is a project's definition folder.
func ProjectDir(root string) string { return filepath.Join(root, ".flint", "agents") }

// LoadDir reads the definitions of dir. Files that can't be used are skipped
// and described in problems ("path: reason"); a missing folder is no problem.
// With within set (a project root), dir and its files must stay inside it
// after resolving symlinks (prompts.ReadDir).
func LoadDir(dir, within string, source Source) (list []Definition, problems []string) {
	files, problems := prompts.ReadDir(dir, within, MaxDefinitions)
	for _, f := range files {
		d, err := parseFile(f)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", f.Path, err))
			continue
		}
		d.Source = source
		d.Path = f.Path
		list = append(list, d)
	}
	return list, problems
}

func parseFile(f prompts.File) (Definition, error) {
	if f.Err != nil {
		return Definition{}, f.Err
	}
	return Parse(f.Name, f.Data)
}

// Root is a workspace folder and whether the user trusts it.
type Root struct {
	Path    string
	Trusted bool
}

// Ignored is a folder in Restricted Mode whose definitions were not loaded.
type Ignored struct {
	// Dir is the project's definition folder.
	Dir string `json:"dir"`
	// Count is how many definition files it holds (up to MaxDefinitions).
	Count int `json:"count"`
}

// Set is the definitions of the personal folder and the workspace roots.
type Set struct {
	// Agents are sorted by name.
	Agents []Definition
	// Problems are files that were not used ("path: reason").
	Problems []string
	// Ignored lists the untrusted roots that hold definitions.
	Ignored []Ignored
}

// Load reads the personal definitions and those of every trusted root. An
// untrusted root's folder is only counted (Ignored): its text would steer an
// agent. A name that is a built-in agent or for which reserved is true (a
// custom agent's id) is refused. A personal definition wins over a project
// one with the same name, and the first root over later ones; the losers are
// reported as problems.
func Load(personalDir string, roots []Root, reserved func(name string) bool) Set {
	set := Set{Agents: []Definition{}, Problems: []string{}, Ignored: []Ignored{}}
	byName := map[string]Definition{}
	add := func(list []Definition) {
		for _, d := range list {
			switch prev, taken := byName[d.Name]; {
			case agent.Valid(agent.Kind(d.Name)):
				set.Problems = append(set.Problems, fmt.Sprintf("%s: %s is a built-in agent; choose another name", d.Path, d.Name))
			case reserved != nil && reserved(d.Name):
				set.Problems = append(set.Problems, fmt.Sprintf("%s: %s is a custom agent's id; choose another name", d.Path, d.Name))
			case taken:
				set.Problems = append(set.Problems, fmt.Sprintf("%s: not used, %s is already defined in %s", d.Path, d.Name, prev.Path))
			default:
				byName[d.Name] = d
			}
		}
	}
	list, problems := LoadDir(personalDir, "", SourcePersonal)
	set.Problems = append(set.Problems, problems...)
	add(list)
	for _, r := range roots {
		dir := ProjectDir(r.Path)
		if !r.Trusted {
			if n := prompts.CountDir(dir, r.Path, MaxDefinitions); n > 0 {
				set.Ignored = append(set.Ignored, Ignored{Dir: dir, Count: n})
			}
			continue
		}
		list, problems := LoadDir(dir, r.Path, SourceProject)
		set.Problems = append(set.Problems, problems...)
		add(list)
	}
	for _, d := range byName {
		set.Agents = append(set.Agents, d)
	}
	slices.SortFunc(set.Agents, func(a, b Definition) int { return strings.Compare(a.Name, b.Name) })
	return set
}

// Find returns the definition called name.
func (s Set) Find(name string) (Definition, bool) {
	i := slices.IndexFunc(s.Agents, func(d Definition) bool { return d.Name == name })
	if i < 0 {
		return Definition{}, false
	}
	return s.Agents[i], true
}

// template is what New Agent… writes; it parses as a valid definition.
const template = `---
description: Reviews a change for bugs; reports, never edits
# engine: claude     pin a program (claude or codex); without it, your default agent runs it
# model: strong      a tier (fast, balanced or strong) works on any engine: Settings ▸ Agents maps
#                    it to each engine's model; a model name (opus) needs engine
---
You are the reviewer of this repository. Read the diff and the code around it.
Report bugs with file and line. Do not change files.
`

// Create writes a new definition name.md from a template into dir and returns
// its path. It never overwrites a file. With within set (a project root), dir
// must stay inside it after resolving symlinks.
func Create(dir, within, name string) (string, error) {
	if !ValidName(name) {
		return "", fmt.Errorf("invalid agent name %q: use lowercase letters, digits and dashes, starting with a letter (at most %d)", name, maxName)
	}
	if agent.Valid(agent.Kind(name)) {
		return "", fmt.Errorf("%s is a built-in agent; choose another name", name)
	}
	path, err := prompts.WriteNew(dir, within, name+".md", template)
	if errors.Is(err, fs.ErrExist) {
		return "", fmt.Errorf("an agent named %s already exists", name)
	}
	return path, err
}
