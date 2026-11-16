package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"flint/internal/shellenv"
)

// Server states reported to the UI.
const (
	StateStarting    = "starting"
	StateReady       = "ready"
	StateUnavailable = "unavailable" // no server installed for the language
	StateFailed      = "failed"
)

// Status describes a language server for the status bar.
type Status struct {
	Language string `json:"language"`
	Root     string `json:"root"`
	State    string `json:"state"`
	Message  string `json:"message"`
}

type key struct{ root, language string }

type entry struct {
	ready chan struct{} // closed once srv or err is set
	srv   *server
	err   error
}

type document struct {
	key        key
	languageID string
	version    int
	text       string
	opened     bool // didOpen sent to the current server process
}

// Manager starts servers lazily (on the first opened file of a language) and keeps
// open documents in sync with them. Servers start in the background, so opening a
// file never waits for indexing.
type Manager struct {
	OnDiagnostics func(path string, diags []Diagnostic)
	OnStatus      func(Status)
	// OnApplyEdit receives an edit a language server asks Flint to apply on
	// its own (not as the answer to a request). Nil refuses such edits.
	OnApplyEdit func(WorkspaceEdit)
	// Trusted reports whether a workspace folder is trusted; servers that run
	// the project's code (needsTrust) start only in trusted folders. Nil
	// trusts everything (tests).
	Trusted func(root string) bool
	// Config returns the user's settings for a language's server; it is read
	// on every start, and Reconfigure applies a change. Nil uses the defaults.
	Config func(language string) ServerConfig

	lookPath func(string) (string, error)

	mu       sync.Mutex
	servers  map[key]*entry
	docs     map[string]*document
	restarts map[key]int // crash restarts per server, to stop a crash loop
	// actions are the code actions last listed per document, by id.
	actions    map[string]map[int]storedAction
	nextAction int
	closed     bool
	stopping   sync.WaitGroup // servers Reconfigure stops, so Shutdown waits for them too
}

// maxRestarts bounds automatic restarts of a crashing server.
const maxRestarts = 3

// NewManager creates a manager; callbacks may be nil.
func NewManager() *Manager {
	m := &Manager{
		lookPath: defaultLookPath, servers: map[key]*entry{}, docs: map[string]*document{}, restarts: map[key]int{},
	}
	// An extension installed, updated, turned off or removed: its languages'
	// servers restart (or stop) for the documents open in them.
	onExtensionLanguagesChanged(func(languages []string) {
		for _, l := range languages {
			m.Reconfigure(l)
		}
	})
	return m
}

// Supported reports whether Flint knows a language server for path.
func Supported(path string) bool {
	_, _, ok := languageFor(path)
	return ok
}

// Open registers a document and starts its language server if needed.
func (m *Manager) Open(root, path, text string) Status {
	language, languageID, ok := languageFor(path)
	if !ok {
		return Status{State: StateUnavailable, Message: "Flint has no language server for this file type, so there's no completion or go to definition here. An extension can add one: see Settings ▸ Extensions ▸ Repository"}
	}
	k := key{root: root, language: language}
	m.mu.Lock()
	doc := &document{key: k, languageID: languageID, version: 1, text: text}
	if old, exists := m.docs[path]; exists {
		doc.version = old.version + 1
		doc.opened = old.opened && old.key == k
	}
	m.docs[path] = doc
	e := m.ensureServerLocked(k)
	m.mu.Unlock()

	select {
	case <-e.ready:
		if e.err != nil {
			return m.statusFor(k, e)
		}
		m.mu.Lock()
		m.openDocLocked(e.srv, path, doc)
		m.mu.Unlock()
		return Status{Language: language, Root: root, State: StateReady}
	default:
		return Status{Language: language, Root: root, State: StateStarting}
	}
}

// Change replaces a document's text (full sync; simple and fast enough for source files).
func (m *Manager) Change(path, text string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	doc := m.docs[path]
	if doc == nil {
		return
	}
	doc.version++
	doc.text = text
	if e := m.servers[doc.key]; e != nil && doc.opened && e.srv != nil {
		e.srv.out.notifyLatest("change:"+path, "textDocument/didChange", map[string]any{
			"textDocument":   map[string]any{"uri": PathToURI(path), "version": doc.version},
			"contentChanges": []map[string]any{{"text": text}},
		})
	}
}

