package main

import (
	"log"
	"os"
	"path/filepath"
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
func (s *SessionAPI) Save(root, data string) error { return s.app.sessions.Save(root, data) }

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
