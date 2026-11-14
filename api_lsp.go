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
	eventLSPApplyEdit   = "lsp:applyEdit"
	// lspWorkspaceTimeout is for requests that search or change the whole
	// workspace (references, rename, symbols, code actions with commands).
	lspWorkspaceTimeout = 60 * time.Second
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
		l.app.mcpSources.Diagnostics.Set(path, diags)
		l.app.emit(eventLSPDiagnostics, LSPDiagnostics{Path: path, Diagnostics: diags})
	}
	l.app.lsp.OnStatus = func(s lsp.Status) {
		l.app.emit(eventLSPStatus, s)
	}
	l.app.lsp.OnApplyEdit = func(e lsp.WorkspaceEdit) {
		l.app.emit(eventLSPApplyEdit, e)
	}
}

// Languages lists the languages with language servers and their known servers,
// for Settings.
func (l *LSPAPI) Languages() []lsp.LanguageInfo { return lsp.Languages() }

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
	ctx, cancel := context.WithTimeout(l.app.ctx, lspWorkspaceTimeout)
	defer cancel()
	return l.app.lsp.References(ctx, path, lsp.Position{Line: line, Character: character})
}

// Implementation returns the implementations of the symbol at a position.
func (l *LSPAPI) Implementation(path string, line, character int) ([]lsp.Location, error) {
	ctx, cancel := context.WithTimeout(l.app.ctx, lspWorkspaceTimeout)
	defer cancel()
	return l.app.lsp.Implementation(ctx, path, lsp.Position{Line: line, Character: character})
}

// TypeDefinition returns where the type of the symbol at a position is defined.
func (l *LSPAPI) TypeDefinition(path string, line, character int) ([]lsp.Location, error) {
	ctx, cancel := context.WithTimeout(l.app.ctx, lspTimeout)
	defer cancel()
	return l.app.lsp.TypeDefinition(ctx, path, lsp.Position{Line: line, Character: character})
}

// PrepareRename says what a rename at a position would rename.
func (l *LSPAPI) PrepareRename(path string, line, character int) (lsp.RenameTarget, error) {
	ctx, cancel := context.WithTimeout(l.app.ctx, lspTimeout)
	defer cancel()
	return l.app.lsp.PrepareRename(ctx, path, lsp.Position{Line: line, Character: character})
}

// Rename returns the edits that rename the symbol at a position. Nothing is
// written: the UI applies them (open files in their editors, the rest with
// ApplyToDisk).
func (l *LSPAPI) Rename(path string, line, character int, newName string) (lsp.WorkspaceEdit, error) {
	ctx, cancel := context.WithTimeout(l.app.ctx, lspWorkspaceTimeout)
	defer cancel()
	return l.app.lsp.Rename(ctx, path, lsp.Position{Line: line, Character: character}, newName)
}

// Format returns the edits that format the document.
func (l *LSPAPI) Format(path string, tabSize int, insertSpaces bool) ([]lsp.TextEdit, error) {
	ctx, cancel := context.WithTimeout(l.app.ctx, lspTimeout)
	defer cancel()
	return l.app.lsp.Format(ctx, path, tabSize, insertSpaces)
}

// CodeActions lists the quick fixes and refactorings for a range; only
// narrows them to kinds ("quickfix", "refactor", "source", …).
func (l *LSPAPI) CodeActions(path string, rng lsp.Range, only []string) ([]lsp.CodeAction, error) {
	ctx, cancel := context.WithTimeout(l.app.ctx, lspTimeout)
	defer cancel()
	return l.app.lsp.CodeActions(ctx, path, rng, only)
}

// ApplyCodeAction returns the edits of an action CodeActions listed.
func (l *LSPAPI) ApplyCodeAction(path string, id int) (lsp.WorkspaceEdit, error) {
	ctx, cancel := context.WithTimeout(l.app.ctx, lspWorkspaceTimeout)
	defer cancel()
	return l.app.lsp.ApplyCodeAction(ctx, path, id)
}

// OrganizeImports returns the edits of the server's "organize imports"
// (empty when it has nothing to do or doesn't offer it).
func (l *LSPAPI) OrganizeImports(path string) (lsp.WorkspaceEdit, error) {
	ctx, cancel := context.WithTimeout(l.app.ctx, lspTimeout)
	defer cancel()
	return l.app.lsp.SourceAction(ctx, path, "source.organizeImports")
}

// DocumentSymbols returns the outline of an open document.
func (l *LSPAPI) DocumentSymbols(path string) ([]lsp.Symbol, error) {
	ctx, cancel := context.WithTimeout(l.app.ctx, lspTimeout)
	defer cancel()
	return l.app.lsp.DocumentSymbols(ctx, path)
}

// WorkspaceSymbols searches the declarations of the open folders' running
// language servers.
func (l *LSPAPI) WorkspaceSymbols(query string) ([]lsp.WorkspaceSymbol, error) {
	ctx, cancel := context.WithTimeout(l.app.ctx, lspTimeout)
	defer cancel()
	return l.app.lsp.WorkspaceSymbols(ctx, l.app.projectRoots(), query)
}

// SignatureHelp describes the call around a position.
func (l *LSPAPI) SignatureHelp(path string, line, character int) (lsp.SignatureHelp, error) {
	ctx, cancel := context.WithTimeout(l.app.ctx, lspTimeout)
	defer cancel()
	return l.app.lsp.SignatureHelp(ctx, path, lsp.Position{Line: line, Character: character})
}

// SignatureTriggers lists the characters that open signature help in a document.
func (l *LSPAPI) SignatureTriggers(path string) []string {
	ctx, cancel := context.WithTimeout(l.app.ctx, lspTimeout)
	defer cancel()
	return l.app.lsp.SignatureTriggers(ctx, path)
}

// ApplyToDisk applies a language server's edits to a file that is not open
// in an editor (the other files of a rename). Only files inside an open
// folder with a running language server are written.
func (l *LSPAPI) ApplyToDisk(path string, edits []lsp.TextEdit) error {
	if _, err := l.app.rootOf(path); err != nil {
		return err
	}
	return l.app.lsp.ApplyToDisk(path, edits)
}

// Statuses lists language servers and their state.
func (l *LSPAPI) Statuses() []lsp.Status { return l.app.lsp.Statuses() }
