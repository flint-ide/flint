package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	as "flint/internal/agentsession"
	"flint/internal/agenttask"
	"flint/internal/extensions/sandbox"
	"flint/internal/feature"
	"flint/internal/mcptools"
	"flint/internal/orchestrate"
	"flint/internal/redact"
	"flint/internal/runconfig"
	"flint/internal/runner"
)

// The workflow steps part of the feature Host (the Workflows feature takes
// the steps of a run in one agent task's worktree through it). Nothing here
// is new machinery: a step's chat is opened by openChat and prompted by
// promptChat, waited for in the orchestration registry, and a script step is
// a run configuration started by startConfig in a run tab. Every argument
// comes from the feature's Go code, never from the UI.

const (
	// maxStepPrompt bounds a step's first message.
	maxStepPrompt = 64 << 10
	// maxStepOutputLines bounds ChatOutput.
	maxStepOutputLines = 200
	// stepRunLines and maxStepRunOutput bound the output RunConfiguration
	// returns: its last lines, then its last bytes.
	stepRunLines     = 60
	maxStepRunOutput = 8 << 10
	// maxStepConfigName bounds a run configuration's name in a request.
	maxStepConfigName = 256
	// stepRunPrefix starts the id of a run a workflow started.
	stepRunPrefix = "workflow-"
)

// stepOrigin labels a step's first message: Flint sent it for a workflow.
var stepOrigin = as.Origin{Agent: "Flint", Role: "workflow"}

// StartStepChat opens the step's chat in the task's worktree (the lead's for
// a joint group, as startTaskChat does) and sends prompt as its first
// message. When openChat returns, promptChat has already marked the turn
// started in the registry (orch.Prompted, before agents.Prompt), so a
// WaitChat right after this never sees the idle state of before the prompt.
// A reviewer's chat (opts.Reviewer) is marked on its context
// (as.WithReviewer): its driver loads none of the worktree's project
// settings or instruction files where the engine can leave them out, and the
// approval policy answers none of its requests.
func (h featureHost) StartStepChat(ctx context.Context, taskID, agent, prompt string, opts feature.StepChatOptions) (string, error) {
	a := h.a
	if err := checkStepPrompt(prompt); err != nil {
		return "", err
	}
	task, err := a.stepTask(taskID)
	if err != nil {
		return "", err
	}
	if task, err = a.chatTask(task); err != nil {
		return "", err
	}
	if err := a.checkStepFolder("the worktree", task.Worktree); err != nil {
		return "", err
	}
	sctx, cancel := context.WithTimeout(ctx, agentStartTimeout)
	defer cancel()
	if opts.Reviewer {
		sctx = as.WithReviewer(sctx)
	}
	origin := stepOrigin
	info, err := a.openChat(sctx, ChatRequest{
		Agent: cmp.Or(agent, task.Agent), Cwd: task.Worktree, Prompt: prompt, TaskID: task.ID, Origin: &origin,
	})
	var down *agentStartError
	switch {
	case errors.As(err, &down):
		return "", fmt.Errorf("start %s for task %s: %w", agentLabel(cmp.Or(agent, task.Agent)), task.ID, err)
	case err != nil:
		return info.ID, err // "" unless the chat opened and only its prompt failed
	}
	return info.ID, nil
}

func checkStepPrompt(prompt string) error {
	switch {
	case strings.TrimSpace(prompt) == "":
		return errors.New("a step's chat needs a first message")
	case len(prompt) > maxStepPrompt:
		return fmt.Errorf("a step's first message is %d bytes, more than %d", len(prompt), maxStepPrompt)
	}
	return nil
}

// stepTask is the stored agent task with this id.
func (a *App) stepTask(taskID string) (agenttask.Task, error) {
	if !agenttask.ValidID(taskID) {
		return agenttask.Task{}, fmt.Errorf("invalid agent task id %q", shortText(taskID, 40))
	}
	_, task, ok := a.taskStore().Find(taskID)
	if !ok {
		return agenttask.Task{}, fmt.Errorf("no agent task %s (it was removed)", taskID)
	}
	return task, nil
}

// chatTask is the task whose worktree a chat of task runs in: the task, or
// the lead of its joint group.
func (a *App) chatTask(task agenttask.Task) (agenttask.Task, error) {
	if task.Joint == "" {
		return task, nil
	}
	members := a.taskStore().Joint(task.Joint)
	if len(members) == 0 {
		return agenttask.Task{}, fmt.Errorf("the joint group of agent task %s is gone", task.ID)
	}
	return members[0], nil
}

// checkStepFolder refuses a folder that is gone or in Restricted Mode: a step
// never starts project code (an agent, a script) in an untrusted folder.
func (a *App) checkStepFolder(what, dir string) error {
	if dir == "" || !isDir(dir) {
		return fmt.Errorf("%s %s is gone", what, dir)
	}
	if !a.isTrusted(dir) {
		return fmt.Errorf("%s %s is in Restricted Mode: trust the folder first", what, dir)
	}
	return nil
}

