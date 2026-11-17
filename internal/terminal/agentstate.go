package terminal

import (
	"bytes"
	"sync"
)

// Agent states a terminal can report for an agent CLI running in it. They are
// the strings of Flint's own sequence and of the "term:agent" event; the UI
// and list_agents use them as they are.
const (
	AgentWorking = "working"
	AgentWaiting = "waiting" // needs the user (an approval, a question)
	AgentDone    = "done"
	AgentIdle    = "idle"
)

// flintOSC is the number of Flint's own status sequence:
//
//	ESC ] 6977 ; state=<working|waiting|done|idle> BEL
//
// 6977 is not a number any terminal this project knows of uses for itself. The
// private numbers of the common emulators, as their documentation lists them,
// are: iTerm2 1337 (and 9 for notifications, 133 for shell integration),
// VTE 7/8/777 and 3008, kitty 99 (notifications), 66, 5522, 21, 22 and 30001/30101,
// Ghostty 9/777/133 and kitty's, WezTerm 1337 and 7, foot 176/555/133, Windows
// Terminal and ConEmu 9;n, VS Code 633, FinalTerm 133, xterm 0-2/4/10-19/52/104-119.
// The check is from documentation, not from a registry, so the payload is
// matched exactly ("6977;state=<known value>"): anything else under 6977 is
// ignored, and a collision could at worst show a wrong badge.
const flintOSC = "6977"

// maxOSC bounds one OSC sequence's body (what follows ESC ] up to the
// terminator). A longer one is skipped to its terminator, never buffered.
const maxOSC = 512

const (
	esc = 0x1b
	bel = 0x07
	can = 0x18
	sub = 0x1a
)

type oscMode uint8

const (
	oscGround  oscMode = iota // outside any sequence
	oscEscape                 // saw ESC
	oscBody                   // inside ESC ] … collecting the body
	oscBodyEsc                // saw ESC inside a body: ST if a backslash follows
	oscSkip                   // oversize body: waiting for its terminator
	oscSkipEsc                // saw ESC while skipping
)

// oscScanner finds the agent status in a terminal's output. It only reads: the
// bytes go to the screen unchanged and undelayed, whatever it makes of them.
// One scanner follows one terminal's output; it is not safe for concurrent use.
type oscScanner struct {
	mode oscMode
	n    int
	body [maxOSC]byte
}

// Feed scans a chunk of output. emit is called with each state the chunk
// completes, in order. Sequences may be split across chunks anywhere. A chunk
// with no ESC in it costs one IndexByte and no allocation.
func (p *oscScanner) Feed(chunk []byte, emit func(state string)) {
	for len(chunk) > 0 {
		if p.mode == oscGround {
			i := bytes.IndexByte(chunk, esc)
			if i < 0 {
				return
			}
			chunk, p.mode = chunk[i+1:], oscEscape
			continue
		}
		var c byte
		c, chunk = chunk[0], chunk[1:]
		p.step(c, emit)
	}
}

func (p *oscScanner) step(c byte, emit func(string)) {
	switch p.mode {
	case oscEscape:
		switch c {
		case ']':
			p.mode, p.n = oscBody, 0
		case esc:
			// ESC ESC: the second one may start a sequence.
		default:
			p.mode = oscGround
		}
	case oscBody:
		switch c {
		case bel:
			p.finish(emit)
		case esc:
			p.mode = oscBodyEsc
		case can, sub:
			p.mode = oscGround // cancels the sequence
		default:
			if p.n == maxOSC {
				p.mode = oscSkip
				return
			}
			p.body[p.n] = c
			p.n++
		}
	case oscBodyEsc:
		switch c {
		case '\\':
			p.finish(emit)
		case ']':
			p.mode, p.n = oscBody, 0 // a new sequence: the old one was cut off
		case esc:
			// stays: ESC ESC
		default:
			p.mode = oscGround
		}
	case oscSkip:
		switch c {
		case bel, can, sub:
			p.mode = oscGround
		case esc:
			p.mode = oscSkipEsc
		}
	case oscSkipEsc:
		switch c {
		case '\\':
			p.mode = oscGround
		case ']':
			p.mode, p.n = oscBody, 0
		case esc:
		default:
			p.mode = oscSkip
		}
	}
}

// finish interprets a complete sequence's body.
func (p *oscScanner) finish(emit func(string)) {
	p.mode = oscGround
	if state, ok := agentStateOf(p.body[:p.n]); ok {
		emit(state)
	}
}

// agentStateOf maps an OSC body to an agent state: Flint's own sequence says
// it; a desktop notification (OSC 9, OSC 777;notify) means the agent wants the
// user. Everything else, including the other uses of OSC 9, is not ours.
func agentStateOf(body []byte) (string, bool) {
	num, rest, _ := bytes.Cut(body, []byte{';'})
	switch string(num) {
	case flintOSC:
		switch string(rest) {
		case "state=" + AgentWorking:
			return AgentWorking, true
		case "state=" + AgentWaiting:
			return AgentWaiting, true
		case "state=" + AgentDone:
			return AgentDone, true
		case "state=" + AgentIdle:
			return AgentIdle, true
		}
	case "9":
		// iTerm2's notification is "9;<text>". ConEmu and Windows Terminal use
		// "9;<digits>;…" for progress, the working directory and the like.
		if len(rest) > 0 && !conEmuSubcommand(rest) {
			return AgentWaiting, true
		}
	case "777":
		if verb, _, _ := bytes.Cut(rest, []byte{';'}); string(verb) == "notify" {
			return AgentWaiting, true
		}
	}
	return "", false
}

// conEmuSubcommand reports whether text starts with digits that end it or
// are followed by ';': "4;1;50" (progress), "9;/home" (directory), not "3 files".
func conEmuSubcommand(text []byte) bool {
	i := 0
	for i < len(text) && text[i] >= '0' && text[i] <= '9' {
		i++
	}
	return i > 0 && (i == len(text) || text[i] == ';')
}

// agentTracker remembers a terminal's last reported agent state, so the UI is
// told about changes only. The scanner is used by the output goroutine alone;
// last is also read by the title check.
type agentTracker struct {
	scan oscScanner
	mu   sync.Mutex
	last string
}

// set records state and reports whether it differs from the last one.
func (t *agentTracker) set(state string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.last == state {
		return false
	}
	t.last = state
	return true
}

// active reports whether the terminal has reported an agent that has not gone idle.
func (t *agentTracker) active() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.last != "" && t.last != AgentIdle
}

// scanAgent feeds output to the terminal's scanner and tells the UI about each
// state change. It runs after the bytes were handed on.
func (m *Manager) scanAgent(id string, s *session, data []byte) {
	s.agent.scan.Feed(data, func(state string) { m.setAgentState(id, s, state) })
}

func (m *Manager) setAgentState(id string, s *session, state string) {
	if s.agent.set(state) {
		m.events.AgentState(id, state)
	}
}

// agentLeft is called when what a terminal runs changes: the agent that
// reported a state is gone when the shell is back at its prompt.
func (m *Manager) agentLeft(id string, s *session, was, now Info) {
	if was.Command != "" && now.Command == "" && s.agent.active() {
		m.setAgentState(id, s, AgentIdle)
	}
}
