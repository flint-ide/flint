package workflows

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"flint/internal/mcptools"
	"flint/internal/textsafe"
)

// Bounds of Coordinate with Agent….
const (
	// MaxGoal bounds the goal the user states (characters).
	MaxGoal = 2000
	// maxPromptWorkflows bounds the workflows the prompt lists, and
	// maxPromptProjects the projects.
	maxPromptWorkflows = 30
	maxPromptProjects  = 10
	// maxDelegationsPerChat is what one chat may delegate with
	// delegate_project_item (the Projects feature's own limit, said here).
	maxDelegationsPerChat = 10
)

// CoordinatePrompt is the prompt of Coordinate with Agent… for a chat's
// draft.
type CoordinatePrompt struct {
	// Prompt is the text to put into the chat's draft (the user sends it;
	// nothing is sent for them).
	Prompt string `json:"prompt"`
	// ToolOn: agents can use the workflow tools now (Flint's MCP server is
	// on and Settings ▸ Agent Context ▸ Actions ▸ "Run and steer
	// workflows" is). When false the UI says what to switch on; the prompt
	// is still usable.
	ToolOn bool `json:"toolOn"`
	// DelegateOn: agents can use delegate_project_item now (the server is
	// on and Settings ▸ Agent Context ▸ Actions ▸ "Delegate project items"
	// is). It matters when the folder is in a Flint project.
	DelegateOn bool `json:"delegateOn"`
}

// CoordinatePrompt builds the prompt that asks an agent to coordinate
// workflow runs for goal in the workspace folder dir: read the project when
// there is one, list the workflows, start runs for the goal, wait for them,
// tell the user when a run waits for approval (and never try to approve),
// and stop when the user says so. When dir is in Flint projects
// (Host.FolderProjects), the agent hands their ready items out with
// delegate_project_item, one at a time, and starts plain runs only for
// work that is no item. The goal is one to
// MaxGoal characters without control characters other than tab and newline
// or invisible ones. dir's workspace folder must be trusted: agents get no
// workflow tools in Restricted Mode.
func (a *API) CoordinatePrompt(dir, goal string) (CoordinatePrompt, error) {
	root, dir, err := a.workspaceDir(dir)
	if err != nil {
		return CoordinatePrompt{}, err
	}
	goal = strings.TrimSpace(strings.ReplaceAll(goal, "\r\n", "\n"))
	switch {
	case goal == "":
		return CoordinatePrompt{}, errors.New("say what the goal is")
	case utf8.RuneCountInString(goal) > MaxGoal:
		return CoordinatePrompt{}, fmt.Errorf("the goal is longer than %d characters", MaxGoal)
	}
	if r, bad := textsafe.HiddenRune(goal); bad {
		return CoordinatePrompt{}, fmt.Errorf("the goal contains %s", textsafe.RuneKind(r))
	}
	if !a.host.Trusted(root) {
		return CoordinatePrompt{}, fmt.Errorf("%s is in Restricted Mode: trust it first, so agents can start workflows there", root)
	}
	l, err := a.List(root)
	if err != nil {
		return CoordinatePrompt{}, err
	}
	ctx, cancel := a.ctx(requestTimeout)
	projects := a.host.FolderProjects(ctx, root).Names
	cancel()
	mcp := a.host.Settings().MCPServer
	return CoordinatePrompt{
		Prompt:     coordinateText(dir, goal, l, projects),
		ToolOn:     mcp.Enabled && mcp.Actions.Workflows,
		DelegateOn: mcp.Enabled && mcp.Actions.Delegate,
	}, nil
}

