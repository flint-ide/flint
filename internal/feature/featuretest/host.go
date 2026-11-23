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
	// taskWorkflow plans the runs of requests that name a workflow
	// (SetTaskWorkflow); taskRun answers AgentTask.Run (SetTaskRuns);
	// tasksChanged counts TasksChanged calls.
	taskWorkflow func(context.Context, feature.NewAgentTask) (feature.PlannedWorkflow, error)
	taskRun      func(string) (feature.TaskRun, bool)
	tasksChanged int
	// workflowCheck answers CheckTaskWorkflow (SetTaskWorkflowCheck);
	// folderProjects answers FolderProjects (SetFolderProjects).
	workflowCheck  func(context.Context, feature.NewAgentTask) error
	folderProjects func(context.Context, string) feature.FolderProjects

	// The workflow steps part: the StartStepChat calls, the scripted chat
	// results and outputs, the tasks' latest chats, the stopped chats, and
	// the scripted and recorded RunConfiguration calls.
	stepChats  []StepChat
	stepErr    error
	results    map[string]feature.ChatResult
	finished   map[string]chan struct{} // closed by FinishChat
	outputs    map[string]string
	taskChats  map[string]string
	stopped    []string
	runResults map[string]runScript
	runs       []RunCall
	// launches answer RunLaunch, by checkout and name; sandbox answers
	// ScriptSandbox.
	launches map[string]feature.RunLaunch
	sandbox  feature.SandboxSupport
	// budget is BudgetPause's answer.
	budget error
}

// StepChat is one StartStepChat call and the chat it opened ("" when it
// failed).
type StepChat struct {
	Task, Agent, Prompt string
	Chat                string
	// Reviewer is the call's StepChatOptions.Reviewer.
	Reviewer bool
}

// RunCall is one RunConfiguration call; Sandbox is its options' (a copy).
type RunCall struct {
	Task, Name string
	Sandbox    *feature.Sandbox
}

type runScript struct {
	res feature.RunResult
	err error
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
		results: map[string]feature.ChatResult{}, finished: map[string]chan struct{}{},
		outputs: map[string]string{}, taskChats: map[string]string{}, runResults: map[string]runScript{},
		launches: map[string]feature.RunLaunch{}, sandbox: feature.SandboxSupport{Available: true, Mechanism: "fake"},
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

// TasksChanged counts the calls (TasksChangedCount).
func (h *Host) TasksChanged() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.tasksChanged++
}

// TasksChangedCount is how many times TasksChanged was called.
func (h *Host) TasksChangedCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.tasksChanged
}

// SetTaskWorkflow makes CreateAgentTasks plan a request that names a
// workflow with fn, as the app asks the Workflows feature
// (feature.Set.TaskWorkflow); nil: such a request fails with
// feature.ErrWorkflowsOff.
func (h *Host) SetTaskWorkflow(fn func(context.Context, feature.NewAgentTask) (feature.PlannedWorkflow, error)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.taskWorkflow = fn
}

// SetTaskRuns makes AgentTasks and CreateAgentTasks fill AgentTask.Run from
// fn, as the app asks the Workflows feature (feature.Set.TaskRun).
func (h *Host) SetTaskRuns(fn func(taskID string) (feature.TaskRun, bool)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.taskRun = fn
}

// SetTaskWorkflowCheck makes CheckTaskWorkflow answer with fn, as the app
// asks the Workflows feature (feature.Set.TaskWorkflowCheck). Without it,
// CheckTaskWorkflow plans the request with SetTaskWorkflow's function and
// drops the plan at once (feature.ErrWorkflowsOff without either).
func (h *Host) SetTaskWorkflowCheck(fn func(context.Context, feature.NewAgentTask) error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.workflowCheck = fn
}

// CheckTaskWorkflow answers as SetTaskWorkflowCheck says.
func (h *Host) CheckTaskWorkflow(ctx context.Context, req feature.NewAgentTask) error {
	h.mu.Lock()
	check, plan := h.workflowCheck, h.taskWorkflow
	h.mu.Unlock()
	switch {
	case check != nil:
		return check(ctx, req)
	case plan == nil:
		return feature.ErrWorkflowsOff
	}
	p, err := plan(ctx, req)
	if err == nil && p.Drop != nil {
		p.Drop()
	}
	return err
}

// SetFolderProjects makes FolderProjects answer with fn, as the app asks
// the Projects feature (feature.Set.FolderProjects); nil: no projects.
func (h *Host) SetFolderProjects(fn func(context.Context, string) feature.FolderProjects) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.folderProjects = fn
}

// FolderProjects answers as SetFolderProjects says (outside the lock: fn
// may call the host).
func (h *Host) FolderProjects(ctx context.Context, dir string) feature.FolderProjects {
	h.mu.Lock()
	fn := h.folderProjects
	h.mu.Unlock()
	if fn == nil {
		return feature.FolderProjects{}
	}
	return fn(ctx, dir)
}

