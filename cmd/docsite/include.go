package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Partials are Markdown files under a directory whose name starts with "_"
// (site/reference/_settings/features.md): not pages of their own, but pieces a
// page pulls in, so a feature documents its settings key in its own file
// instead of in a shared page. Two directives, each alone on a line:
//
//	<!-- include-rows _settings/*.md -->      the first line of each partial (a table row)
//	<!-- include-sections _settings/*.md -->  the rest of each partial
//
// Partials are taken in file-name order; the glob is relative to the page,
// and links inside partials are written relative to the page too.
var includeLine = regexp.MustCompile(`(?m)^<!-- include-(rows|sections) (\S+) -->[ \t]*$`)

// isPartial reports whether rel (slash-separated, relative to the site)
// is inside a partials directory.
func isPartial(rel string) bool {
	dir := filepath.ToSlash(filepath.Dir(rel))
	return slices.ContainsFunc(strings.Split(dir, "/"), func(s string) bool { return strings.HasPrefix(s, "_") })
}

// expandIncludes replaces the include directives of the page at file.
func expandIncludes(file, src string) (string, error) {
	var failed error
	out := includeLine.ReplaceAllStringFunc(src, func(line string) string {
		m := includeLine.FindStringSubmatch(line)
		files, err := filepath.Glob(filepath.Join(filepath.Dir(file), filepath.FromSlash(m[2])))
		if err == nil && len(files) == 0 {
			err = fmt.Errorf("%s matches no file", m[2])
		}
		if err != nil {
			failed = fmt.Errorf("%s: include %s: %w", file, m[2], err)
			return line
		}
		slices.Sort(files)
		var parts []string
		for _, f := range files {
			data, err := os.ReadFile(f)
			if err != nil {
				failed = err
				return line
			}
			row, rest, _ := strings.Cut(strings.TrimSpace(string(data)), "\n")
			if m[1] == "rows" {
				parts = append(parts, row)
			} else if rest = strings.TrimSpace(rest); rest != "" {
				parts = append(parts, rest)
			}
		}
		if m[1] == "rows" {
			return strings.Join(parts, "\n")
		}
		return strings.Join(parts, "\n\n")
	})
	return out, failed
}
