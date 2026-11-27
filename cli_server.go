//go:build !server

package main

import (
	"context"
	"maps"

	"flint/internal/agenthost/hostmode"
	"flint/internal/cli"
	"flint/internal/cli/clisock"
	"flint/internal/terminal"
)

// The command line's socket (internal/cli/clisock): Flint listens once it
// has launched and stops on quit. Not in the server build, whose bindings
// already answer anyone on localhost: a second way in would only blur that.

// startCLI listens for the flint command line. A failure is logged: the
// window works without it.
func (a *App) startCLI() {
	reg, err := cli.NewRegistry(a.cliCommands())
	if err != nil {
		a.logf("command line: %v", err)
		return
	}
	srv, err := clisock.Start(clisock.Options{
		Paths: clisock.PathsFor(a.configDir), Registry: reg, Classify: a.classifyCaller, Confirm: a.confirmDecision,
		ConfirmAgent: a.confirmAgentCLI,
	})
	if err != nil {
		a.logf("command line: %v", err)
		return
	}
	a.cliServer.Store(srv)
}

// closeCLI stops listening and removes the socket.
func (a *App) closeCLI() {
	if srv := a.cliServer.Swap(nil); srv != nil {
		srv.Close()
	}
}

// cliCommands are the features' commands and the agent tasks'.
func (a *App) cliCommands() []cli.Command {
	tasks := &TaskAPI{app: a, git: &GitAPI{app: a}, pr: a.github}
	return append(a.features.CLI(), tasks.cliCommands()...)
}

// classifyCaller decides whether the process pid, which connected to the
// command line's socket, runs for an agent (clisock.Classify).
func (a *App) classifyCaller(pid int) cli.Caller {
	return clisock.Classify(pid, clisock.SystemTree{}, a.callerFacts())
}

// confirmDecision asks the user, in the native dialog the web view cannot
// answer (Cancel the default), about a human decision sent from the command
// line. It gives up when the client goes away; a late answer is ignored.
func (a *App) confirmDecision(ctx context.Context, q cli.Question) bool {
	answer := make(chan bool, 1)
	go func() { answer <- a.askNative(q.Title, q.Message, q.OK) }()
	select {
	case yes := <-answer:
		return yes && ctx.Err() == nil
	case <-ctx.Done():
		return false
	}
}

// callerFacts are Flint's terminal tabs' shells, with whether an agent CLI
// reports a state in them, the background host Flint started (by the pid
// it recorded) and the agent CLIs' program names (the known ones and the
// user's configured commands).
func (a *App) callerFacts() clisock.Facts {
	f := clisock.ThisProcess()
	f.HostPID = hostmode.Spawned()
	f.Shells = map[int]clisock.Shell{}
	for pid, id := range a.shellPIDs() {
		sh := clisock.Shell{Tab: id}
		if a.mcpSources != nil {
			if info, ok := a.mcpSources.Terminals.Get(id); ok {
				sh.Agent = info.AgentState == terminal.AgentWorking || info.AgentState == terminal.AgentWaiting
			}
		}
		f.Shells[pid] = sh
	}
	s := a.currentSettings()
	var configured []string
	for _, c := range s.CustomAgents {
		configured = append(configured, c.Command)
		f.Custom = append(f.Custom, clisock.CustomAgent{ID: c.ID, Command: c.Command, Args: c.Args})
	}
	for _, ag := range s.Agents {
		configured = append(configured, ag.Path)
	}
	f.Agents = clisock.AgentNames(configured...)
	return f
}

// shellLister is a terminal manager that runs shells in this process.
type shellLister interface{ ShellPIDs() map[int]string }

// shellPIDs are the shells of the terminal tabs this process runs (the
// background host's are its children, not ours: Classify accepts under the
// host it recorded only a process in a session led by a child of the host).
func (a *App) shellPIDs() map[int]string {
	out := map[int]string{}
	add := func(t any) {
		if l, ok := t.(shellLister); ok {
			maps.Copy(out, l.ShellPIDs())
		}
	}
	if a.termRouter != nil {
		add(a.termRouter.main)
	}
	if a.background != nil {
		add(a.background.cfg.Local)
	}
	return out
}
