package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"flint/internal/extensions/manifest"
)

// Editor decorations (docs/design/extensions.md, "Editor decorations"). The
// frontend tells the host which documents are open in editors (EditorOpened,
// EditorChanged, EditorClosed); the host passes that on, as editor/opened,
// editor/changed and editor/closed, to each extension that has a decoration
// set for the file which is on, starting it when needed. The extension
// answers with editor/setDecorations, which the host checks, bounds and hands
// to the frontend, and with text edits when the user picks a quick fix
// (editor/action).
//
// An extension hears only of files it could read with workspace/readFile:
// inside a trusted folder, not in .git, not a credential file. The text of
// unsaved edits goes only to sets that declare "unsavedText".

// Events for editor decorations.
const (
	// EventDecorations carries a DecorationsEvent.
	EventDecorations = "extensions:decorations"
	// EventDecorationsCleared carries a ClearDecorationsEvent.
	EventDecorationsCleared = "extensions:decorationsCleared"
)

// Limits on editor decorations.
const (
	// maxDecorations per set and file; more are cut.
	maxDecorations  = 5000
	maxInlineText   = 200
	maxDecorationID = 256
	// maxEditorText is the largest document text passed to an extension with
	// editor/opened and editor/changed; a larger document is announced
	// without its text.
	maxEditorText = 1 << 20
	// maxDocuments bounds the open documents the host keeps track of.
	maxDocuments = 500
	// maxEdits and maxEditText bound a quick fix's answer.
	maxEdits    = 1000
	maxEditText = 1 << 20
)

// Decoration kinds and severities an extension may use.
var (
	DecorationKinds      = []string{"diagnostic", "gutter", "inline", "highlight"}
	decorationSeverities = []string{"info", "ok", "warning", "error"}
)

// Decoration is one mark in an editor, as the frontend draws it. Lines are
// 1-based; From and To are columns in UTF-16 code units, as in LSP.
type Decoration struct {
	// Kind is one of DecorationKinds.
	Kind string `json:"kind"`
	// ID is the extension's own name for it, sent back with a quick fix.
	ID   string `json:"id,omitempty"`
	Line int    `json:"line"`
	// EndLine is the last line of a range (Line when it is one line).
	EndLine int `json:"endLine"`
	From    int `json:"from"`
	// To is the end column on EndLine; -1 is the end of that line.
	To int `json:"to"`
	// Severity is info, ok, warning or error: the mark's color.
	Severity string `json:"severity"`
	// Message is the hover text (a diagnostic's message).
	Message string `json:"message,omitempty"`
	// Text is an inline decoration's text after the line.
	Text string `json:"text,omitempty"`
	// Icon names a gutter marker's glyph; unknown names show a dot.
	Icon string `json:"icon,omitempty"`
	// Command runs when an inline decoration is clicked.
	Command *CommandRef `json:"command,omitempty"`
	// Actions are its quick fixes.
	Actions []DecorationAction `json:"actions,omitempty"`
}

// DecorationAction is a quick fix: choosing it sends editor/action, or runs
// Command when it names one of the extension's commands.
type DecorationAction struct {
	ID      string      `json:"id"`
	Label   string      `json:"label"`
	Command *CommandRef `json:"command,omitempty"`
}

// DecorationsEvent is EventDecorations's data: what a set shows in one file,
// replacing what it showed before.
type DecorationsEvent struct {
	Extension string `json:"extension"`
	Set       string `json:"set"`
	Path      string `json:"path"`
	// Version is the document version the decorations were computed for
	// (0: the extension didn't say).
	Version     int          `json:"version"`
	Decorations []Decoration `json:"decorations"`
	// Truncated: the extension sent more than maxDecorations.
	Truncated bool `json:"truncated,omitempty"`
}

// ClearDecorationsEvent is EventDecorationsCleared's data: remove the
// decorations of Set ("": every set of the extension) in Path ("": every file).
type ClearDecorationsEvent struct {
	Extension string `json:"extension"`
	Set       string `json:"set,omitempty"`
	Path      string `json:"path,omitempty"`
}

