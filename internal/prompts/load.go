package prompts

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ProjectDir is a project's prompt folder.
func ProjectDir(root string) string { return filepath.Join(root, ".flint", "prompts") }

// PersonalDir is the user's own prompt folder in Flint's config directory.
func PersonalDir(configDir string) string { return filepath.Join(configDir, "prompts") }

// LoadDir reads the prompts of dir (see ReadDir). Files that can't be used are
// skipped and described in problems ("name.md: reason"); a missing folder is
// no problem.
func LoadDir(dir, within string, source Source) (list []Prompt, problems []string) {
	files, problems := ReadDir(dir, within, MaxPrompts)
	for _, f := range files {
		p, err := f.Prompt()
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s.md: %v", f.Name, err))
			continue
		}
		p.Source = source
		list = append(list, p)
	}
	return list, problems
}

// File is one Markdown file read by ReadDir.
type File struct {
	// Name is the file name without .md; Path is dir joined with the file name.
	Name, Path string
	Data       []byte
	// Err says why the file could not be read; Data is then empty.
	Err error
}

// Prompt parses the file as a prompt.
func (f File) Prompt() (Prompt, error) {
	if f.Err != nil {
		return Prompt{}, f.Err
	}
	p, err := Parse(f.Name, f.Data)
	p.Path = f.Path
	return p, err
}

// ReadDir reads the *.md files of dir (hidden ones are skipped), at most max
// of them; a file that can't be read is returned with Err set. Problems are
// about the folder itself; a missing folder is no problem.
//
// When within is set (a project root), dir and every file must stay inside it
// after resolving symlinks, so a repository can't make Flint read files from
// elsewhere on the disk. Only regular files up to MaxFileSize are read.
func ReadDir(dir, within string, max int) (files []File, problems []string) {
	real, names, problems := listDir(dir, within, max)
	for _, name := range names {
		data, err := readFile(real, name+".md")
		files = append(files, File{Name: name, Path: filepath.Join(dir, name+".md"), Data: data, Err: err})
	}
	return files, problems
}

// CountDir is how many files ReadDir would return, up to max, without
// reading any of them.
func CountDir(dir, within string, max int) int {
	_, names, _ := listDir(dir, within, max)
	return len(names)
}

// listDir resolves dir (see ReadDir) and lists the names of its *.md files
// without the extension, at most max of them.
func listDir(dir, within string, max int) (real string, names, problems []string) {
	real, err := resolveDir(dir, within)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil, nil
	}
	if err != nil {
		return "", nil, []string{err.Error()}
	}
	entries, err := os.ReadDir(real)
	if err != nil {
		return "", nil, []string{fmt.Sprintf("%s: %v", dir, err)}
	}
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".md")
		if !ok || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if len(names) == max {
			problems = append(problems, fmt.Sprintf("%s: more than %d files; the rest are not loaded", dir, max))
			break
		}
		names = append(names, name)
	}
	return real, names, problems
}

// resolveDir resolves dir's symlinks and, with within set, checks that it
// stays inside within.
func resolveDir(dir, within string) (string, error) {
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	if within == "" {
		return real, nil
	}
	base, err := filepath.EvalSymlinks(within)
	if err != nil {
		return "", err
	}
	if !inside(real, base) {
		return "", fmt.Errorf("%s points outside the project; its files are not loaded", dir)
	}
	return real, nil
}

// readFile reads one file of dir (already resolved). A symlink is followed
// only to a file inside dir.
func readFile(dir, file string) ([]byte, error) {
	path := filepath.Join(dir, file)
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	if !inside(real, dir) {
		return nil, errors.New("links outside its folder")
	}
	f, err := os.Open(real)
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
	if info.Size() > MaxFileSize {
		return nil, fmt.Errorf("larger than %d KB", MaxFileSize>>10)
	}
	// Read through a limit too: the file may grow after Stat.
	data, err := io.ReadAll(io.LimitReader(f, MaxFileSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxFileSize {
		return nil, fmt.Errorf("larger than %d KB", MaxFileSize>>10)
	}
	return data, nil
}

// inside reports whether path is dir or under it (both cleaned and resolved).
func inside(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// template is what New Prompt… writes; it parses as a valid prompt.
const template = `---
description: What this prompt asks for (shown in the / picker)
# agent: claude      only offer it in chats with this agent
# args: topic        asked for when the prompt is chosen, used as {{topic}}
# Flint fills {{selection}}, {{file}}, {{problems}}, {{diff}} and {{branch}}.
---
Explain how {{file}} works, starting with the selected code:

{{selection}}
`

// Create writes a new prompt file name.md from a template into dir and
// returns its path. It never overwrites a file. With within set (a project
// root), dir must stay inside it after resolving symlinks.
func Create(dir, within, name string) (string, error) {
	if !ValidName(name) {
		return "", fmt.Errorf("invalid prompt name %q: use letters, digits, - and _", name)
	}
	path, err := WriteNew(dir, within, name+".md", template)
	if errors.Is(err, fs.ErrExist) {
		return "", fmt.Errorf("a prompt named %s already exists", name)
	}
	return path, err
}

// WriteNew creates file in dir (and dir itself) with content and returns its
// path. It never overwrites a file (the error then wraps fs.ErrExist). With
// within set (a project root), dir must stay inside it after resolving
// symlinks. file must be a plain file name the caller has checked.
func WriteNew(dir, within, file, content string) (string, error) {
	if within != "" {
		// Check before creating anything: MkdirAll follows a linked .flint.
		if err := checkCreatable(dir, within); err != nil {
			return "", err
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if within != "" {
		if _, err := resolveDir(dir, within); err != nil {
			return "", err
		}
	}
	path := filepath.Join(dir, file)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		return "", err
	}
	return path, f.Close()
}

// checkCreatable resolves the deepest existing parent of dir and checks that
// it lies inside within.
func checkCreatable(dir, within string) error {
	base, err := filepath.EvalSymlinks(within)
	if err != nil {
		return err
	}
	for p := dir; ; p = filepath.Dir(p) {
		real, err := filepath.EvalSymlinks(p)
		if err == nil {
			if !inside(real, base) {
				return fmt.Errorf("%s points outside the project", p)
			}
			return nil
		}
		if !errors.Is(err, fs.ErrNotExist) || filepath.Dir(p) == p {
			return err
		}
	}
}
