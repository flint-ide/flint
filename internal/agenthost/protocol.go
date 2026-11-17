package agenthost

import (
	"encoding/json"
	"fmt"
	"unicode"
	"unicode/utf8"

	as "flint/internal/agentsession"
	"flint/internal/terminal"
)

// Protocol is the wire protocol version. Changes within a version are additive
// only (new methods, new optional fields); anything else bumps it.
const Protocol = 1

// Methods of protocol 1. Client to host requests:
const (
	MethodHello = "hello"

	MethodTermStart  = "term.start"
	MethodTermWrite  = "term.write"
	MethodTermResize = "term.resize"
	MethodTermCwd    = "term.cwd"
	MethodTermResume = "term.resume"
	MethodTermClose  = "term.close"
	MethodTermAttach = "term.attach"

	MethodAgentAgents    = "agent.agents"
	MethodAgentStart     = "agent.start"
	MethodAgentResume    = "agent.resume"
	MethodAgentPrompt    = "agent.prompt"
	MethodAgentBusy      = "agent.busy"
	MethodAgentCancel    = "agent.cancel"
	MethodAgentRespond   = "agent.respond"
	MethodAgentStop      = "agent.stop"
	MethodAgentPolicy    = "agent.policy"
	MethodAgentSetPolicy = "agent.setPolicy"
	MethodAgentAttach    = "agent.attach"

	MethodHostShutdown = "host.shutdown"
	MethodUsageAck     = "usage.ack"
	MethodUsageBacklog = "usage.backlog"
)

// Host to client notifications.
const (
	NotifyTermData   = "term.data"
	NotifyTermExit   = "term.exit"
	NotifyTermTitle  = "term.title"
	NotifyAgentEvent = "agent.event"
)

// Limits on what the wire may carry. They are constants: they protect the host
// from a confused or hostile peer, they are not preferences.
const (
	// MaxMessage is the largest message either side accepts.
	MaxMessage = 4 << 20
	// MaxID is the longest terminal or chat id.
	MaxID = 256
	// MaxPath bounds a folder, a shell path and an agent name.
	MaxPath = 8 << 10
	// MaxWrite bounds the bytes of one term.write.
	MaxWrite = 1 << 20
	// MaxText bounds a prompt.
	MaxText = 2 << 20
	// MaxArgs bounds the number of shell arguments, of environment entries and
	// of attachments; MaxArg is the size of one argument or entry.
	MaxArgs = 1024
	MaxArg  = 32 << 10
	// MaxSize bounds terminal columns and rows.
	MaxSize = 1000
	// MaxSessions bounds terminals and chats the host keeps, running or not
	// yet released (an exited terminal stays until its tab is closed).
	MaxSessions = 256

	// maxChunk is the largest piece of terminal output with one seq.
	maxChunk = 32 << 10
	// maxEvent is the largest chat event the host forwards; a bigger one
	// is replaced by an error event with the same seq.
	maxEvent = 2 << 20
	// pageBudget bounds the payload of one attach or backlog reply so it
	// stays well under MaxMessage after base64 and JSON framing.
	pageBudget = 1 << 20
)

// Replay buffer bounds (docs/design/agent-runtime.md, "Replay buffers").
const (
	DefaultReplayKiB = 1024
	MaxReplayKiB     = 8192
	// ChatReplayEvents and ChatReplayBytes bound a chat's ring: the smaller wins.
	ChatReplayEvents = 2000
	ChatReplayBytes  = 8 << 20
	// MaxUsageBacklog bounds the usage events kept until acknowledged.
	MaxUsageBacklog = 10000
)

// HelloParams is the first call on every connection.
type HelloParams struct {
	Protocol int    `json:"protocol"`
	Version  string `json:"version"`
	Token    string `json:"token"`
}

// HelloResult says what the host speaks and holds. Compatible is false when the
// protocols differ: the host then only answers host.shutdown, so a newer
// Flint can replace an older host.
type HelloResult struct {
	Protocol   int    `json:"protocol"`
	Version    string `json:"version"`
	Compatible bool   `json:"compatible"`
	// Pid is the host process, so a window can tell which host it found.
	Pid int `json:"pid"`
	// Terminals and Chats are the ids the host holds (running, or exited but
	// not yet released), so a window that attaches later finds its sessions.
	Terminals []string `json:"terminals"`
	Chats     []string `json:"chats"`
}

// TermStartParams is term.start: terminal.Options plus the id.
type TermStartParams struct {
	ID    string   `json:"id"`
	Cwd   string   `json:"cwd,omitempty"`
	Cols  int      `json:"cols,omitempty"`
	Rows  int      `json:"rows,omitempty"`
	Shell string   `json:"shell,omitempty"`
	Args  []string `json:"args,omitempty"`
	Env   []string `json:"env,omitempty"`
}

// Options converts to what the terminal manager takes.
func (p TermStartParams) Options() terminal.Options {
	return terminal.Options{Cwd: p.Cwd, Cols: p.Cols, Rows: p.Rows, Shell: p.Shell, Args: p.Args, Env: p.Env}
}

// IDParams addresses a terminal or a chat.
type IDParams struct {
	ID string `json:"id"`
}

// TermWriteParams is term.write; Data is base64 on the wire.
type TermWriteParams struct {
	ID   string `json:"id"`
	Data []byte `json:"data"`
}

