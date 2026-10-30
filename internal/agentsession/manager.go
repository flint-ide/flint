package agentsession

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
)

// Policy decides which permission requests are answered without asking the user.
// Flint's default: the agent edits and reads files freely (changes are reviewed
// afterwards via git), while commands, fetches and anything else still ask.
type Policy struct {
	AutoApproveEdits bool `json:"autoApproveEdits"`
	AutoApproveReads bool `json:"autoApproveReads"`
	// AutoApproveAll skips every question (the agent's own sandbox still applies).
	AutoApproveAll bool `json:"autoApproveAll"`
}

// DefaultPolicy matches Flint's "no accept/reject for edits" stance.
func DefaultPolicy() Policy { return Policy{AutoApproveEdits: true, AutoApproveReads: true} }

// autoApproves decides for one tool call in a session rooted at cwd. Edits and reads
// are only automatic for known paths inside the project, outside .git and agent
// config: writing ~/.zshrc, a launch agent or a git hook is code execution in
// disguise, and reading ~/.ssh is exfiltration, so those always ask.
func (p Policy) autoApproves(tc ToolCall, cwd string) bool {
	if p.AutoApproveAll {
		return true
	}
	switch tc.Kind {
	case ToolEdit, ToolMove, ToolDelete:
		return p.AutoApproveEdits && allInside(tc.Paths, cwd)
	case ToolRead, ToolSearch:
		return p.AutoApproveReads && allInside(tc.Paths, cwd)
	}
	return false
}

// protectedDirs are never touched without asking, even inside the project.
var protectedDirs = []string{".git", ".claude", ".codex", ".vscode", ".idea"}

// allInside reports whether paths is non-empty and every path stays inside cwd
// (after cleaning; relative paths resolve against cwd) and outside protectedDirs.
func allInside(paths []string, cwd string) bool {
	if len(paths) == 0 || cwd == "" {
		return false
	}
	root := filepath.Clean(cwd)
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}
	for _, p := range paths {
		if !filepath.IsAbs(p) {
			p = filepath.Join(cwd, p)
		}
		p = resolveExisting(filepath.Clean(p))
		rel, err := filepath.Rel(root, p)
		if err != nil || rel == ".." || strings.HasPrefix(rel, "../") || filepath.IsAbs(rel) {
			return false
		}
		first, _, _ := strings.Cut(filepath.ToSlash(rel), "/")
		if slices.Contains(protectedDirs, first) {
			return false
		}
	}
	return true
}

// Inside reports whether path (absolute, or relative to cwd) stays inside cwd after
// resolving symlinks. Unlike the auto-approve check it allows dot-directories.
func Inside(path, cwd string) bool {
	if cwd == "" {
		return false
	}
	root := filepath.Clean(cwd)
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	rel, err := filepath.Rel(root, resolveExisting(filepath.Clean(path)))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, "../") && !filepath.IsAbs(rel)
}

// resolveExisting resolves symlinks in the longest existing prefix of p, so a link
// inside the project pointing outside it is judged by its target.
func resolveExisting(p string) string {
	dir, rest := p, ""
	for {
		if real, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(real, rest)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return p
		}
		rest = filepath.Join(filepath.Base(dir), rest)
		dir = parent
	}
}

// ErrBusy is returned when a prompt is sent while the previous turn still runs.
var ErrBusy = errors.New("the agent is still working on the previous message")

// Manager routes sessions to drivers, applies the approval policy and runs turns in
// the background so the UI never blocks on an agent.
type Manager struct {
	emitRaw func(Event)

	seqMu sync.Mutex
	seq   map[string]uint64

	mu       sync.Mutex
	drivers  map[string]Driver
	sessions map[string]*managed
	policy   Policy
}

type managed struct {
	agent  string
	cwd    string
	driver Driver
	cancel context.CancelFunc // non-nil while a turn runs
}

// NewManager creates a manager that forwards events to emit.
func NewManager(emit func(Event)) *Manager {
	return &Manager{
		emitRaw: emit, seq: map[string]uint64{},
		drivers: map[string]Driver{}, sessions: map[string]*managed{}, policy: DefaultPolicy(),
	}
}

// emit stamps the per-session sequence number and forwards the event. Stamping and
// forwarding happen under one lock so sequence order equals emission order.
func (m *Manager) emit(e Event) {
	m.seqMu.Lock()
	defer m.seqMu.Unlock()
	m.seq[e.SessionID]++
	e.Seq = m.seq[e.SessionID]
	m.emitRaw(e)
}

// Register makes a driver available under name ("claude", "codex", ...).
func (m *Manager) Register(name string, d Driver) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.drivers[name] = d
}

// Agents lists registered driver names, sorted.
func (m *Manager) Agents() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	names := make([]string, 0, len(m.drivers))
	for n := range m.drivers {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Policy returns the current approval policy.
func (m *Manager) Policy() Policy {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.policy
}

// SetPolicy replaces the approval policy (applies to future requests).
func (m *Manager) SetPolicy(p Policy) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.policy = p
}

// ErrNoResume is returned by Resume for agents that cannot reopen a conversation.
var ErrNoResume = errors.New("this agent cannot resume earlier conversations")

// Start opens a session with the named agent.
func (m *Manager) Start(ctx context.Context, agent, cwd string) (SessionInfo, error) {
	return m.open(agent, cwd, func(d Driver, sink Sink) (SessionInfo, error) {
		return d.Start(ctx, cwd, sink)
	})
}

