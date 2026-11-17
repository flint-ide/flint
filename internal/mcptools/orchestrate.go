package mcptools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"

	"flint/internal/mcpserver"
	"flint/internal/orchestrate"
)

// Orchestration tools: they let an agent chat start and direct other agents in
// the same project. They are off by default (settings "orchestration") and, once
// on, covered by a per-chat grant the user gives in one native dialog
// (mcpserver.Tool.Granted), not by a question per call. Every tool needs to
// know which chat is calling (mcpserver.CallerFrom) and that the chat holds the
// grant; what a tool may touch is decided by the Orchestrator, which owns the
// chats.
const (
	ToolListAgents = "list_agents"
	ToolStartAgent = "start_agent"
	ToolSendPrompt = "send_prompt"
	ToolWaitAgent  = "wait_for_agent"
	ToolReadAgent  = "read_agent_output"
	ToolStopAgent  = "stop_agent"
)

// OrchestrationNames lists the orchestration tools. Nothing else may be
// allowed on the strength of a grant (agentsession.ActionGate.Granted).
var OrchestrationNames = []string{ToolListAgents, ToolStartAgent, ToolSendPrompt, ToolWaitAgent, ToolReadAgent, ToolStopAgent}

// IsOrchestrationTool reports whether name is one of the orchestration tools.
func IsOrchestrationTool(name string) bool { return slices.Contains(OrchestrationNames, name) }

// Bounds of the orchestration tools' arguments.
const (
	maxTaskBytes   = 32 << 10 // a start_agent task or a send_prompt text
	maxIDLen       = 128
	maxAgentName   = 64
	maxBaseLen     = 200
	waitSlack      = 15 * time.Second // on top of a wait, for the server's own bookkeeping
	defaultWaitMin = time.Second
)

var (
	idPattern   = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
	namePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)
	// basePattern is a branch name a tool argument may carry: git checks it
	// again, this keeps option-like and odd strings out of its arguments.
	basePattern = regexp.MustCompile(`^[A-Za-z0-9_./][A-Za-z0-9_./-]{0,199}$`)
	// untilStates are the states wait_for_agent can wait for (agenttask's).
	untilStates = []string{"idle", "waiting", "finished", "failed"}
)

// AgentInfo describes one chat of the project for list_agents.
type AgentInfo struct {
	ID     string `json:"id"`
	Agent  string `json:"agent"`
	Role   string `json:"role,omitempty"`
	State  string `json:"state"`
	Parent string `json:"parent,omitempty"`
	Depth  int    `json:"depth"`
	Cwd    string `json:"cwd"`
	Kind   string `json:"kind"`
	// Self marks the calling chat.
	Self bool `json:"self,omitempty"`
}

// StartRequest is a checked start_agent call.
type StartRequest struct {
	Agent, Role, Task string
	Worktree          bool
	Base              string
}

// WaitResult is what wait_for_agent returns.
type WaitResult struct {
	State    string `json:"state"`
	Error    string `json:"error,omitempty"`
	TimedOut bool   `json:"timedOut"`
}

// OrchLimits are the settings the tools read at call time.
type OrchLimits struct {
	// MaxWait caps one wait_for_agent call.
	MaxWait time.Duration
	// OutputLines is the most lines read_agent_output returns.
	OutputLines int
}

// Orchestrator is what the orchestration tools act on (package main
// implements it; tests use a fake). caller is the id of the calling chat and
// root the project of its token. Every method decides, by itself, what that
// caller may touch: the tools only check the shape of the arguments.
type Orchestrator interface {
	// Granted reports whether the chat holds an active grant.
	Granted(caller string) bool
	// Limits returns the current settings.
	Limits() OrchLimits
	// List returns the chats of root.
	List(caller, root string) ([]AgentInfo, error)
	// Start opens a child chat and returns its id.
	Start(ctx context.Context, caller, root string, r StartRequest) (string, error)
	// Send gives a descendant a message.
	Send(caller, root, id, text string) error
	// Wait blocks until the chat is in one of the states or the timeout ends.
	Wait(ctx context.Context, caller, root, id string, until []string, timeout time.Duration) (WaitResult, error)
	// Output returns the last lines of a chat's output.
	Output(caller, root, id string, lines int) (string, error)
	// Stop ends a descendant (and what it started).
	Stop(caller, root, id string) error
}

