package mcptools

import (
	"maps"
	"path/filepath"
	"slices"
	"sync"

	"flint/internal/lsp"
)

// Bounds of the kept diagnostics.
const (
	maxDiagFiles    = 10000
	maxDiagsPerFile = 500
)

// Diagnostics keeps the latest problems each language server published per
// file (the editor's squiggles). It is safe for concurrent use.
type Diagnostics struct {
	mu     sync.Mutex
	byPath map[string][]lsp.Diagnostic
}

// NewDiagnostics returns an empty store.
func NewDiagnostics() *Diagnostics { return &Diagnostics{byPath: map[string][]lsp.Diagnostic{}} }

// Set replaces path's problems; none forgets the file.
func (d *Diagnostics) Set(path string, diags []lsp.Diagnostic) {
	if !filepath.IsAbs(path) {
		return
	}
	path = filepath.Clean(path)
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(diags) == 0 {
		delete(d.byPath, path)
		return
	}
	if _, known := d.byPath[path]; !known && len(d.byPath) >= maxDiagFiles {
		return
	}
	d.byPath[path] = slices.Clone(diags[:min(len(diags), maxDiagsPerFile)])
}

// In returns a copy of the problems of the files in root.
func (d *Diagnostics) In(root string) map[string][]lsp.Diagnostic {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := map[string][]lsp.Diagnostic{}
	for path, diags := range maps.All(d.byPath) {
		if within(path, root) {
			out[path] = slices.Clone(diags)
		}
	}
	return out
}
