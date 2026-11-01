package prompts

import (
	"embed"
	"fmt"
	"io/fs"
	"strings"
)

// The starters shipped with Flint. A personal or project prompt with the same
// name replaces one, and Settings can switch them all off.
//
//go:embed builtin/*.md
var builtinFS embed.FS

// Builtins parses the shipped prompts; problems would be a bug in Flint.
func Builtins() (list []Prompt, problems []string) {
	files, _ := fs.Glob(builtinFS, "builtin/*.md")
	for _, f := range files {
		data, err := builtinFS.ReadFile(f)
		if err == nil {
			name := strings.TrimSuffix(strings.TrimPrefix(f, "builtin/"), ".md")
			var p Prompt
			if p, err = Parse(name, data); err == nil {
				p.Source = SourceBuiltin
				list = append(list, p)
				continue
			}
		}
		problems = append(problems, fmt.Sprintf("built-in %s: %v", f, err))
	}
	return list, problems
}