// DocumentInterest says whether any extension wants to hear about a document.
type DocumentInterest struct {
	// Tracked: an enabled extension has a decoration set, turned on, for the file.
	Tracked bool `json:"tracked"`
	// Text: one of those sets declared unsavedText, so the editor's text is
	// passed along with each change.
	Text bool `json:"text"`
}

// TextEdit replaces the text from (Line, From) to (EndLine, To) with Text;
// positions as in Decoration.
type TextEdit struct {
	Line    int    `json:"line"`
	From    int    `json:"from"`
	EndLine int    `json:"endLine"`
	To      int    `json:"to"`
	Text    string `json:"text"`
}

// ActionRequest names the quick fix the user chose and where its decoration
// is now (it moves with the user's edits).
type ActionRequest struct {
	Set string `json:"set"`
	// Path and Version are the document and its version when the user chose.
	Path    string `json:"path"`
	Version int    `json:"version"`
	Action  string `json:"action"`
	// Decoration is the decoration's id, when the extension gave it one.
	Decoration string `json:"decoration,omitempty"`
	Line       int    `json:"line"`
	EndLine    int    `json:"endLine"`
	From       int    `json:"from"`
	To         int    `json:"to"`
}

// ActionResult is a quick fix's answer: edits for the document at Version,
// which the editor applies only if that is still its version.
type ActionResult struct {
	Version int        `json:"version"`
	Edits   []TextEdit `json:"edits"`
}

// document is a file open in an editor, as the frontend last described it.
type document struct {
	language string
	version  int
	text     string
	hasText  bool
}

// language is Flint's language id of path ("" when it has none).
func (h *Host) language(path string) string {
	if h.o.LanguageOf == nil {
		return ""
	}
	return h.o.LanguageOf(path)
}

// interest returns the extension's decoration sets that are on and match the
// file, and whether one of them is to get unsaved text. It has none for a
// file the extension could not read with workspace/readFile.
func (e *ext) interest(path, language string) (sets []string, text bool) {
	if e.m == nil || len(e.m.Contributes.DecorationSets) == 0 || !e.h.o.App.Enabled(e.id) || !e.m.HasPermission(manifest.PermWorkspaceRead) {
		return nil, false
	}
	for _, s := range e.m.Contributes.DecorationSets {
		if e.h.o.App.SetEnabled(s.ID, s.On()) && s.Matches(path, language) {
			sets = append(sets, s.ID)
			text = text || s.UnsavedText
		}
	}
	if len(sets) == 0 {
		return nil, false
	}
	real, err := e.workspaceFile(path)
	if err != nil || readable(path) != nil || readable(real) != nil {
		return nil, false
	}
	return sets, text
}

// EditorInterest reports whether an extension would be told about the
// document at path, without starting anything.
func (h *Host) EditorInterest(path string) DocumentInterest {
	h.ensureDiscovered()
	var out DocumentInterest
	language := h.language(path)
	for _, e := range h.all() {
		sets, text := e.interest(path, language)
		out.Tracked = out.Tracked || len(sets) > 0
		out.Text = out.Text || text
	}
	return out
}

// EditorOpened records that path is open in an editor at version (text is
// its content when hasText) and tells the interested extensions, starting
// them. Calling it again for an open document updates it.
func (h *Host) EditorOpened(path string, version int, text string, hasText bool) error {
	h.ensureDiscovered()
	if path == "" || len(path) > 4096 {
		return errors.New("invalid document path")
	}
	doc := newDocument(h.language(path), version, text, hasText)
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil
	}
	if _, ok := h.docs[path]; !ok && len(h.docs) >= maxDocuments {
		h.mu.Unlock()
		return fmt.Errorf("more than %d open documents", maxDocuments)
	}
	h.docs[path] = doc
	h.mu.Unlock()
	for _, e := range h.all() {
		e.syncDocument(path, doc)
	}
	return nil
}

func newDocument(language string, version int, text string, hasText bool) *document {
	if !hasText || len(text) > maxEditorText || !utf8.ValidString(text) {
		text, hasText = "", false
	}
	return &document{language: language, version: version, text: text, hasText: hasText}
}