// withRuns fills in the tasks' runs (outside the lock: fn may call the host).
func (h *Host) withRuns(tasks map[string]feature.AgentTask) {
	h.mu.Lock()
	fn := h.taskRun
	h.mu.Unlock()
	if fn == nil {
		return
	}
	for id, t := range tasks {
		if r, ok := fn(id); ok {
			t.Run = r
			tasks[id] = t
		}
	}
}

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
// Dir must be inside a root, as the app requires. As the app does, a request
// that names a workflow is planned first (SetTaskWorkflow; every request
// before any task is made), takes the planned agent, and its run is bound
// once every task exists or dropped when the call fails.
func (h *Host) CreateAgentTasks(ctx context.Context, reqs []feature.NewAgentTask) ([]feature.AgentTask, error) {
	h.mu.Lock()
	h.requested = append(h.requested, slices.Clone(reqs))
	plan := h.taskWorkflow
	h.mu.Unlock()
	reqs = slices.Clone(reqs)
	planned := make([]feature.PlannedWorkflow, len(reqs))
	drop := func() {
		for _, p := range planned {
			if p.Drop != nil {
				p.Drop()
			}
		}
	}
	for i, r := range reqs {
		if r.Workflow == "" {
			continue
		}
		if plan == nil {
			drop()
			return nil, feature.ErrWorkflowsOff
		}
		p, err := plan(ctx, r)
		if err != nil {
			drop()
			return nil, err
		}
		planned[i], reqs[i].Agent = p, p.Agent
	}
	out, err := h.createTasks(ctx, reqs)
	if err != nil {
		drop()
		return nil, err
	}
	for i, p := range planned {
		if p.Bind == nil {
			continue
		}
		if err := p.Bind(out[i].ID); err != nil {
			h.mu.Lock()
			for _, t := range out {
				delete(h.tasks, t.ID)
			}
			h.mu.Unlock()
			drop()
			return nil, err
		}
	}
	byID := map[string]feature.AgentTask{}
	for _, t := range out {
		byID[t.ID] = t
	}
	h.withRuns(byID)
	for i, t := range out {
		out[i] = byID[t.ID]
	}
	return out, nil
}

func (h *Host) createTasks(ctx context.Context, reqs []feature.NewAgentTask) ([]feature.AgentTask, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
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
	if slices.ContainsFunc(reqs, func(r feature.NewAgentTask) bool { return r.Workflow != "" }) {
		return nil, feature.ErrJointWorkflow
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
	out := map[string]feature.AgentTask{}
	for _, id := range ids {
		if t, ok := h.tasks[id]; ok {
			out[id] = t
		}
	}
	h.mu.Unlock()
	h.withRuns(out)
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

// StartStepChat records the call and opens "chat-1", "chat-2"… bound to the
// task (TaskChat then returns it); with SetStepChatError it opens none. The
// task must exist (SetAgentTask or CreateAgentTasks), as the app requires.
func (h *Host) StartStepChat(ctx context.Context, taskID, agent, prompt string, opts feature.StepChatOptions) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	call := StepChat{Task: taskID, Agent: agent, Prompt: prompt, Reviewer: opts.Reviewer}
	err := h.stepErr
	switch {
	case err != nil:
	case ctx.Err() != nil:
		err = ctx.Err()
	case strings.TrimSpace(prompt) == "":
		err = errors.New("a step's chat needs a first message")
	default:
		if _, ok := h.tasks[taskID]; !ok {
			err = fmt.Errorf("no agent task %s (it was removed)", taskID)
		}
	}
	if err != nil {
		h.stepChats = append(h.stepChats, call)
		return "", err
	}
	call.Chat = fmt.Sprintf("chat-%d", len(h.stepChats)+1)
	h.stepChats = append(h.stepChats, call)
	h.chats[call.Chat] = taskID
	h.taskChats[taskID] = call.Chat
	h.finished[call.Chat] = make(chan struct{})
	return call.Chat, nil
}

// SetStepChatError makes StartStepChat fail with err (nil: succeed).
func (h *Host) SetStepChatError(err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.stepErr = err
}

// StepChats returns the StartStepChat calls, in order.
func (h *Host) StepChats() []StepChat {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.stepChats)
}

// TaskChat is the chat SetTaskChat or the last StartStepChat bound to the
// task, "" when none.
func (h *Host) TaskChat(taskID string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.taskChats[taskID]
}

// SetTaskChat makes chat the task's latest chat (as the task's own first
// chat would be) and lets WaitChat wait for it.
func (h *Host) SetTaskChat(taskID, chat string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.taskChats[taskID] = chat
	h.chats[chat] = taskID
	if h.finished[chat] == nil {
		h.finished[chat] = make(chan struct{})
	}
}

