package projects

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"flint/internal/prompts"
)

// Problem is a file of a project that Flint cannot use, and why.
type Problem struct {
	// File is relative to the project folder ("items/x.md").
	File   string `json:"file"`
	Reason string `json:"reason"`
}

// Loaded is a project as read from disk.
type Loaded struct {
	Config   Config
	Items    []Item
	Problems []Problem
	// Context is the project's context file (ContextFile); a file that breaks
	// a rule is also one of Problems.
	Context ProjectContext
}

// Resolve makes folder absolute and resolves its symlinks: a project is
// known by that path.
func Resolve(folder string) (string, error) {
	abs, err := filepath.Abs(folder)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	return filepath.Clean(real), nil
}

// LoadConfig reads folder's flint-project.json (folder resolved).
func LoadConfig(folder string) (Config, error) {
	data, err := readInside(folder, ConfigFile)
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", ConfigFile, err)
	}
	c, err := ParseConfig(data, filepath.Base(folder))
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", ConfigFile, err)
	}
	return c, nil
}

// Load reads a project: its config (an error when that is unusable) and its
// items, at most MaxItems, one directory level, and its context file
// (ReadContext). A file that breaks a rule is a problem, not an item (a
// context file that breaks one is not used). Files are resolved inside the
// folder: a symlink that leaves it is refused.
func Load(folder string) (Loaded, error) {
	c, err := LoadConfig(folder)
	if err != nil {
		return Loaded{}, err
	}
	l := Loaded{Config: c, Items: []Item{}, Problems: []Problem{}, Context: ReadContext(folder)}
	if p, ok := l.Context.problem(); ok {
		l.Problems = append(l.Problems, p)
	}
	files, problems := prompts.ReadDir(filepath.Join(folder, ItemsDir), folder, MaxItems)
	for _, p := range problems {
		l.Problems = append(l.Problems, Problem{File: ItemsDir, Reason: strings.TrimPrefix(p, filepath.Join(folder, ItemsDir)+": ")})
	}
	for _, f := range files {
		file := itemPath(f.Name)
		if f.Err != nil {
			l.Problems = append(l.Problems, Problem{File: file, Reason: describe(f.Err)})
			continue
		}
		it, err := ParseItem(f.Name, f.Data, c)
		if err != nil {
			l.Problems = append(l.Problems, Problem{File: file, Reason: err.Error()})
			continue
		}
		l.Items = append(l.Items, it)
	}
	SortItems(l.Items)
	return l, nil
}

// describe shortens file errors (they carry the full path).
func describe(err error) string {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return err.Error()
}

// ItemFile is the path of item id in folder.
func ItemFile(folder, id string) string {
	return filepath.Join(folder, ItemsDir, id+".md")
}

// ReadItemFile reads item id's file, resolved inside the items folder, and
// returns its data and the real path to write it back to.
func ReadItemFile(folder, id string) (data []byte, real string, err error) {
	if !prompts.ValidName(id) {
		return nil, "", fmt.Errorf("invalid item id %q", id)
	}
	dir := filepath.Join(folder, ItemsDir)
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, "", err
	}
	if !inside(realDir, folder) {
		return nil, "", fmt.Errorf("%s points outside the project", ItemsDir)
	}
	real, err = filepath.EvalSymlinks(filepath.Join(realDir, id+".md"))
	if err != nil {
		return nil, "", err
	}
	if !inside(real, realDir) {
		return nil, "", errors.New("links outside its folder")
	}
	data, err = readRegular(real)
	return data, real, err
}

// readInside reads file of folder (resolved) when it stays inside folder.
func readInside(folder, file string) ([]byte, error) {
	real, err := filepath.EvalSymlinks(filepath.Join(folder, file))
	if err != nil {
		return nil, err
	}
	if !inside(real, folder) {
		return nil, errors.New("links outside the project folder")
	}
	return readRegular(real)
}

// readRegular reads a regular file of at most MaxFileSize bytes.
func readRegular(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxFileSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxFileSize {
		return nil, fmt.Errorf("larger than %d KB", MaxFileSize>>10)
	}
	return data, nil
}

// inside reports whether path is dir or under it (both clean and resolved).
func inside(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
