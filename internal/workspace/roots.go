package workspace

import (
	"fmt"
	"path/filepath"
	"strconv"
)

// RootOf returns the workspace root that contains path. Roots never nest
// (CheckNewRoot), so at most one matches.
func RootOf(roots []string, path string) (string, bool) {
	for _, r := range roots {
		if Within(r, path) {
			return r, true
		}
	}
	return "", false
}

// CheckNewRoot rejects a folder that is already in the workspace, or lies inside or
// around one of its roots (its files would show up twice).
func CheckNewRoot(roots []string, path string) error {
	if err := requireAbs(path); err != nil {
		return err
	}
	for _, r := range roots {
		switch {
		case filepath.Clean(r) == filepath.Clean(path):
			return fmt.Errorf("%s is already in the workspace", filepath.Base(path))
		case Within(r, path):
			return fmt.Errorf("%s is inside %s, which is already in the workspace", filepath.Base(path), filepath.Base(r))
		case Within(path, r):
			return fmt.Errorf("%s contains %s, which is already in the workspace", filepath.Base(path), filepath.Base(r))
		}
	}
	return nil
}

// Labels names roots for display and quick-open: the folder name, plus the parent
// folder's name when two roots share a name ("app (api)"), and a number if that
// still clashes. Labels never contain "/". Keep in sync with rootLabels in
// frontend/src/lib/workspaceRoots.ts.
func Labels(roots []string) []string {
	base := make([]string, len(roots))
	count := map[string]int{}
	for i, r := range roots {
		base[i] = filepath.Base(r)
		count[base[i]]++
	}
	labels := make([]string, len(roots))
	used := map[string]bool{}
	for i, r := range roots {
		l := base[i]
		if count[l] > 1 {
			l = fmt.Sprintf("%s (%s)", l, filepath.Base(filepath.Dir(r)))
		}
		for n := 2; used[l]; n++ {
			l = base[i] + " " + strconv.Itoa(n)
		}
		used[l] = true
		labels[i] = l
	}
	return labels
}
