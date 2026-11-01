//go:build server

package main

import (
	"sync"

	"flint/internal/remote"
)

// The dev-only server build keeps paired devices' token hashes in memory, so
// UI checks and end-to-end runs never write the Keychain; pairings end with
// the process.
func remoteSecretStore() remote.Secrets { return &memorySecrets{v: map[string]string{}} }

type memorySecrets struct {
	mu sync.Mutex
	v  map[string]string
}

func (m *memorySecrets) Get(account string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.v[account], nil
}

func (m *memorySecrets) Set(account, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.v[account] = value
	return nil
}

func (m *memorySecrets) Delete(account string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.v, account)
	return nil
}
