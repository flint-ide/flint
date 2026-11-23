// Package feature is the contract between Flint and a feature module: one
// package (internal/features/<id>) that declares everything the feature adds
// to the Go side, and one line in the list in package main (features.go).
//
// What a module may contribute:
//
//   - Settings: sections of settings.json it owns (settings.Section), loaded,
//     normalized, validated and saved with the built-in keys;
//   - Tools: external programs for the tool check and the bug report;
//   - State: keys of state.json it keeps through Host.LoadState / SaveState;
//   - New: what runs once the app is built: bound Wails services, tools for
//     agents on Flint's MCP server, and Start / Close lifecycle hooks.
//
// Declarations (Settings, Tools, State) are registered by Register before the
// settings are loaded; New is called while the app is built, before the window
// exists, so it must only construct (no I/O, no goroutines): Start is where
// background work begins, after launch.
//
// The frontend half of a feature is a module in frontend/src/features/<id>
// with the same id (see docs/design/extensions.md).
package feature

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"

	"flint/internal/cli"
	"flint/internal/mcpserver"
	"flint/internal/settings"
	"flint/internal/terminal"
	"flint/internal/tools"
)

// Module declares a feature.
type Module struct {
	// ID names the feature ("todo"); lower-case letters, digits and dashes.
	ID string
	// Settings are the settings.json keys the feature owns.
	Settings []settings.AnySection
	// Tools are external programs the feature drives.
	Tools []tools.Tool
	// State lists the state.json keys the feature keeps (Host.LoadState).
	State []string
	// New builds the running parts; nil when the feature has none.
	New func(Host) Instance
	// Optional features can be switched off in Settings ▸ Extensions (the
	// "features" settings key, Toggles); a switched-off feature isn't built,
	// so it binds no services and offers no tools until Flint restarts with
	// it on. Its settings and tools stay registered, so nothing is lost.
	Optional bool
}

