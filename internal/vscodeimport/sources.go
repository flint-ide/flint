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

// knownSources are the editors Flint knows, by where they keep their files
// relative to the home directory (macOS).
var knownSources = []struct{ id, name, userDir, extensionsDir string }{
	{"vscode", "Visual Studio Code", "Library/Application Support/Code/User", ".vscode/extensions"},
	{"vscode-insiders", "Visual Studio Code - Insiders", "Library/Application Support/Code - Insiders/User", ".vscode-insiders/extensions"},
	{"cursor", "Cursor", "Library/Application Support/Cursor/User", ".cursor/extensions"},
	{"vscodium", "VSCodium", "Library/Application Support/VSCodium/User", ".vscode-oss/extensions"},
}

// Sources lists the known editors that have a user directory under home.
func Sources(home string) []Source {
	out := []Source{}
	for _, k := range knownSources {
		user := filepath.Join(home, filepath.FromSlash(k.userDir))
		if info, err := os.Stat(user); err != nil || !info.IsDir() {
			continue
		}
		out = append(out, Source{ID: k.id, Name: k.name, UserDir: user, ExtensionsDir: filepath.Join(home, filepath.FromSlash(k.extensionsDir))})
	}
	return out
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
