// Package featuretest is an in-memory feature.Host for the tests of feature
// modules: settings, roots and state.json keys are plain fields, events and
// opened links are recorded, and nothing touches the disk or a window.
package featuretest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"

	"flint/internal/agenttask"
	"flint/internal/feature"
	"flint/internal/settings"
	"flint/internal/terminal"
)

var _ feature.Host = (*Host)(nil)

// Host implements feature.Host. Its zero value is not ready: use New.
type Host struct {
	Ctx context.Context
	// Version is what Version reports ("v1.0.0" by default).
	VersionString string

	// Dir is ConfigDir (a test's temporary folder).
	Dir string
	// Answer is what Confirm returns; Choice what Choose returns.
	Answer bool
	Choice string
	// OnConfirm, when set, runs in Confirm before it answers (outside the
	// host's lock): what happens while the user reads the question.
	OnConfirm func()

	mu       sync.Mutex
	settings settings.Settings
	roots    []string
	trusted  map[string]bool
	asked    []string
	state    map[string]json.RawMessage
	events   []Event
	opened   []string
	streams  map[string]terminal.Stream

	// The agent tasks part: tasks by id, repositories by folder, agents, the
	// requests CreateAgentTasks got, and an error it fails with.
	tasks     map[string]feature.AgentTask
	repos     map[string]feature.Repository
	agents    feature.Agents
	requested [][]feature.NewAgentTask
	joint     []JointRequest
	createErr error
	nextID    int
	// origins are the tasks' origins by task id; chats the task of each chat.
	origins map[string]string
	chats   map[string]string
}

// Event is one Emit call.
type Event struct {
	Name string
	Data []any
}

// New returns a host with default settings and a context that ends with the test.
func New(ctx context.Context) *Host {
	return &Host{
		Ctx: ctx, VersionString: "v1.0.0", settings: settings.Default(),
		state: map[string]json.RawMessage{}, streams: map[string]terminal.Stream{},
		tasks: map[string]feature.AgentTask{}, repos: map[string]feature.Repository{},
		origins: map[string]string{}, chats: map[string]string{},
	}
}

func (h *Host) Context() context.Context { return h.Ctx }

func (h *Host) Settings() settings.Settings {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.settings.Clone()
}

// SetSettings replaces the settings the feature reads.
func (h *Host) SetSettings(s settings.Settings) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.settings = s
}

func (h *Host) Roots() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string{}, h.roots...)
}

// Trusted reports a folder marked with SetTrusted or inside one, as the app
// does (paths compared as given).
func (h *Host) Trusted(path string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for p := range h.trusted {
		if path == p || strings.HasPrefix(path, p+"/") {
			return true
		}
	}
	return false
}

// SetTrusted marks folders as trusted.
func (h *Host) SetTrusted(paths ...string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.trusted == nil {
		h.trusted = map[string]bool{}
	}
	for _, p := range paths {
		h.trusted[p] = true
	}
}

// SetUntrusted takes the trust of folders marked with SetTrusted back.
func (h *Host) SetUntrusted(paths ...string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, p := range paths {
		delete(h.trusted, p)
	}
}

func (h *Host) ConfigDir() string { return h.Dir }

func (h *Host) Confirm(title, message, _ string) bool {
	h.mu.Lock()
	h.asked = append(h.asked, title+"\n"+message)
	on := h.OnConfirm
	h.mu.Unlock()
	if on != nil {
		on()
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.Answer
}

func (h *Host) Choose(title, message string, _ []string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.asked = append(h.asked, title+"\n"+message)
	return h.Choice
}

// Asked returns the questions Confirm and Choose were asked.
func (h *Host) Asked() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string{}, h.asked...)
}

// SetRoots replaces the open project folders.
func (h *Host) SetRoots(roots ...string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.roots = roots
}

func (h *Host) Emit(event string, data ...any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, Event{event, data})
}

// Events returns the events emitted so far.
func (h *Host) Events() []Event {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]Event{}, h.events...)
}

func (h *Host) OpenURL(link string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.opened = append(h.opened, link)
	return nil
}

