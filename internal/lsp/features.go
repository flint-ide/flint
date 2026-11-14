package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"flint/internal/workspace"
)

// Bounds on answers sent to the UI.
const (
	maxSymbols       = 5000
	maxSymbolResults = 200
	maxCodeActions   = 100
	maxSignatures    = 20
	maxNameLen       = 512
)

// ErrUnsupported is wrapped by the error of a request the document's
// language server doesn't offer (it has no formatter, no rename, …).
var ErrUnsupported = errors.New("not supported by the language server")

func unsupported(language, what string) error {
	return fmt.Errorf("the %s language server can't %s (%w)", displayName(language), what, ErrUnsupported)
}

// RenameTarget is what a rename at a position would rename.
type RenameTarget struct {
	Range       Range  `json:"range"`
	Placeholder string `json:"placeholder"`
	// Known is false when the server can't say (no prepareRename): the UI
	// then offers the word under the cursor.
	Known bool `json:"known"`
}

// CodeAction is a fix or refactoring the server offers; ID names it in
// ApplyCodeAction.
type CodeAction struct {
	ID        int    `json:"id"`
	Title     string `json:"title"`
	Kind      string `json:"kind"`
	Preferred bool   `json:"preferred"`
	// Disabled, when set, is why the action can't be used here.
	Disabled string `json:"disabled"`
}

// Symbol is a declaration in a document. Depth is its nesting level, so the
// list reads as an outline without a tree on the wire.
type Symbol struct {
	Name      string `json:"name"`
	Detail    string `json:"detail"`
	Kind      int    `json:"kind"`
	Depth     int    `json:"depth"`
	Container string `json:"container"`
	Range     Range  `json:"range"`
	// Selection is the name itself, where the cursor goes.
	Selection Range `json:"selection"`
}

// WorkspaceSymbol is a declaration somewhere in the project.
type WorkspaceSymbol struct {
	Name      string `json:"name"`
	Kind      int    `json:"kind"`
	Container string `json:"container"`
	Path      string `json:"path"`
	Range     Range  `json:"range"`
}

// SignatureHelp describes the call around the cursor.
type SignatureHelp struct {
	Signatures      []Signature `json:"signatures"`
	ActiveSignature int         `json:"activeSignature"`
	ActiveParameter int         `json:"activeParameter"`
}

// Signature is one overload; a parameter's Start and End are UTF-16 offsets
// into Label (both zero when the server didn't say where it is).
type Signature struct {
	Label         string      `json:"label"`
	Documentation string      `json:"documentation"`
	Parameters    []Parameter `json:"parameters"`
}

// Parameter is one parameter of a Signature.
type Parameter struct {
	Start         int    `json:"start"`
	End           int    `json:"end"`
	Documentation string `json:"documentation"`
}

// storedAction is a code action kept between listing and applying it.
type storedAction struct {
	srv *server
	raw json.RawMessage
}

// Implementation returns the implementations of the symbol at pos.
func (m *Manager) Implementation(ctx context.Context, path string, pos Position) ([]Location, error) {
	return m.locations(ctx, path, pos, "textDocument/implementation", "implementationProvider", "find implementations")
}

// TypeDefinition returns where the type of the symbol at pos is defined.
func (m *Manager) TypeDefinition(ctx context.Context, path string, pos Position) ([]Location, error) {
	return m.locations(ctx, path, pos, "textDocument/typeDefinition", "typeDefinitionProvider", "go to a type's definition")
}

func (m *Manager) locations(ctx context.Context, path string, pos Position, method, capability, what string) ([]Location, error) {
	srv, err := m.serverFor(ctx, path)
	if err != nil {
		return []Location{}, err
	}
	if !srv.can(capability) {
		return []Location{}, unsupported(srv.language, what)
	}
	var raw json.RawMessage
	if err := m.request(ctx, path, method, positionParams(path, pos), &raw); err != nil {
		return []Location{}, err
	}
	return m.withPreviews(decodeLocations(raw)), nil
}

