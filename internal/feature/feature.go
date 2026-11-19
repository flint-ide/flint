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
}

// TaskStart is the chat of an agent task Flint is starting, for
// Instance.TaskContext.
type TaskStart struct {
	// Origin is the NewAgentTask.Origin the task was made with (never "").
	Origin string
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
	// TaskOrigin is the Origin of the agent task the chat with this session
	// id works for (a chat of a joint group: its lead's), "" when the chat is
	// no task's or its task has none. The id comes from Flint (an agent
	// tool's caller, mcpserver.CallerFrom), not from the UI.
	TaskOrigin(chat string) string
}

// NewAgentTask asks for one agent task: Agent works on Description in a
// worktree of the repository of Dir. Origin, when set, is the feature's own
// note of what the task is for (at most agenttask.MaxOrigin bytes, kept with
// the task): Flint passes it back to Instance.TaskContext when the task's
// chat starts, and Host.TaskOrigin gives it for the task's chat.
type NewAgentTask struct {
	Dir, Description, Agent string
	Origin                  string
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
// starts (Instance.TaskContext), the non-empty answers joined; "" when the
// task has no origin or no feature knows it.
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
