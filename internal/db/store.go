package db

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sync"

	"github.com/zalando/go-keyring"

	"flint/internal/config"
)

const keyringService = "flint-db"

// Secrets stores connection passwords outside the JSON config.
type Secrets interface {
	Get(id string) (string, error)
	Set(id, password string) error
	Delete(id string) error
}

// Keychain stores secrets in the OS credential store (macOS Keychain, Secret Service, wincred).
type Keychain struct{}

func (Keychain) Get(id string) (string, error) {
	pw, err := keyring.Get(keyringService, id)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", nil
	}
	return pw, err
}

func (Keychain) Set(id, password string) error {
	if password == "" {
		return Keychain{}.Delete(id)
	}
	return keyring.Set(keyringService, id, password)
}

func (Keychain) Delete(id string) error {
	err := keyring.Delete(keyringService, id)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

// Store persists connection profiles to connections.json.
type Store struct {
	path    string
	secrets Secrets

	mu    sync.RWMutex
	conns []Connection
}

// OpenStore loads profiles from dir/connections.json.
func OpenStore(dir string, secrets Secrets) (*Store, error) {
	s := &Store{path: filepath.Join(dir, "connections.json"), secrets: secrets}
	if err := config.LoadJSON(s.path, &s.conns); err != nil {
		return nil, err
	}
	return s, nil
}

// List returns a copy of all profiles (never nil, so the frontend gets [] not null).
func (s *Store) List() []Connection {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Connection{}, s.conns...)
}

// Get returns the profile with the given id.
func (s *Store) Get(id string) (Connection, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	i := s.index(id)
	if i < 0 {
		return Connection{}, false
	}
	return s.conns[i], true
}

// Password returns the stored password for a profile ("" if none).
func (s *Store) Password(id string) (string, error) {
	pw, err := s.secrets.Get(id)
	if err != nil {
		return "", fmt.Errorf("read password from keychain: %w", err)
	}
	return pw, nil
}

// Save creates or updates a profile. A nil password keeps the stored one unchanged.
func (s *Store) Save(c Connection, password *string) (Connection, error) {
	c = c.Normalized()
	if err := c.Validate(); err != nil {
		return Connection{}, err
	}
	if c.ID == "" {
		c.ID = newID()
	}
	if password != nil {
		if err := s.secrets.Set(c.ID, *password); err != nil {
			return Connection{}, fmt.Errorf("store password in keychain: %w", err)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	next := slices.Clone(s.conns)
	if i := s.index(c.ID); i >= 0 {
		next[i] = c
	} else {
		next = append(next, c)
	}
	if err := config.SaveJSON(s.path, next); err != nil {
		return Connection{}, err
	}
	s.conns = next
	return c, nil
}

// Delete removes a profile and its password.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.index(id)
	if i < 0 {
		return nil
	}
	next := slices.Delete(slices.Clone(s.conns), i, i+1)
	if err := config.SaveJSON(s.path, next); err != nil {
		return err
	}
	s.conns = next
	if err := s.secrets.Delete(id); err != nil {
		return fmt.Errorf("remove password from keychain: %w", err)
	}
	return nil
}

func (s *Store) index(id string) int {
	return slices.IndexFunc(s.conns, func(c Connection) bool { return c.ID == id })
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
