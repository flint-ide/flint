package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"slices"
	"sync"
	"time"

	"flint/internal/agenthost"
	"flint/internal/agenthost/client"
	"flint/internal/agenthost/hostmode"
	"flint/internal/sessions"
	"flint/internal/terminal"
)

// Background terminals (docs/design/agent-runtime.md, "Phase 3"): with
// sessions.background on, the window's shells run in the background host, a
// headless Flint process that outlives the window. Closing the window or
// quitting detaches; the next start attaches to what still runs.
//
// hostTerminals is the router's main terminals implementation in that mode.
// It forwards to the host while connected and to the in-process manager when
// the host could not be reached (for the rest of the run), so the rest of the
// app sees one sessions.Terminals either way. Output reaches the same
// terminal.Events as in-process output does, so the frontend contract (term:data,
// term:exit, term:title) does not change.
//
// Only terminals: agent chats stay in the window's process until the host has
// the MCP server, the approval gate and agent settings (steps 3.8, 3.9).

const (
	// hostStartWait bounds finding or starting the host, at startup and on the
	// one reconnect: terminals wait for it, so it must stay short.
	hostStartWait = 5 * time.Second
	// hostCallWait bounds the calls the window makes of its own accord.
	hostCallWait = 5 * time.Second
	// hostRestartWait bounds waiting for an incompatible host to go away after
	// it was told to shut down (it may take its 10 s grace to stop sessions).
	hostRestartWait = 15 * time.Second
	hostRestartGap  = 250 * time.Millisecond
)

// Labels of the version-skew dialog.
const (
	restartSessionsLabel = "Restart Background Sessions"
	keepSessionsLabel    = "Keep Them"
)

// hostConn is what the window needs of a connection to the host;
// *client.Client is one, tests use fakes.
type hostConn interface {
	Terminals() sessions.Terminals
	Hello() agenthost.HelloResult
	AttachTerminal(id string, afterSeq uint64) error
	ListTerminals(ctx context.Context) ([]agenthost.TermStatus, error)
	SettingsChanged(ctx context.Context) error
	Positions() map[string]uint64
	Shutdown(ctx context.Context, stopSessions bool) error
	Close() error
}

// hostOpener finds (Dial) or starts (Spawn) the host of a config directory.
type hostOpener func(ctx context.Context, configDir string, cfg client.Config) (hostConn, error)

func dialHost(ctx context.Context, dir string, cfg client.Config) (hostConn, error) {
	return asHostConn(hostmode.Dial(ctx, dir, cfg))
}

func spawnHost(ctx context.Context, dir string, cfg client.Config) (hostConn, error) {
	return asHostConn(hostmode.Spawn(ctx, dir, cfg))
}

// asHostConn keeps a nil *client.Client from becoming a non-nil interface.
func asHostConn(c *client.Client, err error) (hostConn, error) {
	if c == nil {
		return nil, err
	}
	return c, err
}

// hostTermConfig wires hostTerminals.
type hostTermConfig struct {
	ConfigDir string
	Version   string
	// Events gets the output, exits and titles, from the host or in-process.
	Events terminal.Events
	// Local runs terminals in this process when the host cannot be used.
	Local       sessions.Terminals
	Dial, Spawn hostOpener
	// Choose asks a native question with buttons; "" when dismissed.
	Choose func(title, message string, buttons []string) string
	// OnChange is told when the connection came up or went away (the menu
	// item that stops the host follows it).
	OnChange func()
}

type hostTerminals struct {
	cfg   hostTermConfig
	ready chan struct{} // closed once the startup connection is settled

	mu          sync.Mutex
	conn        hostConn // nil: in-process terminals for the rest of this run
	gen         uint64   // generation of conn; disconnects of older ones are ignored
	reconnected bool     // the one automatic reconnect was used
	leaving     bool     // detaching or stopping: a closed connection is expected
	mine        map[string]struct{}
	notice      string // why background terminals are not in use, for the UI, once
}

func newHostTerminals(cfg hostTermConfig) *hostTerminals {
	return &hostTerminals{cfg: cfg, ready: make(chan struct{}), mine: map[string]struct{}{}}
}

// connect finds or starts the host. Terminal calls wait until it is done,
// which takes at most hostStartWait unless the version-skew dialog is up.
func (h *hostTerminals) connect() {
	defer h.changed()    // second: the menu reads connected(), which needs ready
	defer close(h.ready) // first
	ctx, cancel := context.WithTimeout(context.Background(), hostStartWait)
	defer cancel()
	gen := h.nextGen()
	conn, err := h.open(ctx, gen)
	var skew *client.ProtocolError
	switch {
	case errors.As(err, &skew):
		conn = h.resolveSkew(conn, skew, gen)
	case err != nil:
		conn = nil
		h.setNotice(fmt.Sprintf("Background terminals are unavailable (%v); this time terminals run inside Flint and end when it quits.", err))
	}
	h.mu.Lock()
	if h.leaving { // the window closed while this was connecting
		h.mu.Unlock()
		if conn != nil {
			_ = conn.Close()
		}
		return
	}
	h.conn = conn
	h.mu.Unlock()
}

