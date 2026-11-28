package cli

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
)

// agentRegistry has a command agents may ask for ("project delegate"), one
// whose AgentCheck refuses ("project add"), one refused to agents ("project
// start-when-ready") and the two human decisions ("run approve", "run
// continue"). ran records the commands that ran.
func agentRegistry(t *testing.T, ran *[]string) *Registry {
	t.Helper()
	run := func(path string) func(context.Context, *Call) (Result, error) {
		return func(context.Context, *Call) (Result, error) {
			*ran = append(*ran, path)
			return Result{Text: "done"}, nil
		}
	}
	ask := func(_ context.Context, c *Call) (Question, error) {
		return Question{Title: "Approve this step?", Message: "Run " + c.Arg(0), OK: "Approve", Key: "k"}, nil
	}
	item := []Arg{{Name: "item"}}
	project := []Flag{{Name: "project", Value: "<p>"}, {Name: "text", Value: "<text>", File: true}}
	r, err := NewRegistry([]Command{
		{Path: "project delegate", Summary: "Start an item's agent tasks", Kind: Acts, Folder: true, Args: item, Flags: project, Agents: AgentAsks, Run: run("project delegate")},
		{
			Path: "project add", Summary: "Add an item", Kind: Acts, Flags: project, Agents: AgentAsks, Run: run("project add"),
			AgentCheck: func(context.Context, *Call) error {
				return errors.New("agents can add project items only while it is on")
			},
		},
		{Path: "project start-when-ready", Summary: "Start it when ready", Kind: Acts, Args: item, Run: run("project start-when-ready")},
		{Path: "run approve", Summary: "Approve", Kind: Acts, Human: true, Args: []Arg{{Name: "run"}}, Ask: ask, Run: run("run approve")},
		{Path: "run continue", Summary: "Continue", Kind: Acts, Human: true, Args: []Arg{{Name: "run"}}, Ask: ask, Run: run("run continue")},
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// agentEnv answers Flint's questions: ConfirmAgent with agentYes (and
// records the requests), Confirm (a human decision) with yes.
type agentEnv struct {
	agentYes   bool
	agentAsked []AgentRequest
	humanAsked atomic.Int32
	agentErr   error
}

func (e *agentEnv) env(c Caller) Env {
	return Env{
		Who:     func() Caller { return c },
		Confirm: func(context.Context, Question) bool { e.humanAsked.Add(1); return true },
		ConfirmAgent: func(_ context.Context, a AgentRequest) error {
			e.agentAsked = append(e.agentAsked, a)
			if e.agentErr != nil {
				return e.agentErr
			}
			if !e.agentYes {
				return errors.New(NotApproved)
			}
			return nil
		},
	}
}

var claudeOutside = Caller{Agent: true, What: "claude", PID: 4242, Program: "claude"}

// An agent outside Flint is asked about in Flint before a command that
// lets agents ask runs: a yes runs it, with what the dialog needs.
func TestAgentCommandAsksInFlint(t *testing.T) {
	var ran []string
	reg := agentRegistry(t, &ran)
	e := &agentEnv{agentYes: true}
	resp := DispatchWith(t.Context(), reg, Request{
		Args: []string{"project", "delegate", "rate-limit", "--project", "Acme", "--folder", "/w"},
		Cwd:  "/w/api", Files: map[string]string{"text": "from a file"},
	}, e.env(claudeOutside))
	if resp.Exit != ExitOK || resp.Stdout != "done\n" || !slices.Equal(ran, []string{"project delegate"}) {
		t.Fatalf("allowed: %+v, ran %q", resp, ran)
	}
	if len(e.agentAsked) != 1 {
		t.Fatalf("asked %d times", len(e.agentAsked))
	}
	a := e.agentAsked[0]
	if a.Path != "project delegate" || a.Summary != "Start an item's agent tasks" || a.Caller.PID != 4242 ||
		!slices.Equal(a.Args, []string{"rate-limit", "--project", "Acme", "--folder", "/w"}) ||
		a.Folder != "/w" || a.Cwd != "/w/api" || a.Files["text"] != "from a file" {
		t.Errorf("request %+v", a)
	}
	if e.humanAsked.Load() != 0 {
		t.Error("a human decision's dialog was shown")
	}
	if len(reg.waiting) != 0 {
		t.Errorf("%d waiting places still taken", len(reg.waiting))
	}
}

// A no, or no answer in time, runs nothing and says why (exit 1).
func TestAgentCommandDeniedRunsNothing(t *testing.T) {
	for name, e := range map[string]*agentEnv{
		"denied":    {agentYes: false},
		"timed out": {agentErr: errors.New("the user did not answer Flint's question in time, so nothing was run")},
	} {
		t.Run(name, func(t *testing.T) {
			var ran []string
			reg := agentRegistry(t, &ran)
			resp := DispatchWith(t.Context(), reg, Request{Args: []string{"project", "delegate", "x"}, Cwd: "/w"}, e.env(claudeOutside))
			want := "flint: not approved in Flint\n"
			if e.agentErr != nil {
				want = "flint: " + e.agentErr.Error() + "\n"
			}
			if resp.Exit != ExitFailed || resp.Stderr != want || len(ran) != 0 || len(e.agentAsked) != 1 {
				t.Fatalf("%+v, ran %q, asked %d", resp, ran, len(e.agentAsked))
			}
		})
	}
}

// The user's decisions, a command refused to agents and a failed
// AgentCheck are refused to an agent outside Flint without a dialog.
func TestAgentCommandRefusedWithoutAsking(t *testing.T) {
	for _, tc := range []struct {
		args []string
		err  string
	}{
		{[]string{"run", "approve", "r1"}, "flint: only you can take this decision: an agent (claude) cannot run flint run approve, not even with your yes. Run it yourself in a terminal.\n"},
		{[]string{"run", "continue", "r1"}, "flint: only you can take this decision: an agent (claude) cannot run flint run continue, not even with your yes. Run it yourself in a terminal.\n"},
		{[]string{"project", "start-when-ready", "x"}, "flint: an agent (claude) cannot run flint project start-when-ready, not even with your yes. Run it yourself in a terminal.\n"},
		{[]string{"project", "add"}, "flint: agents can add project items only while it is on\n"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			var ran []string
			reg := agentRegistry(t, &ran)
			e := &agentEnv{agentYes: true}
			resp := DispatchWith(t.Context(), reg, Request{Args: tc.args, Cwd: "/w"}, e.env(claudeOutside))
			if resp.Exit != ExitFailed || resp.Stderr != tc.err {
				t.Errorf("%+v", resp)
			}
			if len(ran) != 0 || len(e.agentAsked) != 0 || e.humanAsked.Load() != 0 {
				t.Errorf("ran %q, agent asked %d, human asked %d", ran, len(e.agentAsked), e.humanAsked.Load())
			}
		})
	}
}

// Agents Flint started, and every agent when Flint cannot ask, get the
// refusal of old: they act through Flint's tools.
func TestAgentInFlintIsRefused(t *testing.T) {
	refusal := "flint: this command was run by an agent (claude). Agents change things through Flint's tools, which ask you first.\n"
	inFlint := claudeOutside
	inFlint.InFlint = true
	for name, tc := range map[string]struct {
		caller  Caller
		noAsker bool
	}{
		"an agent Flint started":    {caller: inFlint},
		"no way to ask the user":    {caller: claudeOutside, noAsker: true},
		"a decision, Flint's agent": {caller: inFlint},
	} {
		t.Run(name, func(t *testing.T) {
			var ran []string
			reg := agentRegistry(t, &ran)
			e := &agentEnv{agentYes: true}
			env := e.env(tc.caller)
			if tc.noAsker {
				env.ConfirmAgent = nil
			}
			args := []string{"project", "delegate", "x"}
			if strings.HasPrefix(name, "a decision") {
				args = []string{"run", "approve", "r1"}
			}
			resp := DispatchWith(t.Context(), reg, Request{Args: args, Cwd: "/w"}, env)
			if resp.Exit != ExitFailed || resp.Stderr != refusal || len(ran) != 0 || len(e.agentAsked) != 0 || e.humanAsked.Load() != 0 {
				t.Fatalf("%+v, ran %q, asked %d/%d", resp, ran, len(e.agentAsked), e.humanAsked.Load())
			}
		})
	}
}

// A person's commands are as before: no agent dialog; a human decision is
// still asked in Flint.
func TestPersonIsNotAskedAsAnAgent(t *testing.T) {
	var ran []string
	reg := agentRegistry(t, &ran)
	e := &agentEnv{}
	person := Caller{PID: 7, Program: "zsh"}
	for _, args := range [][]string{{"project", "delegate", "x"}, {"project", "add"}, {"project", "start-when-ready", "x"}, {"run", "approve", "r1"}} {
		if resp := DispatchWith(t.Context(), reg, Request{Args: args, Cwd: "/w"}, e.env(person)); resp.Exit != ExitOK {
			t.Errorf("%q: %+v", args, resp)
		}
	}
	if len(e.agentAsked) != 0 || e.humanAsked.Load() != 1 || len(ran) != 4 {
		t.Errorf("agent asked %d, human asked %d, ran %q", len(e.agentAsked), e.humanAsked.Load(), ran)
	}
}

// Only a command that acts and is no human decision lets agents ask; an
// AgentCheck needs it. The help says what an agent can do.
func TestRegistryChecksAgentUse(t *testing.T) {
	run := func(context.Context, *Call) (Result, error) { return Result{}, nil }
	ask := func(context.Context, *Call) (Question, error) { return Question{}, nil }
	check := func(context.Context, *Call) error { return nil }
	for name, c := range map[string]Command{
		"reads":            {Path: "run show", Summary: "x", Kind: Reads, Agents: AgentAsks, Run: run},
		"human decision":   {Path: "run approve", Summary: "x", Kind: Acts, Human: true, Ask: ask, Agents: AgentAsks, Run: run},
		"check, no asking": {Path: "run stop", Summary: "x", Kind: Acts, AgentCheck: check, Run: run},
		"unknown use":      {Path: "run stop", Summary: "x", Kind: Acts, Agents: AgentUse(9), Run: run},
	} {
		if _, err := NewRegistry([]Command{c}); err == nil {
			t.Errorf("%s: registered", name)
		}
	}
	var ran []string
	reg := agentRegistry(t, &ran)
	if h := Dispatch(t.Context(), reg, Request{Args: []string{"project", "delegate", "--help"}}, nil); !strings.Contains(h.Stdout, "only after you allow\nthat one call in Flint's own dialog") {
		t.Errorf("help of an AgentAsks command %q", h.Stdout)
	}
	if h := Dispatch(t.Context(), reg, Request{Args: []string{"project", "start-when-ready", "--help"}}, nil); !strings.Contains(h.Stdout, "agents cannot run it") {
		t.Errorf("help of a refused command %q", h.Stdout)
	}
}
