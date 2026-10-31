package todo

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// dependencyDirs hold installed dependencies, tool caches or build output, never
// the project's own notes. They are skipped at any depth, gitignored or not.
var dependencyDirs = []string{
	// JavaScript / TypeScript
	"node_modules", "bower_components", "jspm_packages", ".pnpm-store", ".yarn",
	".next", ".nuxt", ".svelte-kit", ".turbo", ".parcel-cache", ".angular",
	// Python
	".venv", "venv", "__pycache__", ".tox", ".nox", ".mypy_cache", ".pytest_cache",
	".ruff_cache", ".eggs", "*.egg-info", "site-packages", "__pypackages__",
	// Go, PHP, Ruby
	"vendor", ".bundle",
	// iOS / Swift, JVM, Dart, Haskell, OCaml, Elm, Terraform
	"Pods", "Carthage", ".build", ".gradle", ".dart_tool", ".pub-cache",
	".stack-work", "_opam", "elm-stuff", ".terraform",
}

// markers are entries that make the folder holding them a dependency folder,
// whatever it is called: virtualenvs ("env", ".env", "py311"…) and Conda
// environments, and cache directories tagged per the Cache Directory Tagging
// spec (Cargo's target/, pytest, ruff…).
var markers = []string{"pyvenv.cfg", "conda-meta", "CACHEDIR.TAG"}

// outputDirs are build folders recognised by the manifest next to them: target/
// is source in some projects, but not beside a pom.xml.
var outputDirs = map[string][]string{
	"target": {"Cargo.toml", "pom.xml", "build.sbt"},
	"build":  {"build.gradle", "build.gradle.kts", "pubspec.yaml"},
	"_build": {"mix.exs", "rebar.config"},
	"deps":   {"mix.exs"},
}

// Bounds for finding marker folders: they sit near the top of a project
// (./.env, services/api/.venv), and a scan must stay cheap on huge trees.
const (
	detectDepth   = 4
	detectMaxDirs = 4000
	maxSkipped    = 200
)

// AutoExcluded lists the folder names skipped by name (for the settings screen).
func AutoExcluded() []string { return slices.Clone(dependencyDirs) }

func isDependencyName(name string) bool {
	for _, d := range dependencyDirs {
		if ok, _ := filepath.Match(d, name); ok {
			return true
		}
	}
	return false
}

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
		if rel != "" && slices.ContainsFunc(markers, func(m string) bool { return slices.Contains(names, m) }) {
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
			if isDependencyName(e.Name()) || besideManifest(e.Name(), names) {
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

func besideManifest(name string, siblings []string) bool {
	return slices.ContainsFunc(outputDirs[name], func(m string) bool { return slices.Contains(siblings, m) })
}

// excludeGlobs are the search excludes for one root: dependency names anywhere
// ("node_modules/" matches folders only), the folders found under root (anchored
// with a leading "/"), and the user's own patterns as written.
func excludeGlobs(found, user []string) []string {
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