func (h *hostTerminals) nextGen() uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.gen++
	return h.gen
}

// open dials the host and starts one when none runs.
func (h *hostTerminals) open(ctx context.Context, gen uint64) (hostConn, error) {
	cfg := h.clientConfig(gen)
	conn, err := h.cfg.Dial(ctx, h.cfg.ConfigDir, cfg)
	if errors.Is(err, hostmode.ErrNotRunning) {
		conn, err = h.cfg.Spawn(ctx, h.cfg.ConfigDir, cfg)
	}
	return conn, err
}

func (h *hostTerminals) clientConfig(gen uint64) client.Config {
	return client.Config{Version: h.cfg.Version, Handlers: client.Handlers{
		Terminal:     h.cfg.Events,
		OnDisconnect: func(error) { go h.lost(gen) },
	}}
}

// resolveSkew asks what to do about a host that speaks another protocol: stop
// it and start ours (its terminals end), or keep it and run terminals in
// process for this run. It returns the connection to use, nil for in-process.
func (h *hostTerminals) resolveSkew(old hostConn, skew *client.ProtocolError, gen uint64) hostConn {
	message := fmt.Sprintf("Your background terminals are run by another version of Flint (%s), which this one cannot talk to.\n\n"+
		"%s stops it: the terminals it runs end. %s leaves them running and this window runs its own terminals until you quit.",
		skew.Version, restartSessionsLabel, keepSessionsLabel)
	if h.cfg.Choose("Background Sessions", message, []string{restartSessionsLabel, keepSessionsLabel}) != restartSessionsLabel {
		_ = old.Close()
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), hostRestartWait)
	defer cancel()
	if err := old.Shutdown(ctx, true); err != nil {
		log.Printf("flint: stop the old background host: %v", err)
	}
	_ = old.Close()
	// The old host gives up its lock only once it has stopped; until then a
	// new one finds it again. Bounded, and only after the user asked for it.
	var err error
	for ctx.Err() == nil {
		var conn hostConn
		if conn, err = h.cfg.Spawn(ctx, h.cfg.ConfigDir, h.clientConfig(gen)); err == nil {
			return conn
		}
		if conn != nil {
			_ = conn.Close()
		}
		select {
		case <-ctx.Done():
		case <-time.After(hostRestartGap):
		}
	}
	h.setNotice(fmt.Sprintf("Could not restart background terminals (%v); this time terminals run inside Flint.", err))
	return nil
}

func (h *hostTerminals) setNotice(text string) {
	log.Printf("flint: %s", text)
	h.mu.Lock()
	h.notice = text
	h.mu.Unlock()
}

// takeNotice returns why background terminals are not in use, once.
func (h *hostTerminals) takeNotice() string {
	<-h.ready
	h.mu.Lock()
	defer h.mu.Unlock()
	n := h.notice
	h.notice = ""
	return n
}

func (h *hostTerminals) changed() {
	if h.cfg.OnChange != nil {
		h.cfg.OnChange()
	}
}

// current is the connection after startup settled, nil when in process.
func (h *hostTerminals) current() hostConn {
	<-h.ready
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.conn
}

// connected reports whether terminals run in the host right now.
func (h *hostTerminals) connected() bool {
	select {
	case <-h.ready:
	default:
		return false
	}
	return h.current() != nil
}

func (h *hostTerminals) pick() sessions.Terminals {
	if c := h.current(); c != nil {
		return c.Terminals()
	}
	return h.cfg.Local
}

func (h *hostTerminals) Start(id string, opts terminal.Options) error {
	c := h.current()
	if c == nil {
		return h.cfg.Local.Start(id, opts)
	}
	if err := c.Terminals().Start(id, opts); err != nil {
		return err
	}
	h.mu.Lock()
	h.mine[id] = struct{}{}
	h.mu.Unlock()
	return nil
}

func (h *hostTerminals) Write(id string, data []byte) error { return h.pick().Write(id, data) }

func (h *hostTerminals) Resize(id string, cols, rows int) error {
	return h.pick().Resize(id, cols, rows)
}

func (h *hostTerminals) Cwd(id string) (string, error) { return h.pick().Cwd(id) }

func (h *hostTerminals) Resume(id string) (terminal.Resume, error) { return h.pick().Resume(id) }

// Close ends the session: closing a tab ends its shell, in the host too.
func (h *hostTerminals) Close(id string) {
	h.mu.Lock()
	delete(h.mine, id)
	h.mu.Unlock()
	h.pick().Close(id)
}