// PrepareRename says what a rename at pos would rename, or why it can't.
func (m *Manager) PrepareRename(ctx context.Context, path string, pos Position) (RenameTarget, error) {
	srv, err := m.serverFor(ctx, path)
	if err != nil {
		return RenameTarget{}, err
	}
	if !srv.can("renameProvider") {
		return RenameTarget{}, unsupported(srv.language, "rename symbols")
	}
	if !srv.canSub("renameProvider", "prepareProvider") {
		return RenameTarget{}, nil
	}
	var raw json.RawMessage
	if err := m.request(ctx, path, "textDocument/prepareRename", positionParams(path, pos), &raw); err != nil {
		return RenameTarget{}, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return RenameTarget{}, errors.New("there is nothing to rename here: put the cursor on a name")
	}
	var w struct {
		Start           *Position `json:"start"`
		End             *Position `json:"end"`
		Range           *Range    `json:"range"`
		Placeholder     string    `json:"placeholder"`
		DefaultBehavior bool      `json:"defaultBehavior"`
	}
	if err := json.Unmarshal(raw, &w); err != nil {
		return RenameTarget{}, nil
	}
	switch {
	case w.Range != nil:
		return RenameTarget{Range: *w.Range, Placeholder: clip(w.Placeholder, maxNameLen), Known: true}, nil
	case w.Start != nil && w.End != nil:
		return RenameTarget{Range: Range{Start: *w.Start, End: *w.End}, Known: true}, nil
	}
	return RenameTarget{}, nil
}

// Rename returns the edits that rename the symbol at pos to newName.
func (m *Manager) Rename(ctx context.Context, path string, pos Position, newName string) (WorkspaceEdit, error) {
	none := WorkspaceEdit{Files: []FileEdit{}}
	if newName == "" || len(newName) > maxNameLen || strings.ContainsAny(newName, "\r\n\x00") {
		return none, errors.New("the new name must be one line of at most 512 bytes")
	}
	srv, err := m.serverFor(ctx, path)
	if err != nil {
		return none, err
	}
	if !srv.can("renameProvider") {
		return none, unsupported(srv.language, "rename symbols")
	}
	params := positionParams(path, pos)
	params["newName"] = newName
	var raw json.RawMessage
	if err := m.request(ctx, path, "textDocument/rename", params, &raw); err != nil {
		return none, err
	}
	return decodeWorkspaceEdit(raw, m.docVersion)
}

// Format returns the edits that format the whole document.
func (m *Manager) Format(ctx context.Context, path string, tabSize int, insertSpaces bool) ([]TextEdit, error) {
	srv, err := m.serverFor(ctx, path)
	if err != nil {
		return []TextEdit{}, err
	}
	if !srv.can("documentFormattingProvider") {
		return []TextEdit{}, unsupported(srv.language, "format files")
	}
	if tabSize < 1 || tabSize > 16 {
		tabSize = 4
	}
	params := map[string]any{
		"textDocument": map[string]any{"uri": PathToURI(path)},
		"options":      map[string]any{"tabSize": tabSize, "insertSpaces": insertSpaces},
	}
	edits := []TextEdit{}
	if err := m.request(ctx, path, "textDocument/formatting", params, &edits); err != nil {
		return []TextEdit{}, err
	}
	if len(edits) > maxEdits {
		return []TextEdit{}, fmt.Errorf("the formatter's change is too large to apply safely (%d edits)", len(edits))
	}
	return edits, nil
}