// coordinateText is the prompt of Coordinate with Agent…; projects are the
// names of the projects dir is in.
func coordinateText(dir, goal string, l Listing, projects []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Coordinate Flint workflow runs in %s for this goal:\n\n%s\n\n", dir, goal)
	var names []string
	for _, w := range l.Workflows {
		if w.Problem == "" {
			names = append(names, w.Name)
		}
	}
	if len(names) > 0 {
		more := ""
		if len(names) > maxPromptWorkflows {
			names, more = names[:maxPromptWorkflows], ", …"
		}
		fmt.Fprintf(&b, "The workflows here: %s%s.\n\n", strings.Join(names, ", "), more)
	}
	if len(projects) > 0 {
		b.WriteString(projectSteps(projects))
		return b.String()
	}
	fmt.Fprintf(&b, "1. If this folder belongs to a Flint project, read it first with Flint's get_project tool: its items may already say what to do and which workflow to use.\n"+
		"2. List the workflows with %s and choose one for each piece of work (read their steps).\n"+
		"3. Start a run for the goal, or one per ready item, with %s (workflow, task: what the run should deliver). Flint asks me before each run starts. Start only what is needed; one chat may start at most %d runs.\n"+
		"4. Wait for each run with %s (no polling) and read it with %s.\n"+
		"5. When a run waits for my approval, or a step failed and waits, tell me which run and why. Never try to approve it or to continue anyway past a failed step: only I can, in Flint. "+
		"You may ask Flint to pause, resume, retry, send back (with a note) or stop a run with %s; I am asked before each.\n"+
		"6. Stop starting and steering runs as soon as I say so, and report what each run delivered.\n\n",
		mcptools.ToolWorkflowList, mcptools.ToolWorkflowStart, maxStartsPerChat, mcptools.ToolWorkflowWait, mcptools.ToolWorkflowRuns, mcptools.ToolWorkflowSteer)
	b.WriteString("If the workflow tools are missing, tell me to switch on \"Run and steer workflows\" in Flint's Settings ▸ Agent Context ▸ Actions.")
	return b.String()
}

// projectSteps are the steps of a coordinator whose folder is in Flint
// projects: their ready items go to their agents or workflows through
// delegate_project_item, one call at a time, each with the user's yes.
func projectSteps(projects []string) string {
	quoted := make([]string, 0, min(len(projects), maxPromptProjects))
	for _, p := range projects[:min(len(projects), maxPromptProjects)] {
		quoted = append(quoted, fmt.Sprintf("%q", oneLine(cut(p, 80))))
	}
	more := ""
	if len(projects) > maxPromptProjects {
		more = ", …"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "This folder is in the Flint project(s) %s%s, whose items may span several repositories. You coordinate them:\n\n", strings.Join(quoted, ", "), more)
	fmt.Fprintf(&b, "1. Read the project with Flint's get_project tool: its items, which are ready, their agents, repositories and workflows, and the state of their runs.\n"+
		"2. Hand each READY item that serves the goal to its agents or workflow with %s (project, item), one call at a time: Flint asks me before each, and makes its agent tasks in every repository it names, as my Delegate click does. "+
		"An item that waits for other items cannot be delegated: leave it until they are done, and never ask to start it when it is ready. One chat may delegate at most %d items.\n"+
		"3. Start a plain run with %s only for work that is not an item (read the workflows with %s first).\n"+
		"4. Wait for each run with %s (no polling) and read it with %s: you see the runs of the project's items in every repository.\n"+
		"5. When a run waits for my approval, or a step failed and waits, tell me plainly which item and run and why. Never try to approve it or to continue anyway past a failed step: only I can, in Flint. "+
		"You may ask Flint to pause, resume, retry, send back (with a note) or stop a run with %s; I am asked before each.\n"+
		"6. Stop delegating, starting and steering as soon as I say so, and report what each item and run delivered.\n\n",
		mcptools.ToolProjectDelegate, maxDelegationsPerChat, mcptools.ToolWorkflowStart, mcptools.ToolWorkflowList,
		mcptools.ToolWorkflowWait, mcptools.ToolWorkflowRuns, mcptools.ToolWorkflowSteer)
	fmt.Fprintf(&b, "If %s is missing, tell me to switch on \"Delegate project items\" in Flint's Settings ▸ Agent Context ▸ Actions; "+
		"if the workflow tools are missing, \"Run and steer workflows\".", mcptools.ToolProjectDelegate)
	return b.String()
}