// Saved tells the server the file was written (some servers lint on save).
func (m *Manager) Saved(path string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	doc := m.docs[path]
	if doc == nil || !doc.opened {
		return
	}
	if e := m.servers[doc.key]; e != nil && e.srv != nil {
		e.srv.out.notify("textDocument/didSave", map[string]any{"textDocument": map[string]any{"uri": PathToURI(path)}})
	}
}

// Close forgets a document.
func (m *Manager) Close(path string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	doc := m.docs[path]
	delete(m.docs, path)
	delete(m.actions, path)
	if doc == nil || !doc.opened {
		return
	}
	if e := m.servers[doc.key]; e != nil && e.srv != nil {
		e.srv.out.notify("textDocument/didClose", map[string]any{"textDocument": map[string]any{"uri": PathToURI(path)}})
	}
}

// Completion returns completion items at pos.
func (m *Manager) Completion(ctx context.Context, path string, pos Position) (CompletionList, error) {
	var raw json.RawMessage
	if err := m.request(ctx, path, "textDocument/completion", positionParams(path, pos), &raw); err != nil {
		return CompletionList{Items: []CompletionItem{}}, err
	}
	return decodeCompletion(raw), nil
}

// Hover returns hover documentation (markdown) at pos.
func (m *Manager) Hover(ctx context.Context, path string, pos Position) (string, error) {
	var res struct {
		Contents json.RawMessage `json:"contents"`
	}
	if err := m.request(ctx, path, "textDocument/hover", positionParams(path, pos), &res); err != nil {
		return "", err
	}
	return markupText(res.Contents), nil
}

// Definition returns where the symbol at pos is defined.
func (m *Manager) Definition(ctx context.Context, path string, pos Position) ([]Location, error) {
	var raw json.RawMessage
	if err := m.request(ctx, path, "textDocument/definition", positionParams(path, pos), &raw); err != nil {
		return []Location{}, err
	}
	return m.withPreviews(decodeLocations(raw)), nil
}

// References returns usages of the symbol at pos (declaration excluded).
func (m *Manager) References(ctx context.Context, path string, pos Position) ([]Location, error) {
	params := positionParams(path, pos)
	params["context"] = map[string]any{"includeDeclaration": false}
	var raw json.RawMessage
	if err := m.request(ctx, path, "textDocument/references", params, &raw); err != nil {
		return []Location{}, err
	}
	return m.withPreviews(decodeLocations(raw)), nil
}

// Statuses lists every server Flint started or tried to start.
func (m *Manager) Statuses() []Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Status, 0, len(m.servers))
	for k, e := range m.servers {
		out = append(out, m.statusFor(k, e))
	}
	return out
}

// Shutdown stops all servers.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	m.closed = true // servers still starting are killed by start() when they finish
	servers := m.servers
	m.servers = map[key]*entry{}
	m.mu.Unlock()
	var wg sync.WaitGroup
	for _, e := range servers {
		select {
		case <-e.ready:
			if e.srv != nil {
				wg.Add(1)
				go func(s *server) { defer wg.Done(); s.shutdown() }(e.srv)
			}
		default:
		}
	}
	wg.Wait()
	m.stopping.Wait()
}

// request waits for the document's server and performs a call.
func (m *Manager) request(ctx context.Context, path, method string, params, out any) error {
	m.mu.Lock()
	doc := m.docs[path]
	if doc == nil {
		m.mu.Unlock()
		return fmt.Errorf("%s is not open", path)
	}
	e := m.ensureServerLocked(doc.key)
	m.mu.Unlock()

	select {
	case <-e.ready:
	case <-ctx.Done():
		return ctx.Err()
	}
	if e.err != nil {
		return e.err
	}
	m.mu.Lock()
	if doc := m.docs[path]; doc != nil {
		m.openDocLocked(e.srv, path, doc)
	}
	m.mu.Unlock()
	// Edits queued before this request must reach the server first.
	if err := e.srv.out.barrier(ctx); err != nil {
		return err
	}
	err := e.srv.conn.Call(ctx, method, params, out)
	if errors.Is(err, ErrClosed) {
		return fmt.Errorf("the %s language server stopped while answering; Flint restarts it, so try again in a moment%s", displayName(e.srv.language), e.srv.stderrTail())
	}
	return err
}