// Instance is what a module contributes once the app is built. Every field
// is optional.
type Instance struct {
	// Services are bound to the frontend: application.NewService(&API{…})
	// written out in the module, where the bindings generator finds it; the
	// exported methods become calls in frontend/bindings.
	Services []application.Service
	// AgentTools are offered to agents by Flint's MCP server; read on every
	// request, so they can follow the feature's settings.
	AgentTools func() []mcpserver.Tool
	// Start runs once Flint has launched, with a context that ends at quit.
	// It must return quickly: long work belongs in a goroutine.
	Start func(ctx context.Context)
	// Close runs on quit, in parallel with the other features and services.
	Close func()
	// SettingsChanged runs after settings.json was saved, with the settings
	// before and after; it must return quickly.
	SettingsChanged func(prev, next settings.Settings)
	// RootsChanged runs after a project folder was opened, attached or
	// removed, or trusted or untrusted; it must return quickly.
	RootsChanged func()
	// TasksChanged runs after an agent task was created, removed or changed
	// state, or its branch was merged or got a pull request in Flint (when
	// the UI hears "tasks:changed"); it must return quickly.
	TasksChanged func()
	// TaskContext is asked, when Flint starts the chat of an agent task the
	// feature made (one whose NewAgentTask had an Origin), for text to put
	// before the task's description in the chat's first message: the feature's
	// context for that task as it is now, at the start (a queued task gets the
	// state of when it leaves the queue). It returns "" for an origin that is
	// not its own or that it no longer knows, and must not fail the start. It
	// is called once per chat, from the goroutine that starts it, so it may
	// read files and the Host's agent tasks; it must not ask the user.
	TaskContext func(ctx context.Context, start TaskStart) string
	// TaskWorkflow is offered by the feature that owns workflows. It is
	// asked by CreateAgentTasks, for every request that names a workflow
	// and before any task is created, to take the task: it checks the
	// request (the workflow exists for Dir, can run, its agents are
	// available, Dir is trusted) and plans the run, which nobody sees until
	// it is bound. Its answer says who takes the task's first chat, what to
	// do once the task exists (Bind, before the chat starts, so TaskContext
	// knows the run) and when the creation failed (Drop). It must not ask
	// the user.
	TaskWorkflow func(ctx context.Context, req NewAgentTask) (PlannedWorkflow, error)
	// TaskWorkflowCheck is offered by the feature that owns workflows with
	// TaskWorkflow: every check TaskWorkflow makes for req, planning nothing
	// and asking nothing (an agent's delegation is checked before the user
	// is asked to approve it). It must not ask the user.
	TaskWorkflowCheck func(ctx context.Context, req NewAgentTask) error
	// FolderProjects is offered by the feature that owns projects: the
	// projects whose folder is trusted that the workspace folder dir is
	// "in" (the project's folder is dir or inside it, or dir's checkout has
	// a remote that is one of the project's repositories) and the agent
	// tasks linked to their items. Nothing for an agent task's worktree. It
	// lets the feature that owns workflows scope a coordinating agent to
	// its projects' runs. It must return quickly and must not ask the user.
	FolderProjects func(ctx context.Context, dir string) FolderProjects
	// TaskRun is the workflow run that takes the agent task with this id
	// (the feature that owns workflows answers), ok false when none does.
	// It must return quickly: it is asked for every task Host.AgentTasks
	// reads.
	TaskRun func(taskID string) (run TaskRun, ok bool)
	// TaskStartCheck is asked before Flint starts the chat of an agent task
	// by itself (the queue, Start now, a feature's create, a restart): an
	// error refuses the start, and the task stays off the queue without a
	// chat (the error is logged). The feature that owns workflows refuses
	// the task of a run that is over or that it does not know, so a stopped
	// run never starts an agent later. It must return quickly and must not
	// ask the user.
	TaskStartCheck func(start TaskStart) error
	// TaskChatStarted tells the feature which chat Flint opened for an
	// agent task's start (start as TaskContext got it), right after the
	// chat registered and before its first message is sent, so the chat's
	// end can never come before the feature knows the chat. It must return
	// quickly.
	TaskChatStarted func(start TaskStart, chat string)
	// CLI are the feature's commands of the flint command line (package
	// cli): each calls the functions the feature's bound API calls, and says
	// whether it reads or acts.
	CLI []cli.Command
}

// CLI are every module's commands, in list order.
func (s *Set) CLI() []cli.Command {
	if s == nil {
		return nil
	}
	var out []cli.Command
	for _, p := range s.parts {
		out = append(out, p.CLI...)
	}
	return out
}

// PlannedWorkflow is a workflow run planned for an agent task that does not
// exist yet (Instance.TaskWorkflow).
type PlannedWorkflow struct {
	// Agent takes the task's first chat: the first step's agent, else the
	// run's (the request's Agent, else the default one).
	Agent string
	// Bind gives the run its task, once every task of the call exists and
	// before any of their chats start. An error rolls the call back.
	Bind func(taskID string) error
	// Drop forgets the run: its task was not created, or the call was
	// rolled back after Bind. Never called once the call succeeded.
	Drop func()
}

// FolderProjects are the projects a workspace folder is in
// (Instance.FolderProjects).
type FolderProjects struct {
	// Names are the projects' names, Tasks the ids of the agent tasks
	// linked to their items (removed ones left out).
	Names, Tasks []string
}

// TaskRun is the workflow run that takes an agent task (AgentTask.Run).
type TaskRun struct {
	// ID is the run's id, Workflow the workflow's name.
	ID, Workflow string
	// State is the run's: running, waiting (a gate waits for the user),
	// failed, paused, done or stopped.
	State string
	// AtLastStep: the run's current step is its last one, so a run that
	// waits at a gate here has nothing left for agents to do.
	AtLastStep bool
}

