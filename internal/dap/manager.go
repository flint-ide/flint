package dap

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// ErrNoSession is returned for requests to a session that ended or never started.
var ErrNoSession = errors.New("no such debug session")

// Events receives what the manager's sessions report. Calls for one session
// arrive in order; Event must not block.
type Events interface {
	Event(id string, e Event)
	// Ended is called once per started session, after its adapter exited.
	Ended(id string)
}

// Manager keeps the running debug sessions by id (chosen by the caller).
type Manager struct {
	events Events

	mu       sync.Mutex
	sessions map[string]*Session
}

func NewManager(events Events) *Manager {
	return &Manager{events: events, sessions: map[string]*Session{}}
}

// StartGo debugs a Go program or test with Delve. A session already running
// under id is stopped first.
func (m *Manager) StartGo(ctx context.Context, id string, env []string, cfg LaunchConfig, breakpoints map[string][]SourceBreakpoint) error {
	dlv, err := FindDelve()
	if err != nil {
		return err
	}
	m.Stop(id)
	s, err := StartDelve(ctx, dlv, env, cfg, breakpoints, func(e Event) { m.events.Event(id, e) })
	if err != nil && strings.Contains(err.Error(), "stub exited") {
		// macOS refuses debugserver until developer mode is on.
		return fmt.Errorf("%w (macOS blocked the debugger: run `sudo DevToolsSecurity -enable`)", err)
	}
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.sessions[id] = s
	m.mu.Unlock()
	go func() {
		<-s.Done()
		s.kill() // dlv exited on its own: release the connection
		m.mu.Lock()
		if m.sessions[id] == s {
			delete(m.sessions, id)
		}
		m.mu.Unlock()
		m.events.Ended(id)
	}()
	return nil
}

// Get returns the session running under id.
func (m *Manager) Get(id string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s := m.sessions[id]; s != nil {
		return s, nil
	}
	return nil, ErrNoSession
}

// Stop ends the session under id, if any, and its program.
func (m *Manager) Stop(id string) {
	m.mu.Lock()
	s := m.sessions[id]
	m.mu.Unlock()
	if s != nil {
		s.Stop()
	}
}

// CloseAll stops every session (app shutdown).
func (m *Manager) CloseAll() {
	m.mu.Lock()
	all := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		all = append(all, s)
	}
	m.mu.Unlock()
	var wg sync.WaitGroup
	for _, s := range all {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Stop()
		}()
	}
	wg.Wait()
}

// FindDelve locates dlv on PATH, then in Go's install directory ($GOBIN or
// $GOPATH/bin, where `go install` puts it but a login PATH often doesn't reach).
func FindDelve() (string, error) {
	if p, err := exec.LookPath("dlv"); err == nil {
		return p, nil
	}
	dirs := []string{os.Getenv("GOBIN")}
	if out, err := exec.Command("go", "env", "GOBIN", "GOPATH").Output(); err == nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		dirs = append(dirs, lines[0])
		if len(lines) > 1 {
			for _, gp := range filepath.SplitList(lines[1]) {
				dirs = append(dirs, filepath.Join(gp, "bin"))
			}
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, "go", "bin"))
	}
	for _, d := range dirs {
		if d == "" {
			continue
		}
		p := filepath.Join(d, "dlv")
		if info, err := os.Stat(p); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return p, nil
		}
	}
	return "", fmt.Errorf("dlv (Delve) not found: install it with `go install github.com/go-delve/delve/cmd/dlv@latest`")
}
