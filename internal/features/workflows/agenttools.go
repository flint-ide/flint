package workflows

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"flint/internal/mcpserver"
	"flint/internal/mcptools"
	"flint/internal/redact"
	"flint/internal/settings"
)

// The agent tools (docs/design/workflows.md, "The coordinator"): an agent
// lists the workflows of its folder, reads and waits for the runs of its
// folder, and, with the user's yes to each call, starts runs there and
// pauses, resumes, retries, sends back or stops them. It can never approve
// an approval step, check a failed step again nor continue past it:
// steer_workflow_run has no such action (agentsteer.go), and nothing in
// these files calls Run.Approve, Run.Rechecked or Run.Skip
// (TestNoToolReachesApproveOrSkip).
//
// Scope: a chat reaches the runs whose checkout (Run.Dir) is its folder (the
// workspace folder of its token) or inside it, or the repository its folder
// is in; and, for a chat in a workspace folder (not an agent task's
// worktree), the runs whose agent task is linked to an item of a project
// its folder is in, whose folder is trusted (Host.FolderProjects, answered
// by Projects): the coordinator of a project spanning several repositories
// watches and steers its items' runs wherever they are. Nothing else, as
// orchestration scopes chats. A chat that is a step of a run (isStep) gets
// none of them, so a run can neither steer itself nor start workflows from
// inside a step.

// Bounds of the agent tools.
const (
	// maxToolOutput bounds an answer (the MCP server cuts at 64 KB).
	maxToolOutput = 60 << 10
	// maxToolText bounds one free text of a run in an answer (bytes).
	maxToolText = 1000
	// toolLogLines is how many of a run's last log entries an answer shows.
	toolLogLines = 5
	// maxStartsPerChat bounds the runs one chat starts; maxStartCounters
	// the chats counted (then the one with the fewest starts below the
	// limit is forgotten: roomForCounter).
	maxStartsPerChat = 10
	maxStartCounters = 1000
	// maxSummaryText is how much of the task the confirmation shows.
	maxSummaryText = 600
	// waitSlack is on top of a wait, for the server's own bookkeeping, and
	// minWait the shortest cap of a wait.
	waitSlack = 15 * time.Second
	minWait   = time.Second
)

// errStepCaller refuses a chat that is a step of a workflow run.
var errStepCaller = errors.New("a workflow step cannot start or steer workflows")

// agentTools are the workflow tools, offered only while Flint's MCP server
// and Settings ▸ Agent Context ▸ Actions ▸ "Run and steer workflows"
// (mcpServer.actions.workflows) are on. Flint's server gives untrusted
// folders no tools at all, and every call checks the folder's trust again.
func (a *API) agentTools() []mcpserver.Tool {
	s := a.host.Settings()
	if !s.MCPServer.Enabled || !s.MCPServer.Actions.Workflows {
		return nil
	}
	return []mcpserver.Tool{a.listTool(), a.runsTool(), a.waitTool(toolMaxWait(s)), a.startTool(), a.steerTool()}
}

// toolMaxWait caps wait_for_workflow_run as wait_for_agent is capped: by
// orchestration.maxWaitSeconds and below the MCP server's cap on a call.
func toolMaxWait(s settings.Settings) time.Duration {
	w := time.Duration(s.Orchestration.MaxWaitSeconds) * time.Second
	return min(max(w, minWait), mcpserver.MaxCallTimeout-waitSlack)
}

// toolCaller is who calls a workflow tool: the chat (mcpserver.CallerFrom,
// from its token), its folder (the token's) and the repository that folder
// is in.
type toolCaller struct {
	chat, root, repo string
	// worktree: the folder is an agent task's worktree.
	worktree bool
	// tasks are the agent tasks of the items of the projects the folder
	// is in (none for a worktree): their runs are reached too.
	tasks []string
}

// counter is what the per-chat count of starts is kept by.
func (c toolCaller) counter() string {
	if c.chat != "" {
		return c.chat
	}
	return "folder:" + c.root
}

// caller checks who calls: a trusted folder, and a chat that is no step of
// a run.
func (a *API) caller(ctx context.Context, root string) (toolCaller, error) {
	if root == "" || !filepath.IsAbs(root) {
		return toolCaller{}, errors.New("Flint cannot tell which folder this chat works in")
	}
	if !a.host.Trusted(root) {
		return toolCaller{}, errors.New("this chat's folder is in Restricted Mode in Flint: agents get no workflow tools there")
	}
	c := toolCaller{chat: mcpserver.CallerFrom(ctx), root: filepath.Clean(root)}
	if repo, err := a.host.Repository(ctx, root); err == nil {
		c.repo, c.worktree = repo.Root, repo.TaskWorktree
	}
	if a.isStep(ctx, c) {
		return toolCaller{}, errStepCaller
	}
	if !c.worktree {
		// An agent that was handed work does not coordinate its project.
		c.tasks = a.host.FolderProjects(ctx, c.root).Tasks
	}
	return c, nil
}

