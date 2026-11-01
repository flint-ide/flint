package terminal

import (
	"fmt"
	"io"
)

// Stream is a session backed by a byte stream instead of a PTY: container
// logs, or a shell inside a container reached through the Docker Engine API.
type Stream struct {
	// Reader is the session's output; the session ends when it does.
	Reader io.Reader
	// Writer receives keystrokes; nil makes the session read-only (logs).
	Writer io.Writer
	// Resize reports the terminal grid size; nil when it doesn't matter.
	Resize func(cols, rows int) error
	// Close stops the stream (hang-up); it must unblock Reader.
	Close func()
	// ExitCode is asked once Reader ended; nil means 0.
	ExitCode func() int
}

type streamSession struct {
	Stream
}

// StartStream starts a stream-backed session under id. Output and the exit
// are reported through the manager's events like those of a PTY session.
func (m *Manager) StartStream(id string, st Stream) error {
	if st.Reader == nil {
		return fmt.Errorf("terminal %q has no output", id)
	}
	m.mu.Lock()
	_, exists := m.sessions[id]
	_, streaming := m.streams[id]
	if exists || streaming {
		m.mu.Unlock()
		return fmt.Errorf("terminal %q already running", id)
	}
	s := &streamSession{st}
	m.streams[id] = s
	m.mu.Unlock()
	go m.pumpStream(id, s)
	return nil
}

func (m *Manager) stream(id string) *streamSession {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.streams[id]
}

func (m *Manager) pumpStream(id string, s *streamSession) {
	chunks := make(chan []byte, 256)
	go func() {
		defer close(chunks)
		buf := make([]byte, 32*1024)
		for {
			n, err := s.Reader.Read(buf)
			if n > 0 {
				chunks <- append([]byte(nil), buf[:n]...)
			}
			if err != nil {
				return
			}
		}
	}()
	Coalesce(chunks, func(data []byte) { m.events.Output(id, data) })
	code := 0
	if s.ExitCode != nil {
		code = s.ExitCode()
	}
	m.mu.Lock()
	if m.streams[id] == s {
		delete(m.streams, id)
	}
	m.mu.Unlock()
	m.events.Exited(id, code)
}