// ErrWorkflowsOff is CreateAgentTasks' refusal of a request that names a
// workflow while no feature takes workflows.
var ErrWorkflowsOff = errors.New("workflows are switched off (Settings ▸ Extensions ▸ Built-in)")

// ErrJointWorkflow is CreateJointAgentTasks' refusal of a request that names
// a workflow: a run takes one task in one repository.
var ErrJointWorkflow = errors.New("a workflow runs in one repository: a joint agent task cannot take one")

// TaskStart is the chat of an agent task Flint is starting, for
// Instance.TaskContext.
type TaskStart struct {
	// Origin is the NewAgentTask.Origin the task was made with (never "").
	Origin string
	// TaskID is the agent task whose chat starts (a joint group's lead).
	TaskID string
	// Worktree is where the chat runs (the lead's for a joint group), Branch
	// its branch and Base the branch it started from.
	Worktree, Branch, Base string
	// Joint: the chat works for a joint group; Flint adds to the first
	// message which folder is which repository (after the description).
	Joint bool
	// MCP: Flint's MCP server, with the features' agent tools, is offered to
	// this chat (the server is on and the worktree is trusted).
	MCP bool
}

// Host is what Flint gives a feature: the parts of the app it may use. New
// needs go here, not into the feature reaching for App.
type Host interface {
	// Context ends when Flint quits; nil until Flint has launched.
	Context() context.Context
	// Settings is a copy of the current settings (sections: Section.Get).
	Settings() settings.Settings
	// Roots are the open project folders, the primary one first.
	Roots() []string
	// Trusted reports whether the user trusts the folder (Restricted Mode is
	// off there).
	Trusted(path string) bool
	// ConfigDir is Flint's configuration folder (settings.json lives there).
	ConfigDir() string
	// Confirm asks the user in a native dialog that the web view can't
	// answer or skip; Cancel is the default button. False when dismissed.
	// It waits until the user answers, however long that takes (false when
	// Flint quits meanwhile: the feature's context is done then).
	Confirm(title, message, ok string) bool
	// Choose asks a native question with these buttons (at most three) and
	// returns the one chosen, "" when dismissed.
	Choose(title, message string, buttons []string) string
	// Emit sends an event to the frontend (dropped before launch).
	Emit(event string, data ...any)
	// OpenURL opens an http(s) link in the default browser.
	OpenURL(link string) error
	// Version is Flint's version ("v1.2.0", or "dev…" for builds from source).
	Version() string
	// TasksChanged tells Flint that something the agent tasks show changed
	// (a workflow run that takes a task moved on): the UI hears
	// "tasks:changed" and every feature's TasksChanged hook runs. It must
	// not be called with a lock the hooks take.
	TasksChanged()
	// LoadState decodes the feature's state.json key into v (left as is when
	// the key is absent); SaveState replaces it. Keys must be in Module.State.
	LoadState(key string, v any) error
	SaveState(key string, v any) error
	// StartStream shows st in the terminal tab termID (the frontend picked the
	// id) and lets agents read it like a terminal (get_terminal_output).
	StartStream(termID, title string, st terminal.Stream) error

	// The agent tasks part (the Projects feature): the narrowest view of agent
	// tasks, agents and repositories that lets a feature hand work to agents
	// through the same path as New Agent Task.

	// CreateAgentTasks creates one agent task per request (a branch and a
	// worktree from Dir's current branch, the queue and its limits, the trust
	// of the worktree) and starts the chats of those that got a slot; the
	// others are queued. Dir must be a folder inside an open project folder.
	// All or nothing: when one cannot be created, none is kept, and a
	// queued task is not started before the call returns. A checkout on a
	// detached HEAD fails with *DetachedHeadError.
	CreateAgentTasks(ctx context.Context, reqs []NewAgentTask) ([]AgentTask, error)
	// CreateJointAgentTasks creates one joint group: an agent task per
	// request, each in another repository (2 to agenttask.MaxJoint of them), and one
	// chat of agent that works in all of their worktrees, started in the
	// first one's (the lead) or queued as a unit. Each request's Agent must
	// be agent or empty. The chat's first message is Flint's: the lead's
	// description, which folder is which repository, and a request's own
	// description only where it differs from the lead's. All or nothing,
	// like CreateAgentTasks, and a detached HEAD fails with
	// *DetachedHeadError. An agent whose engine cannot work in several
	// folders fails with an error that is ErrJointEngine, a repository or
	// worktree that is not trusted with one that is ErrJointUntrusted; both
	// before anything is created. The tasks are returned lead first.
	CreateJointAgentTasks(ctx context.Context, reqs []NewAgentTask, agent string) ([]AgentTask, error)
	// AgentTasks returns the tasks with these ids that still exist, by id.
	AgentTasks(ctx context.Context, ids []string) map[string]AgentTask
	// Repository describes the git repository dir is in (git.ErrNotRepository
	// when none).
	Repository(ctx context.Context, dir string) (Repository, error)
	// Agents are the agents that can start now and the default one.
	Agents() Agents
	// CheckTaskWorkflow makes every check CreateAgentTasks would make for
	// req's workflow (Instance.TaskWorkflowCheck of the feature that owns
	// workflows), creating, planning and asking nothing; ErrWorkflowsOff
	// when no feature takes workflows.
	CheckTaskWorkflow(ctx context.Context, req NewAgentTask) error
	// FolderProjects are the projects the workspace folder dir is in and
	// their items' agent tasks (Instance.FolderProjects of the feature that
	// owns projects; nothing when none runs).
	FolderProjects(ctx context.Context, dir string) FolderProjects
	// TaskOrigin is the Origin of the agent task the chat with this session
	// id works for (a chat of a joint group: its lead's), "" when the chat is
	// no task's or its task has none. The id comes from Flint (an agent
	// tool's caller, mcpserver.CallerFrom), not from the UI.
	TaskOrigin(chat string) string

	// The workflow steps part (the Workflows feature): the steps of a run
	// are taken inside one agent task's worktree, agent steps as chats and
	// script steps as run configurations, through the paths the user's own
	// chats and runs take. Every argument comes from the feature's Go code,
	// never from the UI.

	// StartStepChat opens a new chat of agent ("" = the task's agent) in the
	// worktree of the agent task taskID (the lead's for a joint group) and
	// sends prompt as its first message, shown as coming from Flint; it
	// takes the path of every prompt (usage budget, redaction). It refuses
	// an invalid or unknown task, a worktree that is gone or not trusted
	// (Restricted Mode), and an empty prompt or one over 64 KiB. Returns
	// the chat's id. When the chat opened but the prompt was refused (the
	// usage budget, say), the id is returned next to the error. opts.Reviewer
	// starts a chat that reviews the worktree's code (StepChatOptions).
	StartStepChat(ctx context.Context, taskID, agent, prompt string, opts StepChatOptions) (chat string, err error)
	// WaitChat blocks until the chat's running turn is over (at once when
	// none runs) and returns how it ended; an error the agent reports in
	// the middle of a turn does not end the wait. No polling. A chat that is
	// closed, before or while it waits, is an error; so is ctx ending.
	WaitChat(ctx context.Context, chat string) (ChatResult, error)
	// ChatOutput is the last lines (1 to 200) of the chat's text and
	// tool-call titles, redacted; "" for an unknown chat.
	ChatOutput(chat string, lines int) string
	// StopChat ends the chat and the agents it started; an unknown or empty
	// id is a no-op.
	StopChat(chat string) error
	// RunConfiguration runs the run configuration called name (exactly) in
	// the worktree of the agent task taskID, shows it in a run tab, waits
	// for it and returns how it ended. The configuration is read from the
	// task's checkout (its .flint/run.json, then the detected ones), never
	// from the worktree, so an agent working there cannot change what runs.
	// It refuses an unknown task, a checkout or worktree that is gone or not
	// trusted, and a name the checkout has no configuration for. When ctx
	// ends, the run is stopped and ctx's error returned. opts.Sandbox
	// confines every process of the run (RunOptions); when this system has
	// no sandbox the run fails and nothing starts.
	//
	// Before the run Flint takes a fingerprint of what the worktree holds
	// for later steps to run or read (sandbox.WorktreeProtected: .git,
	// .claude, .codex, .flint, CLAUDE.md, AGENTS.md …) and after it compares
	// it again: what changed is RunResult.Changed. A worktree whose
	// fingerprint cannot be taken fails the run before it starts.
	RunConfiguration(ctx context.Context, taskID, name string, opts RunOptions) (RunResult, error)
	// RunLaunch is what the run configuration called name of the checkout
	// (a folder inside an open, trusted workspace folder: the repository's
	// top folder a run's task branches from) would run, resolved as
	// RunConfiguration resolves it, without running anything. It refuses a
	// checkout that is gone, outside the workspace or not trusted and a name
	// the checkout has no configuration for.
	RunLaunch(checkout, name string) (RunLaunch, error)
	// ScriptSandbox says whether this system can run a script step in the
	// sandbox (RunOptions.Sandbox), and why not.
	ScriptSandbox() SandboxSupport
	// BudgetPause says why a new agent turn may not start now: a usage
	// budget is spent and usage.pauseOverBudget is on (nil otherwise). A
	// feature that starts work by itself (a schedule) asks before it does.
	BudgetPause() error
}

