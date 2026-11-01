package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/zalando/go-keyring"

	"flint/internal/completions"
)

// Keychain entry of the completion endpoint's API key.
const (
	completionsKeyService = "flint-completions"
	completionsKeyAccount = "api-key"
	maxAPIKeyLen          = 4096
	maxEditorKeyLen       = 128
)

// secretStore is the Keychain (keyringSecrets), replaced in tests.
type secretStore interface {
	Get() (string, error)
	Set(value string) error
}

type keyringSecrets struct{}

func (keyringSecrets) Get() (string, error) {
	v, err := keyring.Get(completionsKeyService, completionsKeyAccount)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", nil
	}
	return v, err
}

func (keyringSecrets) Set(value string) error {
	if value == "" {
		err := keyring.Delete(completionsKeyService, completionsKeyAccount)
		if errors.Is(err, keyring.ErrNotFound) {
			return nil
		}
		return err
	}
	return keyring.Set(completionsKeyService, completionsKeyAccount, value)
}

// CompletionsAPI asks the configured code model for inline suggestions
// (settings: completions). One request per editor: a new one cancels the
// editor's previous request, as does cancelling the call.
type CompletionsAPI struct {
	app     *App
	client  *completions.Client
	secrets secretStore
	latest  completions.Latest
}

func newCompletionsAPI(app *App) *CompletionsAPI {
	return &CompletionsAPI{app: app, client: completions.New(), secrets: keyringSecrets{}}
}

// Complete returns the suggestion for the cursor of editor (an id the
// frontend picks per editor) or "" when there is none. It fails when
// completions are off.
func (c *CompletionsAPI) Complete(ctx context.Context, editor string, req completions.Request) (string, error) {
	s := c.app.currentSettings()
	if !s.Completions.Enabled {
		return "", errors.New("completions are off (Settings ▸ Completions)")
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
	key, err := c.secrets.Get()
	if err != nil {
		return "", fmt.Errorf("read the completion API key from the Keychain: %w", err)
	}
	return c.client.Complete(ctx, s.CompletionConfig(), key, req)
}

// Cancel stops editor's request, if any (the user moved the cursor or closed it).
func (c *CompletionsAPI) Cancel(editor string) { c.latest.Cancel(editor) }

// HasAPIKey reports whether an API key is stored for the completion endpoint.
func (c *CompletionsAPI) HasAPIKey() (bool, error) {
	v, err := c.secrets.Get()
	return v != "", err
}

// SetAPIKey stores the completion endpoint's API key in the Keychain ("" removes it).
func (c *CompletionsAPI) SetAPIKey(key string) error {
	key = strings.TrimSpace(key)
	if len(key) > maxAPIKeyLen || strings.ContainsFunc(key, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return errors.New("invalid API key")
	}
	return c.secrets.Set(key)
}