// ensureServerLocked returns the entry for k, (re)starting a dead or missing server.
func (m *Manager) ensureServerLocked(k key) *entry {
	if m.closed {
		return failedEntry(ErrClosed)
	}
	if e := m.servers[k]; e != nil {
		select {
		case <-e.ready:
			if e.srv == nil || e.srv.alive() {
				return e // ready, or failed permanently (not installed / init error)
			}
			// Crashed: restart (a bounded number of times) and re-open its documents.
			if m.restarts[k] >= maxRestarts {
				e.srv.kill()
				failed := failedEntry(fmt.Errorf("the %s language server crashed %d times in a row, so Flint stopped restarting it. Check that it runs from a terminal (or update it), then reopen the folder to try again%s", displayName(k.language), maxRestarts+1, e.srv.stderrTail()))
				m.servers[k] = failed
				return failed
			}
			m.restarts[k]++
			e.srv.kill() // helpers it spawned (tsserver…) would otherwise outlive it
			for _, d := range m.docs {
				if d.key == k {
					d.opened = false
				}
			}
		default:
			return e // still starting
		}
	}
	e := &entry{ready: make(chan struct{})}
	m.servers[k] = e
	m.emit(Status{Language: k.language, Root: k.root, State: StateStarting})
	go m.start(k, e)
	return e
}

func (m *Manager) start(k key, e *entry) {
	shellenv.Wait() // servers live on the login-shell PATH (~/go/bin, ~/.cargo/bin, ...)
	var srv *server
	argv, err := m.commandFor(k)
	if err == nil {
		srv, err = startServer(context.Background(), k.language, k.root, argv, m.diagnostics, m.applyEdit)
	}

	m.mu.Lock()
	// Reconfigure replaced the entry while the server was booting: its
	// documents belong to the new server.
	stale := !m.closed && m.servers[k] != e
	if (m.closed || stale) && srv != nil {
		// Flint quit while the server was booting: don't leave it running.
		m.mu.Unlock()
		srv.kill()
		srv, err = nil, ErrClosed
		m.mu.Lock()
	}
	e.srv, e.err = srv, err
	if stale {
		close(e.ready)
		m.mu.Unlock()
		return
	}
	if srv != nil {
		for path, d := range m.docs {
			if d.key == k {
				m.openDocLocked(srv, path, d)
			}
		}
	}
	close(e.ready)
	status := m.statusFor(k, e)
	m.mu.Unlock()
	m.emit(status)
}

// commandFor is the command line of k's server under the user's settings and
// Restricted Mode: servers that run the project's code (needsTrust) and
// servers Flint doesn't know stay off in untrusted folders, and the extra
// arguments apply only in trusted ones.
func (m *Manager) commandFor(k key) ([]string, error) {
	spec, _ := specFor(k.language)
	var cfg ServerConfig
	if m.Config != nil {
		cfg = m.Config(k.language)
	}
	if cfg.Disabled {
		return nil, fmt.Errorf("the %s %w. Turn it on there to get completion and go to definition", displayName(k.language), ErrDisabled)
	}
	trusted := m.Trusted == nil || m.Trusted(k.root)
	argv, custom, ok := spec.command(cfg, m.lookPath)
	reason, gated := needsTrust[k.language]
	if _, builtin := builtinSpec(k.language); !builtin {
		reason, gated = extensionTrustReason, true
	}
	if custom && !gated {
		reason, gated = customServerReason, true
	}
	name := displayName(k.language)
	switch {
	case gated && !trusted:
		return nil, fmt.Errorf("%w: %s. To get completion and go to definition for %s, run Trust Folder from the command palette (or click Trust Folder in the Restricted Mode banner)", ErrRestricted, reason, name)
	case !ok && cfg.Command != "":
		return nil, notInstalled(fmt.Sprintf("the %s language server set in Settings ▸ Language Servers (%q) wasn't found. Fix the path there, or clear it to use an installed default server", name, cfg.Command))
	case !ok && spec.Install == "":
		return nil, notInstalled(fmt.Sprintf("completion and go to definition for %s need a language server, and none is installed. Install the one its extension names (see Settings ▸ Extensions), then reopen the file", name))
	case !ok:
		return nil, notInstalled(fmt.Sprintf("completion and go to definition for %s need a language server, and none is installed. Install one with: %s — then reopen the file", name, spec.Install))
	case !trusted:
		return argv, nil
	}
	if err := CheckArgs(cfg.Args); err != nil {
		return nil, fmt.Errorf("the %s language server arguments in Settings ▸ Language Servers are invalid (%w). Fix them there, then reopen the file", name, err)
	}
	return append(argv, cfg.Args...), nil
}

