package host

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"flint/internal/extensions/manifest"
)

// Terminal tabs and the data folder (docs/design/extensions.md, "Separate
// packages"): window/openTerminal opens a terminal tab that runs a command
// the extension chose, and FLINT_EXTENSION_DATA is a folder of its own.

// EventOpenTerminal carries an OpenTerminalEvent.
const EventOpenTerminal = "extensions:openTerminal"

// Limits on window/openTerminal.
const (
	maxTerminalArgs = 64
	maxTerminalArg  = 4096
	maxTerminalEnv  = 50
)

// OpenTerminalEvent is EventOpenTerminal's data: open a terminal tab in Cwd
// ("" for the active project) with Env added, typing Command (argv, quoted by
// the frontend) when there is one.
type OpenTerminalEvent struct {
	Extension string            `json:"extension"`
	Title     string            `json:"title,omitempty"`
	Cwd       string            `json:"cwd,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	Command   []string          `json:"command,omitempty"`
	Tag       string            `json:"tag,omitempty"`
}

var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

// Variables an extension may not set for a terminal: they would change what
// runs or how the shell starts.
var reservedEnv = map[string]bool{"PATH": true, "HOME": true, "SHELL": true, "USER": true, "LOGNAME": true, "ENV": true, "BASH_ENV": true, "ZDOTDIR": true, "PROMPT_COMMAND": true, "IFS": true, "LD_PRELOAD": true, "LD_LIBRARY_PATH": true, "DYLD_INSERT_LIBRARIES": true, "DYLD_LIBRARY_PATH": true}

func plain(s string, limit int) bool {
	return len(s) <= limit && !strings.ContainsFunc(s, func(r rune) bool { return unicode.IsControl(r) && r != '\t' })
}

// openTerminal checks window/openTerminal and asks the frontend to open it.
func (e *ext) openTerminal(params json.RawMessage) error {
	if err := e.need(manifest.PermTerminal); err != nil {
		return err
	}
	var p OpenTerminalEvent
	if err := decode(params, &p); err != nil {
		return err
	}
	if len(p.Command) > maxTerminalArgs {
		return invalid("at most %d command arguments", maxTerminalArgs)
	}
	for _, a := range p.Command {
		if !plain(a, maxTerminalArg) {
			return invalid("a command argument is too long or contains control characters")
		}
	}
	if len(p.Command) > 0 && p.Command[0] == "" {
		return invalid("the command is empty")
	}
	if len(p.Env) > maxTerminalEnv {
		return invalid("at most %d environment variables", maxTerminalEnv)
	}
	for k, v := range p.Env {
		if !envName.MatchString(k) || reservedEnv[strings.ToUpper(k)] || strings.HasPrefix(strings.ToUpper(k), "FLINT_") {
			return invalid("environment variable %q can't be set", cleanText(k, 40))
		}
		if !plain(v, maxTerminalArg) {
			return invalid("environment variable %s: too long or contains control characters", k)
		}
	}
	cwd := ""
	if p.Cwd != "" {
		real, err := e.workspaceFile(p.Cwd)
		if err != nil {
			return err
		}
		if st, err := os.Stat(real); err != nil || !st.IsDir() {
			return invalid("%s is not a folder", p.Cwd)
		}
		cwd = real
	}
	e.h.o.App.Emit(EventOpenTerminal, OpenTerminalEvent{
		Extension: e.id, Title: cleanText(p.Title, 100), Cwd: cwd, Env: p.Env, Command: p.Command, Tag: cleanText(p.Tag, 60),
	})
	return nil
}

// dataDir is the extension's own folder for data (FLINT_EXTENSION_DATA):
// <config>/extension-data/<id>, created on start and kept on uninstall like
// its settings.
func (e *ext) dataDir() (string, error) {
	dir := filepath.Join(filepath.Dir(e.h.o.Dir), "extension-data", e.id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}
