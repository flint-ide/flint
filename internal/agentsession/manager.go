package agentsession

import (
	"context"
	"errors"
	"fmt"
	"maps"
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
// Each matches a path component at any depth (vendor/x/.git/config, a new
// x/.git that makes a repository) in any letter case: macOS file systems
// ignore case, so .GIT is the same directory.
var protectedDirs = []string{".git", ".claude", ".codex", ".vscode", ".idea"}

// allInside reports whether paths is non-empty and every path stays inside cwd
// (after cleaning; relative paths resolve against cwd) and outside protectedDirs.
func allInside(paths []string, cwd string) bool {
	if len(paths) == 0 || cwd == "" {
		return false
	}
	for _, p := range paths {
		if !Inside(p, cwd) || ProtectedDir(p, cwd) != "" {
			return false
		}
	}
	return true
}

// ProtectedDir returns the protected directory (".git", ".vscode", ...) that
// path (absolute, or relative to cwd) lies in or names below cwd, or "" for
// none. The path is checked as given and with its symlinks resolved: tools
// read .vscode/settings.json through a link named .vscode, and a link named
// otherwise may point into .git.
func ProtectedDir(path, cwd string) string {
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	path = filepath.Clean(path)
	for _, pair := range [][2]string{{filepath.Clean(cwd), path}, {Resolve(cwd), resolveExisting(path)}} {
		rel, err := filepath.Rel(pair[0], pair[1])
		if err != nil {
			continue
		}
		for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
			for _, dir := range protectedDirs {
				if strings.EqualFold(part, dir) {
					return dir
				}
			}
		}
	}
	return ""
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

// Resolve cleans path and resolves the symlinks in its longest existing prefix.
func Resolve(path string) string { return resolveExisting(filepath.Clean(path)) }

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
	retired  []Driver // replaced or removed drivers that may still run sessions
	sessions map[string]*managed
	policy   Policy
	closed   bool // Close ran; sessions that finish starting afterwards are stopped
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

// Register makes a driver available under name ("claude", "codex", ...). A
// driver it replaces keeps the sessions it runs until they stop.
func (m *Manager) Register(name string, d Driver) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.retire(m.drivers[name])
	m.drivers[name] = d
}

// Unregister removes name from new sessions; running ones keep their driver.
func (m *Manager) Unregister(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.retire(m.drivers[name])
	delete(m.drivers, name)
}

// retire keeps d for Close while sessions still use it, and forgets retired
// drivers that no session uses any more (they have nothing left to stop).
// Called with m.mu held.
func (m *Manager) retire(d Driver) {
	if d != nil {
		m.retired = append(m.retired, d)
	}
	m.retired = slices.DeleteFunc(m.retired, func(r Driver) bool {
		for _, s := range m.sessions {
			if s.driver == r {
				return false
			}
		}
		return true
	})
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

// ErrClosed is returned when a session would open after Close.
var ErrClosed = errors.New("agent sessions are shutting down")

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
	d, closed := m.drivers[agent], m.closed
	m.mu.Unlock()
	if closed {
		return SessionInfo{}, ErrClosed
	}
	if d == nil {
		return SessionInfo{}, fmt.Errorf("unknown agent %q", agent)
	}
	s := &managed{agent: agent, cwd: cwd, driver: d}
	var sessionID string
	var idMu sync.Mutex
	info, err := fn(d, func(e Event) {
		idMu.Lock()
		if e.SessionID == "" {
			e.SessionID = sessionID
		}
		idMu.Unlock()
		m.route(s, e)
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
	switch {
	case m.closed:
		// Close took its snapshot while this session started; nothing
		// else would stop its process.
		m.mu.Unlock()
		_ = d.Stop(info.ID)
		return SessionInfo{}, ErrClosed
	case m.sessions[info.ID] != nil:
		// Session ids are chosen by the agents; a second chat under the same
		// id would orphan the first one's process and answer its approvals
		// with the wrong folder.
		m.mu.Unlock()
		_ = d.Stop(info.ID)
		return SessionInfo{}, fmt.Errorf("%s reused the id of an open chat (%s)", agent, info.ID)
	}
	m.sessions[info.ID] = s
	// The driver may have been replaced while the session started; keep it
	// for Close, or its process would outlive Flint.
	if !slices.Contains(m.retired, d) && !slices.Contains(slices.Collect(maps.Values(m.drivers)), d) {
		m.retired = append(m.retired, d)
	}
	m.mu.Unlock()
	return info, nil
}

// route applies the policy to permission requests and forwards everything
// else. s is the session the events come from: its folder decides
// auto-approval, never a lookup by the id an event carries, and events that
// name another open chat are dropped.
func (m *Manager) route(s *managed, e Event) {
	m.mu.Lock()
	owner, policy := m.sessions[e.SessionID], m.policy
	m.mu.Unlock()
	if owner != nil && owner != s {
		return
	}
	if e.Kind == KindPermission && e.Permission != nil {
		if opt, ok := autoOption(policy, s.cwd, *e.Permission); ok {
			// Answer off the driver's goroutine: drivers may hold locks while emitting.
			go func() {
				if err := s.driver.Respond(e.SessionID, e.Permission.ID, opt); err != nil {
					m.emit(Event{SessionID: e.SessionID, Kind: KindError, Text: "auto-approve failed: " + err.Error()})
				}
			}()
			return
		}
	}
	m.emit(e)
}

func autoOption(policy Policy, cwd string, req PermissionRequest) (string, bool) {
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
	m.closed = true
	drivers := slices.Concat(slices.Collect(maps.Values(m.drivers)), m.retired)
	m.retired = nil
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
