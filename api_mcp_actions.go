package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	as "flint/internal/agentsession"
	"flint/internal/httpfile"
	"flint/internal/mcpserver"
	"flint/internal/mcptools"
	"flint/internal/redact"
	"flint/internal/runconfig"
	"flint/internal/runner"
	"flint/internal/testrun"
)

// The tools of Flint's MCP server that act for an agent (run_tests,
// run_configuration, send_http_request, and run_sql_query of the Database
// feature) do what the user does in the UI, through the same code, so the
// result shows in Flint's own tabs as well as in the tool's answer.
//
// No such call runs without the user's yes to that one call. Flint's server
// asks before it runs anything (mcpserver.Options.Approve → approveAction):
//
//   - When the agent's own permission request for the call reached the chat
//     and the user allowed it there (Claude Code: every tool call of a chat
//     comes through --permission-prompt-tool), that answer is kept as a
//     one-time grant for exactly this tool and these arguments, and the call
//     goes through without a second question.
//   - Otherwise (an agent that doesn't ask for MCP tools, a rule in the
//     agent's own settings that allows them, "Do everything", Codex, custom
//     agents, a program the agent started with the chat's token) Flint asks in
//     a native dialog that the web view can't answer.

// Events telling the frontend about what an agent started.
const (
	// eventRunAdopt: a run Flint started by itself; the frontend shows it in
	// a run tab like one it started (store/run.ts adoptRun).
	eventRunAdopt = "run:adopt"
	// eventHTTPAgent: a response an agent's request got; the file's response
	// tab shows it (store/httpClient.ts).
	eventHTTPAgent = "http:agent-response"
)

// RunAdopt describes a run started for an agent, sent before its first event.
type RunAdopt struct {
	ID string `json:"id"`
	// Kind is "tests" or "config".
	Kind  string `json:"kind"`
	Title string `json:"title"`
	// Root is the project folder the run belongs to.
	Root    string            `json:"root"`
	Request *testrun.Request  `json:"request,omitempty"`
	Config  *runconfig.Config `json:"config,omitempty"`
}

// HTTPAgentResponse names a kept response of a .http file.
type HTTPAgentResponse struct {
	File string `json:"file"`
	ID   string `json:"id"`
}

const (
	// grantTTL is how long a yes given in the chat waits for the agent's call.
	grantTTL  = 2 * time.Minute
	maxGrants = 64
	// maxDialogArgs bounds the arguments shown in the confirmation dialog.
	maxDialogArgs = 600
)

// actionGrants are the calls of acting tools the user allowed in a chat and
// the agent hasn't made yet: each is good for one call with the same project,
// tool and arguments.
type actionGrants struct {
	mu    sync.Mutex
	until map[[sha256.Size]byte]time.Time
	now   func() time.Time // tests; nil is time.Now
}

func (g *actionGrants) clock() time.Time {
	if g.now != nil {
		return g.now()
	}
	return time.Now()
}

func grantKey(root, tool string, args json.RawMessage) [sha256.Size]byte {
	return sha256.Sum256([]byte(filepath.Clean(root) + "\x00" + tool + "\x00" + canonicalArgs(args)))
}

// canonicalArgs renders a call's arguments so that the same values compare
// equal however they were written (key order, spacing).
func canonicalArgs(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return "{}"
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber() // numbers as written: no rounding makes two calls equal
	var v any
	if err := dec.Decode(&v); err != nil || dec.More() {
		return "invalid:" + string(raw)
	}
	out, err := json.Marshal(v) // maps are written with sorted keys
	if err != nil {
		return "invalid:" + string(raw)
	}
	return string(out)
}

func (g *actionGrants) add(root, tool string, args json.RawMessage) {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.clock()
	for k, t := range g.until {
		if !now.Before(t) {
			delete(g.until, k)
		}
	}
	if len(g.until) >= maxGrants {
		return // the dialog asks instead
	}
	if g.until == nil {
		g.until = map[[sha256.Size]byte]time.Time{}
	}
	g.until[grantKey(root, tool, args)] = now.Add(grantTTL)
}

// take uses up the grant for this call, if there is one.
func (g *actionGrants) take(root, tool string, args json.RawMessage) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	key := grantKey(root, tool, args)
	t, ok := g.until[key]
	delete(g.until, key)
	return ok && g.clock().Before(t)
}

// actionGate is the agent manager's side of the hand-over: which tools of
// Flint's server act, and what the user allowed in a chat.
func (a *App) actionGate() as.ActionGate {
	return as.ActionGate{
		Acts: func(tool string) bool { return a.mcp != nil && a.mcp.Acts(tool) },
		Approved: func(cwd, tool string, args json.RawMessage) {
			if filepath.IsAbs(cwd) {
				a.actionGrants.add(cwd, tool, args)
			}
		},
	}
}

// approveAction is the MCP server's question before an acting tool runs.
func (a *App) approveAction(ctx context.Context, call mcpserver.Approval) error {
	if a.actionGrants.take(call.Root, call.Tool, call.Args) {
		return nil
	}
	answer := make(chan bool, 1)
	go func() { answer <- a.askUser("Allow this agent action?", actionQuestion(call), "Allow") }()
	select {
	case ok := <-answer:
		if ok {
			return nil
		}
		return mcpserver.ErrNotApproved
	case <-ctx.Done():
		// The dialog may still be open; its answer no longer counts.
		return errors.New("the user did not answer Flint's question in time, so nothing was run")
	}
}