// CodeActions lists the fixes and refactorings for rng. only narrows them to
// kinds (for example "quickfix" or "source.organizeImports"); empty asks for
// everything. The list replaces the one kept for path, so ids from an
// earlier call stop working.
func (m *Manager) CodeActions(ctx context.Context, path string, rng Range, only []string) ([]CodeAction, error) {
	srv, err := m.serverFor(ctx, path)
	if err != nil {
		return []CodeAction{}, err
	}
	if !srv.can("codeActionProvider") {
		return []CodeAction{}, unsupported(srv.language, "offer quick fixes")
	}
	context := map[string]any{"diagnostics": srv.diagnosticsIn(path, rng), "triggerKind": 1}
	if len(only) > 0 {
		context["only"] = only
	}
	params := map[string]any{
		"textDocument": map[string]any{"uri": PathToURI(path)},
		"range":        rng,
		"context":      context,
	}
	var raw []json.RawMessage
	if err := m.request(ctx, path, "textDocument/codeAction", params, &raw); err != nil {
		return []CodeAction{}, err
	}
	if len(raw) > maxCodeActions {
		raw = raw[:maxCodeActions]
	}
	out := make([]CodeAction, 0, len(raw))
	stored := make(map[int]storedAction, len(raw))
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, item := range raw {
		var w struct {
			Title       string `json:"title"`
			Kind        string `json:"kind"`
			IsPreferred bool   `json:"isPreferred"`
			Disabled    *struct {
				Reason string `json:"reason"`
			} `json:"disabled"`
		}
		if json.Unmarshal(item, &w) != nil || w.Title == "" {
			continue
		}
		m.nextAction++
		a := CodeAction{ID: m.nextAction, Title: clip(w.Title, 300), Kind: w.Kind, Preferred: w.IsPreferred}
		if w.Disabled != nil {
			a.Disabled = clip(w.Disabled.Reason, 300)
		}
		out = append(out, a)
		stored[a.ID] = storedAction{srv: srv, raw: item}
	}
	if m.actions == nil {
		m.actions = map[string]map[int]storedAction{}
	}
	m.actions[path] = stored
	return out, nil
}