// StepChatOptions are how Host.StartStepChat opens a step's chat.
type StepChatOptions struct {
	// Reviewer: the chat reviews code another agent wrote in the worktree.
	// It is started without the worktree's project settings and instruction
	// files where the agent's engine has a switch for it (Claude Code:
	// --setting-sources user, which leaves out .claude/settings*.json, its
	// hooks and the project's CLAUDE.md; Codex: AGENTS.md off), without the
	// user's extra arguments (a relative path in them would resolve in the
	// worktree), and no tool call of it is approved by the approval policy:
	// every edit, read and command asks.
	Reviewer bool
}

// RunLaunch is a run configuration's resolved launch (Host.RunLaunch).
type RunLaunch struct {
	// Name is the configuration's name.
	Name string
	// Commands are its processes in order.
	Commands []RunCommand
	// Cwd is the working folder relative to the checkout ("." for its top).
	Cwd string
	// Env are the KEY=VALUE pairs it adds to the environment.
	Env []string
}

// RunCommand is one process of a RunLaunch.
type RunCommand struct {
	// Line is the command line as the run tab shows it.
	Line    string
	Program string
	Args    []string
}

// CommandLine is the launch's command lines joined with " && " (how the
// processes run: a failing one stops the rest).
func (l RunLaunch) CommandLine() string {
	lines := make([]string, len(l.Commands))
	for i, c := range l.Commands {
		lines[i] = c.Line
	}
	return strings.Join(lines, " && ")
}