// orchestrationTools returns the six tools; none while orchestration is off or
// nothing can carry them out.
func (s *Sources) orchestrationTools(cfg Config) []mcpserver.Tool {
	o := s.Orchestrator
	if o == nil || !cfg.Orchestration {
		return nil
	}
	maxWait := max(cfg.OrchMaxWait, defaultWaitMin)
	maxWait = min(maxWait, mcpserver.MaxCallTimeout-waitSlack)
	outLines := max(1, cfg.OrchOutputLines)
	granted := func(ctx context.Context) bool { return o.Granted(mcpserver.CallerFrom(ctx)) }
	// tool wraps a handler: a caller and a grant first, then the arguments.
	tool := func(name, title, desc, schema string, timeout time.Duration, h func(ctx context.Context, caller, root string, args json.RawMessage) (string, error)) mcpserver.Tool {
		return mcpserver.Tool{
			Name: name, Title: title, Description: desc, InputSchema: json.RawMessage(schema),
			Acts: true, Granted: granted, Timeout: timeout,
			Call: func(ctx context.Context, root string, args json.RawMessage) (string, error) {
				caller := mcpserver.CallerFrom(ctx)
				if caller == "" {
					return "", errors.New("Flint cannot tell which chat is calling, so it cannot direct agents for it")
				}
				if !o.Granted(caller) {
					return "", errors.New("this chat has no permission from the user to direct agents")
				}
				text, err := h(ctx, caller, root, args)
				return text, refusal(err)
			},
		}
	}
	return []mcpserver.Tool{
		tool(ToolListAgents, "List agents",
			"Lists the agent chats of this project that Flint knows: id, agent, role, state (idle, working, waiting for the user's approval, finished, failed), who started it (parent), depth and kind. "+
				"Kind \"chat\" is an agent chat Flint runs; kind \"terminal\" is an agent CLI running in a terminal tab, of which Flint only reports the state (the other agent tools refuse it). "+
				"Use the ids of chats with the other agent tools. Only available when the user allowed this chat to direct agents.",
			`{"type":"object","properties":{},"additionalProperties":false}`, 0,
			func(_ context.Context, caller, root string, args json.RawMessage) (string, error) {
				if err := decodeStrict(args, &struct{}{}); err != nil {
					return "", err
				}
				list, err := o.List(caller, root)
				if err != nil {
					return "", err
				}
				return jsonText(map[string]any{"agents": list})
			}),
		tool(ToolStartAgent, "Start an agent",
			"Starts another agent chat in this project as your child and sends it the task. Optionally name the agent (default: yours) and a role (a template from Flint's roles folders whose text is put before the task). "+
				"The child appears as a chat in Flint and what you send it is shown there. Returns its id; follow it with "+ToolWaitAgent+", "+ToolReadAgent+" and "+ToolSendPrompt+". "+
				"Flint also tells you when a child waits, fails or finishes. Limits on how many agents and how deep apply.",
			fmt.Sprintf(`{"type":"object","properties":{`+
				`"agent":{"type":"string","maxLength":%d,"description":"Which agent to start (claude, codex or a custom agent's id). Default: a role's agent, else yours."},`+
				`"role":{"type":"string","maxLength":%d,"description":"Name of a role template (letters, digits, - and _)."},`+
				`"task":{"type":"string","maxLength":%d,"description":"What the new agent should do; it knows nothing else about your work."},`+
				`"worktree":{"type":"boolean","description":"Run it in its own git branch and worktree (a task in Flint's Tasks panel), so its changes do not touch yours. Refused while the limit of tasks working at once is reached."},`+
				`"base":{"type":"string","maxLength":%d,"description":"Local branch the worktree starts from (default: the branch of this project's folder). Only with worktree."}`+
				`},"required":["task"],"additionalProperties":false}`, maxAgentName, maxAgentName, maxTaskBytes, maxBaseLen), 2*time.Minute,
			func(ctx context.Context, caller, root string, args json.RawMessage) (string, error) {
				r, err := startRequest(args)
				if err != nil {
					return "", err
				}
				id, err := o.Start(ctx, caller, root, r)
				if err != nil {
					return "", err
				}
				return jsonText(map[string]string{"id": id})
			}),
		tool(ToolSendPrompt, "Send a prompt to an agent",
			"Sends a message to one of the agents you started (or that they started). It appears in that chat as a message from you. It is refused, and nothing is sent, "+
				"while that agent waits for the user's approval, while the user has a draft open in its chat, or while it is working: wait for it ("+ToolWaitAgent+") or retry later.",
			fmt.Sprintf(`{"type":"object","properties":{`+
				`"id":{"type":"string","maxLength":%d,"description":"Id of the agent, from %s or %s."},`+
				`"text":{"type":"string","maxLength":%d,"description":"The message."}`+
				`},"required":["id","text"],"additionalProperties":false}`, maxIDLen, ToolListAgents, ToolStartAgent, maxTaskBytes), 0,
			func(_ context.Context, caller, root string, args json.RawMessage) (string, error) {
				var in struct{ ID, Text string }
				if err := decodeStrict(args, &in); err != nil {
					return "", err
				}
				if err := checkID(in.ID); err != nil {
					return "", err
				}
				if err := checkText("text", in.Text); err != nil {
					return "", err
				}
				if err := o.Send(caller, root, in.ID, in.Text); err != nil {
					return "", err
				}
				return "Sent. It appears in that agent's chat.", nil
			}),
		tool(ToolWaitAgent, "Wait for an agent",
			fmt.Sprintf("Waits until an agent is in one of the given states, without polling, then returns {state, error, timedOut}. States: idle (no turn running), waiting (it waits for the user's approval), finished, failed. "+
				"It returns at once when the agent already is in one, so a finished earlier turn counts: send a prompt first when you wait for its answer. It gives up after timeoutSeconds (at most %d) and then returns timedOut true with the current state.", int(maxWait/time.Second)),
			fmt.Sprintf(`{"type":"object","properties":{`+
				`"id":{"type":"string","maxLength":%d,"description":"Id of the agent."},`+
				`"until":{"type":"array","items":{"type":"string","enum":["idle","waiting","finished","failed"]},"minItems":1,"maxItems":4,"description":"States to wait for."},`+
				`"timeoutSeconds":{"type":"integer","minimum":1,"maximum":%d,"description":"Longest wait. Default: the maximum."}`+
				`},"required":["id","until"],"additionalProperties":false}`, maxIDLen, int(maxWait/time.Second)), maxWait+waitSlack,
			func(ctx context.Context, caller, root string, args json.RawMessage) (string, error) {
				var in struct {
					ID             string   `json:"id"`
					Until          []string `json:"until"`
					TimeoutSeconds *int     `json:"timeoutSeconds"`
				}
				if err := decodeStrict(args, &in); err != nil {
					return "", err
				}
				if err := checkID(in.ID); err != nil {
					return "", err
				}
				until, err := checkUntil(in.Until)
				if err != nil {
					return "", err
				}
				timeout := maxWait
				if in.TimeoutSeconds != nil {
					if *in.TimeoutSeconds < 1 {
						return "", errors.New("timeoutSeconds must be at least 1")
					}
					timeout = min(timeout, time.Duration(*in.TimeoutSeconds)*time.Second)
				}
				res, err := o.Wait(ctx, caller, root, in.ID, until, timeout)
				if err != nil {
					return "", err
				}
				return jsonText(res)
			}),
		tool(ToolReadAgent, "Read an agent's output",
			fmt.Sprintf("The last lines of what an agent of this project said and did (its text and the titles of its tool calls), redacted. At most %d lines.", outLines),
			fmt.Sprintf(`{"type":"object","properties":{`+
				`"id":{"type":"string","maxLength":%d,"description":"Id of the agent."},`+
				`"lines":{"type":"integer","minimum":1,"maximum":%d,"description":"Number of lines (default %d)."}`+
				`},"required":["id"],"additionalProperties":false}`, maxIDLen, outLines, outLines), 0,
			func(_ context.Context, caller, root string, args json.RawMessage) (string, error) {
				var in struct {
					ID    string `json:"id"`
					Lines int    `json:"lines"`
				}
				if err := decodeStrict(args, &in); err != nil {
					return "", err
				}
				if err := checkID(in.ID); err != nil {
					return "", err
				}
				if in.Lines < 0 {
					return "", errors.New("lines must be positive")
				}
				lines := outLines
				if in.Lines > 0 {
					lines = min(in.Lines, outLines)
				}
				text, err := o.Output(caller, root, in.ID, lines)
				if err != nil {
					return "", err
				}
				if text == "" {
					return "(no output yet)", nil
				}
				return text, nil
			}),
		tool(ToolStopAgent, "Stop an agent",
			"Stops one of the agents you started and everything it started. Its chat closes in Flint.",
			fmt.Sprintf(`{"type":"object","properties":{"id":{"type":"string","maxLength":%d,"description":"Id of the agent."}},"required":["id"],"additionalProperties":false}`, maxIDLen), 0,
			func(_ context.Context, caller, root string, args json.RawMessage) (string, error) {
				var in struct{ ID string }
				if err := decodeStrict(args, &in); err != nil {
					return "", err
				}
				if err := checkID(in.ID); err != nil {
					return "", err
				}
				if err := o.Stop(caller, root, in.ID); err != nil {
					return "", err
				}
				return "Stopped.", nil
			}),
	}
}

