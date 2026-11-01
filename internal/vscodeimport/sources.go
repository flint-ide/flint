// Package vscodeimport reads the settings, keybindings and color theme of VS
// Code (or Cursor, VSCodium) and maps what has a Flint equivalent onto Flint's
// settings. It only reads: the caller shows the Plan and saves it through the
// normal settings path when the user confirms.
package vscodeimport

import (
	"os"
	"path/filepath"
)

// Source is an installed VS Code-family editor whose settings can be imported.
type Source struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// UserDir holds settings.json and keybindings.json.
	UserDir string `json:"userDir"`
	// ExtensionsDir holds installed extensions (and their color themes).
	ExtensionsDir string `json:"extensionsDir"`
}

// knownSources are the editors Flint knows: the name of their data directory
// and where their extensions live, relative to the home directory.
var knownSources = []struct{ id, name, dataDir, extensionsDir string }{
	{"vscode", "Visual Studio Code", "Code", ".vscode/extensions"},
	{"vscode-insiders", "Visual Studio Code - Insiders", "Code - Insiders", ".vscode-insiders/extensions"},
	{"cursor", "Cursor", "Cursor", ".cursor/extensions"},
	{"vscodium", "VSCodium", "VSCodium", ".vscode-oss/extensions"},
}

// userDirParents are where VS Code-family editors keep their data directory,
// relative to the home directory: macOS, then Linux (the XDG config default).
// Windows (%APPDATA%) is not supported yet.
var userDirParents = []string{"Library/Application Support", ".config"}

// Sources lists the known editors that have a user directory under home.
func Sources(home string) []Source {
	out := []Source{}
	for _, k := range knownSources {
		user, ok := userDir(home, k.dataDir)
		if !ok {
			continue
		}
		out = append(out, Source{ID: k.id, Name: k.name, UserDir: user, ExtensionsDir: filepath.Join(home, filepath.FromSlash(k.extensionsDir))})
	}
	return out
}

// userDir is the first existing <parent>/<dataDir>/User under home.
func userDir(home, dataDir string) (string, bool) {
	for _, parent := range userDirParents {
		user := filepath.Join(home, filepath.FromSlash(parent), dataDir, "User")
		if info, err := os.Stat(user); err == nil && info.IsDir() {
			return user, true
		}
	}
	return "", false
}

// FindSource returns the source with this id, if it is installed under home.
// The id comes from the UI, so only known ids resolve.
func FindSource(home, id string) (Source, bool) {
	for _, s := range Sources(home) {
		if s.ID == id {
			return s, true
		}
	}
	return Source{}, false
}
