package todo

import (
	"os"
	"path/filepath"
	"strings"

	"flint/internal/depdirs"
)

// Dependency folders (internal/depdirs) hold installed dependencies, tool
// caches or build output, never the project's own notes. They are skipped at
// any depth, gitignored or not.

// Bounds for finding marker folders: they sit near the top of a project
// (./.env, services/api/.venv), and a scan must stay cheap on huge trees.
const (
	detectDepth   = 4
	detectMaxDirs = 4000
	maxSkipped    = 200
)

// AutoExcluded lists the folder names skipped by name (for the settings screen).
func AutoExcluded() []string { return depdirs.Names() }

func isDependencyName(name string) bool { return depdirs.IsName(name) }

// detectDependencyDirs walks the top of root and returns the folders (relative,
// slash-separated) that hold dependencies: by name, by a marker inside, or as a
// build folder beside its manifest. Skipping by name also applies deeper (see
// excludeGlobs); markers and manifests are only seen within detectDepth.
func detectDependencyDirs(root string) []string {
	var found []string
	visited := 0
	var walk func(dir, rel string, depth int)
	walk = func(dir, rel string, depth int) {
		if visited >= detectMaxDirs || len(found) >= maxSkipped {
			return
		}
		visited++
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		if rel != "" && depdirs.HasMarker(names) {
			found = append(found, rel)
			return
		}
		for _, e := range entries {
			if !e.IsDir() || e.Name() == ".git" {
				continue
			}
			child := e.Name()
			if rel != "" {
				child = rel + "/" + e.Name()
			}
			if isDependencyName(e.Name()) || depdirs.BesideManifest(e.Name(), names) {
				if len(found) < maxSkipped {
					found = append(found, child)
				}
				continue
			}
			if depth < detectDepth {
				walk(filepath.Join(dir, e.Name()), child, depth+1)
			}
		}
	}
	walk(root, "", 0)
	return found
}

// excludeGlobs are the search excludes for one root: dependency names anywhere
// ("node_modules/" matches folders only), the folders found under root (anchored
// with a leading "/"), and the user's own patterns as written.
func excludeGlobs(found, user []string) []string {
	dependencyDirs := depdirs.Names()
	globs := make([]string, 0, len(dependencyDirs)+len(found)+len(user))
	for _, d := range dependencyDirs {
		globs = append(globs, d+"/")
	}
	for _, rel := range found {
		if !isDependencyName(rel[strings.LastIndexByte(rel, '/')+1:]) {
			globs = append(globs, "/"+rel+"/")
		}
	}
	return append(globs, user...)
}
