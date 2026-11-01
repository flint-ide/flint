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
// file (the editor's squiggles). When too many files have problems, the
// file updated longest ago is forgotten. It is safe for concurrent use.
type Diagnostics struct {
	mu     sync.Mutex
	byPath map[string]diagEntry
	seq    uint64
}

type diagEntry struct {
	diags []lsp.Diagnostic
	seq   uint64 // when it was last set
}

// NewDiagnostics returns an empty store.
func NewDiagnostics() *Diagnostics { return &Diagnostics{byPath: map[string]diagEntry{}} }

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
		d.evictOldestLocked()
	}
	d.seq++
	d.byPath[path] = diagEntry{diags: slices.Clone(diags[:min(len(diags), maxDiagsPerFile)]), seq: d.seq}
}

func (d *Diagnostics) evictOldestLocked() {
	var oldest string
	var at uint64
	for path, e := range d.byPath {
		if oldest == "" || e.seq < at {
			oldest, at = path, e.seq
		}
	}
	delete(d.byPath, oldest)
}

// ClearRoot forgets the problems of the files in root (the project closed).
func (d *Diagnostics) ClearRoot(root string) {
	root = filepath.Clean(root)
	d.mu.Lock()
	defer d.mu.Unlock()
	maps.DeleteFunc(d.byPath, func(path string, _ diagEntry) bool { return within(path, root) })
}

// In returns a copy of the problems of the files in root.
func (d *Diagnostics) In(root string) map[string][]lsp.Diagnostic {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := map[string][]lsp.Diagnostic{}
	for path, e := range d.byPath {
		if within(path, root) {
			out[path] = slices.Clone(e.diags)
		}
	}
	return out
}
