// Package workspace provides file-system access for an opened project folder.
package workspace

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"unicode/utf8"

	"flint/internal/config"
)

// MaxFileSize is the largest file the editor will load.
const MaxFileSize = 10 << 20

// hiddenNames are never shown in the tree.
var hiddenNames = map[string]bool{".git": true, ".DS_Store": true}

// Entry is a single item of a directory listing.
type Entry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsDir bool   `json:"isDir"`
}

// FileContent is what the editor receives when opening a file.
type FileContent struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	Binary   bool   `json:"binary"`
	TooLarge bool   `json:"tooLarge"`
	Size     int64  `json:"size"`
}

// ListDir returns the children of dir, directories first, each group sorted case-insensitively.
func ListDir(dir string) ([]Entry, error) {
	if err := requireAbs(dir); err != nil {
		return nil, err
	}
	items, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", dir, err)
	}
	entries := make([]Entry, 0, len(items))
	for _, it := range items {
		if hiddenNames[it.Name()] {
			continue
		}
		entries = append(entries, Entry{
			Name:  it.Name(),
			Path:  filepath.Join(dir, it.Name()),
			IsDir: isDir(dir, it),
		})
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	return entries, nil
}

// isDir follows symlinks so linked directories can be expanded in the tree.
func isDir(parent string, e fs.DirEntry) bool {
	if e.Type()&fs.ModeSymlink == 0 {
		return e.IsDir()
	}
	info, err := os.Stat(filepath.Join(parent, e.Name()))
	return err == nil && info.IsDir()
}

// ReadFile loads a text file. Binary and oversized files are reported, not loaded.
func ReadFile(path string) (FileContent, error) {
	if err := requireAbs(path); err != nil {
		return FileContent{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return FileContent{}, fmt.Errorf("stat %s: %w", path, err)
	}
	if info.IsDir() {
		return FileContent{}, fmt.Errorf("%s is a directory", path)
	}
	fc := FileContent{Path: path, Size: info.Size()}
	if info.Size() > MaxFileSize {
		fc.TooLarge = true
		return fc, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return FileContent{}, fmt.Errorf("read %s: %w", path, err)
	}
	if looksBinary(data) {
		fc.Binary = true
		return fc, nil
	}
	fc.Content = string(data)
	return fc, nil
}

// WriteFile saves content, keeping the existing file's permissions. Symlinks are
// followed (saving CLAUDE.md -> AGENTS.md updates AGENTS.md) and hard-linked files are
// written in place; everything else is replaced atomically.
func WriteFile(path, content string) error {
	if err := requireAbs(path); err != nil {
		return err
	}
	target, err := filepath.EvalSymlinks(path)
	if errors.Is(err, fs.ErrNotExist) {
		target = path // new file
	} else if err != nil {
		return fmt.Errorf("resolve %s: %w", path, err)
	}

	info, err := os.Stat(target)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return config.WriteFileAtomic(target, []byte(content), 0o644)
	case err != nil:
		return fmt.Errorf("stat %s: %w", target, err)
	case hardLinked(info):
		return writeInPlace(target, []byte(content))
	}
	return config.WriteFileAtomic(target, []byte(content), info.Mode().Perm())
}

func hardLinked(info fs.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && st.Nlink > 1
}

// writeInPlace truncates and rewrites the file so every hard link sees the change.
func writeInPlace(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}
	return nil
}

// looksBinary flags files the editor must not load: NUL bytes, or invalid UTF-8
// anywhere (loading would replace bytes with U+FFFD and corrupt the file on save).
func looksBinary(data []byte) bool {
	if bytes.IndexByte(data[:min(len(data), 8000)], 0) >= 0 {
		return true
	}
	return !utf8.Valid(data)
}

func requireAbs(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("path must be absolute: %q", path)
	}
	return nil
}
