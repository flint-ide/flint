package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/zalando/go-keyring"

	"flint/internal/completions"
)

// Keychain entries of completion API keys: one per endpoint origin
// (scheme://host:port), so a key is only ever sent to the server it was
// stored for.
const (
	completionsKeyService = "flint-completions"
	maxAPIKeyLen          = 4096
	maxEditorKeyLen       = 128
)

// secretStore is the Keychain (keyringSecrets), replaced in tests.
type secretStore interface {
	Get(account string) (string, error)
	Set(account, value string) error
}

// keyringSecrets is one service of the OS keyring (Keychain, Secret
// Service); Set with "" deletes the entry.
type keyringSecrets struct{ service string }

func (k keyringSecrets) Get(account string) (string, error) {
	v, err := keyring.Get(k.service, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", nil
	}
	return v, err
}

func (k keyringSecrets) Set(account, value string) error {
	if value == "" {
		err := keyring.Delete(k.service, account)
		if errors.Is(err, keyring.ErrNotFound) {
			return nil
		}
		return err
	}
	return keyring.Set(k.service, account, value)
}

// CompletionsAPI asks the configured code model for inline suggestions
// (settings: completions). One request per editor: a new one cancels the
// editor's previous request, as does cancelling the call.
type CompletionsAPI struct {
	app     *App
	client  *completions.Client
	secrets secretStore
	latest  completions.Latest

	keysMu sync.Mutex
	keys   map[string]string // origin → API key ("" = none), read from the Keychain once
}

func newCompletionsAPI(app *App) *CompletionsAPI {
	return &CompletionsAPI{app: app, client: completions.New(), secrets: keyringSecrets{service: completionsKeyService}}
}

// Complete returns the suggestion for the cursor of editor (an id the
// frontend picks per editor) or "" when there is none. It fails when
// completions are off.
func (c *CompletionsAPI) Complete(ctx context.Context, editor string, req completions.Request) (string, error) {
	s := c.app.currentSettings()
	if !s.Completions.Enabled {
		return "", errors.New("completions are off (Settings ▸ AI in the Editor)")
	}
	if editor == "" || len(editor) > maxEditorKeyLen {
		return "", errors.New("invalid editor id")
	}
	ctx, done := c.latest.Start(ctx, editor)
	defer done()
	if c.app.ctx != nil {
		stop := context.AfterFunc(c.app.ctx, done) // Flint quits
		defer stop()
	}
	key, err := c.apiKey(s.Completions.Endpoint)
	if err != nil {
		return "", err
	}
	return c.client.Complete(ctx, s.CompletionConfig(), key, req)
}

// apiKey is the key stored for endpoint's origin, cached after the first
// Keychain read (each read starts /usr/bin/security).
func (c *CompletionsAPI) apiKey(endpoint string) (string, error) {
	origin, err := completions.Origin(endpoint)
	if err != nil {
		return "", err
	}
	c.keysMu.Lock()
	defer c.keysMu.Unlock()
	if key, ok := c.keys[origin]; ok {
		return key, nil
	}
	key, err := c.secrets.Get(origin)
	if err != nil {
		return "", fmt.Errorf("read the completion API key from the Keychain: %w", err)
	}
	if c.keys == nil {
		c.keys = map[string]string{}
	}
	c.keys[origin] = key
	return key, nil
}

// HasAPIKey reports whether an API key is stored for the configured endpoint.
func (c *CompletionsAPI) HasAPIKey() (bool, error) {
	key, err := c.apiKey(c.app.currentSettings().Completions.Endpoint)
	return key != "", err
}

// SetAPIKey stores the API key of the configured endpoint in the Keychain
// ("" removes it). It is sent only to that endpoint's origin.
func (c *CompletionsAPI) SetAPIKey(key string) error {
	key = strings.TrimSpace(key)
	if len(key) > maxAPIKeyLen || strings.ContainsFunc(key, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return errors.New("invalid API key")
	}
	origin, err := completions.Origin(c.app.currentSettings().Completions.Endpoint)
	if err != nil {
		return err
	}
	c.keysMu.Lock()
	defer c.keysMu.Unlock()
	if err := c.secrets.Set(origin, key); err != nil {
		return err
	}
	if c.keys == nil {
		c.keys = map[string]string{}
	}
	c.keys[origin] = key
	return nil
}
