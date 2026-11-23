package clisock

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"flint/internal/cli"
	"flint/internal/proctree"
	"flint/internal/terminal"
)

// Tree reads the process table (the system's, or a fake in tests).
type Tree interface {
	Parent(pid int) (proctree.Proc, error)
	Args(pid int) ([]string, error)
}

// SystemTree is the system's process table.
type SystemTree struct{}

// Parent is proctree.Parent.
func (SystemTree) Parent(pid int) (proctree.Proc, error) { return proctree.Parent(pid) }

// Args is terminal.ProcessArgs.
func (SystemTree) Args(pid int) ([]string, error) { return terminal.ProcessArgs(pid) }

// Facts are what Flint knows about its own processes when a command arrives.
type Facts struct {
	// Self is Flint's pid; UID the user Flint runs as.
	Self int
	UID  uint32
	// Shells are the shells of Flint's own terminal tabs, by pid.
	Shells map[int]Shell
	// Agents are the program names of agent CLIs (KnownAgents plus the
	// user's configured ones).
	Agents map[string]bool
	// HostPID is the background session host this Flint started
	// (hostmode.Spawned), 0 when none: only terminal tabs run there.
	HostPID int
	// Custom are the user's custom (ACP) agents, so an ancestor that runs
	// one is named after the agent, not its program ("node").
	Custom []CustomAgent
}

// CustomAgent is a custom agent's command line, as the settings say.
type CustomAgent struct {
	// ID names the agent ("fake-cli").
	ID string
	// Command and Args are what Flint starts: a name or an absolute path,
	// then the arguments.
	Command string
	Args    []string
}

// customAgent is the ID of the custom agent whose command line args are
// (the program by its base name, then its configured arguments first), ""
// when none is. An interpreter's script counts as the program too
// (ProgramName).
func customAgent(args []string, custom []CustomAgent) string {
	if len(args) == 0 {
		return ""
	}
	base, script := filepath.Base(args[0]), terminal.ProgramName(args)
	for _, c := range custom {
		prog := filepath.Base(c.Command)
		if c.ID == "" || c.Command == "" || prog != base && prog != script {
			continue
		}
		rest := args[1:]
		if prog != base { // the interpreter's script is the program
			rest = rest[min(1, len(rest)):]
		}
		if len(rest) >= len(c.Args) && slices.Equal(rest[:len(c.Args)], c.Args) {
			return c.ID
		}
	}
	return ""
}

// Shell is a terminal tab's shell.
type Shell struct {
	// Tab is the terminal's id.
	Tab string
	// Agent: an agent CLI in the tab reports that it is working or waiting.
	Agent bool
}

// KnownAgents are the program names of agent CLIs Flint recognises in a
// command's ancestry wherever it runs.
var KnownAgents = []string{
	"claude", "codex", "gemini", "opencode", "aider", "cursor-agent", "goose", "amp",
	"qwen", "copilot", "crush", "droid", "kimi", "cline", "auggie", "kiro-cli",
}

// maxDepth bounds the walk up the process tree.
const maxDepth = 128