// CloseAll ends every session of this window, in the host and in process.
// Quitting does not call it (see detach).
func (h *hostTerminals) CloseAll() {
	if c := h.current(); c != nil {
		c.Terminals().CloseAll()
	}
	h.cfg.Local.CloseAll()
}

// live lists the terminals the host holds now; nil when terminals are not in
// the host this run.
func (h *hostTerminals) live() ([]agenthost.TermStatus, error) {
	c := h.current()
	if c == nil {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), hostCallWait)
	defer cancel()
	return c.ListTerminals(ctx)
}

// attach makes this window show a terminal the host kept: the host replays
// what it buffered (after a reset, a cleared screen and a resize so
// full-screen programs redraw), then live output follows. Only an id the host
// holds now and this window does not show yet may be attached.
func (h *hostTerminals) attach(id string) error {
	c := h.current()
	if c == nil {
		return errors.New("background terminals are not in use")
	}
	list, err := h.live()
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(list, func(s agenthost.TermStatus) bool { return s.ID == id }) {
		return fmt.Errorf("no background terminal %q", id)
	}
	h.mu.Lock()
	_, shown := h.mine[id]
	if !shown {
		h.mine[id] = struct{}{}
	}
	h.mu.Unlock()
	if shown {
		return fmt.Errorf("terminal %q is already open", id)
	}
	if err := c.AttachTerminal(id, 0); err != nil {
		h.mu.Lock()
		delete(h.mine, id)
		h.mu.Unlock()
		return err
	}
	return nil
}

// settingsChanged tells the host to read settings.json again.
func (h *hostTerminals) settingsChanged() {
	c := h.current()
	if c == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), hostCallWait)
	defer cancel()
	if err := c.SettingsChanged(ctx); err != nil {
		log.Printf("flint: tell the background host about settings: %v", err)
	}
}

// detach leaves the host running the shells (closing the window, Quit). With
// stop it asks the host to end them and exit first (Quit and Stop Background
// Sessions). In-process terminals end either way.
func (h *hostTerminals) detach(stop bool) {
	h.mu.Lock() // not waiting for ready: a connection that comes up later is closed by connect
	h.leaving = true
	c := h.conn
	h.mu.Unlock()
	if c != nil {
		if stop {
			ctx, cancel := context.WithTimeout(context.Background(), hostCallWait)
			if err := c.Shutdown(ctx, true); err != nil {
				log.Printf("flint: stop the background host: %v", err)
			}
			cancel()
		}
		_ = c.Close() // the detach: the client's CloseAll is never called
	}
	h.cfg.Local.CloseAll()
}

// lost handles a connection that closed while the window is open: one
// reconnect, re-attaching every terminal from the last output it showed.
// Terminals the host no longer has (it was gone and a new one started) end
// on their tabs; a second loss ends them all.
func (h *hostTerminals) lost(gen uint64) {
	h.mu.Lock()
	if h.leaving || gen != h.gen || h.conn == nil {
		h.mu.Unlock()
		return
	}
	old, retry := h.conn, !h.reconnected
	h.reconnected = true
	h.mu.Unlock()
	positions := old.Positions()
	if !retry {
		h.giveUp(positions, "the connection to the background host was lost again")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), hostStartWait)
	defer cancel()
	gen = h.nextGen()
	conn, err := h.open(ctx, gen)
	if err != nil {
		if conn != nil {
			_ = conn.Close()
		}
		h.giveUp(positions, fmt.Sprintf("could not reconnect to the background host: %v", err))
		return
	}
	h.mu.Lock()
	if h.leaving {
		h.mu.Unlock()
		_ = conn.Close()
		return
	}
	h.conn = conn
	h.mu.Unlock()
	held := conn.Hello().Terminals
	for id, last := range positions {
		if slices.Contains(held, id) && conn.AttachTerminal(id, last) == nil {
			continue
		}
		h.forget(id)
		h.ended(id, "This terminal's session ended: the background host stopped.")
	}
	h.changed()
}

// giveUp ends the window's host terminals on their tabs; new terminals run in
// process for the rest of the run.
func (h *hostTerminals) giveUp(positions map[string]uint64, reason string) {
	log.Printf("flint: %s", reason)
	h.mu.Lock()
	h.conn = nil
	h.mine = map[string]struct{}{}
	h.mu.Unlock()
	for id := range positions {
		h.ended(id, "Lost this terminal: "+reason+".")
	}
	h.changed()
}

func (h *hostTerminals) forget(id string) {
	h.mu.Lock()
	delete(h.mine, id)
	h.mu.Unlock()
}

// ended shows why a terminal stopped and reports it exited, through the same
// events as its output, so the tab shows it like any other ended shell.
func (h *hostTerminals) ended(id, why string) {
	h.cfg.Events.Output(id, []byte("\r\n\x1b[2m["+why+"]\x1b[0m\r\n"))
	h.cfg.Events.Exited(id, -1)
}
