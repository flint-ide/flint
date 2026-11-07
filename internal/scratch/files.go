package scratch

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// File is a scratch file: any type (an .http request, a .json sample, a Go
// snippet), kept outside every project and edited like a project file, so the
// editor's language support, the HTTP client and the rest apply to it.
type File struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	UpdatedAt int64  `json:"updatedAt"`
}

// maxFileName bounds a scratch file's name (file systems allow 255 bytes).
const maxFileName = 200

// FilesDir is the folder scratch files live in.
func (s *Store) FilesDir() string { return filepath.Join(s.dir, "files") }

// Files lists the scratch files, most recently changed first.
func (s *Store) Files() ([]File, error) {
	entries, err := os.ReadDir(s.FilesDir())
	if errors.Is(err, fs.ErrNotExist) {
		return []File{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list scratch files: %w", err)
	}
	out := []File{} // never nil: the frontend expects []
	for _, e := range entries {
		if !e.Type().IsRegular() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue // removed meanwhile
		}
		out = append(out, File{Name: e.Name(), Path: filepath.Join(s.FilesDir(), e.Name()), UpdatedAt: info.ModTime().UnixMilli()})
	}
	slices.SortStableFunc(out, func(a, b File) int { return cmp.Compare(b.UpdatedAt, a.UpdatedAt) })
	return out, nil
}

// CreateFile writes a new scratch file named name (made unique: "requests-2.http")
// with content and returns it.
func (s *Store) CreateFile(name, content string) (File, error) {
	name, err := cleanFileName(name)
	if err != nil {
		return File{}, err
	}
	if err := os.MkdirAll(s.FilesDir(), 0o700); err != nil {
		return File{}, fmt.Errorf("create scratch folder: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := 1; i < 1000; i++ {
		path := filepath.Join(s.FilesDir(), numbered(name, i))
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return File{}, fmt.Errorf("create scratch file: %w", err)
		}
		_, werr := f.WriteString(content)
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			_ = os.Remove(path)
			return File{}, fmt.Errorf("write scratch file: %w", werr)
		}
		return s.fileAt(path)
	}
	return File{}, fmt.Errorf("too many scratch files named %s", name)
}

// RenameFile renames the scratch file at path to name (which must be free).
func (s *Store) RenameFile(path, name string) (File, error) {
	if err := s.requireFile(path); err != nil {
		return File{}, err
	}
	name, err := cleanFileName(name)
	if err != nil {
		return File{}, err
	}
	target := filepath.Join(s.FilesDir(), name)
	if target == path {
		return s.fileAt(path)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := os.Lstat(target); err == nil {
		return File{}, fmt.Errorf("a scratch file named %s already exists", name)
	}
	if err := os.Rename(path, target); err != nil {
		return File{}, fmt.Errorf("rename scratch file: %w", err)
	}
	return s.fileAt(target)
}

// DeleteFile removes the scratch file at path.
func (s *Store) DeleteFile(path string) error {
	if err := s.requireFile(path); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("delete scratch file: %w", err)
	}
	return nil
}

// requireFile accepts only a regular file directly in FilesDir (no links, no other folders).
func (s *Store) requireFile(path string) error {
	if !filepath.IsAbs(path) || filepath.Dir(filepath.Clean(path)) != s.FilesDir() {
		return fmt.Errorf("%s is not a scratch file", path)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("scratch file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a scratch file", path)
	}
	return nil
}

func (s *Store) fileAt(path string) (File, error) {
	info, err := os.Stat(path)
	if err != nil {
		return File{}, fmt.Errorf("scratch file: %w", err)
	}
	return File{Name: filepath.Base(path), Path: path, UpdatedAt: info.ModTime().UnixMilli()}, nil
}

// cleanFileName accepts a plain file name: no folders, not hidden, not too long.
func cleanFileName(name string) (string, error) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return "", errors.New("a scratch file needs a name")
	case strings.ContainsAny(name, `/\`) || strings.ContainsRune(name, 0):
		return "", fmt.Errorf("%q: a scratch file name can't contain / or \\", name)
	case strings.HasPrefix(name, "."):
		return "", fmt.Errorf("%q: a scratch file name can't start with a dot", name)
	case len(name) > maxFileName:
		return "", fmt.Errorf("scratch file name is too long (max %d bytes)", maxFileName)
	}
	return name, nil
}

// numbered is name for i == 1, else name with -i before the extension ("a.http" → "a-2.http").
func numbered(name string, i int) string {
	if i == 1 {
		return name
	}
	ext := filepath.Ext(name)
	return fmt.Sprintf("%s-%d%s", strings.TrimSuffix(name, ext), i, ext)
}