// SandboxSupport is whether this system has the sandbox a script step runs
// in (Host.ScriptSandbox).
type SandboxSupport struct {
	Available bool
	// Mechanism names it ("sandbox-exec", "Landlock"); Reason says why there
	// is none.
	Mechanism, Reason string
}

// RunOptions are how Host.RunConfiguration runs a configuration.
type RunOptions struct {
	// Sandbox, when set, runs every process of the configuration inside the
	// operating system's sandbox (the one extensions run in).
	Sandbox *Sandbox
}

// Sandbox is what a sandboxed run may do besides reading what the user can
// read (except credentials, keys, history and Flint's own folder) and
// writing the task's worktree and the temporary folders.
type Sandbox struct {
	// Network: it may use the network (Internet and local sockets).
	Network bool
	// Writable are more absolute folders it may write.
	Writable []string
}

// ChatResult is how a chat's turn ended (Host.WaitChat).
type ChatResult struct {
	// State is agenttask.Finished, agenttask.Failed, or agenttask.Idle for
	// a turn that was cancelled (or a chat that never started one).
	State string
	// Error is what the agent reported when it failed (redacted, short).
	Error string
}

// RunResult is how a run configuration ended (Host.RunConfiguration).
type RunResult struct {
	// ID is the run's id (its run tab).
	ID string
	// Code is the exit code of the last step that ran; -1 when the process
	// was killed by a signal or did not start.
	Code int
	// Stopped: the run was stopped (by the user, in its tab).
	Stopped bool
	// Error is set when a step could not be started.
	Error string
	// Output is the end of what the run printed: plain text, redacted, at
	// most 8 KiB.
	Output string
	// Changed lists what the run changed in the worktree that later steps
	// run or read (paths relative to the worktree: ".claude/settings.json",
	// "CLAUDE.md"), or why that could not be checked after it; empty when
	// nothing changed.
	Changed []string
}

