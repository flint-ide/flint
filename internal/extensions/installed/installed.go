// Package installed manages the extensions installed in a directory, one
// folder per extension named by its id (<config>/extensions/acme.jira/). It
// reads manifests and copies folders; it never starts an extension.
package installed

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"flint/internal/extensions/manifest"
)

// Bounds on what Discover reads and Install copies.
const (
	MaxExtensions = 500
	MaxFiles      = 5000
	MaxBytes      = 256 << 20
)

// Extension is a folder of the extensions directory: its manifest, or why
// it can't be used.
type Extension struct {
	Dir      string
	Manifest manifest.Manifest // zero when Err is set
	Err      error
}

// ID is the manifest's id, or the folder name when the manifest is broken.
func (e Extension) ID() string {
	if e.Err == nil {
		return e.Manifest.ID
	}
	return filepath.Base(e.Dir)
}

// Discover lists the extensions in root, sorted by folder name. A missing
// root has none; hidden folders (an install in progress) are skipped.
func Discover(root string) ([]Extension, error) {
	entries, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Extension
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if len(out) == MaxExtensions {
			return out, fmt.Errorf("more than %d extensions in %s; the rest are ignored", MaxExtensions, root)
		}
		dir := filepath.Join(root, e.Name())
		m, err := manifest.Load(dir)
		if err == nil && m.ID != e.Name() {
			err = fmt.Errorf("the folder is named %q but the manifest's id is %q", e.Name(), m.ID)
		}
		if err != nil {
			out = append(out, Extension{Dir: dir, Err: err})
			continue
		}
		out = append(out, Extension{Dir: dir, Manifest: m})
	}
	return out, nil
}

// Install copies the extension in src to root/<id>, replacing an installed
// version. confirmed is the manifest the user approved: if src changed in
// between, nothing is installed.
func Install(src, root string, confirmed manifest.Manifest) (Extension, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return Extension{}, err
	}
	if inside(root, src) || inside(src, root) {
		return Extension{}, errors.New("the folder is inside Flint's extensions folder, or contains it")
	}
	tmp, err := os.MkdirTemp(root, ".install-")
	if err != nil {
		return Extension{}, err
	}
	defer os.RemoveAll(tmp) // left over only on failure
	if err := copyTree(src, tmp); err != nil {
		return Extension{}, err
	}
	m, err := manifest.Load(tmp)
	if err != nil {
		return Extension{}, err
	}
	if !reflect.DeepEqual(m, confirmed) {
		return Extension{}, errors.New("the extension's manifest changed while it was being installed; nothing was installed")
	}
	dest := filepath.Join(root, m.ID)
	old := ""
	if _, err := os.Lstat(dest); err == nil {
		old = filepath.Join(root, ".old-"+m.ID)
		_ = os.RemoveAll(old)
		if err := os.Rename(dest, old); err != nil {
			return Extension{}, err
		}
	}
	if err := os.Rename(tmp, dest); err != nil {
		if old != "" {
			_ = os.Rename(old, dest)
		}
		return Extension{}, err
	}
	if old != "" {
		_ = os.RemoveAll(old)
	}
	return Extension{Dir: dest, Manifest: m}, nil
}

// Remove deletes the extension folder root/<id>.
func Remove(root, id string) error {
	if id == "" || id != filepath.Base(id) || strings.HasPrefix(id, ".") {
		return fmt.Errorf("invalid extension id %q", id)
	}
	dir := filepath.Join(root, id)
	if _, err := os.Lstat(dir); err != nil {
		return fmt.Errorf("%s is not installed", id)
	}
	return os.RemoveAll(dir)
}

func inside(parent, child string) bool {
	p, err1 := filepath.Abs(parent)
	c, err2 := filepath.Abs(child)
	if err1 != nil || err2 != nil {
		return false
	}
	rel, err := filepath.Rel(p, c)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// copyTree copies regular files and folders. Symlinks and other special
// files are refused: an extension is copied as it is, never pointing
// outside itself. The .git folder is left out.
func copyTree(src, dst string) error {
	files, total := 0, int64(0)
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		target := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case info.IsDir():
			return os.Mkdir(target, 0o755)
		case !info.Mode().IsRegular():
			return fmt.Errorf("%s is a symlink or special file, which extensions may not contain", rel)
		}
		files++
		total += info.Size()
		if files > MaxFiles || total > MaxBytes {
			return fmt.Errorf("the extension has more than %d files or %d MiB", MaxFiles, MaxBytes>>20)
		}
		return copyFile(path, target, info.Mode().Perm())
	})
}

func copyFile(src, dst string, perm fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	mode := fs.FileMode(0o644)
	if perm&0o111 != 0 {
		mode = 0o755 // keep programs runnable
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
