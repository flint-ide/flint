package main

import (
	"errors"

	"flint/internal/diag"
	"flint/internal/mcptools"
	"flint/internal/remote"
	"flint/internal/sessions"
	"flint/internal/terminal"
)

// The App's side of background terminals (background_terminals.go): which
// implementation runs terminals, what quitting does to them, and the bindings
// that let a restored tab attach to a shell that kept running.

// wireTerminals builds the terminals the app uses from sessions.background,
// which is read once, at start. dial and spawn find or start the host
// (dialHost and spawnHost; fakes in tests). Docker and extension streams
// always run in process: they need the window's pipes.
func (a *App) wireTerminals(events terminal.Events, dial, spawn hostOpener) {
	local := terminal.NewManager(events)
	var main sessions.Terminals = local
	if a.settings.Sessions.Background {
		a.background = newHostTerminals(hostTermConfig{
			ConfigDir: a.configDir, Version: diag.Version(version), Events: events, Local: local,
			Dial: dial, Spawn: spawn, Choose: a.chooseUser, OnChange: a.refreshMenu,
		})
		main = a.background
	}
	a.termRouter = newTerminalsRouter(main, terminal.NewManager(events))
	a.terminals, a.termStreams = a.termRouter, a.termRouter
	a.wireRemote(events)
}

// closeTerminals is what quitting does to terminals. In process they end; in
// the background host they keep running and the window detaches, unless the
// user chose Quit and Stop Background Sessions.
func (a *App) closeTerminals() {
	if a.background == nil {
		a.terminals.CloseAll()
		return
	}
	a.termRouter.CloseStreams()
	a.termRouter.DetachRemote() // remote sessions keep running on their machines
	a.background.detach(a.stopSessionsOnQuit.Load())
}

// quitAndStopSessions is the menu item: quit through the usual handshake
// (unsaved files are asked about first), and on the way out tell the host to
// end its terminals and exit. Cancelling the quit cancels the stop
// (AppAPI.CancelQuit), so the shells only end when Flint really quits.
func (a *App) quitAndStopSessions() {
	a.stopSessionsOnQuit.Store(true)
	a.quitApp()
}

// backgroundConnected reports whether terminals run in the host right now.
func (a *App) backgroundConnected() bool {
	return a.background != nil && a.background.connected()
}

// hostSettingsChanged tells the host that settings.json changed.
func (a *App) hostSettingsChanged() {
	if a.background != nil {
		go a.background.settingsChanged()
	}
}

// LiveTerminals is what SessionAPI.Live reports.
type LiveTerminals struct {
	// Background: this run's terminals are in the background host.
	Background bool `json:"background"`
	// Terminals the host holds now; empty when Background is false.
	Terminals []LiveTerminal `json:"terminals"`
	// Notice says, once, why background terminals are not in use although
	// they are switched on; "" otherwise.
	Notice string `json:"notice"`
}

// LiveTerminal is a terminal the host holds: its id and whether its shell
// still runs. Nothing else: the tab that attaches learns the rest from the
// terminal itself.
type LiveTerminal struct {
	ID      string `json:"id"`
	Running bool   `json:"running"`
}

// Live returns the terminals the background host holds, so a restored tab
// whose id is among them attaches instead of starting a new shell. It waits
// for the startup connection (a few seconds at most).
func (s *SessionAPI) Live() LiveTerminals {
	out := LiveTerminals{Terminals: []LiveTerminal{}}
	h := s.app.background
	if h == nil {
		return out
	}
	out.Notice = h.takeNotice()
	list, err := h.live()
	if err != nil {
		s.app.logf("list background terminals: %v", err)
		return out
	}
	out.Background = h.connected()
	for _, t := range list {
		out.Terminals = append(out.Terminals, LiveTerminal{ID: t.ID, Running: !t.Exited})
	}
	return out
}

// Attach shows a terminal the background host kept running in a new tab of
// this window: what the host buffered is replayed, then live output follows
// on term:data as for any terminal. id must be one Live lists and no tab of
// this window shows yet.
func (t *TerminalAPI) Attach(id string) error {
	if remote.IsRemoteID(id) {
		return t.attachRemote(id) // a restored remote tab: the machine's own host kept the shell
	}
	if t.app.background == nil {
		return errors.New("background terminals are off")
	}
	// Kept before the replay starts, so the agent tools see it too.
	opened := t.app.mcpSources.Terminals.OpenNew(id, mcptools.KindTerminal, "", "")
	if err := t.app.background.attach(id); err != nil {
		if opened {
			t.app.mcpSources.Terminals.Close(id)
		}
		return err
	}
	return nil
}
