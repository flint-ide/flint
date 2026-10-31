// Package terminal runs interactive shells in pseudo-terminals, so agents like
// claude or codex can run inside Flint exactly as in a regular terminal emulator.
package terminal

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

// maxEmit caps how many bytes are coalesced into one output event.
const maxEmit = 64 * 1024

// Events receives terminal output and exit notifications.
type Events interface {
	Output(id string, data []byte)
	Exited(id string, code int)
	// Title reports a change of what the terminal runs or where its shell is.
	Title(id string, info Info)
}

// Options configure a new terminal session.
type Options struct {
	Cwd  string
	Cols int
	Rows int
	// Shell and Args default to the user's login shell ($SHELL -l).
	Shell string
	Args  []string
	// Env adds variables ("AWS_PROFILE=prod") on top of Environ.
	Env []string
}

type session struct {
	cmd *exec.Cmd
	pty *os.File

	titleMu    sync.Mutex
	titleTimer *time.Timer
	info       Info // last reported
	fgGroup    int  // foreground process group of info.Command, 0 at the prompt
	done       bool
}

// Manager owns all running terminal sessions.
type Manager struct {
	events Events

	mu       sync.Mutex
	sessions map[string]*session
}

// NewManager creates a manager that reports output through events.
func NewManager(events Events) *Manager {
	return &Manager{events: events, sessions: map[string]*session{}}
}

// Start launches a shell in a new PTY under the given id.
func (m *Manager) Start(id string, opts Options) error {
	m.mu.Lock()
	_, exists := m.sessions[id]
	m.mu.Unlock()
	if exists {
		return fmt.Errorf("terminal %q already running", id)
	}

	shell, args := opts.Shell, opts.Args
	if shell == "" {
		shell, args = loginShell()
	}
	cmd := exec.Command(shell, args...)
	cmd.Dir = opts.Cwd
	cmd.Env = append(Environ(), opts.Env...)

	ptmx, err := pty.StartWithSize(cmd, winsize(opts.Cols, opts.Rows))
	if err != nil {
		return fmt.Errorf("start shell %s: %w", shell, err)
	}
	s := &session{cmd: cmd, pty: ptmx}

	m.mu.Lock()
	m.sessions[id] = s
	m.mu.Unlock()

	go m.pump(id, s)
	return nil
}

// Write sends user input (keystrokes, pastes) to the shell.
func (m *Manager) Write(id string, data []byte) error {
	s, err := m.get(id)
	if err != nil {
		return err
	}
	_, err = s.pty.Write(data)
	return err
}

// Resize updates the PTY window size; full-screen programs redraw on SIGWINCH.
func (m *Manager) Resize(id string, cols, rows int) error {
	s, err := m.get(id)
	if err != nil {
		return err
	}
	return pty.Setsize(s.pty, winsize(cols, rows))
}

// Cwd returns the shell's current directory (it follows `cd`), so a restarted
// Flint can reopen the terminal where the user left it.
func (m *Manager) Cwd(id string) (string, error) {
	s, err := m.get(id)
	if err != nil {
		return "", err
	}
	return processCwd(s.cmd.Process.Pid)
}

// Close hangs up the shell and its children.
func (m *Manager) Close(id string) {
	m.mu.Lock()
	s := m.sessions[id]
	delete(m.sessions, id)
	m.mu.Unlock()
	if s != nil {
		hangUp(s)
	}
}

// CloseAll terminates every session, e.g. on app shutdown.
func (m *Manager) CloseAll() {
	m.mu.Lock()
	all := m.sessions
	m.sessions = map[string]*session{}
	m.mu.Unlock()
	for _, s := range all {
		hangUp(s)
	}
}

func (m *Manager) get(id string) (*session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, fmt.Errorf("terminal %q not running", id)
	}
	return s, nil
}

// pump forwards PTY output until the shell exits. Reads are coalesced while more data
// is immediately available, so floods (e.g. `cat` of a big file) don't produce
// thousands of tiny events, while interactive echo stays instant.
func (m *Manager) pump(id string, s *session) {
	chunks := make(chan []byte, 256)
	go func() {
		defer close(chunks)
		buf := make([]byte, 32*1024)
		for {
			n, err := s.pty.Read(buf)
			if n > 0 {
				chunks <- append([]byte(nil), buf[:n]...)
			}
			if err != nil {
				return
			}
		}
	}()

	Coalesce(chunks, func(data []byte) {
		m.events.Output(id, data)
		m.noteActivity(id, s)
	})
	s.stopTitle()

	code := 0
	if err := s.cmd.Wait(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		} else {
			code = -1
		}
	}
	_ = s.pty.Close()

	m.mu.Lock()
	if m.sessions[id] == s {
		delete(m.sessions, id)
	}
	m.mu.Unlock()
	m.events.Exited(id, code)
}

// Coalesce forwards chunks to emit until the channel closes, merging chunks that
// are already waiting (up to maxEmit bytes) into one call: floods become a few
// large events while interactive echo stays instant.
func Coalesce(chunks <-chan []byte, emit func([]byte)) {
	for chunk := range chunks {
		pending := chunk
	drain:
		for len(pending) < maxEmit {
			select {
			case more, ok := <-chunks:
				if !ok {
					break drain
				}
				pending = append(pending, more...)
			default:
				break drain
			}
		}
		emit(pending)
	}
}

// hangUp sends SIGHUP to the shell's process group (pty.Start makes it a session leader),
// which is what closing a terminal window does.
func hangUp(s *session) {
	if s.cmd.Process != nil {
		_ = syscall.Kill(-s.cmd.Process.Pid, syscall.SIGHUP)
	}
	_ = s.pty.Close()
}

func winsize(cols, rows int) *pty.Winsize {
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}
	return &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)}
}

// loginShell returns the user's shell started as a login shell, so PATH and tools
// (claude, codex, brew, nvm...) match what a regular terminal sees — GUI apps on macOS
// otherwise inherit a minimal launchd environment.
func loginShell() (string, []string) {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}
	return shell, []string{"-l"}
}

// Environ is the process environment plus what terminal programs expect (TERM,
// colors, a UTF-8 locale).
func Environ() []string {
	env := os.Environ()
	return append(env,
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"TERM_PROGRAM=flint",
		"LANG="+langOrDefault(),
	)
}

func langOrDefault() string {
	if lang := os.Getenv("LANG"); lang != "" {
		return lang
	}
	return "en_US.UTF-8"
}
