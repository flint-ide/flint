// Package depdirs recognises folders that hold installed dependencies, tool
// caches or build output: never the project's own sources or notes. The TODO
// list leaves them out of its search, and the Linux file watcher doesn't watch
// them (their churn would cost inotify watches and refreshes for nothing).
package depdirs

import (
	"os"
	"path/filepath"
	"slices"
)

// names are dependency folders by name, matched at any depth.
var names = []string{
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

// Names lists the folder names recognised by name (for the settings screen).
func Names() []string { return slices.Clone(names) }

// IsName reports whether a folder called name is a dependency folder by name.
func IsName(name string) bool {
	for _, d := range names {
		if ok, _ := filepath.Match(d, name); ok {
			return true
		}
	}
	return false
}

// HasMarker reports whether a folder's entries include a dependency marker.
func HasMarker(entries []string) bool {
	return slices.ContainsFunc(markers, func(m string) bool { return slices.Contains(entries, m) })
}

// BesideManifest reports whether a folder called name is build output given
// the entries of the folder holding it (target/ beside Cargo.toml).
func BesideManifest(name string, siblings []string) bool {
	return slices.ContainsFunc(outputDirs[name], func(m string) bool { return slices.Contains(siblings, m) })
}

// Is reports whether the folder at path is a dependency folder, by its name,
// a marker inside it, or the manifest beside it. It stats a few files, so it
// suits a walk that visits each folder once.
func Is(path string) bool {
	name := filepath.Base(path)
	if IsName(name) {
		return true
	}
	for _, m := range markers {
		if _, err := os.Lstat(filepath.Join(path, m)); err == nil {
			return true
		}
	}
	for _, m := range outputDirs[name] {
		if _, err := os.Lstat(filepath.Join(filepath.Dir(path), m)); err == nil {
			return true
		}
	}
	return false
}
