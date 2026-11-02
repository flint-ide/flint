//go:build server

package main

import (
	"os"
	"sync"

	"flint/internal/github"
)

// devGitHubEnv points a development server build at a local fake GitHub
// (go run ./cmd/fakegithub): FLINT_DEV_GITHUB=http://127.0.0.1:<port>. Only
// loopback addresses are accepted, only in the server build, and tokens are
// then kept in memory (never in the keyring) and never taken from the
// environment, so this can't send a real token anywhere.
const devGitHubEnv = "FLINT_DEV_GITHUB"

func devGitHubEndpoints() (github.Endpoints, bool, error) {
	base := os.Getenv(devGitHubEnv)
	if base == "" {
		return github.Endpoints{}, false, nil
	}
	e, err := github.LocalEndpoints(base)
	return e, true, err
}

// devSecrets replaces the keyring while a fake GitHub is configured.
func devSecrets() (secretStore, bool) {
	if os.Getenv(devGitHubEnv) == "" {
		return nil, false
	}
	return &memorySecrets{v: map[string]string{}}, true
}

type memorySecrets struct {
	mu sync.Mutex
	v  map[string]string
}

func (m *memorySecrets) Get(k string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.v[k], nil
}

func (m *memorySecrets) Set(k, v string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if v == "" {
		delete(m.v, k)
	} else {
		m.v[k] = v
	}
	return nil
}