// Opened returns the links passed to OpenURL.
func (h *Host) Opened() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string{}, h.opened...)
}

func (h *Host) Version() string { return h.VersionString }

func (h *Host) LoadState(key string, v any) error {
	h.mu.Lock()
	raw, ok := h.state[key]
	h.mu.Unlock()
	if !ok {
		return nil
	}
	return json.Unmarshal(raw, v)
}

func (h *Host) SaveState(key string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.state[key] = raw
	return nil
}

// State returns the raw state of key (nil when never saved).
func (h *Host) State(key string) json.RawMessage {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.state[key]
}

func (h *Host) StartStream(termID, _ string, st terminal.Stream) error {
	if st.Reader == nil {
		return errors.New("no output")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.streams[termID] = st
	return nil
}

// CreateAgentTasks records the requests and makes a task for each, working
// in a folder named after the request's; with SetCreateError it creates none.
// Dir must be inside a root, as the app requires.
func (h *Host) CreateAgentTasks(ctx context.Context, reqs []feature.NewAgentTask) ([]feature.AgentTask, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.requested = append(h.requested, slices.Clone(reqs))
	if h.createErr != nil {
		return nil, h.createErr
	}
	if err := ctx.Err(); err != nil { // as the app's git calls would
		return nil, err
	}
	if len(reqs) == 0 {
		return nil, errors.New("no agent task to create")
	}
	for _, r := range reqs {
		if !h.inRoot(r.Dir) {
			return nil, fmt.Errorf("%s is outside the open folders", r.Dir)
		}
		if !slices.Contains(h.agents.Available, r.Agent) {
			return nil, fmt.Errorf("the agent %q is not installed", r.Agent)
		}
	}
	var out []feature.AgentTask
	for _, r := range reqs {
		h.nextID++
		id := fmt.Sprintf("%016x", h.nextID)
		t := feature.AgentTask{ID: id, Title: r.Description, Agent: r.Agent, Branch: "task-" + id, Base: "main", Worktree: r.Dir + "-" + id, State: "working"}
		h.tasks[id] = t
		h.origins[id] = r.Origin
		out = append(out, t)
	}
	return out, nil
}

// JointRequest is one CreateJointAgentTasks call.
type JointRequest struct {
	Reqs  []feature.NewAgentTask
	Agent string
}

// CreateJointAgentTasks records the call and makes one joint group, a task
// per request (lead first, State as CreateAgentTasks'), with the checks the
// app makes that need no git: 2 to agenttask.MaxJoint requests in folders
// inside a root, each a different one, agent available. With
// SetCreateError it fails with that error and creates none: to fake the
// app's refusals, wrap feature.ErrJointEngine or feature.ErrJointUntrusted.
func (h *Host) CreateJointAgentTasks(ctx context.Context, reqs []feature.NewAgentTask, agent string) ([]feature.AgentTask, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.joint = append(h.joint, JointRequest{slices.Clone(reqs), agent})
	if h.createErr != nil {
		return nil, h.createErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch {
	case len(reqs) < 2:
		return nil, errors.New("a joint agent task needs at least two repositories")
	case len(reqs) > agenttask.MaxJoint:
		return nil, fmt.Errorf("a joint agent task works in at most %d repositories", agenttask.MaxJoint)
	case !slices.Contains(h.agents.Available, agent):
		return nil, fmt.Errorf("the agent %q is not installed", agent)
	}
	seen := map[string]bool{}
	for _, r := range reqs {
		switch {
		case !h.inRoot(r.Dir):
			return nil, fmt.Errorf("%s is outside the open folders", r.Dir)
		case r.Agent != "" && r.Agent != agent:
			return nil, fmt.Errorf("%s: a joint agent task has one agent", r.Dir)
		case seen[r.Dir]:
			return nil, fmt.Errorf("%s is listed twice", r.Dir)
		}
		seen[r.Dir] = true
	}
	h.nextID++
	group := fmt.Sprintf("j%015x", h.nextID)
	var out []feature.AgentTask
	for i, r := range reqs {
		h.nextID++
		id := fmt.Sprintf("%016x", h.nextID)
		t := feature.AgentTask{ID: id, Title: r.Description, Agent: agent, Branch: "task-" + id, Base: "main", Worktree: r.Dir + "-" + id,
			State: "working", Joint: group, Lead: i == 0}
		h.tasks[id] = t
		h.origins[id] = r.Origin
		out = append(out, t)
	}
	return out, nil
}

// JointRequested returns the CreateJointAgentTasks calls.
func (h *Host) JointRequested() []JointRequest {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.joint)
}

func (h *Host) inRoot(dir string) bool {
	for _, root := range h.roots {
		if dir == root || len(dir) > len(root) && dir[:len(root)] == root && dir[len(root)] == '/' {
			return true
		}
	}
	return false
}

// SetCreateError makes CreateAgentTasks and CreateJointAgentTasks fail with
// err (nil: succeed).
func (h *Host) SetCreateError(err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.createErr = err
}

// Requested returns the requests CreateAgentTasks got, one slice per call.
func (h *Host) Requested() [][]feature.NewAgentTask {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.requested)
}

