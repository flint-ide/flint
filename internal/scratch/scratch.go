// Package scratch persists scratchpads: SQL consoles and notes that live outside any project.
package scratch

import (
	"cmp"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"flint/internal/config"
)

// Kind decides the editor language of a scratchpad.
type Kind string

const (
	SQL      Kind = "sql"
	Markdown Kind = "markdown"
	Text     Kind = "text"
)

var extensions = map[Kind]string{SQL: ".sql", Markdown: ".md", Text: ".txt"}

// Scratch is a scratchpad's metadata; the content lives in its own file.
type Scratch struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Kind      Kind   `json:"kind"`
	ConnID    string `json:"connId"` // selected connection for SQL scratchpads
	UpdatedAt int64  `json:"updatedAt"`
}

// Store keeps scratchpads in dir/scratch.
type Store struct {
	dir string

	mu    sync.Mutex
	items []Scratch

	writeMu sync.Mutex // serializes content writes so an older body can't land last
}

// Open loads the scratchpad index from configDir/scratch.
func Open(configDir string) (*Store, error) {
	s := &Store{dir: filepath.Join(configDir, "scratch")}
	if err := config.LoadJSON(s.indexPath(), &s.items); err != nil {
		return nil, err
	}
	return s, nil
}

// List returns scratchpads, most recently updated first.
func (s *Store) List() []Scratch {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]Scratch{}, s.items...) // never nil: the frontend expects []
	slices.SortStableFunc(out, func(a, b Scratch) int { return cmp.Compare(b.UpdatedAt, a.UpdatedAt) })
	return out
}

// Create adds an empty scratchpad.
func (s *Store) Create(name string, kind Kind, connID string) (Scratch, error) {
	if _, ok := extensions[kind]; !ok {
		return Scratch{}, fmt.Errorf("unknown scratch kind %q", kind)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = fmt.Sprintf("scratch %s", time.Now().Format("2006-01-02 15:04"))
	}
	item := Scratch{ID: newID(), Name: name, Kind: kind, ConnID: connID, UpdatedAt: time.Now().UnixMilli()}
	if err := config.WriteFileAtomic(s.contentPath(item), nil, 0o600); err != nil {
		return Scratch{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.persist(append(slices.Clone(s.items), item)); err != nil {
		return Scratch{}, err
	}
	return item, nil
}

// Content reads a scratchpad's text.
func (s *Store) Content(id string) (string, error) {
	item, err := s.get(id)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(s.contentPath(item))
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read scratch %s: %w", id, err)
	}
	return string(data), nil
}

// SaveContent writes a scratchpad's text and bumps its timestamp.
func (s *Store) SaveContent(id, content string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	item, err := s.get(id)
	if err != nil {
		return err
	}
	if err := config.WriteFileAtomic(s.contentPath(item), []byte(content), 0o600); err != nil {
		return err
	}
	return s.update(id, func(sc Scratch) Scratch {
		sc.UpdatedAt = time.Now().UnixMilli()
		return sc
	})
}

// Rename changes a scratchpad's display name.
func (s *Store) Rename(id, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("name must not be empty")
	}
	return s.update(id, func(sc Scratch) Scratch {
		sc.Name = name
		return sc
	})
}

// SetConnection binds an SQL scratchpad to a connection.
func (s *Store) SetConnection(id, connID string) error {
	return s.update(id, func(sc Scratch) Scratch {
		sc.ConnID = connID
		return sc
	})
}

// Delete removes a scratchpad and its content file.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.index(id)
	if i < 0 {
		return nil
	}
	item := s.items[i]
	if err := s.persist(slices.Delete(slices.Clone(s.items), i, i+1)); err != nil {
		return err
	}
	if err := os.Remove(s.contentPath(item)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove scratch file: %w", err)
	}
	return nil
}

func (s *Store) get(id string) (Scratch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.index(id)
	if i < 0 {
		return Scratch{}, fmt.Errorf("unknown scratch %q", id)
	}
	return s.items[i], nil
}

func (s *Store) update(id string, fn func(Scratch) Scratch) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.index(id)
	if i < 0 {
		return fmt.Errorf("unknown scratch %q", id)
	}
	next := slices.Clone(s.items)
	next[i] = fn(next[i])
	return s.persist(next)
}

// persist writes the index and swaps in the new item list; callers hold s.mu.
func (s *Store) persist(items []Scratch) error {
	if err := config.SaveJSON(s.indexPath(), items); err != nil {
		return err
	}
	s.items = items
	return nil
}

func (s *Store) index(id string) int {
	return slices.IndexFunc(s.items, func(sc Scratch) bool { return sc.ID == id })
}

func (s *Store) indexPath() string { return filepath.Join(s.dir, "index.json") }

func (s *Store) contentPath(sc Scratch) string {
	return filepath.Join(s.dir, sc.ID+extensions[sc.Kind])
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