// isStep reports whether the caller is a step of a run: its task's origin
// is a run's, it is the chat of a run's step, or it works in the worktree
// of a run's task (every chat a step's agent could open, or have opened for
// it, works there; a project item's task a run takes has the project's
// origin, so the worktree is what tells).
func (a *API) isStep(ctx context.Context, c toolCaller) bool {
	if c.chat != "" && strings.HasPrefix(a.host.TaskOrigin(c.chat), originPrefix) {
		return true
	}
	runs := a.engine.list()
	ids := make([]string, 0, len(runs))
	for _, r := range runs {
		if c.chat != "" && slices.ContainsFunc(r.Records, func(rec Record) bool { return rec.Chat == c.chat }) {
			return true
		}
		ids = append(ids, r.Task)
	}
	if len(ids) == 0 {
		return false
	}
	for _, t := range a.host.AgentTasks(ctx, ids) {
		if t.Worktree == "" {
			continue
		}
		if under(c.root, t.Worktree) || c.repo != "" && under(c.repo, t.Worktree) {
			return true
		}
	}
	return false
}

// reaches reports whether the caller may see and steer r: its checkout is
// the caller's folder or inside it, or the caller's repository; or its
// agent task is linked to an item of a project the caller's folder is in.
// (The run a caller is a step of is never reached: such a caller is
// refused every tool, isStep.)
func (c toolCaller) reaches(r Run) bool {
	if r.Task != "" && slices.Contains(c.tasks, r.Task) {
		return true
	}
	return r.Dir != "" && (under(r.Dir, c.root) || c.repo != "" && under(r.Dir, c.repo) && under(c.repo, r.Dir))
}

// under reports whether path is dir or inside it, as written or once their
// links are resolved.
func under(path, dir string) bool {
	if within(filepath.Clean(path), filepath.Clean(dir)) {
		return true
	}
	rp, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	rd, err := filepath.EvalSymlinks(dir)
	return err == nil && within(rp, rd)
}

// toolRun finds the run id among those the caller reaches.
func (a *API) toolRun(c toolCaller, id string) (Run, error) {
	notFound := fmt.Errorf("no workflow run %q in this folder or of its projects' items: %s lists the runs you can reach", cut(id, 40), mcptools.ToolWorkflowRuns)
	if !runID.MatchString(id) {
		return Run{}, notFound
	}
	r, ok := a.engine.get(id)
	if !ok || r.Task == "" || !c.reaches(r) {
		return Run{}, notFound
	}
	return r, nil
}