// NewAgentTask asks for one agent task: Agent works on Description in a
// worktree of the repository of Dir. Origin, when set, is the feature's own
// note of what the task is for (at most agenttask.MaxOrigin bytes, kept with
// the task): Flint passes it back to Instance.TaskContext when the task's
// chat starts, and Host.TaskOrigin gives it for the task's chat.
//
// Workflow, when set, names the workflow that takes the task ("" = one
// agent): the feature that owns workflows checks and plans the run
// (Instance.TaskWorkflow) before anything is created, the task's agent is
// the run's first step's, and the first chat starts with the step's prompt
// after the other features' context. The request's Agent is then the run's
// agent (for the steps that name none; "" = the default one).
//
// DelegatedBy, when set, says that an agent's call the user approved asked
// for the task (delegate_project_item): "an agent in <folder> (chat <id>)".
// A workflow run that takes such a task is the agent's (its origin and log
// say so), and script steps without a sandbox are refused until the user
// allowed them in that checkout, never asked.
type NewAgentTask struct {
	Dir, Description, Agent string
	Origin                  string
	Workflow                string
	DelegatedBy             string
}

// DetachedHeadError is CreateAgentTasks' error for a request whose Dir is a
// checkout on a detached HEAD: a task starts from the checkout's current
// branch and merges back into it, so there must be one.
type DetachedHeadError struct {
	// Dir is the request's folder.
	Dir string
}

func (e *DetachedHeadError) Error() string { return e.Dir + " is on a detached HEAD" }

// CreateJointAgentTasks' refusals, matched with errors.Is; the error's own
// text is the one to show (it names the agent, engine or folder).
var (
	// ErrJointEngine: the agent's engine cannot be given additional folders
	// (only Claude Code can; not Codex, not a custom ACP agent).
	ErrJointEngine = errors.New("the agent cannot work in several folders at once")
	// ErrJointUntrusted: a repository or a worktree of the group is not
	// trusted (Restricted Mode).
	ErrJointUntrusted = errors.New("a joint agent task works only in trusted folders")
)

// AgentTask is an agent task as the Agent Tasks panel shows it.
type AgentTask struct {
	ID, Title, Agent, Branch, Base string
	// Worktree is the task's folder.
	Worktree string
	// State: queued, idle, working, waiting, finished, failed or interrupted
	// (a turn cut off before it ended, e.g. by quitting Flint).
	State, Error string
	// PullRequest is the URL of the pull request Flint created for it.
	PullRequest string
	// Merged: the branch has commits and its base has all of them.
	Merged bool
	// Stale: the worktree folder is gone.
	Stale bool
	// Joint is the id of the task's joint group ("" for a task with a chat
	// of its own); Lead marks the group's task whose worktree its one chat
	// runs in.
	Joint string
	Lead  bool
	// Run is the workflow run that takes the task (zero when none).
	Run TaskRun
}

