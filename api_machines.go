package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"flint/internal/diag"
	"flint/internal/remote"
	"flint/internal/settings"
	"flint/internal/terminal"
)

// Remote terminals (docs/design/agent-runtime.md, "Phase 4"): shells on
// machines Flint reaches over the user's own ssh. This file is the App's side:
// the Hub that owns one connection per machine, the native first-use dialog,
// and the bindings for the Machines settings page and the tabs. Files, git,
// language servers and search stay local; remote agent chats are not offered.
//
// Restricted Mode: a remote terminal may be opened from an untrusted folder.
// It runs nothing from the project (no cwd and no environment are sent, the
// shell starts in the remote user's home), exactly like the local New
// Terminal command, which is also available there; only the automatic first
// terminal waits for trust.

// eventMachineStatus tells the UI that a machine's connection state changed;
// the payload is remote.MachineStatus.
const eventMachineStatus = "machine:status"

// remoteAttachWait bounds re-attaching a restored tab: ssh's own connect
// timeout is 10 s and the remote host may have to start.
const remoteAttachWait = 45 * time.Second

// wireRemote makes the Hub that remote terminal ids are routed to. Before
// this the router answers remote ids with an error.
func (a *App) wireRemote(events terminal.Events) {
	a.remote = remote.NewHub(remote.HubConfig{
		Version:  diag.Version(version),
		Events:   events,
		Confirm:  a.confirmRemoteCommand,
		OnStatus: func(st remote.MachineStatus) { a.emit(eventMachineStatus, st) },
	})
	a.remote.SetMachines(a.settings.Machines)
	a.termRouter.SetRemote(a.remote)
}

// confirmRemoteCommand asks the user, in a native dialog the web view cannot
// answer, before an ssh command of a machine runs for the first time this run.
// Settings are saved by the web view, which renders untrusted content, so a
// machine being in settings proves nothing about who put it there.
func (a *App) confirmRemoteCommand(m settings.Machine, kind string, argv []string) bool {
	ask := a.askNowYes
	name := m.Name
	if name == "" {
		name = m.ID
	}
	line := commandLine(argv[0], argv[1:])
	if kind == remote.ConfirmCheck {
		return ask(fmt.Sprintf("Check %q over ssh?", name),
			"Flint will run this command on your computer to read the version of Flint on that machine:\n\n"+line+
				"\n\nIt uses your own ssh keys and configuration, never asks for a password, and runs nothing else there. Continue only if you added this machine yourself.",
			"Run Check")
	}
	return ask(fmt.Sprintf("Connect to %q over ssh?", name),
		"Flint will run this command on your computer to open terminals on that machine:\n\n"+line+
			"\n\nIt uses your own ssh keys and configuration and never asks for a password. Shells start in your home folder there; nothing from the open folders is sent. Continue only if you added this machine yourself.",
		"Connect")
}

// MachinesAPI serves the Machines settings page and the machine picker.
type MachinesAPI struct {
	app *App
}

// Status lists the configured machines and their connection states.
func (m *MachinesAPI) Status() []remote.MachineStatus {
	if m.app.remote == nil {
		return []remote.MachineStatus{}
	}
	return m.app.remote.Statuses()
}

// Issues lists the entries of the "machines" setting that were dropped when
// settings.json was read, with the reason.
func (m *MachinesAPI) Issues() []settings.MachineIssue {
	return m.app.currentSettings().MachineIssues()
}

// CheckList reports why a list of machines would not be saved ("" when it
// would), so the form can show the error next to the field. An empty command
// means the default, as it does when saving.
func (m *MachinesAPI) CheckList(list []settings.Machine) string {
	filled := make([]settings.Machine, len(list))
	for i, machine := range list {
		if machine.Command == "" {
			machine.Command = settings.DefaultMachineCmd
		}
		filled[i] = machine
	}
	return errorText(settings.CheckMachines(filled))
}

// Check runs "<command> --version" on a saved machine over ssh, after the
// first-use dialog, and says whether Flint is there and compatible. It takes
// only the machine's id: nothing else about the command comes from the web view.
func (m *MachinesAPI) Check(id string) (remote.CheckResult, error) {
	if m.app.remote == nil {
		return remote.CheckResult{}, errors.New("remote machines are not available")
	}
	ctx := m.app.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return m.app.remote.Check(ctx, id)
}

// InstallSteps returns the steps to install the headless host on a machine
// whose Check found no usable Flint. Nothing is installed by this call.
func (m *MachinesAPI) InstallSteps() remote.InstallInstructions {
	return remote.Install(settings.Machine{})
}

// StartRemote opens a shell on the machine the id names ("<machineId>:<id>").
// The id picks the machine; the shell starts in the remote user's home and no
// working directory, environment or shell is accepted from the web view.
func (t *TerminalAPI) StartRemote(id string, cols, rows int) error {
	if _, _, err := remote.SplitID(id); err != nil {
		return err
	}
	return t.app.terminals.Start(id, terminal.Options{Cols: cols, Rows: rows})
}

// attachRemote re-attaches a restored tab to a session the machine kept.
func (t *TerminalAPI) attachRemote(id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), remoteAttachWait)
	defer cancel()
	return t.app.termRouter.AttachRemote(ctx, id)
}
