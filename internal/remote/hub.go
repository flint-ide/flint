package remote

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"flint/internal/agenthost/client"
	"flint/internal/sessions"
	"flint/internal/settings"
	"flint/internal/terminal"
)

var _ sessions.Terminals = (*Hub)(nil)

// Errors of the Hub.
var (
	// ErrUnknownMachine: the id names a machine that is not in settings.
	ErrUnknownMachine = errors.New("unknown machine")
	// ErrNotConfirmed: the user declined to run the ssh command in the
	// native dialog, so no connection was made.
	ErrNotConfirmed = errors.New("connection not confirmed: the ssh command was declined")
)

// Kinds of ssh command the first-use dialog is asked about.
const (
	ConfirmConnect = "connect"
	ConfirmCheck   = "check"
)

// MachineStatus is the state of one machine as the UI shows it.
type MachineStatus struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	State State  `json:"state"`
	// Error and Stderr are redacted (see Status).
	Error   string `json:"error"`
	Stderr  string `json:"stderr"`
	Attempt int    `json:"attempt"`
	// RetryInSeconds is the wait before the next attempt; 0 when none.
	RetryInSeconds int `json:"retryInSeconds"`
}

// HubConfig wires a Hub.
type HubConfig struct {
	// Version is Flint's version, sent in hello.
	Version string
	// Events gets the output, titles, agent states and exits of remote
	// terminals, under their prefixed ids.
	Events terminal.Events
	// Confirm is the first-use dialog: it is asked before ssh runs for a
	// machine for the first time in this run (and again after the machine
	// was changed), with the exact argument vector. kind is ConfirmConnect or
	// ConfirmCheck. It must not be answerable by the web view. nil refuses.
	Confirm func(m settings.Machine, kind string, argv []string) bool
	// OnStatus is told whenever a machine's state changes.
	OnStatus func(MachineStatus)
	// newConn is a test seam; nil uses New.
	newConn func(Config) (*Conn, error)
}

// Hub owns one Conn per machine, made when the machine is first used, and
// implements sessions.Terminals for ids of the form "<machine>:<session>": it
// strips the prefix and sends the call to that machine's Conn. It sends no
// working directory and no environment: a local project path means nothing on
// the remote, which starts the shell in the remote user's home.
type Hub struct {
	cfg HubConfig

	confirmMu sync.Mutex // one dialog at a time

	mu        sync.Mutex
	machines  map[string]settings.Machine
	conns     map[string]*Conn
	confirmed map[string]bool // commands the user allowed this run, see confirmKey
	status    map[string]MachineStatus
	closed    bool
}

// NewHub makes a Hub with no machines; SetMachines configures them.
func NewHub(cfg HubConfig) *Hub {
	if cfg.newConn == nil {
		cfg.newConn = New
	}
	return &Hub{
		cfg: cfg, machines: map[string]settings.Machine{}, conns: map[string]*Conn{},
		confirmed: map[string]bool{}, status: map[string]MachineStatus{},
	}
}

// sameConnection: whether two machines make the same ssh command (the name
// is only a label).
func sameConnection(a, b settings.Machine) bool {
	return a.ID == b.ID && a.Target == b.Target && a.Port == b.Port && a.Command == b.Command
}

// SetMachines makes list the configured machines. A machine that was removed
// or whose connection details changed loses its Conn: the connection is
// closed (the sessions keep running on the remote), its tabs are told their
// terminal ended, and the first-use dialog is asked again for the new details.
func (h *Hub) SetMachines(list []settings.Machine) {
	next := make(map[string]settings.Machine, len(list))
	for _, m := range list {
		next[m.ID] = m
	}
	type dropped struct {
		id    string
		conn  *Conn
		why   string
		inUse []string
	}
	var drop []dropped
	h.mu.Lock()
	for id, old := range h.machines {
		now, kept := next[id]
		if kept && sameConnection(old, now) {
			if st, ok := h.status[id]; ok { // only the label changed
				st.Name = displayName(now)
				h.status[id] = st
			}
			continue
		}
		why := "the machine was changed in Settings"
		if !kept {
			why = "the machine was removed from Settings"
		}
		d := dropped{id: id, conn: h.conns[id], why: why}
		if d.conn != nil {
			d.inUse = d.conn.TerminalIDs()
		}
		drop = append(drop, d)
		delete(h.conns, id)
		for k := range h.confirmed {
			if confirmedFor(k, id) {
				delete(h.confirmed, k)
			}
		}
	}
	h.machines = next
	h.mu.Unlock()

	for _, d := range drop {
		if d.conn != nil {
			_ = d.conn.Close()
		}
		for _, s := range d.inUse {
			h.ended(JoinID(d.id, s), "Disconnected: "+d.why+". The session keeps running on the machine.")
		}
		h.publish(MachineStatus{ID: d.id, State: StateIdle})
	}
}