// PromptStepChat sends text as Flint's next message in a step's chat
// (promptChat, as StartStepChat's first). A busy chat (its agent started a
// turn of its own) gets nothing: the step waits for that turn instead.
func (h featureHost) PromptStepChat(ctx context.Context, chat, text string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := checkStepPrompt(text); err != nil {
		return err
	}
	if _, ok := h.a.orch.Get(chat); !ok {
		return fmt.Errorf("the chat %s was closed", shortText(chat, 80))
	}
	origin := stepOrigin
	if err := h.a.promptChat(chat, text, nil, false, &origin); err != nil && !errors.Is(err, as.ErrBusy) {
		return fmt.Errorf("ask chat %s again: %w", shortText(chat, 80), err)
	}
	return nil
}

// WaitChat waits in the registry until the chat's turn is over
// (orchestrate.TurnOver): not merely until its state is failed, which a
// non-fatal error in the middle of a turn makes it while the turn goes on.
func (h featureHost) WaitChat(ctx context.Context, chat string) (feature.ChatResult, error) {
	live, err := h.a.orch.WaitFor(ctx, chat, orchestrate.TurnOver)
	switch {
	case errors.Is(err, orchestrate.ErrUnknown), errors.Is(err, orchestrate.ErrRemoved):
		return feature.ChatResult{}, fmt.Errorf("the chat %s was closed", shortText(chat, 80))
	case err != nil:
		return feature.ChatResult{}, fmt.Errorf("wait for chat %s: %w", shortText(chat, 80), err)
	}
	return feature.ChatResult{State: live.State, Error: live.Error}, nil
}

// ChatOutput is the registry's output of the chat, which Registry.Output
// already redacts.
func (h featureHost) ChatOutput(chat string, lines int) string {
	return h.a.orch.Output(chat, min(max(lines, 1), maxStepOutputLines))
}

// StopChat ends the chat and the agents it started (stopChatTree). An id
// the registry does not know, with no children either, is a no-op.
func (h featureHost) StopChat(chat string) error {
	if chat == "" {
		return nil
	}
	if _, ok := h.a.orch.Get(chat); !ok && len(h.a.orch.Children(chat)) == 0 {
		return nil
	}
	return h.a.stopChatTree(chat)
}

// RunConfiguration takes the configuration from the task's checkout and runs
// it in the task's worktree: an agent that edits .flint/run.json in its
// worktree cannot change the command a later step runs. With opts.Sandbox
// every process of it runs in the sandbox (features_steps_sandbox.go); a
// sandbox that cannot be set up fails the step before anything starts.
func (h featureHost) RunConfiguration(ctx context.Context, taskID, name string, opts feature.RunOptions) (feature.RunResult, error) {
	a := h.a
	if err := ctx.Err(); err != nil {
		return feature.RunResult{}, err
	}
	task, err := a.stepTask(taskID)
	if err != nil {
		return feature.RunResult{}, err
	}
	if err := a.checkStepFolder("the checkout", task.Checkout); err != nil {
		return feature.RunResult{}, err
	}
	if err := a.checkStepFolder("the worktree", task.Worktree); err != nil {
		return feature.RunResult{}, err
	}
	cfg, err := checkoutConfig(task.Checkout, name)
	if err != nil {
		return feature.RunResult{}, err
	}
	var confine func(runconfig.Launch) (runconfig.Launch, string, error)
	if opts.Sandbox != nil {
		sb, err := a.stepSandbox(task.Worktree, *opts.Sandbox)
		if err != nil {
			return feature.RunResult{}, err
		}
		confine = sb.confine
	}
	// What later steps run or read in the worktree (agent settings and
	// hooks, instruction files, the .git file): the macOS sandbox denies
	// writing it, and on every system, sandboxed or not, a change fails
	// the step (sandbox.WorktreeProtected).
	before, err := sandbox.TakeFingerprint(task.Worktree)
	if err != nil {
		return feature.RunResult{}, fmt.Errorf("Flint cannot check what the worktree holds for later steps, so the script did not run: %w", err)
	}
	id := stepRunPrefix + randomID()
	res, err := a.runStepConfig(ctx, id, task.Worktree, cfg, confine)
	changed := sandbox.ChangedSince(task.Worktree, before)
	if err != nil {
		return feature.RunResult{ID: id, Changed: changed}, err
	}
	return feature.RunResult{
		ID: id, Code: res.Code, Stopped: res.Stopped, Error: res.Error,
		Output: a.stepRunOutput(id, task.Worktree), Changed: changed,
	}, nil
}