// EditorChanged records an open document's new version (and text) and tells
// the extensions that follow it. The frontend debounces these.
func (h *Host) EditorChanged(path string, version int, text string, hasText bool) {
	h.mu.Lock()
	old := h.docs[path]
	if old == nil || h.closed {
		h.mu.Unlock()
		return
	}
	doc := newDocument(old.language, version, text, hasText)
	h.docs[path] = doc
	h.mu.Unlock()
	for _, e := range h.all() {
		e.mu.Lock()
		key, follows := e.docs[path]
		p := e.procLocked()
		e.mu.Unlock()
		if follows && p != nil {
			_ = p.conn.Notify("editor/changed", documentParams(path, doc, key, false))
		}
	}
}

// EditorClosed forgets an open document and tells the extensions that
// followed it.
func (h *Host) EditorClosed(path string) {
	h.mu.Lock()
	delete(h.docs, path)
	h.mu.Unlock()
	for _, e := range h.all() {
		e.syncDocument(path, nil)
	}
}

// syncDocuments brings every extension in line with the open documents,
// after something that changes who is interested: settings, trusted folders,
// the installed extensions.
func (h *Host) syncDocuments() {
	h.mu.Lock()
	docs := make(map[string]*document, len(h.docs))
	for path, doc := range h.docs {
		docs[path] = doc
	}
	h.mu.Unlock()
	for _, e := range h.all() {
		for _, path := range e.followed() {
			if docs[path] == nil {
				e.syncDocument(path, nil)
			}
		}
		for path, doc := range docs {
			e.syncDocument(path, doc)
		}
	}
}

// followed are the documents the extension was told are open.
func (e *ext) followed() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]string, 0, len(e.docs))
	for path := range e.docs {
		out = append(out, path)
	}
	slices.Sort(out)
	return out
}

// procLocked is the live process, if any; the caller holds e.mu.
func (e *ext) procLocked() *proc {
	if e.starting != nil {
		return nil
	}
	return e.proc
}

// documentKey is what the extension was last told about a document: the
// sets that apply, and whether it gets the text.
func documentKey(sets []string, text bool) string {
	key := strings.Join(sets, ",")
	if text {
		key += "|text"
	}
	return key
}

// documentParams are editor/opened's (opened) or editor/changed's params.
func documentParams(path string, doc *document, key string, opened bool) map[string]any {
	params := map[string]any{"path": path, "version": doc.version}
	sets, text := strings.CutSuffix(key, "|text")
	if opened {
		params["language"] = doc.language
		params["sets"] = strings.Split(sets, ",")
	}
	if text && doc.hasText {
		params["text"] = doc.text
	}
	return params
}

// syncDocument tells the extension that the document at path is open (doc)
// or closed (nil), if that is news to it: editor/opened when one of its sets
// starts to apply (again when the sets that apply change), editor/closed
// when none does any more, which also removes its decorations there. A
// stopped extension is started for a document it should see: start
// announces every document it follows.
func (e *ext) syncDocument(path string, doc *document) {
	var sets []string
	var text bool
	if doc != nil {
		sets, text = e.interest(path, doc.language)
	}
	key := documentKey(sets, text)
	e.mu.Lock()
	had, followed := e.docs[path]
	if len(sets) == 0 {
		delete(e.docs, path)
	} else {
		e.docs[path] = key
	}
	p := e.procLocked()
	e.mu.Unlock()
	switch {
	case len(sets) == 0 && followed:
		if p != nil {
			_ = p.conn.Notify("editor/closed", map[string]string{"path": path})
		}
		e.h.o.App.Emit(EventDecorationsCleared, ClearDecorationsEvent{Extension: e.id, Path: path})
		e.touch()
	case len(sets) > 0 && (!followed || had != key):
		if p != nil {
			_ = p.conn.Notify("editor/opened", documentParams(path, doc, key, true))
			return
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), e.h.o.InitTimeout+time.Second)
			defer cancel()
			_, _ = e.session(ctx)
		}()
	}
}

// announceDocuments sends editor/opened for the documents in keys (path →
// key, read when the process became the running one) to a process that just
// started.
func (e *ext) announceDocuments(p *proc, keys map[string]string) {
	for path, key := range keys {
		e.h.mu.Lock()
		doc := e.h.docs[path]
		e.h.mu.Unlock()
		if doc != nil {
			_ = p.conn.Notify("editor/opened", documentParams(path, doc, key, true))
		}
	}
}

