// Package manifest reads and checks an external extension's manifest,
// flint-extension.json (docs/design/extensions.md, level 2). It only parses:
// nothing here starts a process or touches the UI. A manifest that passes
// Parse is safe to show and to translate into contributions; one that fails
// disables its extension, and the error says why.
package manifest

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// FileName is the manifest's name inside an extension's directory.
const FileName = "flint-extension.json"

// Contract is the major version of the extension contract this Flint speaks.
const Contract = 1

// MaxSize bounds the manifest file, in bytes.
const MaxSize = 1 << 20

// Manifest is a parsed flint-extension.json.
type Manifest struct {
	Contract    int         `json:"contract"`
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Version     string      `json:"version"`
	Publisher   string      `json:"publisher,omitempty"`
	Description string      `json:"description,omitempty"`
	Run         *Run        `json:"run,omitempty"`
	Activation  []string    `json:"activation,omitempty"`
	Permissions []string    `json:"permissions,omitempty"`
	Contributes Contributes `json:"contributes"`
}

// Run is the extension's program: a path inside its directory, never looked
// up on PATH or run through a shell, with a fixed argument list.
type Run struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
	Darwin  *Program `json:"darwin,omitempty"`
	Linux   *Program `json:"linux,omitempty"`
}

// Program overrides Run on one platform.
type Program struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
}

// Contributes is everything the extension adds to Flint.
type Contributes struct {
	Commands    []Command    `json:"commands,omitempty"`
	Panels      []Panel      `json:"panels,omitempty"`
	Settings    *Settings    `json:"settings,omitempty"`
	StatusItems []StatusItem `json:"statusItems,omitempty"`
	FileMenu    []FileMenu   `json:"fileMenu,omitempty"`
	TabViews    []TabView    `json:"tabViews,omitempty"`
	AgentTools  []AgentTool  `json:"agentTools,omitempty"`
	Themes      []Theme      `json:"themes,omitempty"`
}

// Command is a palette command; running it sends command/execute.
type Command struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Category string `json:"category,omitempty"`
	Keys     string `json:"keys,omitempty"`
}

// Panel is a sidebar panel drawn by Flint from view/resolve.
type Panel struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Icon  string `json:"icon,omitempty"`
	Order int    `json:"order"`
	View  string `json:"view,omitempty"`
}

// StatusItem is a status bar item whose text comes from status/update.
type StatusItem struct {
	ID    string `json:"id"`
	Order int    `json:"order"`
}

// FileMenu is an entry of the file tree's context menu that runs a command.
type FileMenu struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Command string `json:"command"`
}

// TabView is a kind of editor tab the extension opens with window/openTab.
type TabView struct {
	ID   string `json:"id"`
	View string `json:"view,omitempty"`
}

// AgentTool is a tool offered to agents on Flint's MCP server.
type AgentTool struct {
	Name        string          `json:"name"`
	Title       string          `json:"title,omitempty"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// Theme is a color theme: the same CSS variables and terminal colors as a
// custom theme (settings.CustomTheme), checked by the same rules.
type Theme struct {
	ID       string            `json:"id"`
	Label    string            `json:"label"`
	Base     string            `json:"base"`
	Colors   map[string]string `json:"colors"`
	Terminal map[string]string `json:"terminal,omitempty"`
}

// Parse decodes and checks a manifest. Every problem found is in the error
// (errors.Join), so Settings can list them together.
func Parse(data []byte) (Manifest, error) {
	if len(data) > MaxSize {
		return Manifest{}, fmt.Errorf("%s is larger than %d bytes", FileName, MaxSize)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("%s: %w", FileName, err)
	}
	if err := m.check(); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

// Load reads dir's manifest, which must be a regular file (not a symlink).
func Load(dir string) (Manifest, error) {
	path := filepath.Join(dir, FileName)
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Manifest{}, fmt.Errorf("no %s in %s", FileName, dir)
		}
		return Manifest{}, err
	}
	if !info.Mode().IsRegular() {
		return Manifest{}, fmt.Errorf("%s is not a regular file", FileName)
	}
	f, err := os.Open(path)
	if err != nil {
		return Manifest{}, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxSize+1))
	if err != nil {
		return Manifest{}, err
	}
	return Parse(data)
}

// NeedsProgram reports whether the manifest contributes something that only
// a running extension can serve; themes and settings alone need no program.
func (m Manifest) NeedsProgram() bool {
	c := m.Contributes
	return len(c.Commands)+len(c.Panels)+len(c.StatusItems)+len(c.FileMenu)+len(c.TabViews)+len(c.AgentTools) > 0 || len(m.Activation) > 0
}