// RunLaunch resolves the configuration called name of checkout as
// RunConfiguration would (read from the checkout, never a worktree), with
// RunConfiguration's checks of the checkout, and runs nothing: what a
// workflow's script step will run, for the user to see and for a schedule's
// fingerprint.
func (h featureHost) RunLaunch(checkout, name string) (feature.RunLaunch, error) {
	if !filepath.IsAbs(checkout) || filepath.Clean(checkout) != checkout {
		return feature.RunLaunch{}, fmt.Errorf("invalid checkout %q", shortText(checkout, 80))
	}
	if err := h.a.checkStepFolder("the checkout", checkout); err != nil {
		return feature.RunLaunch{}, err
	}
	cfg, err := checkoutConfig(checkout, name)
	if err != nil {
		return feature.RunLaunch{}, err
	}
	l, err := runconfig.Resolve(checkout, cfg)
	if err != nil {
		return feature.RunLaunch{}, fmt.Errorf("run configuration %q: %w", name, err)
	}
	return runLaunch(checkout, cfg.Name, l), nil
}

// runLaunch is l, resolved in checkout, as a feature sees it.
func runLaunch(checkout, name string, l runconfig.Launch) feature.RunLaunch {
	out := feature.RunLaunch{Name: name, Env: append([]string{}, l.Env...), Cwd: "."}
	if rel, err := filepath.Rel(checkout, l.Cwd); err == nil {
		out.Cwd = rel
	}
	for _, c := range l.Steps {
		out.Commands = append(out.Commands, feature.RunCommand{Line: c.Label, Program: c.Program, Args: append([]string{}, c.Args...)})
	}
	return out
}

// ScriptSandbox is what this system can enforce for a script step.
func (h featureHost) ScriptSandbox() feature.SandboxSupport {
	s := probeSandbox()
	return feature.SandboxSupport{Available: s.Available, Mechanism: s.Mechanism, Reason: s.Reason}
}

// checkoutConfig finds the configuration called name in checkout: the saved
// ones (.flint/run.json) first, then the detected ones.
func checkoutConfig(checkout, name string) (runconfig.Config, error) {
	if name == "" || len(name) > maxStepConfigName {
		return runconfig.Config{}, fmt.Errorf("invalid run configuration name %q", shortText(name, 80))
	}
	saved, loadErr := runconfig.Load(checkout)
	for _, c := range saved {
		if c.Name == name {
			return c, nil
		}
	}
	for _, c := range runconfig.Detect(checkout) {
		if c.Name == name {
			return c, nil
		}
	}
	err := fmt.Errorf("no run configuration named %q in %s: add it to %s", name, checkout, runconfig.Path(checkout))
	if loadErr != nil {
		err = fmt.Errorf("%w (%s cannot be used: %v)", err, runconfig.FileName, loadErr)
	}
	return runconfig.Config{}, err
}

// runStepConfig starts cfg under id with root as its project folder, in a
// run tab the UI adopts (as mcpActor.StartConfig does), and waits for its
// end without polling. When ctx ends first the run is stopped. confine, when
// set, rewrites the launch (startConfigWith).
func (a *App) runStepConfig(ctx context.Context, id, root string, cfg runconfig.Config, confine func(runconfig.Launch) (runconfig.Launch, string, error)) (runner.Result, error) {
	ended := a.runEvents.await(id)
	a.emit(eventRunAdopt, RunAdopt{ID: id, Kind: "config", Title: cfg.Name, Root: root, Config: &cfg})
	if err := a.startConfigWith(id, root, cfg, mcptools.RunCols, mcptools.RunRows, confine); err != nil {
		a.runEvents.failed(id, err)
		return runner.Result{}, fmt.Errorf("run %q: %w", cfg.Name, err)
	}
	select {
	case res := <-ended:
		return res, nil
	case <-ctx.Done():
		a.runs.Stop(id) // returns once it ended; Exited then fills ended, which nobody reads
		return runner.Result{}, ctx.Err()
	}
}

// stepRunOutput is the end of what the run printed in root, redacted and
// at most maxStepRunOutput bytes, keeping the end.
func (a *App) stepRunOutput(id, root string) string {
	tail, _ := a.mcpSources.Terminals.Tail(id, root, stepRunLines)
	return keepEnd(redact.Secrets(tail), maxStepRunOutput)
}

// keepEnd is the last n bytes of s at most, cut at a rune boundary.
func keepEnd(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[len(s)-n:]
	for len(s) > 0 && !utf8.RuneStart(s[0]) {
		s = s[1:]
	}
	return s
}

// shortText bounds a value echoed in an error.
func shortText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "") + "…"
}

// BudgetPause is budgetPause for no task: today's budget, which every new
// turn is checked against.
func (h featureHost) BudgetPause() error { return h.a.budgetPause("") }
