package settings

import "fmt"

// Sessions configures background sessions: terminals that keep running in a
// separate host process when Flint's window closes
// (docs/design/agent-runtime.md, "Phase 3"). Agent chats are not covered yet:
// they stay in the window's process whatever Background says.
type Sessions struct {
	// Background runs terminals in the background host, so closing the
	// window or quitting detaches from them instead of ending them. It is
	// read when Flint starts: a change applies from the next start.
	Background bool `json:"background"`
	// ReplayKiB is how much recent output the host keeps per terminal for a
	// window that attaches later. The host reads it when it starts a
	// terminal, so a change applies to terminals started afterwards.
	ReplayKiB int `json:"replayKiB"`
}

// Bounds of sessions.replayKiB; the largest is the host's own cap
// (agenthost.MaxReplayKiB, which a test keeps equal).
const (
	DefaultReplayKiB = 1024
	MinReplayKiB     = 64
	MaxReplayKiB     = 8192
)

func defaultSessions() Sessions { return Sessions{ReplayKiB: DefaultReplayKiB} }

// CheckSessions reports a value out of range.
func CheckSessions(s Sessions) error {
	if s.ReplayKiB < MinReplayKiB || s.ReplayKiB > MaxReplayKiB {
		return fmt.Errorf("sessions.replayKiB %d out of range %d–%d", s.ReplayKiB, MinReplayKiB, MaxReplayKiB)
	}
	return nil
}

// normalizeSessions clamps the replay size into range; 0 (a missing key) is
// the default.
func normalizeSessions(s Sessions) Sessions {
	switch {
	case s.ReplayKiB == 0:
		s.ReplayKiB = DefaultReplayKiB
	case s.ReplayKiB < MinReplayKiB:
		s.ReplayKiB = MinReplayKiB
	case s.ReplayKiB > MaxReplayKiB:
		s.ReplayKiB = MaxReplayKiB
	}
	return s
}
