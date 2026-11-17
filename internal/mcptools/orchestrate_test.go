package mcptools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"flint/internal/mcpserver"
	"flint/internal/orchestrate"
)

// fakeOrch records what the tools ask for.
type fakeOrch struct {
	granted bool
	sent    []string
	err     error
	waited  time.Duration
	started []StartRequest
}

func (f *fakeOrch) Granted(string) bool                      { return f.granted }
func (f *fakeOrch) Limits() OrchLimits                       { return OrchLimits{} }
func (f *fakeOrch) List(string, string) ([]AgentInfo, error) { return nil, f.err }
func (f *fakeOrch) Start(_ context.Context, _, _ string, r StartRequest) (string, error) {
	f.started = append(f.started, r)
	return "c9", f.err
}
func (f *fakeOrch) Send(_, _, id, text string) error {
	f.sent = append(f.sent, id+":"+text)
	return f.err
}
func (f *fakeOrch) Wait(_ context.Context, _, _, _ string, _ []string, d time.Duration) (WaitResult, error) {
	f.waited = d
	return WaitResult{State: "finished"}, f.err
}
func (f *fakeOrch) Output(string, string, string, int) (string, error) { return "", f.err }
func (f *fakeOrch) Stop(string, string, string) error                  { return f.err }

func orchTools(f *fakeOrch, cfg Config) map[string]mcpserver.Tool {
	s := NewSources()
	s.Orchestrator = f
	out := map[string]mcpserver.Tool{}
	for _, tl := range s.Tools(cfg) {
		out[tl.Name] = tl
	}
	return out
}

var orchOn = Config{Orchestration: true, OrchMaxWait: 5 * time.Minute, OrchOutputLines: 50}

func TestOrchestrationToolsNeedTheSettingAndAnOrchestrator(t *testing.T) {
	if got := orchTools(&fakeOrch{}, Config{}); len(got) != 0 {
		t.Errorf("%d tools while off", len(got))
	}
	s := NewSources()
	if got := s.Tools(orchOn); len(got) != 0 {
		t.Errorf("%d tools without an Orchestrator", len(got))
	}
	got := orchTools(&fakeOrch{}, orchOn)
	for _, name := range OrchestrationNames {
		tl, ok := got[name]
		if !ok || !tl.Acts || tl.Granted == nil {
			t.Errorf("%s: %+v", name, tl)
		}
	}
	if len(got) != len(OrchestrationNames) {
		t.Errorf("%d tools, want %d", len(got), len(OrchestrationNames))
	}
}

func TestOrchestrationToolsRequireACallerAndAGrant(t *testing.T) {
	f := &fakeOrch{}
	tl := orchTools(f, orchOn)[ToolSendPrompt]
	args := json.RawMessage(`{"id":"c2","text":"hi"}`)
	if _, err := tl.Call(context.Background(), "/p", args); err == nil || !strings.Contains(err.Error(), "which chat") {
		t.Errorf("no caller: %v", err)
	}
	ctx := mcpserver.WithCaller(context.Background(), "c1")
	if _, err := tl.Call(ctx, "/p", args); err == nil || !strings.Contains(err.Error(), "no permission") {
		t.Errorf("no grant: %v", err)
	}
	if len(f.sent) != 0 {
		t.Fatal("sent without a grant")
	}
	f.granted = true
	if _, err := tl.Call(ctx, "/p", args); err != nil || len(f.sent) != 1 || f.sent[0] != "c2:hi" {
		t.Errorf("granted: %v %v", err, f.sent)
	}
	if !tl.Granted(ctx) || tl.Granted(context.Background()) && !f.granted {
		t.Error("Granted does not follow the orchestrator")
	}
}

func TestOrchestrationRefusalsAreSpelledOut(t *testing.T) {
	ctx := mcpserver.WithCaller(context.Background(), "c1")
	for _, tc := range []struct {
		err  error
		want string
	}{
		{orchestrate.ErrWaitingApproval, "waiting for approval"},
		{orchestrate.ErrDraftOpen, "draft open"},
		{orchestrate.ErrBusy, "busy"},
		{errors.New("something else"), "something else"},
	} {
		f := &fakeOrch{granted: true, err: tc.err}
		tl := orchTools(f, orchOn)[ToolSendPrompt]
		if _, err := tl.Call(ctx, "/p", json.RawMessage(`{"id":"c2","text":"hi"}`)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v became %v, want %q", tc.err, err, tc.want)
		}
	}
}

func TestWaitIsCappedBySettingAndServer(t *testing.T) {
	f := &fakeOrch{granted: true}
	ctx := mcpserver.WithCaller(context.Background(), "c1")
	tl := orchTools(f, orchOn)[ToolWaitAgent]
	for _, tc := range []struct {
		args string
		want time.Duration
	}{
		{`{"id":"c2","until":["finished"]}`, 5 * time.Minute},
		{`{"id":"c2","until":["finished"],"timeoutSeconds":30}`, 30 * time.Second},
		{`{"id":"c2","until":["finished"],"timeoutSeconds":3600}`, 5 * time.Minute},
	} {
		if _, err := tl.Call(ctx, "/p", json.RawMessage(tc.args)); err != nil || f.waited != tc.want {
			t.Errorf("%s: waited %v (%v), want %v", tc.args, f.waited, err, tc.want)
		}
	}
	// Even a setting above the server's own cap stays below it.
	big := tl
	long := orchTools(f, Config{Orchestration: true, OrchMaxWait: time.Hour})[ToolWaitAgent]
	if _, err := long.Call(ctx, "/p", json.RawMessage(`{"id":"c2","until":["idle"]}`)); err != nil || f.waited >= mcpserver.MaxCallTimeout {
		t.Errorf("waited %v (%v) with a huge setting", f.waited, err)
	}
	if long.Timeout > mcpserver.MaxCallTimeout || big.Timeout == 0 {
		t.Errorf("tool timeouts %v / %v", long.Timeout, big.Timeout)
	}
}

func TestStartAgentWorktreeIsNotSupportedYet(t *testing.T) {
	f := &fakeOrch{granted: true}
	ctx := mcpserver.WithCaller(context.Background(), "c1")
	tl := orchTools(f, orchOn)[ToolStartAgent]
	_, err := tl.Call(ctx, "/p", json.RawMessage(`{"task":"x","worktree":true}`))
	if !errors.Is(err, ErrWorktreeUnsupported) || len(f.started) != 0 {
		t.Errorf("worktree: %v, started %v", err, f.started)
	}
	if text, err := tl.Call(ctx, "/p", json.RawMessage(`{"task":"x","role":"r","agent":"codex"}`)); err != nil || !strings.Contains(text, "c9") || f.started[0] != (StartRequest{Agent: "codex", Role: "r", Task: "x"}) {
		t.Errorf("start: %q %v %+v", text, err, f.started)
	}
}