// Machines returns the configured machines, sorted by id.
func (h *Hub) Machines() []settings.Machine {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]settings.Machine, 0, len(h.machines))
	for _, m := range h.machines {
		out = append(out, m)
	}
	slices.SortFunc(out, func(a, b settings.Machine) int { return strings.Compare(a.ID, b.ID) })
	return out
}

// Statuses lists every machine's state, for a UI that starts after the events.
func (h *Hub) Statuses() []MachineStatus {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]MachineStatus, 0, len(h.machines))
	for id, m := range h.machines {
		st, ok := h.status[id]
		if !ok {
			st = MachineStatus{ID: id, Name: displayName(m), State: StateIdle}
		}
		out = append(out, st)
	}
	slices.SortFunc(out, func(a, b MachineStatus) int { return strings.Compare(a.ID, b.ID) })
	return out
}

func displayName(m settings.Machine) string {
	if m.Name != "" {
		return m.Name
	}
	return m.ID
}

// machineOf resolves an id to its machine and the remote session id.
func (h *Hub) machineOf(id string) (settings.Machine, string, error) {
	machine, session, err := SplitID(id)
	if err != nil {
		return settings.Machine{}, "", err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	m, ok := h.machines[machine]
	if !ok {
		return settings.Machine{}, "", fmt.Errorf("%w %q: add it under Settings ▸ Machines", ErrUnknownMachine, machine)
	}
	return m, session, nil
}

// connFor returns the machine's Conn, making it (after the first-use dialog)
// when there is none yet.
func (h *Hub) connFor(m settings.Machine) (*Conn, error) {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil, ErrClosed
	}
	if c := h.conns[m.ID]; c != nil {
		h.mu.Unlock()
		return c, nil
	}
	h.mu.Unlock()

	argv, err := HostArgv(m)
	if err != nil {
		return nil, err
	}
	if err := h.confirm(m, ConfirmConnect, argv); err != nil {
		h.publish(MachineStatus{ID: m.ID, State: StateFailed, Error: err.Error()})
		return nil, err
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	cur, known := h.machines[m.ID]
	switch {
	case h.closed:
		return nil, ErrClosed
	case h.conns[m.ID] != nil:
		return h.conns[m.ID], nil
	case !known || !sameConnection(cur, m):
		return nil, fmt.Errorf("%w %q: it was changed meanwhile", ErrUnknownMachine, m.ID)
	}
	c, err := h.cfg.newConn(Config{
		Machine: m, Version: h.cfg.Version,
		Handlers: client.Handlers{Terminal: prefixed{machine: m.ID, next: h.cfg.Events}},
		OnStatus: func(st Status) { h.onStatus(m, st) },
		OnLost: func(kind, id string) {
			if kind == "terminal" {
				h.ended(JoinID(m.ID, id), "session ended on "+displayName(m)+": the machine no longer has it (its Flint host was restarted, or the session was closed elsewhere)")
			}
		},
	})
	if err != nil {
		return nil, err
	}
	h.conns[m.ID] = c
	return c, nil
}

// confirmKey identifies one ssh command of one machine; machine id first, so
// the entries of a machine can be found again.
func confirmKey(kind string, m settings.Machine, argv []string) string {
	return m.ID + "\x00" + kind + "\x00" + strings.Join(argv, "\x00")
}

func confirmedFor(key, machineID string) bool { return strings.HasPrefix(key, machineID+"\x00") }

// confirm asks the user before this exact ssh command runs for the first time
// in this run. A yes is remembered until the machine changes.
func (h *Hub) confirm(m settings.Machine, kind string, argv []string) error {
	key := confirmKey(kind, m, argv)
	h.confirmMu.Lock()
	defer h.confirmMu.Unlock()
	h.mu.Lock()
	ok := h.confirmed[key]
	h.mu.Unlock()
	if ok {
		return nil
	}
	if h.cfg.Confirm == nil || !h.cfg.Confirm(m, kind, argv) {
		return ErrNotConfirmed
	}
	h.mu.Lock()
	h.confirmed[key] = true
	h.mu.Unlock()
	return nil
}

func (h *Hub) onStatus(m settings.Machine, st Status) {
	h.publish(MachineStatus{
		ID: m.ID, Name: displayName(m), State: st.State, Error: st.Err, Stderr: st.Stderr,
		Attempt: st.Attempt, RetryInSeconds: int(st.RetryIn.Seconds()),
	})
}

func (h *Hub) publish(st MachineStatus) {
	h.mu.Lock()
	if m, ok := h.machines[st.ID]; ok {
		st.Name = displayName(m)
		h.status[st.ID] = st
	} else {
		delete(h.status, st.ID)
	}
	h.mu.Unlock()
	if h.cfg.OnStatus != nil {
		h.cfg.OnStatus(st)
	}
}

// ended shows why a remote terminal stopped and reports it exited, through
// the same events as its output.
func (h *Hub) ended(id, why string) {
	if h.cfg.Events == nil {
		return
	}
	h.cfg.Events.Output(id, []byte("\r\n\x1b[2m["+why+"]\x1b[0m\r\n"))
	h.cfg.Events.Exited(id, -1)
}

// Start opens a shell on the machine the id names. Only the size is used:
// the working directory and the environment are not sent (see Hub).
func (h *Hub) Start(id string, opts terminal.Options) error {
	m, session, err := h.machineOf(id)
	if err != nil {
		return err
	}
	c, err := h.connFor(m)
	if err != nil {
		return err
	}
	return c.Terminals().Start(session, terminal.Options{Cols: opts.Cols, Rows: opts.Rows})
}

// Attach shows a terminal the machine's host kept from an earlier run. It
// never starts a shell.
func (h *Hub) Attach(ctx context.Context, id string) error {
	m, session, err := h.machineOf(id)
	if err != nil {
		return err
	}
	c, err := h.connFor(m)
	if err != nil {
		return err
	}
	return c.AttachTerminal(ctx, session)
}

// existing returns the Conn of a machine that already has one: Write, Resize
// and the like never connect.
func (h *Hub) existing(id string) (*Conn, string, error) {
	m, session, err := h.machineOf(id)
	if err != nil {
		return nil, "", err
	}
	h.mu.Lock()
	c := h.conns[m.ID]
	h.mu.Unlock()
	if c == nil {
		return nil, "", fmt.Errorf("%w (%s): no session was opened on it in this run", ErrNotConnected, m.ID)
	}
	return c, session, nil
}

func (h *Hub) Write(id string, data []byte) error {
	c, session, err := h.existing(id)
	if err != nil {
		return err
	}
	return c.Terminals().Write(session, data)
}

func (h *Hub) Resize(id string, cols, rows int) error {
	c, session, err := h.existing(id)
	if err != nil {
		return err
	}
	return c.Terminals().Resize(session, cols, rows)
}

func (h *Hub) Cwd(id string) (string, error) {
	c, session, err := h.existing(id)
	if err != nil {
		return "", err
	}
	return c.Terminals().Cwd(session)
}

func (h *Hub) Resume(id string) (terminal.Resume, error) {
	c, session, err := h.existing(id)
	if err != nil {
		return terminal.Resume{}, err
	}
	return c.Terminals().Resume(session)
}

// Close ends the remote session (closing a tab ends its shell).
func (h *Hub) Close(id string) {
	if c, session, err := h.existing(id); err == nil {
		c.Terminals().Close(session)
	}
}

// CloseAll ends every remote session this Hub's Conns hold.
func (h *Hub) CloseAll() {
	for _, c := range h.allConns() {
		c.Terminals().CloseAll()
	}
}

// Detach closes every connection and leaves the sessions running on their
// machines (closing the window does this: that is the point of remote
// sessions). The Hub makes no new connection afterwards.
func (h *Hub) Detach() {
	h.mu.Lock()
	h.closed = true
	conns := make([]*Conn, 0, len(h.conns))
	for _, c := range h.conns {
		conns = append(conns, c)
	}
	h.conns = map[string]*Conn{}
	h.mu.Unlock()
	for _, c := range conns {
		_ = c.Close()
	}
}

func (h *Hub) allConns() []*Conn {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]*Conn, 0, len(h.conns))
	for _, c := range h.conns {
		out = append(out, c)
	}
	return out
}