// decodeArgs decodes a tool's arguments strictly: unknown ones are refused.
func decodeArgs(raw json.RawMessage, v any) error {
	if len(bytes.TrimSpace(raw)) == 0 || string(raw) == "null" {
		raw = json.RawMessage("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("invalid arguments: %v", err)
	}
	if dec.More() {
		return errors.New("invalid arguments: more than one object")
	}
	return nil
}

// ---- list_workflows

func (a *API) listTool() mcpserver.Tool {
	return mcpserver.Tool{
		Name:  mcptools.ToolWorkflowList,
		Title: "List the workflows",
		Description: "Lists the Flint workflows this folder offers (built-in ones, the user's own, and the repository's .flint/workflows): name, description and steps with their kinds " +
			"(agent, script, review, approval: an approval step waits for the user). Use a name with " + mcptools.ToolWorkflowStart + ".",
		InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		Call: func(ctx context.Context, root string, args json.RawMessage) (string, error) {
			if err := decodeArgs(args, &struct{}{}); err != nil {
				return "", err
			}
			c, err := a.caller(ctx, root)
			if err != nil {
				return "", err
			}
			l, err := a.List(c.root)
			if err != nil {
				return "", err
			}
			return listingText(l), nil
		},
	}
}

// listingText is list_workflows' answer, bounded.
func listingText(l Listing) string {
	var b strings.Builder
	left := 0
	for _, w := range l.Workflows {
		if w.Problem != "" {
			left++
			continue
		}
		var s strings.Builder
		fmt.Fprintf(&s, "## %s (%s)\n", w.Name, w.Source)
		if d := strings.TrimSpace(w.Description); d != "" {
			s.WriteString(cut(redact.Secrets(d), maxToolText) + "\n")
		}
		s.WriteString("Steps: " + stepsLine(w.Steps) + "\n\n")
		if b.Len()+s.Len() > maxToolOutput-512 {
			left++
			continue
		}
		b.WriteString(s.String())
	}
	if b.Len() == 0 {
		b.WriteString("This folder offers no workflow that can run.\n")
	}
	if left > 0 {
		fmt.Fprintf(&b, "(%d workflow(s) not listed: they have a problem or did not fit.)\n", left)
	}
	if l.Restricted {
		b.WriteString("(The repository's own workflows are left out: its folder is in Restricted Mode.)\n")
	}
	return strings.TrimSpace(b.String())
}

// stepsLine is "1. Plan (agent: planner) · 2. Test (script: test) · 3. Approve (approval: the user approves)".
func stepsLine(steps []StepView) string {
	parts := make([]string, len(steps))
	for i, s := range steps {
		kind := s.Kind
		switch {
		case s.Kind == string(KindGate):
			kind = "approval: the user approves"
		case s.Kind == string(KindScript) && s.Run != "":
			kind += ": " + s.Run
		case s.Agent != "":
			kind += ": " + s.Agent
		}
		parts[i] = fmt.Sprintf("%d. %s (%s)", i+1, oneLine(s.Title), kind)
	}
	return strings.Join(parts, " · ")
}

// ---- get_workflow_runs

func (a *API) runsTool() mcpserver.Tool {
	return mcpserver.Tool{
		Name:  mcptools.ToolWorkflowRuns,
		Title: "Read the workflow runs",
		Description: "Returns the Flint workflow runs of this folder and of the items of the Flint projects it is in, wherever their repository is (newest first): id, workflow, title, state (running, waiting: an approval step waits for the user, failed, paused, done, stopped), " +
			"the current step, why it is paused by a limit, what you may ask for, and the last lines of its log. Pass run for one run. Read-only.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` +
			`"run":{"type":"string","maxLength":16,"description":"Only this run (its id)."}` +
			`},"additionalProperties":false}`),
		Call: func(ctx context.Context, root string, args json.RawMessage) (string, error) {
			var in struct {
				Run string `json:"run"`
			}
			if err := decodeArgs(args, &in); err != nil {
				return "", err
			}
			c, err := a.caller(ctx, root)
			if err != nil {
				return "", err
			}
			if in.Run != "" {
				if _, err := a.toolRun(c, in.Run); err != nil {
					return "", err
				}
			}
			return a.runsText(c, in.Run), nil
		},
	}
}

// runsText lists the runs c reaches (only run id when set), newest first,
// within maxToolOutput.
func (a *API) runsText(c toolCaller, id string) string {
	var b strings.Builder
	shown, left := 0, 0
	for _, v := range a.Runs() {
		r, ok := a.engine.get(v.ID)
		if !ok || !c.reaches(r) || id != "" && v.ID != id {
			continue
		}
		s := runText(v)
		if b.Len()+len(s) > maxToolOutput-512 {
			left++
			continue
		}
		b.WriteString(s)
		shown++
	}
	if shown == 0 && left == 0 {
		return "No workflow run in this folder or of its projects' items."
	}
	if left > 0 {
		fmt.Fprintf(&b, "(%d more run(s) not shown: the answer is limited to %d KB; ask for one run.)\n", left, maxToolOutput>>10)
	}
	return strings.TrimSpace(b.String())
}

// agentActions are the actions an agent may ask for (steer_workflow_run),
// by the run view's names.
var agentActions = map[string]string{
	ActionPause: steerPause, ActionResume: steerResume, ActionRetry: steerRetry,
	ActionSendBack: steerSendBack, ActionStop: steerStop,
}

