package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"

	"flint/internal/redact"
)

// SessionAPI saves and restores what was open in a project (tabs, terminals,
// agent chats) so a restart picks up where the user left off.
type SessionAPI struct {
	app *App
}

// Load returns the saved session of root as JSON, "" when there is none or it
// cannot be read (a broken file must never block opening the project).
func (s *SessionAPI) Load(root string) string {
	data, err := s.app.sessions.Load(root)
	if err != nil {
		log.Printf("session: load %s: %v", root, err)
		return ""
	}
	return data
}

// Save stores the session of root; data is a JSON document owned by the UI.
// Terminal scrollback in it may show tokens or passwords: credentials are
// removed from every string value before the file is written.
func (s *SessionAPI) Save(root, data string) error {
	return s.app.sessions.Save(root, redactJSONStrings(data))
}

// redactJSONStrings removes credentials from the string values of a JSON
// document (keys and structure stay); invalid JSON is stored unchanged.
func redactJSONStrings(data string) string {
	var doc any
	if json.Unmarshal([]byte(data), &doc) != nil {
		return data
	}
	var walk func(v any) any
	walk = func(v any) any {
		switch x := v.(type) {
		case string:
			return redact.Secrets(x)
		case []any:
			for i := range x {
				x[i] = walk(x[i])
			}
		case map[string]any:
			for k := range x {
				x[k] = walk(x[k])
			}
		}
		return v
	}
	out, err := json.Marshal(walk(doc))
	if err != nil {
		return data
	}
	return string(out)
}

// Forget drops the saved session of root.
func (s *SessionAPI) Forget(root string) error { return s.app.sessions.Forget(root) }

// Exists reports whether path is an existing directory (used to skip restoring
// terminals whose folder is gone).
func (s *SessionAPI) Exists(path string) bool {
	if !filepath.IsAbs(path) {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