// actionQuestion is the dialog's text: who asks, the tool, what it would do
// and its arguments.
func actionQuestion(call mcpserver.Approval) string {
	var b strings.Builder
	title := call.Title
	if title == "" {
		title = call.Tool
	}
	fmt.Fprintf(&b, "An agent working in %s wants to use Flint's tool \"%s\" (%s).\n", call.Root, title, call.Tool)
	if call.Summary != "" {
		b.WriteString("\n" + call.Summary + "\n")
	}
	args := redact.Secrets(canonicalArgs(call.Args))
	if len(args) > maxDialogArgs {
		args = strings.ToValidUTF8(args[:maxDialogArgs], "") + "…"
	}
	fmt.Fprintf(&b, "\nArguments: %s\n\nAllow it this once?", args)
	return b.String()
}

// mcpActor does what the acting tools ask (mcptools.Actor).
type mcpActor struct{ app *App }

var _ mcptools.Actor = mcpActor{}

func (m mcpActor) PlanTests(_ string, req testrun.Request) (testrun.Plan, error) {
	return testrun.PlanRun(req)
}

// StartTests starts the run like RunAPI.StartTests does for the gutter ▶; the
// frontend is told first, so the Tests tab shows it from its first event.
func (m mcpActor) StartTests(root string, req testrun.Request, plan testrun.Plan) (string, <-chan struct{}, error) {
	a := m.app
	id := "agent-" + randomID()
	done := a.runEvents.watch(id)
	a.emit(eventRunAdopt, RunAdopt{ID: id, Kind: "tests", Title: plan.Title, Root: root, Request: &req})
	if err := a.startTestPlan(id, plan); err != nil {
		a.runEvents.failed(id, err)
		return "", nil, err
	}
	return id, done, nil
}

func (m mcpActor) RunConfigs(root string) ([]runconfig.Config, error) {
	if err := checkRoot(root); err != nil {
		return nil, err
	}
	saved, err := runconfig.Load(root)
	configs := append(saved, runconfig.Detect(root)...)
	if len(configs) == 0 && err != nil {
		return nil, err
	}
	return configs, nil
}

// StartConfig starts cfg in its run tab; a run of the same configuration
// that is still going (the user's or an agent's) is stopped first, as the
// toolbar's ▶ restarts it.
func (m mcpActor) StartConfig(root string, cfg runconfig.Config) (string, error) {
	a := m.app
	if prev := a.configRuns.current(root, cfg.ID); prev != "" {
		a.runs.Stop(prev)
	}
	id := "agent-" + randomID()
	a.emit(eventRunAdopt, RunAdopt{ID: id, Kind: "config", Title: cfg.Name, Root: root, Config: &cfg})
	if err := a.startConfig(id, root, cfg, mcptools.RunCols, mcptools.RunRows); err != nil {
		a.runEvents.failed(id, err)
		return "", err
	}
	return id, nil
}

// SendHTTP sends the request like HTTPAPI.Send; the response stays in the
// file's history and its response tab is told.
func (m mcpActor) SendHTTP(ctx context.Context, _ string, file string, p httpfile.Prepared) (*httpfile.Response, error) {
	a := m.app
	resp, err := a.http.Send(ctx, "agent-"+randomID(), file, p, httpfile.Options{FollowRedirects: true})
	if err != nil {
		return nil, err
	}
	a.emit(eventHTTPAgent, HTTPAgentResponse{File: file, ID: resp.ID})
	return resp, nil
}

// configRuns remembers the latest run of each run configuration, so a
// restart for an agent can stop it first.
type configRuns struct {
	mu   sync.Mutex
	byID map[string]string // root + NUL + configuration id → run id
}

const maxConfigRuns = 256

func (c *configRuns) set(root, configID, runID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.byID == nil {
		c.byID = map[string]string{}
	}
	if len(c.byID) >= maxConfigRuns {
		clear(c.byID) // only costs a restart its "stop the previous run first"
	}
	c.byID[root+"\x00"+configID] = runID
}

func (c *configRuns) current(root, configID string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.byID[root+"\x00"+configID]
}

// watch returns a channel closed once run id ended (call it before the run
// starts).
func (e *runEvents) watch(id string) <-chan struct{} {
	e.mu.Lock()
	defer e.mu.Unlock()
	done := make(chan struct{})
	if e.done == nil {
		e.done = map[string]chan struct{}{}
	}
	e.done[id] = done
	return done
}

// failed ends a run that could not start, for the tab that already shows it.
func (e *runEvents) failed(id string, err error) {
	e.emit(RunEvent{ID: id, Exit: &runner.Result{Code: -1, Error: err.Error()}})
	e.mu.Lock()
	delete(e.seq, id)
	if done := e.done[id]; done != nil {
		close(done)
		delete(e.done, id)
	}
	e.mu.Unlock()
}