func (h *Host) AgentTasks(_ context.Context, ids []string) map[string]feature.AgentTask {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := map[string]feature.AgentTask{}
	for _, id := range ids {
		if t, ok := h.tasks[id]; ok {
			out[id] = t
		}
	}
	return out
}

// SetAgentTask adds or replaces a task.
func (h *Host) SetAgentTask(t feature.AgentTask) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.tasks[t.ID] = t
}

// UpdateAgentTask changes a task in place (nothing when it does not exist).
func (h *Host) UpdateAgentTask(id string, fn func(*feature.AgentTask)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if t, ok := h.tasks[id]; ok {
		fn(&t)
		h.tasks[id] = t
	}
}

// RemoveAgentTask forgets a task, as removing it in the Agent Tasks panel does.
func (h *Host) RemoveAgentTask(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.tasks, id)
}

// AllAgentTasks returns every task.
func (h *Host) AllAgentTasks() map[string]feature.AgentTask {
	h.mu.Lock()
	defer h.mu.Unlock()
	return maps.Clone(h.tasks)
}

// Repository describes the repository set for dir or a folder above it
// (SetRepository); git.ErrNotRepository's text otherwise.
func (h *Host) Repository(_ context.Context, dir string) (feature.Repository, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for d := dir; ; {
		if r, ok := h.repos[d]; ok {
			r.Remotes = slices.Clone(r.Remotes)
			return r, nil
		}
		i := len(d) - 1
		for i > 0 && d[i] != '/' {
			i--
		}
		if i <= 0 {
			return feature.Repository{}, errors.New("not a git repository")
		}
		d = d[:i]
	}
}

// SetRepository makes dir (and folders in it) a checkout with these remotes.
func (h *Host) SetRepository(dir string, remotes ...string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.repos[dir] = feature.Repository{Root: dir, Remotes: remotes}
}

// SetTaskWorktree marks the checkout at dir as an agent task's worktree.
func (h *Host) SetTaskWorktree(dir string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	r := h.repos[dir]
	r.TaskWorktree = true
	h.repos[dir] = r
}

func (h *Host) Agents() feature.Agents {
	h.mu.Lock()
	defer h.mu.Unlock()
	return feature.Agents{Available: slices.Clone(h.agents.Available), Default: h.agents.Default}
}

// TaskOrigin is the origin of the task SetChat bound chat to ("" when none).
func (h *Host) TaskOrigin(chat string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if id, ok := h.chats[chat]; ok {
		return h.origins[id]
	}
	return ""
}

// SetChat binds a chat (a session id) to the task with this id, as the app's
// tracker does for a chat in the task's worktree.
func (h *Host) SetChat(chat, taskID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.chats[chat] = taskID
}

// Origin is the origin a task was created with.
func (h *Host) Origin(taskID string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.origins[taskID]
}

// SetAgents sets the available agents and the default one.
func (h *Host) SetAgents(def string, available ...string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.agents = feature.Agents{Available: available, Default: def}
}
