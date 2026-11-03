package dap

import (
	"encoding/json"
	"slices"
	"sync"
)

// ExtensionDebugger is a debugger an installed, enabled extension adds
// (manifest contributes.debuggers). Flint debugs Go itself, so an extension's
// debugger for Go is left out.
type ExtensionDebugger struct {
	// Type is the adapterID; Label names it for people ("Ruby (rdbg)").
	Type  string
	Label string
	// Languages are the language ids whose files it debugs.
	Languages []string
	// Candidates are the adapter command lines, programs already resolved (a
	// name for PATH, or an absolute path inside the extension); the first
	// installed one runs.
	Candidates [][]string
	Install    string
	// Launch is the launch arguments with variables still to substitute.
	Launch json.RawMessage
	// Source names the extension, for messages.
	Source string
}

// builtinDebugLanguages are debugged by Flint itself (Delve).
var builtinDebugLanguages = []string{"go"}

var debuggers struct {
	sync.RWMutex
	byLanguage map[string]ExtensionDebugger
}

// SetExtensionDebuggers replaces the extensions' debuggers; between
// extensions the first one listed wins a language.
func SetExtensionDebuggers(list []ExtensionDebugger) {
	byLanguage := map[string]ExtensionDebugger{}
	for _, d := range list {
		for _, l := range d.Languages {
			if _, taken := byLanguage[l]; !taken && !slices.Contains(builtinDebugLanguages, l) {
				byLanguage[l] = d
			}
		}
	}
	debuggers.Lock()
	debuggers.byLanguage = byLanguage
	debuggers.Unlock()
}

// ExtensionDebuggerFor is the extension debugger for a language.
func ExtensionDebuggerFor(language string) (ExtensionDebugger, bool) {
	debuggers.RLock()
	defer debuggers.RUnlock()
	d, ok := debuggers.byLanguage[language]
	return d, ok
}
