package manifest

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// Editor decorations (docs/design/extensions.md, "Editor decorations"): an
// extension declares decoration sets, each for the files a selector names,
// and then publishes marks for open documents (editor/setDecorations). A set
// is data; the user can turn each one on or off.

// Limits of the decoration sets of one extension.
const (
	maxDecorationSets = 20
	maxSetSelectors   = 50
)

// DecorationSet is a group of marks an extension draws in editors: gutter
// markers, underlines with a message, text after a line and highlights.
type DecorationSet struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Default: false leaves the set off until the user turns it on in
	// Settings ▸ Extensions (missing: on).
	Default *bool `json:"default,omitempty"`
	// The files the set is for; a file matching any selector counts.
	// Languages are Flint's language ids ("go", "typescript", "python",
	// "rust", or a language an extension adds), Extensions file extensions
	// with the dot (".md"), Globs patterns for the file's name ("*.test.ts",
	// "Dockerfile*", "*").
	Languages  []string `json:"languages,omitempty"`
	Extensions []string `json:"extensions,omitempty"`
	Globs      []string `json:"globs,omitempty"`
	// UnsavedText: editor/opened and editor/changed carry the document's
	// text as it is in the editor, unsaved edits included.
	UnsavedText bool `json:"unsavedText,omitempty"`
}

// On reports whether the set is on unless the user says otherwise.
func (s DecorationSet) On() bool { return s.Default == nil || *s.Default }

// Matches reports whether the set is for file, whose language (Flint's id,
// "" when it has none) the caller knows.
func (s DecorationSet) Matches(file, language string) bool {
	if language != "" && slices.Contains(s.Languages, language) {
		return true
	}
	base := filepath.Base(file)
	if ext := strings.ToLower(filepath.Ext(base)); ext != "" {
		for _, e := range s.Extensions {
			if strings.ToLower(e) == ext {
				return true
			}
		}
	}
	for _, g := range s.Globs {
		if ok, _ := path.Match(g, base); ok {
			return true
		}
	}
	return false
}

// DecorationSet returns the contributed decoration set id, if there is one.
func (m *Manifest) DecorationSet(id string) (DecorationSet, bool) {
	i := slices.IndexFunc(m.Contributes.DecorationSets, func(s DecorationSet) bool { return s.ID == id })
	if i < 0 {
		return DecorationSet{}, false
	}
	return m.Contributes.DecorationSets[i], true
}

func (m *Manifest) checkDecorationSets(own func(kind, id string) error) error {
	sets := m.Contributes.DecorationSets
	if err := checkCount("decorationSets", len(sets), maxDecorationSets); err != nil {
		return err
	}
	if len(sets) > 0 && !m.HasPermission(PermWorkspaceRead) {
		// Flint tells the extension which files are open, and it reads them.
		return fmt.Errorf("contributes.decorationSets needs the %q permission", PermWorkspaceRead)
	}
	for _, s := range sets {
		if err := own("decoration set", s.ID); err != nil {
			return err
		}
		if err := s.check(); err != nil {
			return fmt.Errorf("decoration set %s: %w", s.ID, err)
		}
	}
	return nil
}

func (s DecorationSet) check() error {
	if err := checkText("title", s.Title, 60, true); err != nil {
		return err
	}
	n := len(s.Languages) + len(s.Extensions) + len(s.Globs)
	if n == 0 {
		return errors.New(`no "languages", "extensions" or "globs" say which files it is for`)
	}
	if n > maxSetSelectors {
		return fmt.Errorf("at most %d languages, extensions and globs", maxSetSelectors)
	}
	for _, l := range s.Languages {
		if !languagePattern.MatchString(l) {
			return fmt.Errorf("language %q: use lower-case letters, digits, +, #, _ or -, at most 40", l)
		}
	}
	for _, e := range s.Extensions {
		if !extPattern.MatchString(e) {
			return fmt.Errorf("extension %q is not like \".rb\"", e)
		}
	}
	for _, g := range s.Globs {
		if err := checkText("glob", g, 80, true); err != nil {
			return err
		}
		if strings.ContainsAny(g, `/\`) {
			return fmt.Errorf("glob %q: a glob matches a file's name, without folders", g)
		}
		if _, err := path.Match(g, ""); err != nil {
			return fmt.Errorf("glob %q is not a pattern like \"*.test.ts\"", g)
		}
	}
	return nil
}