// runText describes one run for an agent; every free text is redacted and
// bounded.
func runText(v RunView) string {
	var b strings.Builder
	text := func(s string) string { return cut(redact.Secrets(strings.TrimSpace(s)), maxToolText) }
	fmt.Fprintf(&b, "## Run %s: %s\n", v.ID, oneLine(text(v.Title)))
	fmt.Fprintf(&b, "workflow: %s; state: %s", v.Workflow, v.State)
	if v.Paused && v.State == StateRunning {
		b.WriteString(" (pauses before the next step)")
	}
	if v.Queued {
		b.WriteString(" (its task waits for a free slot)")
	}
	fmt.Fprintf(&b, "; folder: %s; agent task: %s", v.Dir, v.Task)
	if v.TaskState != "" {
		fmt.Fprintf(&b, " (%s)", v.TaskState)
	}
	b.WriteString("\n")
	if v.Current < len(v.Steps) {
		s := v.Steps[v.Current]
		fmt.Fprintf(&b, "current step: %d of %d, %q (%s, %s", v.Current+1, len(v.Steps), oneLine(s.Title), s.Kind, s.State)
		if s.Attempts > 0 {
			fmt.Fprintf(&b, ", attempt %d", s.Attempts)
		}
		b.WriteString(")\n")
	}
	switch v.State {
	case StateWaiting:
		b.WriteString("It waits at an approval step. Only the user can approve it, in Flint: tell them.\n")
	case StateFailed:
		b.WriteString("It failed and waits: the user may check it again or continue anyway; you may ask to retry, send back or stop.\n")
	}
	if v.Error != "" {
		b.WriteString("error: " + text(v.Error) + "\n")
	}
	if h := v.Hold; h != nil {
		fmt.Fprintf(&b, "paused by a limit (%s): %s", h.Kind, oneLine(text(h.Reason)))
		if h.Until > 0 {
			fmt.Fprintf(&b, " (resumes by itself at %s)", time.Unix(h.Until, 0).UTC().Format(time.RFC3339))
		} else {
			b.WriteString(" (waits for resume)")
		}
		b.WriteString("\n")
	}
	if v.Feedback != "" {
		b.WriteString("note for the current step: " + oneLine(text(v.Feedback)) + "\n")
	}
	var can []string
	for _, act := range v.Actions {
		if name, ok := agentActions[act]; ok {
			can = append(can, name)
		}
	}
	if len(can) > 0 {
		fmt.Fprintf(&b, "you may ask for (%s): %s\n", mcptools.ToolWorkflowSteer, strings.Join(can, ", "))
	}
	if log := v.Log[max(0, len(v.Log)-toolLogLines):]; len(log) > 0 {
		b.WriteString("log:\n")
		for _, l := range log {
			step := ""
			if l.Step != "" {
				step = " [" + l.Step + "]"
			}
			fmt.Fprintf(&b, "- %s%s %s\n", time.Unix(l.At, 0).UTC().Format(time.RFC3339), step, oneLine(text(l.Text)))
		}
	}
	b.WriteString("\n")
	return b.String()
}

// ---- wait_for_workflow_run

// settled: the run needs the user (an approval step, a failure), is paused (by the
// user, an agent or a limit) or is over.
func settled(r Run) bool { return r.NeedsUser() || r.State == StatePaused || r.Over() }

func (a *API) waitTool(maxWait time.Duration) mcpserver.Tool {
	secs := int(maxWait / time.Second)
	return mcpserver.Tool{
		Name:  mcptools.ToolWorkflowWait,
		Title: "Wait for a workflow run",
		Description: fmt.Sprintf("Waits, without polling, until a workflow run you can reach (this folder's, or one of its projects' items) needs the user (an approval step waits, or a step failed), is paused, or is over (done, stopped), "+
			"then returns the run as %s does, with timedOut. It returns at once when the run already is in such a state. It gives up after timeoutSeconds (at most %d). Read-only.", mcptools.ToolWorkflowRuns, secs),
		InputSchema: json.RawMessage(fmt.Sprintf(`{"type":"object","properties":{`+
			`"run":{"type":"string","maxLength":16,"description":"The run's id."},`+
			`"timeoutSeconds":{"type":"integer","minimum":1,"maximum":%d,"description":"Longest wait. Default: the maximum."}`+
			`},"required":["run"],"additionalProperties":false}`, secs)),
		Timeout: maxWait + waitSlack,
		Call: func(ctx context.Context, root string, args json.RawMessage) (string, error) {
			var in struct {
				Run            string `json:"run"`
				TimeoutSeconds *int   `json:"timeoutSeconds"`
			}
			if err := decodeArgs(args, &in); err != nil {
				return "", err
			}
			wait := maxWait
			if in.TimeoutSeconds != nil {
				if *in.TimeoutSeconds < 1 {
					return "", errors.New("timeoutSeconds must be at least 1")
				}
				wait = min(wait, time.Duration(*in.TimeoutSeconds)*time.Second)
			}
			c, err := a.caller(ctx, root)
			if err != nil {
				return "", err
			}
			if _, err := a.toolRun(c, in.Run); err != nil {
				return "", err
			}
			wctx, cancel := context.WithTimeout(ctx, wait)
			defer cancel()
			_, err = a.engine.waitRun(wctx, in.Run, settled)
			timedOut := false
			switch {
			case errors.Is(err, errNoRun):
				return "", errors.New("the run was removed")
			case err != nil && ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded):
				timedOut = true
			case err != nil:
				return "", err
			}
			return fmt.Sprintf("timedOut: %v\n\n%s", timedOut, a.runsText(c, in.Run)), nil
		},
	}
}

// cut keeps the first n bytes of s at most, on a character boundary.
func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s + "…"
}