// Reconfigure restarts language's servers after its settings changed, for
// the documents open in them; a disabled server just stops.
func (m *Manager) Reconfigure(language string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	for k, e := range m.servers {
		if k.language != language {
			continue
		}
		delete(m.servers, k)
		delete(m.restarts, k)
		select {
		case <-e.ready:
			if e.srv != nil {
				m.stopping.Add(1)
				go func(s *server) { defer m.stopping.Done(); s.shutdown() }(e.srv)
			}
		default:
			// Still starting: start() sees the entry was replaced and stops it.
		}
	}
	for _, d := range m.docs {
		if d.key.language == language {
			d.opened = false
		}
	}
	for _, d := range m.docs {
		if d.key.language == language {
			if _, running := m.servers[d.key]; !running {
				m.ensureServerLocked(d.key)
			}
		}
	}
}

func failedEntry(err error) *entry {
	e := &entry{ready: make(chan struct{}), err: err}
	close(e.ready)
	return e
}

func (m *Manager) openDocLocked(srv *server, path string, d *document) {
	if d.opened {
		return
	}
	d.opened = true
	srv.out.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri": PathToURI(path), "languageId": d.languageID, "version": d.version, "text": d.text,
		},
	})
}

func (m *Manager) statusFor(k key, e *entry) Status {
	st := Status{Language: k.language, Root: k.root}
	select {
	case <-e.ready:
	default:
		st.State = StateStarting
		return st
	}
	switch {
	case e.err != nil && (errors.Is(e.err, ErrNotInstalled) || errors.Is(e.err, ErrDisabled)):
		st.State, st.Message = StateUnavailable, e.err.Error()
	case e.err != nil:
		st.State, st.Message = StateFailed, e.err.Error()
	case !e.srv.alive():
		st.State, st.Message = StateFailed, "server exited"+e.srv.stderrTail()
	default:
		st.State = StateReady
	}
	return st
}

func (m *Manager) diagnostics(path string, diags []Diagnostic) {
	if m.OnDiagnostics != nil {
		m.OnDiagnostics(path, diags)
	}
}

func (m *Manager) emit(s Status) {
	if m.OnStatus != nil {
		m.OnStatus(s)
	}
}

// withPreviews fills each location's Preview with the text of its first line.
func (m *Manager) withPreviews(locs []Location) []Location {
	if locs == nil {
		return []Location{}
	}
	cache := map[string][]string{}
	for i := range locs {
		lines, ok := cache[locs[i].Path]
		if !ok {
			lines = m.fileLines(locs[i].Path)
			cache[locs[i].Path] = lines
		}
		if l := locs[i].Range.Start.Line; l >= 0 && l < len(lines) {
			locs[i].Preview = strings.TrimSpace(lines[l])
		}
	}
	return locs
}

func (m *Manager) fileLines(path string) []string {
	m.mu.Lock()
	doc := m.docs[path]
	var text string
	if doc != nil {
		text = doc.text // unsaved buffer wins
	}
	m.mu.Unlock()
	if doc == nil {
		data, err := os.ReadFile(path)
		if err != nil || len(data) > 10<<20 {
			return nil
		}
		text = string(data)
	}
	return strings.Split(text, "\n")
}

func positionParams(path string, pos Position) map[string]any {
	return map[string]any{
		"textDocument": map[string]any{"uri": PathToURI(path)},
		"position":     pos,
	}
}

func decodeCompletion(raw json.RawMessage) CompletionList {
	list := CompletionList{Items: []CompletionItem{}}
	if len(raw) == 0 || string(raw) == "null" {
		return list
	}
	var items []wireCompletionItem
	if raw[0] == '{' {
		var wl struct {
			IsIncomplete bool                 `json:"isIncomplete"`
			Items        []wireCompletionItem `json:"items"`
		}
		if json.Unmarshal(raw, &wl) != nil {
			return list
		}
		list.Incomplete, items = wl.IsIncomplete, wl.Items
	} else if json.Unmarshal(raw, &items) != nil {
		return list
	}
	for _, it := range items {
		list.Items = append(list.Items, it.toItem())
	}
	return list
}

// Retrust starts the servers that Restricted Mode held back in root, now
// that the folder is trusted, for the documents already open there.
func (m *Manager) Retrust(root string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, e := range m.servers {
		if k.root == root && errors.Is(e.err, ErrRestricted) {
			delete(m.servers, k)
		}
	}
	for _, d := range m.docs {
		if d.key.root == root {
			if _, running := m.servers[d.key]; !running {
				m.ensureServerLocked(d.key)
			}
		}
	}
}
