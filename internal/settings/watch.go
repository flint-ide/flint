package settings

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// defaultWatchExclude are build output folders that churn on every build; the
// dependency folders (internal/depdirs) are skipped on top of these.
func defaultWatchExclude() []string { return []string{"dist", ".cache"} }

// cleanWatchExcludes keeps the valid folder names, trimmed and unique, up to the limit.
func cleanWatchExcludes(names []string) []string {
	out := []string{}
	for _, n := range names {
		n = strings.TrimSpace(n)
		if len(out) < maxExcludes && validWatchExclude(n) == nil && !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	return out
}

func validateWatchExcludes(names []string) error {
	if len(names) > maxExcludes {
		return fmt.Errorf("too many watch exclude names (max %d)", maxExcludes)
	}
	for _, n := range names {
		if err := validWatchExclude(n); err != nil {
			return err
		}
	}
	return nil
}

// validWatchExclude accepts a folder name or a glob over one ("*.egg-info"):
// it is matched against each folder's own name, so it has no slashes.
func validWatchExclude(n string) error {
	if err := validExclude(n); err != nil {
		return err
	}
	if strings.ContainsAny(n, `/\`) {
		return fmt.Errorf("watch exclude %q must be a folder name, without slashes", n)
	}
	if _, err := filepath.Match(n, ""); err != nil {
		return fmt.Errorf("watch exclude %q is not a valid pattern", n)
	}
	return nil
}

// WatchExcluded reports whether a folder called name is in the user's watch excludes.
func WatchExcluded(excludes []string, name string) bool {
	for _, p := range excludes {
		if ok, _ := filepath.Match(p, name); ok {
			return true
		}
	}
	return false
}
