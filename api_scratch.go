package main

import "flint/internal/scratch"

// ScratchAPI exposes persistent scratchpads (SQL consoles, notes).
type ScratchAPI struct {
	app *App
}

// List returns all scratchpads, most recently edited first.
func (s *ScratchAPI) List() []scratch.Scratch {
	return s.app.scratch.List()
}

// Create adds an empty scratchpad of the given kind ("sql", "markdown", "text").
// The kind is a plain string because Wails doesn't generate TS types for named strings.
func (s *ScratchAPI) Create(name, kind, connID, database string) (scratch.Scratch, error) {
	return s.app.scratch.Create(name, scratch.Kind(kind), connID, database)
}

// Content loads a scratchpad's text.
func (s *ScratchAPI) Content(id string) (string, error) {
	return s.app.scratch.Content(id)
}

// Save stores a scratchpad's text.
func (s *ScratchAPI) Save(id, content string) error {
	return s.app.scratch.SaveContent(id, content)
}

// Rename changes a scratchpad's name.
func (s *ScratchAPI) Rename(id, name string) error {
	return s.app.scratch.Rename(id, name)
}

// SetConnection binds an SQL scratchpad to a connection profile and one of its databases.
func (s *ScratchAPI) SetConnection(id, connID, database string) error {
	return s.app.scratch.SetConnection(id, connID, database)
}

// Delete removes a scratchpad.
func (s *ScratchAPI) Delete(id string) error {
	return s.app.scratch.Delete(id)
}

// Files lists the scratch files (any type, edited like project files), newest first.
func (s *ScratchAPI) Files() ([]scratch.File, error) {
	return s.app.scratch.Files()
}

// CreateFile writes a new scratch file (name made unique) with content.
func (s *ScratchAPI) CreateFile(name, content string) (scratch.File, error) {
	return s.app.scratch.CreateFile(name, content)
}

// RenameFile renames a scratch file.
func (s *ScratchAPI) RenameFile(path, name string) (scratch.File, error) {
	return s.app.scratch.RenameFile(path, name)
}

// DeleteFile removes a scratch file.
func (s *ScratchAPI) DeleteFile(path string) error {
	return s.app.scratch.DeleteFile(path)
}