// followsSet reports whether the extension was told path is open with set
// among the sets that apply.
func (e *ext) followsSet(path, set string) bool {
	e.mu.Lock()
	key, ok := e.docs[path]
	e.mu.Unlock()
	if !ok {
		return false
	}
	sets, _ := strings.CutSuffix(key, "|text")
	return slices.Contains(strings.Split(sets, ","), set)
}

// wireDecoration is a decoration as an extension sends it.
type wireDecoration struct {
	Kind     string             `json:"kind"`
	ID       string             `json:"id"`
	Line     int                `json:"line"`
	EndLine  int                `json:"endLine"`
	From     *int               `json:"from"`
	To       *int               `json:"to"`
	Severity string             `json:"severity"`
	Message  string             `json:"message"`
	Hover    string             `json:"hover"`
	Text     string             `json:"text"`
	Icon     string             `json:"icon"`
	Command  *CommandRef        `json:"command"`
	Actions  []DecorationAction `json:"actions"`
}

// setDecorations handles editor/setDecorations: the set's decorations in one
// open document, replacing the earlier ones. A publish for a set that is
// off, for a file the extension doesn't follow or for a version older than
// the editor's is dropped.
func (e *ext) setDecorations(params json.RawMessage) error {
	if err := e.need(manifest.PermWorkspaceRead); err != nil {
		return err
	}
	var p struct {
		Set         string           `json:"set"`
		Path        string           `json:"path"`
		Version     *int             `json:"version"`
		Decorations []wireDecoration `json:"decorations"`
	}
	if err := decode(params, &p); err != nil {
		return err
	}
	if _, ok := e.m.DecorationSet(p.Set); !ok {
		return invalid("no decoration set %q in the manifest", cleanText(p.Set, 100))
	}
	if !e.followsSet(p.Path, p.Set) {
		return fmt.Errorf("dropped: %s is not an open document of %s", cleanText(p.Path, 300), p.Set)
	}
	e.h.mu.Lock()
	doc := e.h.docs[p.Path]
	e.h.mu.Unlock()
	if doc == nil {
		return nil // closed meanwhile
	}
	version := 0
	if p.Version != nil {
		if version = *p.Version; version < doc.version {
			return nil // stale: the document changed since
		}
	}
	list, truncated := cleanDecorations(p.Decorations, e.m)
	e.h.o.App.Emit(EventDecorations, DecorationsEvent{Extension: e.id, Set: p.Set, Path: p.Path, Version: version, Decorations: list, Truncated: truncated})
	return nil
}

// clearDecorations handles editor/clearDecorations: a set's decorations in
// one file, or in every file.
func (e *ext) clearDecorations(params json.RawMessage) error {
	var p struct {
		Set  string `json:"set"`
		Path string `json:"path"`
	}
	if err := decode(params, &p); err != nil {
		return err
	}
	if _, ok := e.m.DecorationSet(p.Set); !ok {
		return invalid("no decoration set %q in the manifest", cleanText(p.Set, 100))
	}
	if len(p.Path) > 4096 {
		return invalid("invalid path")
	}
	e.h.o.App.Emit(EventDecorationsCleared, ClearDecorationsEvent{Extension: e.id, Set: p.Set, Path: p.Path})
	return nil
}

// cleanDecorations makes what an extension sent safe to draw: known kinds
// and severities, plain text cut to its limits, the extension's own commands
// only, at most maxDecorations.
func cleanDecorations(list []wireDecoration, m *manifest.Manifest) ([]Decoration, bool) {
	out := []Decoration{}
	for _, w := range list {
		if len(out) == maxDecorations {
			return out, true
		}
		if !slices.Contains(DecorationKinds, w.Kind) || w.Line < 1 {
			continue
		}
		d := Decoration{
			Kind: w.Kind, ID: cleanText(w.ID, maxDecorationID), Line: w.Line, EndLine: max(w.EndLine, w.Line), To: -1,
			Severity: w.Severity, Message: cleanMultiline(w.Message, maxLabel), Actions: cleanDecorationActions(w.Actions, m),
		}
		if d.Message == "" {
			d.Message = cleanMultiline(w.Hover, maxLabel)
		}
		if w.From != nil {
			d.From = max(*w.From, 0)
		}
		if w.To != nil {
			d.To = max(*w.To, 0)
			if d.EndLine == d.Line {
				d.To = max(d.To, d.From)
			}
		}
		if !slices.Contains(decorationSeverities, d.Severity) {
			d.Severity = "info"
		}
		switch w.Kind {
		case "diagnostic":
			if d.Message == "" {
				continue
			}
		case "gutter":
			if iconName.MatchString(w.Icon) {
				d.Icon = w.Icon
			}
		case "inline":
			if d.Text = cleanText(w.Text, maxInlineText); d.Text == "" {
				continue
			}
			d.Command = cleanCommand(w.Command, m)
		}
		out = append(out, d)
	}
	return out, false
}

