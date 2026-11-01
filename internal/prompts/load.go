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

// LoadDir reads the *.md files of dir. Files that can't be used are skipped and
// described in problems ("name.md: reason"); a missing folder is no problem.
//
// When within is set (a project root), dir and every file must stay inside it
// after resolving symlinks, so a repository can't make Flint read files from
// elsewhere on the disk. Only regular files up to MaxFileSize are read, and at
// most MaxPrompts of them.
func LoadDir(dir, within string, source Source) (list []Prompt, problems []string) {
	real, err := resolveDir(dir, within)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, []string{err.Error()}
	}
	entries, err := os.ReadDir(real)
	if err != nil {
		return nil, []string{fmt.Sprintf("%s: %v", dir, err)}
	}
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".md")
		if !ok || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if len(list) == MaxPrompts {
			problems = append(problems, fmt.Sprintf("%s: more than %d prompts; the rest are not loaded", dir, MaxPrompts))
			break
		}
		p, err := loadFile(real, e.Name(), name)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", e.Name(), err))
			continue
		}
		p.Source = source
		p.Path = filepath.Join(dir, e.Name())
		list = append(list, p)
	}
	return list, problems
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
		return "", fmt.Errorf("%s points outside the project; its prompts are not loaded", dir)
	}
	return real, nil
}

// loadFile reads one prompt from dir (already resolved). A symlink is
// followed only to a file inside dir.
func loadFile(dir, file, name string) (Prompt, error) {
	path := filepath.Join(dir, file)
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return Prompt{}, err
	}
	if !inside(real, dir) {
		return Prompt{}, errors.New("links outside the prompt folder")
	}
	f, err := os.Open(real)
	if err != nil {
		return Prompt{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return Prompt{}, err
	}
	if !info.Mode().IsRegular() {
		return Prompt{}, errors.New("not a regular file")
	}
	if info.Size() > MaxFileSize {
		return Prompt{}, fmt.Errorf("larger than %d KB", MaxFileSize>>10)
	}
	// Read through a limit too: the file may grow after Stat.
	data, err := io.ReadAll(io.LimitReader(f, MaxFileSize+1))
	if err != nil {
		return Prompt{}, err
	}
	if len(data) > MaxFileSize {
		return Prompt{}, fmt.Errorf("larger than %d KB", MaxFileSize>>10)
	}
	return Parse(name, data)
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
	path := filepath.Join(dir, name+".md")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, fs.ErrExist) {
		return "", fmt.Errorf("a prompt named %s already exists", name)
	}
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(template); err != nil {
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