// Resume reopens an earlier conversation of the named agent.
func (m *Manager) Resume(ctx context.Context, agent, cwd, sessionID string) (SessionInfo, error) {
	return m.open(agent, cwd, func(d Driver, sink Sink) (SessionInfo, error) {
		r, ok := d.(Resumer)
		if !ok {
			return SessionInfo{}, ErrNoResume
		}
		return r.Resume(ctx, cwd, sessionID, sink)
	})
}

// open starts or resumes a session through fn and registers it.
func (m *Manager) open(agent, cwd string, fn func(Driver, Sink) (SessionInfo, error)) (SessionInfo, error) {
	m.mu.Lock()
	d := m.drivers[agent]
	m.mu.Unlock()
	if d == nil {
		return SessionInfo{}, fmt.Errorf("unknown agent %q", agent)
	}
	var sessionID string
	var idMu sync.Mutex
	info, err := fn(d, func(e Event) {
		idMu.Lock()
		if e.SessionID == "" {
			e.SessionID = sessionID
		}
		idMu.Unlock()
		m.route(d, e)
	})
	if err != nil {
		return SessionInfo{}, err
	}
	idMu.Lock()
	sessionID = info.ID
	idMu.Unlock()
	if info.Agent == "" {
		info.Agent = agent
	}
	m.mu.Lock()
	m.sessions[info.ID] = &managed{agent: agent, cwd: cwd, driver: d}
	m.mu.Unlock()
	return info, nil
}

// route applies the policy to permission requests and forwards everything else.
func (m *Manager) route(d Driver, e Event) {
	if e.Kind == KindPermission && e.Permission != nil {
		if opt, ok := m.autoOption(e.SessionID, *e.Permission); ok {
			// Answer off the driver's goroutine: drivers may hold locks while emitting.
			go func() {
				if err := d.Respond(e.SessionID, e.Permission.ID, opt); err != nil {
					m.emit(Event{SessionID: e.SessionID, Kind: KindError, Text: "auto-approve failed: " + err.Error()})
				}
			}()
			return
		}
	}
	m.emit(e)
}

func (m *Manager) autoOption(sessionID string, req PermissionRequest) (string, bool) {
	m.mu.Lock()
	policy := m.policy
	cwd := ""
	if s := m.sessions[sessionID]; s != nil {
		cwd = s.cwd
	}
	m.mu.Unlock()
	if !policy.autoApproves(req.ToolCall, cwd) {
		return "", false
	}
	// Prefer a one-off allow so a policy change takes effect immediately.
	for _, kind := range []string{AllowOnce, AllowAlways} {
		if i := slices.IndexFunc(req.Options, func(o PermissionOption) bool { return o.Kind == kind }); i >= 0 {
			return req.Options[i].ID, true
		}
	}
	return "", false
}

// Prompt starts a turn in the background; progress arrives as events, ending with
// KindTurnEnd. It fails fast when a turn is already running.
func (m *Manager) Prompt(sessionID, text string, attachments []Attachment) error {
	m.mu.Lock()
	s := m.sessions[sessionID]
	if s == nil {
		m.mu.Unlock()
		return fmt.Errorf("unknown session %q", sessionID)
	}
	if s.cancel != nil {
		m.mu.Unlock()
		return ErrBusy
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	m.mu.Unlock()

	go func() {
		defer cancel()
		_, err := s.driver.Prompt(ctx, sessionID, text, attachments)
		m.mu.Lock()
		s.cancel = nil
		m.mu.Unlock()
		if err != nil && ctx.Err() == nil {
			// Drivers emit KindError/KindTurnEnd themselves; this covers early failures.
			m.emit(Event{SessionID: sessionID, Kind: KindError, Text: err.Error()})
		}
	}()
	return nil
}

// Busy reports whether a turn is running.
func (m *Manager) Busy(sessionID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.sessions[sessionID]
	return s != nil && s.cancel != nil
}

// Cancel interrupts the running turn.
func (m *Manager) Cancel(sessionID string) error {
	s, err := m.get(sessionID)
	if err != nil {
		return err
	}
	return s.driver.Cancel(sessionID)
}

// Respond answers a permission request the user saw.
func (m *Manager) Respond(sessionID, requestID, optionID string) error {
	s, err := m.get(sessionID)
	if err != nil {
		return err
	}
	return s.driver.Respond(sessionID, requestID, optionID)
}

// Stop ends a session.
func (m *Manager) Stop(sessionID string) error {
	m.mu.Lock()
	s := m.sessions[sessionID]
	delete(m.sessions, sessionID)
	var cancel context.CancelFunc
	if s != nil {
		cancel = s.cancel // read under the lock: the Prompt goroutine writes it
	}
	m.mu.Unlock()
	if s == nil {
		return nil
	}
	if cancel != nil {
		cancel()
	}
	return s.driver.Stop(sessionID)
}

// Close stops every session and driver.
func (m *Manager) Close() error {
	m.mu.Lock()
	drivers := m.drivers
	for _, s := range m.sessions {
		if s.cancel != nil {
			s.cancel()
		}
	}
	m.sessions = map[string]*managed{}
	m.mu.Unlock()
	// Drivers drain and kill their processes with grace periods; do it in parallel
	// so quitting Flint doesn't wait for each agent in turn.
	var wg sync.WaitGroup
	errs := make([]error, 0, len(drivers))
	var errMu sync.Mutex
	for _, d := range drivers {
		wg.Add(1)
		go func(d Driver) {
			defer wg.Done()
			if err := d.Close(); err != nil {
				errMu.Lock()
				errs = append(errs, err)
				errMu.Unlock()
			}
		}(d)
	}
	wg.Wait()
	return errors.Join(errs...)
}

func (m *Manager) get(id string) (*managed, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.sessions[id]
	if s == nil {
		return nil, fmt.Errorf("unknown session %q", id)
	}
	return s, nil
}