// WaitChat blocks until FinishChat ends the chat's turn or ctx ends; a chat
// that StartStepChat or SetTaskChat did not open, or that StopChat
// stopped, is "closed".
func (h *Host) WaitChat(ctx context.Context, chat string) (feature.ChatResult, error) {
	h.mu.Lock()
	done := h.finished[chat]
	h.mu.Unlock()
	if done == nil {
		return feature.ChatResult{}, fmt.Errorf("the chat %s was closed", chat)
	}
	select {
	case <-done:
	case <-ctx.Done():
		return feature.ChatResult{}, ctx.Err()
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	res, ok := h.results[chat]
	if !ok {
		return feature.ChatResult{}, fmt.Errorf("the chat %s was closed", chat)
	}
	return res, nil
}

// FinishChat ends the chat's turn with res: its WaitChat calls, those
// waiting and later ones, return res. A chat finishes once; it may be
// unknown yet (it is then made known).
func (h *Host) FinishChat(chat string, res feature.ChatResult) {
	h.mu.Lock()
	defer h.mu.Unlock()
	done := h.finished[chat]
	if done == nil {
		done = make(chan struct{})
		h.finished[chat] = done
	}
	select {
	case <-done:
		return
	default:
	}
	h.results[chat] = res
	close(done)
}

// ChatOutput is what SetChatOutput set, its last lines (1 to 200).
func (h *Host) ChatOutput(chat string, lines int) string {
	h.mu.Lock()
	out := h.outputs[chat]
	h.mu.Unlock()
	all := strings.Split(out, "\n")
	n := min(max(lines, 1), 200)
	if len(all) > n {
		all = all[len(all)-n:]
	}
	return strings.Join(all, "\n")
}

// SetChatOutput sets what ChatOutput returns for the chat.
func (h *Host) SetChatOutput(chat, output string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.outputs[chat] = output
}

// StopChat records the chat and ends its waits with "closed" (an unknown
// or empty id is a no-op, as in the app).
func (h *Host) StopChat(chat string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	done := h.finished[chat]
	if chat == "" || done == nil {
		return nil
	}
	h.stopped = append(h.stopped, chat)
	select {
	case <-done:
	default:
		close(done) // no result: WaitChat says the chat was closed
	}
	return nil
}

// Stopped returns the chats StopChat stopped, in order.
func (h *Host) Stopped() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.stopped)
}

// RunConfiguration records the call and returns what SetRunResult scripted
// for name; an unscripted name fails as the app's lookup does. The task
// must exist. ctx's end is returned as its error.
func (h *Host) RunConfiguration(ctx context.Context, taskID, name string, opts feature.RunOptions) (feature.RunResult, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	call := RunCall{Task: taskID, Name: name}
	if opts.Sandbox != nil {
		sb := *opts.Sandbox
		sb.Writable = slices.Clone(sb.Writable)
		call.Sandbox = &sb
	}
	h.runs = append(h.runs, call)
	if err := ctx.Err(); err != nil {
		return feature.RunResult{}, err
	}
	if _, ok := h.tasks[taskID]; !ok {
		return feature.RunResult{}, fmt.Errorf("no agent task %s (it was removed)", taskID)
	}
	sc, ok := h.runResults[name]
	if !ok {
		return feature.RunResult{}, fmt.Errorf("no run configuration named %q: add it to .flint/run.json", name)
	}
	return sc.res, sc.err
}

// BudgetPause returns what SetBudgetPause set (nil by default).
func (h *Host) BudgetPause() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.budget
}

// SetBudgetPause makes BudgetPause return err.
func (h *Host) SetBudgetPause(err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.budget = err
}

// SetRunResult makes RunConfiguration of name return res and err.
func (h *Host) SetRunResult(name string, res feature.RunResult, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.runResults[name] = runScript{res, err}
}

// Runs returns the RunConfiguration calls, in order.
func (h *Host) Runs() []RunCall {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.runs)
}

// RunLaunch returns what SetRunLaunch set for the checkout and name; an
// unset one fails as the app's lookup does.
func (h *Host) RunLaunch(checkout, name string) (feature.RunLaunch, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	l, ok := h.launches[checkout+"\x00"+name]
	if !ok {
		return feature.RunLaunch{}, fmt.Errorf("no run configuration named %q in %s: add it to .flint/run.json", name, checkout)
	}
	l.Commands = slices.Clone(l.Commands)
	l.Env = slices.Clone(l.Env)
	return l, nil
}

// SetRunLaunch makes RunLaunch of checkout and name return l.
func (h *Host) SetRunLaunch(checkout, name string, l feature.RunLaunch) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.launches[checkout+"\x00"+name] = l
}

// ScriptSandbox returns what SetScriptSandbox set (available by default).
func (h *Host) ScriptSandbox() feature.SandboxSupport {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.sandbox
}

// SetScriptSandbox makes ScriptSandbox return s.
func (h *Host) SetScriptSandbox(s feature.SandboxSupport) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sandbox = s
}