func startRequest(args json.RawMessage) (StartRequest, error) {
	var in struct {
		Agent    string `json:"agent"`
		Role     string `json:"role"`
		Task     string `json:"task"`
		Worktree bool   `json:"worktree"`
		Base     string `json:"base"`
	}
	if err := decodeStrict(args, &in); err != nil {
		return StartRequest{}, err
	}
	for what, v := range map[string]string{"agent": in.Agent, "role": in.Role} {
		if v != "" && !namePattern.MatchString(v) {
			return StartRequest{}, fmt.Errorf("%s must be 1–%d letters, digits, '.', '_' or '-'", what, maxAgentName)
		}
	}
	if err := checkText("task", in.Task); err != nil {
		return StartRequest{}, err
	}
	switch {
	case in.Base != "" && !in.Worktree:
		return StartRequest{}, errors.New("base is only used with worktree")
	case in.Base != "" && !basePattern.MatchString(in.Base):
		return StartRequest{}, fmt.Errorf("base must be a local branch name of 1–%d characters (letters, digits, '.', '_', '/' or '-', not starting with '-')", maxBaseLen)
	}
	return StartRequest{Agent: in.Agent, Role: in.Role, Task: in.Task, Worktree: in.Worktree, Base: in.Base}, nil
}

// checkID bounds an agent id from the arguments.
func checkID(id string) error {
	if !idPattern.MatchString(id) {
		return fmt.Errorf("id must be 1–%d letters, digits, '.', '_', ':' or '-' (as %s returns it)", maxIDLen, ToolListAgents)
	}
	return nil
}