// TermResizeParams is term.resize.
type TermResizeParams struct {
	ID   string `json:"id"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

// TermCwdResult is the reply of term.cwd.
type TermCwdResult struct {
	Cwd string `json:"cwd"`
}

// AttachParams is term.attach and agent.attach: everything after AfterSeq.
type AttachParams struct {
	ID       string `json:"id"`
	AfterSeq uint64 `json:"afterSeq"`
}

// Chunk is one piece of terminal output; Data is base64 on the wire.
type Chunk struct {
	Seq  uint64 `json:"seq"`
	Data []byte `json:"data"`
}

// TermAttachResult is the reply of term.attach. Reset says the chunks after
// AfterSeq were no longer buffered: the chunks are the whole ring and the
// client should clear its screen first. More says the reply was cut to fit a
// message: ask again with the last seq received. Last is the newest seq the
// host has; Exited and Code tell a terminal whose shell ended (sent with the
// final page).
type TermAttachResult struct {
	Reset  bool    `json:"reset"`
	Chunks []Chunk `json:"chunks"`
	More   bool    `json:"more,omitempty"`
	Last   uint64  `json:"last"`
	Exited bool    `json:"exited,omitempty"`
	Code   int     `json:"code,omitempty"`
}

// TermDataParams is the term.data notification.
type TermDataParams struct {
	ID   string `json:"id"`
	Seq  uint64 `json:"seq"`
	Data []byte `json:"data"`
}

// TermExitParams is the term.exit notification; Seq is the last term.data
// seq, so a client that missed output can catch up before reporting the exit.
type TermExitParams struct {
	ID   string `json:"id"`
	Code int    `json:"code"`
	Seq  uint64 `json:"seq"`
}

// TermTitleParams is the term.title notification.
type TermTitleParams struct {
	ID      string `json:"id"`
	Command string `json:"command"`
	Cwd     string `json:"cwd"`
}

// AgentStartParams is agent.start.
type AgentStartParams struct {
	Agent string `json:"agent"`
	Cwd   string `json:"cwd"`
}

// AgentResumeParams is agent.resume.
type AgentResumeParams struct {
	Agent   string `json:"agent"`
	Cwd     string `json:"cwd"`
	Session string `json:"session"`
}

// AgentPromptParams is agent.prompt.
type AgentPromptParams struct {
	ID          string          `json:"id"`
	Text        string          `json:"text"`
	Attachments []as.Attachment `json:"attachments,omitempty"`
}

// AgentRespondParams is agent.respond.
type AgentRespondParams struct {
	ID      string `json:"id"`
	Request string `json:"request"`
	Option  string `json:"option"`
}

// AgentsResult is the reply of agent.agents.
type AgentsResult struct {
	Agents []string `json:"agents"`
}

// BusyResult is the reply of agent.busy.
type BusyResult struct {
	Busy bool `json:"busy"`
}

// EventMsg is an agent.event notification: the event, plus UsageSeq on usage
// events, which the host keeps until usage.ack covers it.
type EventMsg struct {
	as.Event
	UsageSeq uint64 `json:"usageSeq,omitempty"`
}

// AgentAttachResult is the reply of agent.attach, like TermAttachResult with
// events (EventMsg objects) instead of chunks.
type AgentAttachResult struct {
	Reset  bool              `json:"reset"`
	Events []json.RawMessage `json:"events"`
	More   bool              `json:"more,omitempty"`
	Last   uint64            `json:"last"`
}

// UsageAckParams is usage.ack: events up to and including UpTo may be dropped.
type UsageAckParams struct {
	UpTo uint64 `json:"upTo"`
}

// UsageBacklogParams is usage.backlog: unacknowledged usage events after After.
type UsageBacklogParams struct {
	After uint64 `json:"after"`
}

// UsageBacklogResult is the reply of usage.backlog; Events are EventMsg objects.
type UsageBacklogResult struct {
	Events []json.RawMessage `json:"events"`
	More   bool              `json:"more,omitempty"`
}

// ShutdownParams is host.shutdown.
type ShutdownParams struct {
	StopSessions bool `json:"stopSessions"`
}

// checkID validates a terminal or chat id from the wire.
func checkID(id string) error {
	if id == "" || len(id) > MaxID {
		return fmt.Errorf("id must be 1 to %d bytes", MaxID)
	}
	if !utf8.ValidString(id) {
		return fmt.Errorf("id is not UTF-8")
	}
	for _, r := range id {
		if unicode.IsControl(r) {
			return fmt.Errorf("id has a control character")
		}
	}
	return nil
}

// checkText bounds a string from the wire.
func checkText(what, s string, limit int) error {
	if len(s) > limit {
		return fmt.Errorf("%s is longer than %d bytes", what, limit)
	}
	return nil
}

// checkList bounds a list of strings from the wire.
func checkList(what string, list []string) error {
	if len(list) > MaxArgs {
		return fmt.Errorf("%s has more than %d entries", what, MaxArgs)
	}
	for _, s := range list {
		if err := checkText(what, s, MaxArg); err != nil {
			return err
		}
	}
	return nil
}

// checkSize validates terminal columns and rows; 0 means the default.
func checkSize(cols, rows int) error {
	if cols < 0 || rows < 0 || cols > MaxSize || rows > MaxSize {
		return fmt.Errorf("terminal size must be 0 to %d", MaxSize)
	}
	return nil
}

func (p TermStartParams) validate() error {
	for _, err := range []error{
		checkID(p.ID), checkSize(p.Cols, p.Rows),
		checkText("cwd", p.Cwd, MaxPath), checkText("shell", p.Shell, MaxPath),
		checkList("args", p.Args), checkList("env", p.Env),
	} {
		if err != nil {
			return err
		}
	}
	return nil
}
