package vscodeimport

import (
	"cmp"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// extension is one installed extension folder ("publisher.name-1.2.3").
type extension struct {
	id      string // "publisher.name", lower case
	version []int  // nil when the folder name has none
	dir     string // symlinks resolved
}

// extensionFolder splits "publisher.name-1.10.0" and platform builds like
// "ms-python.python-2024.1.0-darwin-arm64"; ids may contain hyphens.
var extensionFolder = regexp.MustCompile(`^(.+?)-(\d+(?:\.\d+)*)(?:-[a-z0-9-]+)?$`)

// installedExtensions lists the extensions in dir: the newest version of each
// (compared as numbers, so 1.10.0 beats 1.9.0), leaving out the folders VS
// Code marked obsolete (.obsolete: uninstalled or replaced, deleted later).
// Symlinked folders count when they lead to a directory.
func installedExtensions(dir string) []extension {
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var obsolete map[string]bool
	_ = readJSONC(filepath.Join(root, ".obsolete"), maxPackageSize, &obsolete)
	newest := map[string]extension{}
	for i, e := range entries {
		if i >= maxExtensions {
			break
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") || obsolete[name] {
			continue
		}
		path, err := filepath.EvalSymlinks(filepath.Join(root, name))
		if err != nil {
			continue
		}
		if info, err := os.Stat(path); err != nil || !info.IsDir() {
			continue
		}
		ext := parseExtension(name, path)
		if prev, ok := newest[ext.id]; !ok || compareVersions(ext.version, prev.version) > 0 {
			newest[ext.id] = ext
		}
	}
	out := make([]extension, 0, len(newest))
	for _, ext := range newest {
		out = append(out, ext)
	}
	slices.SortFunc(out, func(a, b extension) int { return cmp.Compare(a.id, b.id) })
	return out
}

func parseExtension(name, dir string) extension {
	m := extensionFolder.FindStringSubmatch(name)
	if m == nil {
		return extension{id: strings.ToLower(name), dir: dir}
	}
	var version []int
	for _, part := range strings.Split(m[2], ".") {
		n, _ := strconv.Atoi(part)
		version = append(version, n)
	}
	return extension{id: strings.ToLower(m[1]), version: version, dir: dir}
}

// compareVersions compares dotted versions part by part (missing parts are 0).
func compareVersions(a, b []int) int {
	for i := range max(len(a), len(b)) {
		var x, y int
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if c := cmp.Compare(x, y); c != 0 {
			return c
		}
	}
	return 0
}

// hasExtension reports whether the extension with this id is installed in dir.
func hasExtension(dir, id string) bool {
	return slices.ContainsFunc(installedExtensions(dir), func(e extension) bool { return e.id == id })
}