// checkText bounds a message or a task: not blank, not huge, no control
// characters other than line breaks and tabs.
func checkText(what, text string) error {
	switch {
	case strings.TrimSpace(text) == "":
		return fmt.Errorf("%s is required", what)
	case len(text) > maxTaskBytes:
		return fmt.Errorf("%s is longer than %d KiB", what, maxTaskBytes>>10)
	case strings.ContainsFunc(text, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' }):
		return fmt.Errorf("%s contains a control character", what)
	}
	return nil
}

// checkUntil validates wait_for_agent's states and drops duplicates.
func checkUntil(states []string) ([]string, error) {
	if len(states) == 0 {
		return nil, fmt.Errorf("until is required: one or more of %s", strings.Join(untilStates, ", "))
	}
	var out []string
	for _, s := range states {
		if !slices.Contains(untilStates, s) {
			return nil, fmt.Errorf("until: %q is not a state (use %s)", s, strings.Join(untilStates, ", "))
		}
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out, nil
}

// refusal gives the typed refusals of the orchestration layer the plain words
// the design promises the calling agent, so it knows what to do next.
func refusal(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, orchestrate.ErrWaitingApproval):
		return errors.New("waiting for approval: that agent waits for the user to approve something; nothing was sent. Wait for it or ask the user")
	case errors.Is(err, orchestrate.ErrDraftOpen):
		return errors.New("draft open: the user is typing in that agent's chat; nothing was sent. Wait a moment and retry")
	case errors.Is(err, orchestrate.ErrBusy):
		return errors.New("busy: that agent is working on a turn; nothing was sent. Wait for it (wait_for_agent) and retry")
	}
	return err
}

func jsonText(v any) (string, error) {
	out, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		return "", fmt.Errorf("encode answer: %v", err)
	}
	return string(out), nil
}
