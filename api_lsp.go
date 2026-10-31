package main

import (
	"context"
	"time"

	"flint/internal/lsp"
)

const (
	lspTimeout          = 15 * time.Second
	eventLSPDiagnostics = "lsp:diagnostics"
	eventLSPStatus      = "lsp:status"
)

// LSPDiagnostics is pushed whenever a server publishes problems for a file.
type LSPDiagnostics struct {
	Path        string           `json:"path"`
	Diagnostics []lsp.Diagnostic `json:"diagnostics"`
}

// LSPAPI exposes language-server features to the editor.
type LSPAPI struct {
	app *App
}

func (l *LSPAPI) wire() {
	l.app.lsp.OnDiagnostics = func(path string, diags []lsp.Diagnostic) {
		l.app.emit(eventLSPDiagnostics, LSPDiagnostics{Path: path, Diagnostics: diags})
	}
	l.app.lsp.OnStatus = func(s lsp.Status) {
		l.app.emit(eventLSPStatus, s)
	}
}

// Supported reports whether a language server exists for the file type.
func (l *LSPAPI) Supported(path string) bool { return lsp.Supported(path) }

// Open starts tracking a document; the server boots in the background.
func (l *LSPAPI) Open(root, path, text string) lsp.Status { return l.app.lsp.Open(root, path, text) }

// Change sends the document's full new text.
func (l *LSPAPI) Change(path, text string) { l.app.lsp.Change(path, text) }

// Saved notifies the server that the file was written.
func (l *LSPAPI) Saved(path string) { l.app.lsp.Saved(path) }

// Close stops tracking a document.
func (l *LSPAPI) Close(path string) { l.app.lsp.Close(path) }

// Completion returns completion items at a position.
func (l *LSPAPI) Completion(path string, line, character int) (lsp.CompletionList, error) {
	ctx, cancel := context.WithTimeout(l.app.ctx, lspTimeout)
	defer cancel()
	return l.app.lsp.Completion(ctx, path, lsp.Position{Line: line, Character: character})
}

// Hover returns documentation for the symbol at a position.
func (l *LSPAPI) Hover(path string, line, character int) (string, error) {
	ctx, cancel := context.WithTimeout(l.app.ctx, lspTimeout)
	defer cancel()
	return l.app.lsp.Hover(ctx, path, lsp.Position{Line: line, Character: character})
}

// Definition returns where the symbol at a position is defined.
func (l *LSPAPI) Definition(path string, line, character int) ([]lsp.Location, error) {
	ctx, cancel := context.WithTimeout(l.app.ctx, lspTimeout)
	defer cancel()
	return l.app.lsp.Definition(ctx, path, lsp.Position{Line: line, Character: character})
}

// References returns the usages of the symbol at a position.
func (l *LSPAPI) References(path string, line, character int) ([]lsp.Location, error) {
	ctx, cancel := context.WithTimeout(l.app.ctx, 60*time.Second) // whole-workspace search
	defer cancel()
	return l.app.lsp.References(ctx, path, lsp.Position{Line: line, Character: character})
}

// Statuses lists language servers and their state.
func (l *LSPAPI) Statuses() []lsp.Status { return l.app.lsp.Statuses() }
