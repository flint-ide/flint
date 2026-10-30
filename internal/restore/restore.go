// Package restore keeps each project's UI session (open tabs, terminals, agent
// chats) so a restarted Flint comes back as it was left. The payload is owned
// by the frontend; this package only stores it, one file per project.
package restore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"flint/internal/config"
)

// MaxSize caps one saved session; transcripts are trimmed by the frontend.
const MaxSize = 4 << 20

// Store reads and writes session files under dir.
type Store struct {
	dir string
}

// New returns a store that keeps its files in dir (created on first save).
func New(dir string) *Store { return &Store{dir: dir} }

type file struct {
	Root    string          `json:"root"`
	SavedAt time.Time       `json:"savedAt"`
	Data    json.RawMessage `json:"data"`
}

// Load returns the saved session of root, or "" when there is none.
func (s *Store) Load(root string) (string, error) {
	var f file
	if err := config.LoadJSON(s.path(root), &f); err != nil {
		return "", err
	}
	if f.Root != root { // hash collision or a hand-edited file
		return "", nil
	}
	return string(f.Data), nil
}

// Save replaces the saved session of root with data (a JSON document).
func (s *Store) Save(root, data string) error {
	if root == "" || !filepath.IsAbs(root) {
		return fmt.Errorf("restore: root %q is not absolute", root)
	}
	if len(data) > MaxSize {
		return fmt.Errorf("restore: session is %d bytes, limit is %d", len(data), MaxSize)
	}
	if !json.Valid([]byte(data)) {
		return errors.New("restore: session is not valid JSON")
	}
	out, err := json.Marshal(file{Root: root, SavedAt: time.Now().UTC(), Data: json.RawMessage(data)})
	if err != nil {
		return err
	}
	return config.WriteFileAtomic(s.path(root), out, 0o600)
}

// Forget deletes the saved session of root.
func (s *Store) Forget(root string) error {
	err := os.Remove(s.path(root))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s *Store) path(root string) string {
	sum := sha256.Sum256([]byte(root))
	return filepath.Join(s.dir, hex.EncodeToString(sum[:8])+".json")
}