// Check runs the machine's Check after the first-use dialog for its command
// line. Only a configured machine can be checked.
func (h *Hub) Check(ctx context.Context, machineID string) (CheckResult, error) {
	h.mu.Lock()
	m, ok := h.machines[machineID]
	h.mu.Unlock()
	if !ok {
		return CheckResult{}, fmt.Errorf("%w %q: save it first", ErrUnknownMachine, machineID)
	}
	argv, err := CheckArgv(m)
	if err != nil {
		return CheckResult{}, err
	}
	if err := h.confirm(m, ConfirmCheck, argv); err != nil {
		return CheckResult{}, err
	}
	return Check(ctx, m), nil
}

// prefixed puts the machine's prefix on the ids of terminal events.
type prefixed struct {
	machine string
	next    terminal.Events
}

func (p prefixed) Output(id string, data []byte) {
	if p.next != nil {
		p.next.Output(JoinID(p.machine, id), data)
	}
}

func (p prefixed) Exited(id string, code int) {
	if p.next != nil {
		p.next.Exited(JoinID(p.machine, id), code)
	}
}

func (p prefixed) Title(id string, info terminal.Info) {
	if p.next != nil {
		p.next.Title(JoinID(p.machine, id), info)
	}
}

func (p prefixed) AgentState(id, state string) {
	if p.next != nil {
		p.next.AgentState(JoinID(p.machine, id), state)
	}
}