// cleanMultiline is cleanText for hover text, which may have several lines.
func cleanMultiline(s string, limit int) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	for i, l := range lines {
		lines[i] = cleanText(l, limit)
	}
	out := strings.TrimSpace(strings.Join(lines, "\n"))
	if utf8.RuneCountInString(out) > limit {
		out = string([]rune(out)[:limit-1]) + "…"
	}
	return out
}

func cleanDecorationActions(list []DecorationAction, m *manifest.Manifest) []DecorationAction {
	var out []DecorationAction
	for _, a := range list {
		if len(out) == maxItemActions {
			break
		}
		a.ID, a.Label, a.Command = cleanText(a.ID, 64), cleanText(a.Label, 60), cleanCommand(a.Command, m)
		if a.ID != "" && a.Label != "" {
			out = append(out, a)
		}
	}
	return out
}

// EditorAction sends the quick fix the user chose to the extension
// (editor/action) and returns its edits, checked and bounded. The editor
// applies them only if the document is still at the request's version.
func (h *Host) EditorAction(ctx context.Context, id string, req ActionRequest) (ActionResult, error) {
	e, err := h.get(id)
	if err != nil {
		return ActionResult{}, err
	}
	if e.m == nil {
		return ActionResult{}, e.usable()
	}
	if _, ok := e.m.DecorationSet(req.Set); !ok {
		return ActionResult{}, fmt.Errorf("%s has no decoration set %q", e.name(), req.Set)
	}
	if !e.followsSet(req.Path, req.Set) {
		return ActionResult{}, fmt.Errorf("%s does not decorate this file", e.name())
	}
	req.Action, req.Decoration = cleanText(req.Action, 64), cleanText(req.Decoration, maxDecorationID)
	if req.Action == "" {
		return ActionResult{}, errors.New("no action")
	}
	var res struct {
		Edits []struct {
			Line    int    `json:"line"`
			From    int    `json:"from"`
			EndLine int    `json:"endLine"`
			To      *int   `json:"to"`
			Text    string `json:"text"`
		} `json:"edits"`
	}
	if err := e.call(ctx, h.o.CallTimeout, "editor/action", req, &res); err != nil {
		return ActionResult{}, err
	}
	out := ActionResult{Version: req.Version, Edits: []TextEdit{}}
	if len(res.Edits) > maxEdits {
		return ActionResult{}, fmt.Errorf("%s answered with more than %d edits", e.name(), maxEdits)
	}
	size := 0
	for _, ed := range res.Edits {
		edit := TextEdit{Line: ed.Line, From: ed.From, EndLine: max(ed.EndLine, ed.Line), To: ed.From, Text: ed.Text}
		if ed.To != nil {
			edit.To = *ed.To
		}
		size += len(ed.Text)
		switch {
		case edit.Line < 1 || edit.From < 0 || edit.To < 0 || (edit.EndLine == edit.Line && edit.To < edit.From):
			return ActionResult{}, fmt.Errorf("%s answered with an edit at an invalid position", e.name())
		case !utf8.ValidString(ed.Text) || strings.ContainsRune(ed.Text, 0):
			return ActionResult{}, fmt.Errorf("%s answered with an edit that is not text", e.name())
		case size > maxEditText:
			return ActionResult{}, fmt.Errorf("%s answered with more than %d MiB of edits", e.name(), maxEditText>>20)
		}
		out.Edits = append(out.Edits, edit)
	}
	return out, nil
}
