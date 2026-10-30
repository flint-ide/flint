package workspace

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// ErrOutsideProject is returned for paths that don't lie inside the project root.
var ErrOutsideProject = errors.New("path is outside the project")

// Within reports whether path is root itself or lies under it. The check is lexical
// on cleaned absolute paths: symlinked folders inside the project are allowed.
func Within(root, path string) bool {
	if !filepath.IsAbs(root) || !filepath.IsAbs(path) {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, "../")
}

// requireInside checks that path lies strictly under root (the root itself can't be
// renamed or deleted from the tree).
func requireInside(root, path string) error {
	if err := requireAbs(path); err != nil {
		return err
	}
	if !Within(root, path) || filepath.Clean(path) == filepath.Clean(root) {
		return fmt.Errorf("%w: %s", ErrOutsideProject, path)
	}
	return nil
}

// ValidName checks a single file or folder name typed in the tree.
func ValidName(name string) error {
	switch {
	case strings.TrimSpace(name) == "":
		return errors.New("name is empty")
	case name == "." || name == "..":
		return fmt.Errorf("%q is not a valid name", name)
	case strings.ContainsAny(name, "/\x00"):
		return fmt.Errorf("%q must not contain '/'", name)
	}
	return nil
}

// childPath validates dir (root or inside it) and name, returning dir/name.
func childPath(root, dir, name string) (string, error) {
	if err := requireAbs(dir); err != nil {
		return "", err
	}
	if !Within(root, dir) {
		return "", fmt.Errorf("%w: %s", ErrOutsideProject, dir)
	}
	if err := ValidName(name); err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

// CreateFile creates an empty file named name in dir. It fails if the name is taken.
func CreateFile(root, dir, name string) (string, error) {
	path, err := childPath(root, dir, name)
	if err != nil {
		return "", err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return "", fmt.Errorf("create %s: %w", name, err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("create %s: %w", name, err)
	}
	return path, nil
}

// CreateDir creates a folder named name in dir. It fails if the name is taken.
func CreateDir(root, dir, name string) (string, error) {
	path, err := childPath(root, dir, name)
	if err != nil {
		return "", err
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		return "", fmt.Errorf("create folder %s: %w", name, err)
	}
	return path, nil
}

// Rename gives path a new name in the same folder and returns the new path. It never
// overwrites another entry; a case-only rename (Foo.go → foo.go) is allowed.
func Rename(root, path, newName string) (string, error) {
	if err := requireInside(root, path); err != nil {
		return "", err
	}
	if err := ValidName(newName); err != nil {
		return "", err
	}
	src := filepath.Clean(path)
	dst := filepath.Join(filepath.Dir(src), newName)
	if dst == src {
		return dst, nil
	}
	srcInfo, err := os.Lstat(src)
	if err != nil {
		return "", fmt.Errorf("rename %s: %w", filepath.Base(src), err)
	}
	if dstInfo, err := os.Lstat(dst); err == nil {
		// On case-insensitive volumes the "existing" target is the source itself.
		if !os.SameFile(srcInfo, dstInfo) {
			return "", fmt.Errorf("rename %s: %s already exists", filepath.Base(src), newName)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("rename %s: %w", filepath.Base(src), err)
	}
	if err := os.Rename(src, dst); err != nil {
		return "", fmt.Errorf("rename %s: %w", filepath.Base(src), err)
	}
	return dst, nil
}

// TopLevel drops paths nested under another path in the list (deleting a folder
// already takes its contents) and duplicates, keeping the input order.
func TopLevel(paths []string) []string {
	cleaned := make([]string, 0, len(paths))
	for _, p := range paths {
		if c := filepath.Clean(p); !slices.Contains(cleaned, c) {
			cleaned = append(cleaned, c)
		}
	}
	out := make([]string, 0, len(cleaned))
	for _, p := range cleaned {
		nested := slices.ContainsFunc(cleaned, func(other string) bool {
			return other != p && Within(other, p)
		})
		if !nested {
			out = append(out, p)
		}
	}
	return out
}

// Trash moves paths to the Trash with trash (the platform's MoveToTrash in the app),
// after checking every one of them first. It returns the paths actually moved.
func Trash(root string, paths []string, trash func(string) error) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	for _, p := range paths {
		if err := requireInside(root, p); err != nil {
			return nil, err
		}
	}
	top := TopLevel(paths)
	moved := make([]string, 0, len(top))
	for _, p := range top {
		if err := trash(p); err != nil {
			return moved, fmt.Errorf("move %s to Trash: %w", filepath.Base(p), err)
		}
		moved = append(moved, p)
	}
	return moved, nil
}
