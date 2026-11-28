package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"flint/internal/agenttask"
	"flint/internal/cli"
)

// agentCLIApp is an App with a trusted workspace folder holding an agent
// task's worktree, and a recorded answer to Flint's question.
type agentCLIApp struct {
	*App
	folder, worktree string

	mu     sync.Mutex
	asked  []string
	answer bool
}

func newAgentCLIApp(t *testing.T) *agentCLIApp {
	t.Helper()
	folder, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	wt := filepath.Join(folder, ".worktrees", "rate")
	if err := os.MkdirAll(filepath.Join(wt, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	a := &agentCLIApp{App: &App{configDir: t.TempDir()}, folder: folder, worktree: wt}
	a.state.AgentTasks = agenttask.Store{filepath.Join(folder, ".git"): {{ID: "0123456789abcdef", Title: "Rate limits", Worktree: wt, Checkout: folder}}}
	if err := a.setTrust(folder, true); err != nil {
		t.Fatal(err)
	}
	a.ask = func(title, message, ok string) bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		if title != "Allow this agent action?" || ok != "Allow" {
			t.Errorf("dialog %q / %q", title, ok)
		}
		a.asked = append(a.asked, message)
		return a.answer
	}
	return a
}

func (a *agentCLIApp) questions() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string{}, a.asked...)
}

func delegateRequest(folder, cwd string) cli.AgentRequest {
	return cli.AgentRequest{
		Caller: cli.Caller{Agent: true, What: "claude", PID: 4242, Program: "claude"},
		Path:   "project delegate", Summary: "Start an item's agent tasks (or its workflow runs)",
		Args:   []string{"rate-limit", "--project", "Acme"},
		Files:  map[string]string{"note": "two words"},
		Folder: folder, Cwd: cwd,
	}
}

// An agent's command waits for the user's answer in Flint's dialog, the
// same one the acting MCP tools show: Allow runs it, Cancel refuses it.
func TestAgentCLICommandIsAsked(t *testing.T) {
	a := newAgentCLIApp(t)
	req := delegateRequest(a.folder, a.folder)
	if err := a.confirmAgentCLI(t.Context(), req); err == nil || err.Error() != cli.NotApproved {
		t.Fatalf("declined: %v", err)
	}
	a.answer = true
	if err := a.confirmAgentCLI(t.Context(), req); err != nil {
		t.Fatalf("allowed: %v", err)
	}
	q := a.questions()
	if len(q) != 2 {
		t.Fatalf("asked %d times", len(q))
	}
	for _, want := range []string{
		"An agent (claude) working in " + a.folder + ` wants to run Flint's command "flint project delegate" (process 4242, outside Flint).`,
		"Start an item's agent tasks (or its workflow runs).",
		`Command: flint project delegate rate-limit --project Acme --note "two words"`,
		"Allow it this once?",
	} {
		if !strings.Contains(q[0], want) {
			t.Errorf("question lacks %q:\n%s", want, q[0])
		}
	}
}

// Nothing is asked for a command sent from inside handed-out work (an agent
// task's worktree: a task's or a workflow step's) or for a folder in
// Restricted Mode.
func TestAgentCLICommandRefusedWithoutAsking(t *testing.T) {
	a := newAgentCLIApp(t)
	a.answer = true
	untrusted, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		req  cli.AgentRequest
		want string
	}{
		"cwd in a worktree":         {delegateRequest(a.folder, filepath.Join(a.worktree, "src")), "an agent that was handed work cannot act"},
		"folder is a worktree":      {delegateRequest(a.worktree, a.folder), "an agent that was handed work cannot act"},
		"folder in Restricted Mode": {delegateRequest(untrusted, untrusted), "is in Restricted Mode in Flint: agents cannot act there"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := a.confirmAgentCLI(t.Context(), tc.req); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want %q", err, tc.want)
			}
		})
	}
	if q := a.questions(); len(q) != 0 {
		t.Fatalf("asked %d times", len(q))
	}
	// A command without a folder from a folder outside the workspace is
	// asked about: the command's own checks name what it acts on.
	if err := a.confirmAgentCLI(t.Context(), delegateRequest("", untrusted)); err != nil {
		t.Fatalf("no folder: %v", err)
	}
}

// The question is bounded and redacted, and an answer that comes after the
// command gave up runs nothing.
func TestAgentCLIQuestionAndTimeout(t *testing.T) {
	req := delegateRequest("/p", "/p")
	req.Files = map[string]string{"text": "AKIAABCDEFGHIJKLMNOP " + strings.Repeat("y", 5000)}
	q := agentCommandQuestion(req)
	if len(q) > maxDialogArgs+400 || strings.Contains(q, "AKIAABCDEFGHIJKLMNOP") || !strings.Contains(q, "…") {
		t.Fatalf("question (%d bytes): %s", len(q), q)
	}

	a := &App{}
	release := make(chan struct{})
	a.ask = func(string, string, string) bool {
		<-release
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := a.confirmAgentCLI(ctx, cli.AgentRequest{Path: "task new", Caller: cli.Caller{Agent: true, What: "claude"}})
	close(release)
	if !errors.Is(err, errNotAnsweredInTime) {
		t.Fatalf("unanswered question: %v", err)
	}
}