// Classify decides who runs pid, the process at the other end of the
// socket, by walking its ancestors. It is a second line of defence: a
// decision only the user takes (approving a step) is asked in Flint's own
// dialog whoever runs the command (cli.Command.Human), and a process can
// always hide its ancestry (docs/ARCHITECTURE.md, "Command-line socket").
//
//   - An ancestor (or pid itself) of this user that runs an agent CLI
//     (f.Agents, by program name: "node …/claude" is claude) makes it an
//     agent's, wherever it runs.
//   - Reaching Flint makes it an agent's unless the child of Flint it came
//     through is one of Flint's terminal tabs' shells in which no agent CLI
//     reports a state.
//   - Reaching the background host Flint started (f.HostPID, recorded at
//     its start, never recognised by a command line), or the top of the
//     tree (a process whose parent is init or launchd) without Flint, it
//     must come from a terminal: it has a controlling terminal, and its
//     session's leader (the terminal's shell) is itself or one of the
//     ancestors walked (under the host: a child of the host, a tab's
//     shell). A process that left its terminal (a background job whose
//     shell ended, nohup, setsid, a daemon, launchd or cron) is Detached.
//   - A walk that cannot be finished (a process of this user whose parent
//     or command line cannot be read, a tree deeper than maxDepth) is
//     unknown.
//
// Dispatch refuses every command that acts to an agent's, a detached and
// an unknown caller.
func Classify(pid int, t Tree, f Facts) cli.Caller {
	if pid <= 0 {
		return cli.Caller{Unknown: true, What: "this system does not say which process connected"}
	}
	me := cli.Caller{PID: pid}
	if args, err := t.Args(pid); err == nil {
		me.Program = terminal.ProgramName(args)
	}
	var chain []proctree.Proc
	cur := pid
	for range maxDepth {
		if cur == f.Self {
			return viaFlint(chain, f, me)
		}
		p, err := t.Parent(cur)
		if err != nil {
			return unknown(me, err.Error())
		}
		if p.UID == f.UID {
			args, err := t.Args(cur)
			if err != nil {
				return unknown(me, fmt.Sprintf("the command line of process %d cannot be read", cur))
			}
			if name := terminal.ProgramName(args); f.Agents[name] {
				if id := customAgent(args, f.Custom); id != "" {
					me.AgentName = id
				}
				return agent(me, name)
			}
		}
		if f.HostPID > 0 && cur == f.HostPID {
			return fromTerminal(chain, me, f.HostPID)
		}
		chain = append(chain, p)
		if p.PPID <= 1 {
			return fromTerminal(chain, me, 0)
		}
		cur = p.PPID
	}
	return unknown(me, fmt.Sprintf("more than %d processes above it", maxDepth))
}

func unknown(c cli.Caller, what string) cli.Caller {
	c.Unknown, c.What = true, what
	return c
}

func agent(c cli.Caller, what string) cli.Caller {
	c.Agent, c.What = true, what
	return c
}

// viaFlint decides for a process that descends from Flint: chain is its
// ancestry below Flint, the child of Flint last.
func viaFlint(chain []proctree.Proc, f Facts, me cli.Caller) cli.Caller {
	if len(chain) == 0 {
		return agent(me, "Flint itself")
	}
	child := chain[len(chain)-1].PID
	if sh, ok := f.Shells[child]; ok {
		if sh.Agent {
			return agent(me, "an agent CLI in a terminal tab")
		}
		return me
	}
	return agent(me, "a process started by Flint: an agent chat, a workflow step or a run")
}

// fromTerminal decides for a process whose walk ended without Flint: at the
// top of the tree, or (host > 0) at the background host Flint started.
// chain is its ancestry below that point, the caller first. It must have a
// controlling terminal whose session leader is in chain (under the host,
// a child of the host: a tab's shell).
func fromTerminal(chain []proctree.Proc, me cli.Caller, host int) cli.Caller {
	if len(chain) == 0 {
		return agent(me, "the background host itself")
	}
	caller := chain[0]
	if !caller.TTY {
		me.Detached, me.What = true, "it has no terminal: it runs in the background or under a service"
		return me
	}
	for _, p := range chain {
		if p.PID == caller.SID && (host == 0 || p.PPID == host) {
			return me
		}
	}
	me.Detached, me.What = true, "it is no longer in the terminal session it was started in"
	return me
}

// AgentNames are KnownAgents plus the program names of the configured
// commands (absolute paths or names; "" is skipped).
func AgentNames(configured ...string) map[string]bool {
	out := map[string]bool{}
	for _, n := range KnownAgents {
		out[n] = true
	}
	for _, c := range configured {
		if c == "" {
			continue
		}
		if n := filepath.Base(c); n != "." && n != "/" && n != "flint" {
			out[n] = true
		}
	}
	return out
}

// ThisProcess are the Facts this process knows about itself: its pid and
// its user. The caller adds the shells, the host and the agent names.
func ThisProcess() Facts {
	return Facts{Self: os.Getpid(), UID: uint32(os.Getuid())}
}