// ApplyCodeAction resolves the action id that CodeActions listed for path
// and returns its edits, including those its command makes the server send.
func (m *Manager) ApplyCodeAction(ctx context.Context, path string, id int) (WorkspaceEdit, error) {
	none := WorkspaceEdit{Files: []FileEdit{}}
	m.mu.Lock()
	a, ok := m.actions[path][id]
	m.mu.Unlock()
	if !ok {
		return none, errors.New("that quick fix is out of date: open the list again")
	}
	if !a.srv.alive() {
		return none, fmt.Errorf("the %s language server stopped; Flint restarts it, so try again in a moment", displayName(a.srv.language))
	}
	type wireAction struct {
		Edit    json.RawMessage `json:"edit"`
		Command json.RawMessage `json:"command"`
		// A bare Command has these instead of Edit.
		Arguments json.RawMessage `json:"arguments"`
	}
	var w wireAction
	if err := json.Unmarshal(a.raw, &w); err != nil {
		return none, err
	}
	// Edits queued before this request must reach the server first.
	if err := a.srv.out.barrier(ctx); err != nil {
		return none, err
	}
	command := w.Command
	if len(command) > 0 && command[0] == '"' {
		command = a.raw // a bare Command: {title, command: "name", arguments}
	} else if isNull(w.Edit) && a.srv.canSub("codeActionProvider", "resolveProvider") {
		var resolved wireAction
		if err := a.srv.conn.Call(ctx, "codeAction/resolve", a.raw, &resolved); err != nil {
			return none, err
		}
		w.Edit = resolved.Edit
		if !isNull(resolved.Command) {
			command = resolved.Command
		}
	}
	edit, err := decodeWorkspaceEdit(w.Edit, m.docVersion)
	if err != nil {
		return none, err
	}
	if isNull(command) {
		return edit, nil
	}
	var cmd struct {
		Command   string          `json:"command"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(command, &cmd); err != nil || cmd.Command == "" {
		return edit, nil
	}
	// The command's own changes arrive as workspace/applyEdit requests while
	// it runs; they are collected and applied after the action's edit.
	collected := a.srv.collectEdits()
	params := map[string]any{"command": cmd.Command}
	if !isNull(cmd.Arguments) {
		params["arguments"] = cmd.Arguments
	}
	err = a.srv.conn.Call(ctx, "workspace/executeCommand", params, nil)
	raws := collected()
	if err != nil {
		return none, err
	}
	for _, raw := range raws {
		more, err := decodeWorkspaceEdit(raw, m.docVersion)
		if err != nil {
			return none, err
		}
		edit.Files = append(edit.Files, more.Files...)
	}
	return edit, nil
}

// SourceAction runs the first action of a source kind (for example
// "source.organizeImports") on the whole document and returns its edits; no
// such action is an empty edit.
func (m *Manager) SourceAction(ctx context.Context, path, kind string) (WorkspaceEdit, error) {
	m.mu.Lock()
	doc := m.docs[path]
	var end Position
	if doc != nil {
		end = Position{Line: strings.Count(doc.text, "\n") + 1}
	}
	m.mu.Unlock()
	actions, err := m.CodeActions(ctx, path, Range{End: end}, []string{kind})
	if err != nil {
		return WorkspaceEdit{Files: []FileEdit{}}, err
	}
	for _, a := range actions {
		if a.Disabled == "" && (a.Kind == kind || strings.HasPrefix(a.Kind, kind+".")) {
			return m.ApplyCodeAction(ctx, path, a.ID)
		}
	}
	return WorkspaceEdit{Files: []FileEdit{}}, nil
}

// DocumentSymbols returns the outline of a document, in document order.
func (m *Manager) DocumentSymbols(ctx context.Context, path string) ([]Symbol, error) {
	srv, err := m.serverFor(ctx, path)
	if err != nil {
		return []Symbol{}, err
	}
	if !srv.can("documentSymbolProvider") {
		return []Symbol{}, unsupported(srv.language, "list a file's symbols")
	}
	var raw json.RawMessage
	params := map[string]any{"textDocument": map[string]any{"uri": PathToURI(path)}}
	if err := m.request(ctx, path, "textDocument/documentSymbol", params, &raw); err != nil {
		return []Symbol{}, err
	}
	return decodeDocumentSymbols(raw), nil
}

// WorkspaceSymbols searches the declarations of every running language
// server whose folder is one of roots.
func (m *Manager) WorkspaceSymbols(ctx context.Context, roots []string, query string) ([]WorkspaceSymbol, error) {
	out := []WorkspaceSymbol{}
	if len(query) > maxNameLen {
		return out, nil
	}
	m.mu.Lock()
	var servers []*server
	for k, e := range m.servers {
		if !containsString(roots, k.root) {
			continue
		}
		select {
		case <-e.ready:
			if e.srv != nil && e.srv.alive() && e.srv.can("workspaceSymbolProvider") {
				servers = append(servers, e.srv)
			}
		default:
		}
	}
	m.mu.Unlock()
	sort.Slice(servers, func(i, j int) bool {
		if servers[i].root != servers[j].root {
			return servers[i].root < servers[j].root
		}
		return servers[i].language < servers[j].language
	})
	var firstErr error
	for _, srv := range servers {
		var raw json.RawMessage
		if err := srv.conn.Call(ctx, "workspace/symbol", map[string]any{"query": query}, &raw); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		out = append(out, decodeWorkspaceSymbols(raw)...)
		if len(out) >= maxSymbolResults {
			out = out[:maxSymbolResults]
			break
		}
	}
	if len(out) == 0 && firstErr != nil {
		return out, firstErr
	}
	return out, nil
}

// SignatureHelp describes the call around pos; no call is an empty answer.
func (m *Manager) SignatureHelp(ctx context.Context, path string, pos Position) (SignatureHelp, error) {
	none := SignatureHelp{Signatures: []Signature{}}
	srv, err := m.serverFor(ctx, path)
	if err != nil {
		return none, err
	}
	if !srv.can("signatureHelpProvider") {
		return none, nil
	}
	var raw json.RawMessage
	if err := m.request(ctx, path, "textDocument/signatureHelp", positionParams(path, pos), &raw); err != nil {
		return none, err
	}
	return decodeSignatureHelp(raw), nil
}

// SignatureTriggers lists the characters after which the document's server
// wants to be asked for signature help ("(" and "," for most).
func (m *Manager) SignatureTriggers(ctx context.Context, path string) []string {
	srv, err := m.serverFor(ctx, path)
	if err != nil {
		return []string{}
	}
	var p struct {
		TriggerCharacters   []string `json:"triggerCharacters"`
		RetriggerCharacters []string `json:"retriggerCharacters"`
	}
	_ = json.Unmarshal(srv.capability("signatureHelpProvider"), &p)
	out := []string{}
	for _, c := range append(p.TriggerCharacters, p.RetriggerCharacters...) {
		if utf8.RuneCountInString(c) == 1 && !containsString(out, c) && len(out) < 16 {
			out = append(out, c)
		}
	}
	return out
}

// ApplyToDisk applies a language server's edits to a file that isn't open
// in an editor. Only files inside a folder with a running language server
// are written, never inside .git, and never a file with an open buffer (its
// edits belong in the editor, where they can be undone).
func (m *Manager) ApplyToDisk(path string, edits []TextEdit) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("%s is not a clean absolute path", path)
	}
	m.mu.Lock()
	_, open := m.docs[path]
	inside := false
	for k := range m.servers {
		if rel, err := filepath.Rel(k.root, path); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			inside = true
			for _, part := range strings.Split(rel, string(filepath.Separator)) {
				if part == ".git" {
					inside = false
				}
			}
			if inside {
				break
			}
		}
	}
	m.mu.Unlock()
	switch {
	case open:
		return fmt.Errorf("%s is open in an editor; its edits are applied there", path)
	case !inside:
		return fmt.Errorf("%s is outside the folders of the running language servers, so Flint won't change it", path)
	case len(edits) > maxEdits:
		return fmt.Errorf("too many edits for %s", path)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > 10<<20 {
		return fmt.Errorf("%s is not a regular file under 10 MB", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !utf8.Valid(data) {
		return fmt.Errorf("%s is not UTF-8 text", path)
	}
	text, err := ApplyTextEdits(string(data), edits)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return workspace.WriteFile(path, text)
}

// serverFor waits for path's server.
func (m *Manager) serverFor(ctx context.Context, path string) (*server, error) {
	m.mu.Lock()
	doc := m.docs[path]
	if doc == nil {
		m.mu.Unlock()
		return nil, fmt.Errorf("%s is not open", path)
	}
	e := m.ensureServerLocked(doc.key)
	m.mu.Unlock()
	select {
	case <-e.ready:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if e.err != nil {
		return nil, e.err
	}
	return e.srv, nil
}

// docVersion is the version of an open document.
func (m *Manager) docVersion(path string) (int, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if d := m.docs[path]; d != nil {
		return d.version, true
	}
	return 0, false
}

// applyEdit handles a workspace/applyEdit a server sent on its own (outside
// ApplyCodeAction): the edit goes to the UI.
func (m *Manager) applyEdit(raw json.RawMessage) error {
	edit, err := decodeWorkspaceEdit(raw, m.docVersion)
	if err != nil {
		return err
	}
	if m.OnApplyEdit == nil {
		return errors.New("no editor to apply the edit")
	}
	if len(edit.Files) > 0 {
		m.OnApplyEdit(edit)
	}
	return nil
}

func decodeDocumentSymbols(raw json.RawMessage) []Symbol {
	out := []Symbol{}
	if isNull(raw) {
		return out
	}
	type wire struct {
		Name           string `json:"name"`
		Detail         string `json:"detail"`
		Kind           int    `json:"kind"`
		Range          *Range `json:"range"`
		SelectionRange *Range `json:"selectionRange"`
		Children       []wire `json:"children"`
		// SymbolInformation (flat) instead of DocumentSymbol:
		ContainerName string `json:"containerName"`
		Location      *struct {
			Range Range `json:"range"`
		} `json:"location"`
	}
	var list []wire
	if json.Unmarshal(raw, &list) != nil {
		return out
	}
	var walk func(items []wire, depth int, container string)
	walk = func(items []wire, depth int, container string) {
		for _, w := range items {
			if len(out) >= maxSymbols || depth > 32 {
				return
			}
			s := Symbol{Name: clip(w.Name, 200), Detail: clip(w.Detail, 200), Kind: w.Kind, Depth: depth, Container: container}
			switch {
			case w.Range != nil:
				s.Range, s.Selection = *w.Range, *w.Range
				if w.SelectionRange != nil {
					s.Selection = *w.SelectionRange
				}
			case w.Location != nil:
				s.Range, s.Selection = w.Location.Range, w.Location.Range
				s.Container = clip(w.ContainerName, 200)
			default:
				continue
			}
			if s.Name == "" {
				continue
			}
			out = append(out, s)
			walk(w.Children, depth+1, s.Name)
		}
	}
	walk(list, 0, "")
	return out
}

func decodeWorkspaceSymbols(raw json.RawMessage) []WorkspaceSymbol {
	out := []WorkspaceSymbol{}
	if isNull(raw) {
		return out
	}
	var list []struct {
		Name          string `json:"name"`
		Kind          int    `json:"kind"`
		ContainerName string `json:"containerName"`
		Location      struct {
			URI   string `json:"uri"`
			Range *Range `json:"range"`
		} `json:"location"`
	}
	if json.Unmarshal(raw, &list) != nil {
		return out
	}
	for _, w := range list {
		path := URIToPath(w.Location.URI)
		if path == "" || w.Name == "" {
			continue
		}
		s := WorkspaceSymbol{Name: clip(w.Name, 200), Kind: w.Kind, Container: clip(w.ContainerName, 200), Path: path}
		if w.Location.Range != nil {
			s.Range = *w.Location.Range
		}
		out = append(out, s)
		if len(out) >= maxSymbolResults {
			break
		}
	}
	return out
}

func decodeSignatureHelp(raw json.RawMessage) SignatureHelp {
	out := SignatureHelp{Signatures: []Signature{}}
	if isNull(raw) {
		return out
	}
	var w struct {
		Signatures []struct {
			Label           string          `json:"label"`
			Documentation   json.RawMessage `json:"documentation"`
			ActiveParameter *int            `json:"activeParameter"`
			Parameters      []struct {
				Label         json.RawMessage `json:"label"`
				Documentation json.RawMessage `json:"documentation"`
			} `json:"parameters"`
		} `json:"signatures"`
		ActiveSignature int  `json:"activeSignature"`
		ActiveParameter *int `json:"activeParameter"`
	}
	if json.Unmarshal(raw, &w) != nil {
		return out
	}
	if len(w.Signatures) > maxSignatures {
		w.Signatures = w.Signatures[:maxSignatures]
	}
	for i, ws := range w.Signatures {
		sig := Signature{Label: clip(ws.Label, 2000), Documentation: clip(markupText(ws.Documentation), 4000), Parameters: []Parameter{}}
		from := 0 // string labels are found left to right, so repeated names match in order
		for _, wp := range ws.Parameters {
			p := Parameter{Documentation: clip(markupText(wp.Documentation), 2000)}
			var offsets [2]int
			var name string
			if json.Unmarshal(wp.Label, &offsets) == nil {
				p.Start, p.End = offsets[0], offsets[1]
			} else if json.Unmarshal(wp.Label, &name) == nil && name != "" {
				if at := strings.Index(sig.Label[min(from, len(sig.Label)):], name); at >= 0 {
					start := from + at
					p.Start, p.End = utf16Len(sig.Label[:start]), utf16Len(sig.Label[:start+len(name)])
					from = start + len(name)
				}
			}
			if p.Start < 0 || p.End < p.Start || p.End > utf16Len(sig.Label) {
				p.Start, p.End = 0, 0
			}
			sig.Parameters = append(sig.Parameters, p)
		}
		out.Signatures = append(out.Signatures, sig)
		if i == w.ActiveSignature && ws.ActiveParameter != nil {
			out.ActiveParameter = *ws.ActiveParameter
		}
	}
	if w.ActiveSignature >= 0 && w.ActiveSignature < len(out.Signatures) {
		out.ActiveSignature = w.ActiveSignature
	}
	if w.ActiveParameter != nil && (w.ActiveSignature >= len(w.Signatures) || w.Signatures[max(w.ActiveSignature, 0)].ActiveParameter == nil) {
		out.ActiveParameter = *w.ActiveParameter
	}
	if out.ActiveParameter < 0 {
		out.ActiveParameter = 0
	}
	return out
}

func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// clip cuts s to at most n bytes at a rune boundary.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func isNull(raw json.RawMessage) bool { return len(raw) == 0 || string(raw) == "null" }

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