// Repository is the git repository a folder is in.
type Repository struct {
	// Root is the checkout's top folder.
	Root string
	// Remotes are the URLs of its remotes, in git config order.
	Remotes []string
	// TaskWorktree: the checkout is an agent task's worktree.
	TaskWorktree bool
}

// Agents are the agents that can start now (defined, native and custom
// ones) and the default agent ("" when none can start).
type Agents struct {
	Available []string
	Default   string
}

var idPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`)

// Register checks the modules and registers their settings sections and
// tools; call it once, before settings are loaded.
func Register(mods []Module) error {
	seen := map[string]bool{}
	stateKeys := map[string]string{}
	for _, m := range mods {
		if !idPattern.MatchString(m.ID) {
			return fmt.Errorf("feature: invalid id %q", m.ID)
		}
		if seen[m.ID] {
			return fmt.Errorf("feature: %q listed twice", m.ID)
		}
		seen[m.ID] = true
		for _, key := range m.State {
			if other, ok := stateKeys[key]; ok {
				return fmt.Errorf("feature: state key %q used by %s and %s", key, other, m.ID)
			}
			stateKeys[key] = m.ID
		}
	}
	settings.Register(Section)
	for _, m := range mods {
		settings.Register(m.Settings...)
		tools.Register(m.Tools...)
	}
	return nil
}

// StateKeys are the state.json keys the modules keep.
func StateKeys(mods []Module) []string {
	var out []string
	for _, m := range mods {
		out = append(out, m.State...)
	}
	return out
}

// Set is the running modules; a nil Set has none.
type Set struct {
	parts  []Instance
	active []string
}

// New builds every module that is switched on (Toggles) and has running
// parts, in list order.
func New(mods []Module, h Host) *Set {
	s := &Set{}
	toggles := Section.Default()
	if h != nil {
		toggles = Section.Get(h.Settings())
	}
	for _, m := range mods {
		if !toggles.On(m) {
			continue
		}
		s.active = append(s.active, m.ID)
		if m.New != nil {
			s.parts = append(s.parts, m.New(h))
		}
	}
	return s
}

// Has reports whether the module with this id runs in this process.
func (s *Set) Has(id string) bool {
	return s != nil && slices.Contains(s.active, id)
}

// Active are the ids of the modules running in this process, in list order:
// the frontend installs the halves of these and leaves the others out.
func (s *Set) Active() []string {
	if s == nil {
		return nil
	}
	return slices.Clone(s.active)
}

// Services are every module's bound services.
func (s *Set) Services() []application.Service {
	if s == nil {
		return nil
	}
	var out []application.Service
	for _, p := range s.parts {
		out = append(out, p.Services...)
	}
	return out
}

// AgentTools are every module's tools for agents right now.
func (s *Set) AgentTools() []mcpserver.Tool {
	if s == nil {
		return nil
	}
	var out []mcpserver.Tool
	for _, p := range s.parts {
		if p.AgentTools != nil {
			out = append(out, p.AgentTools()...)
		}
	}
	return out
}

// Start runs the Start hooks.
func (s *Set) Start(ctx context.Context) {
	if s == nil {
		return
	}
	for _, p := range s.parts {
		if p.Start != nil {
			p.Start(ctx)
		}
	}
}

// SettingsChanged runs the SettingsChanged hooks.
func (s *Set) SettingsChanged(prev, next settings.Settings) {
	if s == nil {
		return
	}
	for _, p := range s.parts {
		if p.SettingsChanged != nil {
			p.SettingsChanged(prev, next)
		}
	}
}

// RootsChanged runs the RootsChanged hooks.
func (s *Set) RootsChanged() {
	if s == nil {
		return
	}
	for _, p := range s.parts {
		if p.RootsChanged != nil {
			p.RootsChanged()
		}
	}
}

// TasksChanged runs the TasksChanged hooks.
func (s *Set) TasksChanged() {
	if s == nil {
		return
	}
	for _, p := range s.parts {
		if p.TasksChanged != nil {
			p.TasksChanged()
		}
	}
}

// TaskContext is the context the features give for a task's chat that
// starts (Instance.TaskContext), the non-empty answers joined in the
// modules' list order; "" when the task has no origin or no feature knows
// it. package main lists Projects before Workflows, so an item's task that
// a workflow takes starts with the project's block, then the step's block,
// then the task's description (which ends the step's block).
func (s *Set) TaskContext(ctx context.Context, start TaskStart) string {
	if s == nil || start.Origin == "" {
		return ""
	}
	var out []string
	for _, p := range s.parts {
		if p.TaskContext == nil {
			continue
		}
		if text := p.TaskContext(ctx, start); text != "" {
			out = append(out, text)
		}
	}
	return strings.Join(out, "\n\n")
}

// TaskStartCheck asks every module whether Flint may start the chat of
// the agent task start describes (Instance.TaskStartCheck); the first
// refusal wins.
func (s *Set) TaskStartCheck(start TaskStart) error {
	if s == nil {
		return nil
	}
	for _, p := range s.parts {
		if p.TaskStartCheck == nil {
			continue
		}
		if err := p.TaskStartCheck(start); err != nil {
			return err
		}
	}
	return nil
}

// TaskChatStarted tells every module which chat Flint opened for an agent
// task's start (Instance.TaskChatStarted).
func (s *Set) TaskChatStarted(start TaskStart, chat string) {
	if s == nil || chat == "" {
		return
	}
	for _, p := range s.parts {
		if p.TaskChatStarted != nil {
			p.TaskChatStarted(start, chat)
		}
	}
}

// TaskWorkflow asks the one module that takes workflows to plan the run of
// req (Instance.TaskWorkflow); ErrWorkflowsOff when none runs.
func (s *Set) TaskWorkflow(ctx context.Context, req NewAgentTask) (PlannedWorkflow, error) {
	if s != nil {
		for _, p := range s.parts {
			if p.TaskWorkflow != nil {
				return p.TaskWorkflow(ctx, req)
			}
		}
	}
	return PlannedWorkflow{}, ErrWorkflowsOff
}

// TaskWorkflowCheck asks the one module that takes workflows to check req
// (Instance.TaskWorkflowCheck); ErrWorkflowsOff when none runs.
func (s *Set) TaskWorkflowCheck(ctx context.Context, req NewAgentTask) error {
	if s != nil {
		for _, p := range s.parts {
			if p.TaskWorkflowCheck != nil {
				return p.TaskWorkflowCheck(ctx, req)
			}
		}
	}
	return ErrWorkflowsOff
}

// FolderProjects joins the modules' answers (Instance.FolderProjects).
func (s *Set) FolderProjects(ctx context.Context, dir string) FolderProjects {
	var out FolderProjects
	if s == nil {
		return out
	}
	for _, p := range s.parts {
		if p.FolderProjects == nil {
			continue
		}
		fp := p.FolderProjects(ctx, dir)
		out.Names = append(out.Names, fp.Names...)
		out.Tasks = append(out.Tasks, fp.Tasks...)
	}
	return out
}

// TaskRun is the workflow run that takes the task (Instance.TaskRun), the
// zero TaskRun when none does.
func (s *Set) TaskRun(taskID string) TaskRun {
	if s == nil {
		return TaskRun{}
	}
	for _, p := range s.parts {
		if p.TaskRun == nil {
			continue
		}
		if r, ok := p.TaskRun(taskID); ok {
			return r
		}
	}
	return TaskRun{}
}

// Closers are the Close hooks, for the app's parallel shutdown.
func (s *Set) Closers() []func() {
	if s == nil {
		return nil
	}
	var out []func()
	for _, p := range s.parts {
		if p.Close != nil {
			out = append(out, p.Close)
		}
	}
	return slices.Clip(out)
}
