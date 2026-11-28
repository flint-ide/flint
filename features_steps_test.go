package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	as "flint/internal/agentsession"
	"flint/internal/agenttask"
	"flint/internal/feature"
	"flint/internal/runner"
)

// announceRecorder is the session manager with its announced messages (a
// prompt's user_message event, with its origin) recorded.
type announceRecorder struct {
	*as.Manager
	mu  sync.Mutex
	got []as.Event
}

func (r *announceRecorder) Announce(id string, e as.Event) error {
	r.mu.Lock()
	e.SessionID = id
	r.got = append(r.got, e)
	r.mu.Unlock()
	return r.Manager.Announce(id, e)
}

func (r *announceRecorder) announced(id string) []as.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []as.Event
	for _, e := range r.got {
		if e.SessionID == id {
			out = append(out, e)
		}
	}
	return out
}

// stepFixture is a trusted repository with one agent task (no chat yet), the
// fake agent, a runner and the consoles agents read.
type stepFixture struct {
	*taskFixture
	h      featureHost
	task   TaskView
	rec    *announceRecorder
	mu     sync.Mutex
	opened []ChatOpened
}

func newStepFixture(t *testing.T) *stepFixture {
	t.Helper()
	f := &stepFixture{taskFixture: queueFixture(t, 0)}
	a := f.app
	f.rec = &announceRecorder{Manager: a.agentMgr}
	a.agents = f.rec
	a.runEvents = newRunEvents(a)
	a.runs = runner.NewManager(a.runEvents)
	a.mcp, a.mcpSources = newMCP(a)
	t.Cleanup(func() {
		a.runs.CloseAll()
		_ = a.mcp.Close()
	})
	a.emitHook = func(name string, data ...any) {
		if name == eventChatOpened {
			f.mu.Lock()
			f.opened = append(f.opened, data[0].(ChatOpened))
			f.mu.Unlock()
		}
	}
	f.h = featureHost{a}
	f.task = f.create("Rate limit")
	return f
}

func (f *stepFixture) start(prompt string) string {
	f.t.Helper()
	chat, err := f.h.StartStepChat(context.Background(), f.task.ID, "", prompt, feature.StepChatOptions{})
	if err != nil || chat == "" {
		f.t.Fatalf("StartStepChat = %q, %v", chat, err)
	}
	return chat
}

// waitAsync runs WaitChat in the background.
func (f *stepFixture) waitAsync(ctx context.Context, chat string) <-chan stepWait {
	ch := make(chan stepWait, 1)
	go func() {
		res, err := f.h.WaitChat(ctx, chat)
		ch <- stepWait{res, err}
	}()
	return ch
}

type stepWait struct {
	res feature.ChatResult
	err error
}

func notYet(t *testing.T, ch <-chan stepWait) {
	t.Helper()
	select {
	case w := <-ch:
		t.Fatalf("WaitChat returned while the turn runs: %+v", w)
	case <-time.After(30 * time.Millisecond):
	}
}

func waited(t *testing.T, ch <-chan stepWait) stepWait {
	t.Helper()
	select {
	case w := <-ch:
		return w
	case <-time.After(5 * time.Second):
		t.Fatal("WaitChat did not return")
		return stepWait{}
	}
}

func TestStepChatStartsInTheWorktreeAsFlint(t *testing.T) {
	f := newStepFixture(t)
	chat := f.start("Write PLAN.md, token ghp_0123456789abcdefghijklmnopqrstuvwxyzAB")

	n, ok := f.app.orch.Get(chat)
	if !ok || n.Agent != "fake" || n.Cwd != f.task.Worktree || n.Parent != "" {
		t.Fatalf("node = %+v, %v", n, ok)
	}
	msgs := f.rec.announced(chat)
	if len(msgs) != 1 || msgs[0].Kind != as.KindUserMessage || msgs[0].Origin == nil || *msgs[0].Origin != stepOrigin ||
		strings.Contains(msgs[0].Text, "ghp_0123") {
		t.Fatalf("first message events = %+v", msgs)
	}
	if prompts := f.driver.waitPrompts(t, chat, 1); prompts[0] != msgs[0].Text {
		t.Errorf("the agent got %q, the transcript shows %q", prompts[0], msgs[0].Text)
	}
	f.mu.Lock()
	if len(f.opened) != 1 || f.opened[0].TaskID != f.task.ID || !f.opened[0].Background {
		t.Errorf("chat:opened = %+v", f.opened)
	}
	f.mu.Unlock()
	if live := f.app.tasks.State(f.task.ID, ""); live.State != agenttask.Working {
		t.Errorf("the task is %s, want working", live.State)
	}
}

func TestStepChatRefusals(t *testing.T) {
	f := newStepFixture(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name, task, prompt, want string
	}{
		{"invalid id", "../x", "go", "invalid agent task id"},
		{"unknown task", agenttask.NewID(), "go", "no agent task"},
		{"empty prompt", f.task.ID, " \n", "needs a first message"},
		{"long prompt", f.task.ID, strings.Repeat("x", maxStepPrompt+1), "more than"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			chat, err := f.h.StartStepChat(ctx, tc.task, "", tc.prompt, feature.StepChatOptions{})
			if err == nil || chat != "" || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("StartStepChat = %q, %v; want an error with %q", chat, err, tc.want)
			}
		})
	}
	t.Run("unknown agent", func(t *testing.T) {
		if chat, err := f.h.StartStepChat(ctx, f.task.ID, "nobody", "go", feature.StepChatOptions{}); err == nil || chat != "" {
			t.Fatalf("StartStepChat = %q, %v", chat, err)
		}
	})
	t.Run("untrusted worktree", func(t *testing.T) {
		if err := f.app.setTrust(f.task.Worktree, false); err != nil {
			t.Fatal(err)
		}
		if f.app.isTrusted(f.task.Worktree) {
			t.Fatal("the worktree is still trusted")
		}
		chat, err := f.h.StartStepChat(ctx, f.task.ID, "", "go", feature.StepChatOptions{})
		if err == nil || chat != "" || !strings.Contains(err.Error(), "Restricted Mode: trust the folder first") {
			t.Fatalf("StartStepChat = %q, %v", chat, err)
		}
	})
	if all := f.app.orch.All(); len(all) != 0 {
		t.Errorf("a refused step opened chats: %+v", all)
	}
}

// The budget refuses the first message of a chat that did open: the id comes
// with the error, so the caller can still show or close it.
func TestStepChatOverBudgetReturnsTheOpenChat(t *testing.T) {
	f := newStepFixture(t)
	first := f.start("plan")
	f.app.settings.Usage.PauseOverBudget = true
	f.app.settings.Usage.DailyBudgetTokens = 100
	f.app.openUsage()
	t.Cleanup(f.app.closeUsage)
	f.app.recordUsage(first, mustChat(t, f.app, first), &as.Usage{InputTokens: 90, OutputTokens: 20})

	chat, err := f.h.StartStepChat(context.Background(), f.task.ID, "", "implement", feature.StepChatOptions{})
	if err == nil || !strings.Contains(err.Error(), "paused") || chat == "" || chat == first {
		t.Fatalf("StartStepChat = %q, %v", chat, err)
	}
	if _, ok := f.app.orch.Get(chat); !ok {
		t.Error("the chat whose prompt was refused is not open")
	}
	if len(f.driver.got(chat)) != 0 {
		t.Error("a refused prompt reached the agent")
	}
}

// WaitChat called right after StartStepChat waits for the turn the first
// message started: the registry is already working then (promptChat marks
// it before handing the prompt to the agent), so the idle state of a chat
// that was just registered can't be what it returns.
func TestWaitChatRightAfterStartWaitsForTheTurn(t *testing.T) {
	f := newStepFixture(t)
	for _, tc := range []struct {
		name, stop, want string
	}{
		{"finished", as.StopEndTurn, agenttask.Finished},
		{"failed", as.StopError, agenttask.Failed},
		{"cancelled", as.StopCancelled, agenttask.Idle},
	} {
		t.Run(tc.name, func(t *testing.T) {
			chat := f.start("step " + tc.name)
			ch := f.waitAsync(context.Background(), chat)
			notYet(t, ch)
			// A non-fatal error mid-turn makes the chat "failed" for a moment;
			// the turn goes on, and so does the wait.
			f.app.orch.Observe(as.Event{SessionID: chat, Kind: as.KindError, Text: "auto-approve failed"})
			notYet(t, ch)
			f.app.orch.Observe(as.Event{SessionID: chat, Kind: as.KindMessageChunk, Text: "RESULT: DONE"})
			f.app.orch.Observe(as.Event{SessionID: chat, Kind: as.KindTurnEnd, StopReason: tc.stop})
			w := waited(t, ch)
			if w.err != nil || w.res.State != tc.want {
				t.Fatalf("WaitChat = %+v, %v; want %s", w.res, w.err, tc.want)
			}
			if tc.want == agenttask.Failed && w.res.Error == "" {
				t.Error("a failed turn has no error")
			}
			// Over: a second wait answers at once.
			if again, err := f.h.WaitChat(context.Background(), chat); err != nil || again.State != tc.want {
				t.Errorf("second WaitChat = %+v, %v", again, err)
			}
		})
	}
}

func TestWaitChatOnAClosedChat(t *testing.T) {
	f := newStepFixture(t)
	if _, err := f.h.WaitChat(context.Background(), "no-such-chat"); err == nil || !strings.Contains(err.Error(), "was closed") {
		t.Fatalf("unknown chat: %v", err)
	}
	chat := f.start("work")
	ch := f.waitAsync(context.Background(), chat)
	notYet(t, ch)
	if err := f.h.StopChat(chat); err != nil {
		t.Fatal(err)
	}
	if w := waited(t, ch); w.err == nil || !strings.Contains(w.err.Error(), "was closed") {
		t.Fatalf("WaitChat on a chat stopped meanwhile = %+v", w)
	}
	if _, ok := f.app.orch.Get(chat); ok {
		t.Error("StopChat left the chat registered")
	}
	if err := f.h.StopChat(chat); err != nil {
		t.Errorf("stopping it again: %v", err)
	}
	if err := f.h.StopChat(""); err != nil {
		t.Errorf("StopChat(\"\"): %v", err)
	}
	if err := f.h.StopChat("no-such-chat"); err != nil {
		t.Errorf("StopChat(unknown): %v", err)
	}
}

func TestWaitChatEndsWithItsContext(t *testing.T) {
	f := newStepFixture(t)
	chat := f.start("work")
	ctx, cancel := context.WithCancel(context.Background())
	ch := f.waitAsync(ctx, chat)
	notYet(t, ch)
	cancel()
	if w := waited(t, ch); !errors.Is(w.err, context.Canceled) {
		t.Fatalf("WaitChat = %+v", w)
	}
}

func TestChatOutputIsRedactedAndBounded(t *testing.T) {
	f := newStepFixture(t)
	chat := f.start("work")
	var text strings.Builder
	for i := 0; i < 300; i++ {
		text.WriteString("line\n")
	}
	text.WriteString("the key is ghp_0123456789abcdefghijklmnopqrstuvwxyzAB\nRESULT: DONE")
	f.app.orch.Observe(as.Event{SessionID: chat, Kind: as.KindMessageChunk, Text: text.String()})

	out := f.h.ChatOutput(chat, 2)
	if strings.Contains(out, "ghp_0123") || !strings.HasSuffix(out, "RESULT: DONE") || strings.Count(out, "\n") != 1 {
		t.Fatalf("ChatOutput(2) = %q", out)
	}
	if out := f.h.ChatOutput(chat, 0); out != "RESULT: DONE" {
		t.Errorf("ChatOutput(0) = %q, want the last line", out)
	}
	if out := f.h.ChatOutput(chat, 10_000); strings.Count(out, "\n")+1 > maxStepOutputLines {
		t.Errorf("ChatOutput(10000) has %d lines", strings.Count(out, "\n")+1)
	}
	if out := f.h.ChatOutput("no-such-chat", 5); out != "" {
		t.Errorf("unknown chat: %q", out)
	}
}

// A review step's chat is started as a reviewer's (as.WithReviewer): its
// driver loads none of the worktree's project settings, and the manager
// approves none of its requests; an agent step's is not.
func TestStepChatOfAReviewer(t *testing.T) {
	f := newStepFixture(t)
	for _, reviewer := range []bool{false, true} {
		if _, err := f.h.StartStepChat(context.Background(), f.task.ID, "", "review", feature.StepChatOptions{Reviewer: reviewer}); err != nil {
			t.Fatal(err)
		}
	}
	f.driver.mu.Lock()
	got := slices.Clone(f.driver.reviewers)
	f.driver.mu.Unlock()
	if !slices.Equal(got, []bool{false, true}) {
		t.Errorf("reviewer marks %v", got)
	}
}

// writeRunJSON saves root's .flint/run.json with one configuration, "test",
// that runs script in sh.
func writeRunJSON(t *testing.T, root, script string) {
	t.Helper()
	cfg := map[string]any{"id": "test", "name": "test", "type": "command", "command": "sh", "args": []string{"-c", script}}
	data, err := json.Marshal(map[string]any{"version": 1, "configurations": []any{cfg}})
	if err != nil {
		t.Fatal(err)
	}
	writeT(t, filepath.Join(root, ".flint", "run.json"), string(data))
}

func TestRunConfigurationComesFromTheCheckout(t *testing.T) {
	f := newStepFixture(t)
	writeRunJSON(t, f.repo, "pwd; echo from-checkout ghp_0123456789abcdefghijklmnopqrstuvwxyzAB; exit 3")
	// The agent rewrites the command in its worktree, and adds one.
	writeT(t, filepath.Join(f.task.Worktree, ".flint", "run.json"),
		`{"version":1,"configurations":[{"id":"test","name":"test","type":"command","command":"sh","args":["-c","echo from-worktree"]},`+
			`{"id":"evil","name":"evil","type":"command","command":"sh","args":["-c","echo evil"]}]}`)

	res, err := f.h.RunConfiguration(context.Background(), f.task.ID, "test", feature.RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Code != 3 || res.Stopped || res.Error != "" || !strings.HasPrefix(res.ID, stepRunPrefix) {
		t.Fatalf("result = %+v", res)
	}
	if !strings.Contains(res.Output, "from-checkout") || strings.Contains(res.Output, "from-worktree") {
		t.Fatalf("the configuration did not come from the checkout:\n%s", res.Output)
	}
	if !strings.Contains(res.Output, f.task.Worktree) {
		t.Errorf("the run did not run in the worktree:\n%s", res.Output)
	}
	if strings.Contains(res.Output, "ghp_0123") {
		t.Errorf("the output is not redacted:\n%s", res.Output)
	}
	if info, ok := f.app.mcpSources.Terminals.Get(res.ID); !ok || !info.Exited || info.Code != 3 {
		t.Errorf("console = %+v, %v", info, ok)
	}

	_, err = f.h.RunConfiguration(context.Background(), f.task.ID, "evil", feature.RunOptions{})
	if err == nil || !strings.Contains(err.Error(), `no run configuration named "evil" in `+f.repo) ||
		!strings.Contains(err.Error(), ".flint/run.json") {
		t.Fatalf("a configuration only the worktree has: %v", err)
	}
}

func TestRunConfigurationAdoptsItsRunTab(t *testing.T) {
	f := newStepFixture(t)
	writeRunJSON(t, f.repo, "echo ok")
	var mu sync.Mutex
	var adopted []RunAdopt
	f.app.emitHook = func(name string, data ...any) {
		if name == eventRunAdopt {
			mu.Lock()
			adopted = append(adopted, data[0].(RunAdopt))
			mu.Unlock()
		}
	}
	res, err := f.h.RunConfiguration(context.Background(), f.task.ID, "test", feature.RunOptions{})
	if err != nil || res.Code != 0 || !strings.Contains(res.Output, "ok") {
		t.Fatalf("RunConfiguration = %+v, %v", res, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(adopted) != 1 || adopted[0].ID != res.ID || adopted[0].Kind != "config" || adopted[0].Root != f.task.Worktree ||
		adopted[0].Config == nil || adopted[0].Config.Name != "test" {
		t.Errorf("run:adopt = %+v", adopted)
	}
}

func TestRunConfigurationRefusals(t *testing.T) {
	f := newStepFixture(t)
	writeRunJSON(t, f.repo, "echo ok")
	ctx := context.Background()
	if _, err := f.h.RunConfiguration(ctx, agenttask.NewID(), "test", feature.RunOptions{}); err == nil || !strings.Contains(err.Error(), "no agent task") {
		t.Errorf("unknown task: %v", err)
	}
	if _, err := f.h.RunConfiguration(ctx, f.task.ID, "", feature.RunOptions{}); err == nil {
		t.Error("an empty name was accepted")
	}
	if _, err := f.h.RunConfiguration(ctx, f.task.ID, "Test", feature.RunOptions{}); err == nil || !strings.Contains(err.Error(), "no run configuration named") {
		t.Errorf("the name is not matched exactly: %v", err)
	}
	if err := f.app.setTrust(f.repo, false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.h.RunConfiguration(ctx, f.task.ID, "test", feature.RunOptions{}); err == nil || !strings.Contains(err.Error(), "the checkout") ||
		!strings.Contains(err.Error(), "Restricted Mode") {
		t.Errorf("untrusted checkout: %v", err)
	}
	if err := f.app.setTrust(f.repo, true); err != nil {
		t.Fatal(err)
	}
	if err := f.app.setTrust(f.task.Worktree, false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.h.RunConfiguration(ctx, f.task.ID, "test", feature.RunOptions{}); err == nil || !strings.Contains(err.Error(), "the worktree") ||
		!strings.Contains(err.Error(), "Restricted Mode") {
		t.Errorf("untrusted worktree: %v", err)
	}
	if list := f.app.mcpSources.Terminals.List(f.task.Worktree); len(list) != 0 {
		t.Errorf("a refused run started %+v", list)
	}
	if err := f.app.setTrust(f.task.Worktree, true); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(f.task.Worktree); err != nil {
		t.Fatal(err)
	}
	if _, err := f.h.RunConfiguration(ctx, f.task.ID, "test", feature.RunOptions{}); err == nil || !strings.Contains(err.Error(), "is gone") {
		t.Errorf("a removed worktree: %v", err)
	}
}

func TestRunConfigurationStopsTheRunWhenCancelled(t *testing.T) {
	f := newStepFixture(t)
	writeRunJSON(t, f.repo, "echo started; sleep 30")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	var res feature.RunResult
	go func() {
		var err error
		res, err = f.h.RunConfiguration(ctx, f.task.ID, "test", feature.RunOptions{})
		done <- err
	}()
	var id string
	for i := 0; i < 500 && id == ""; i++ {
		if cur := f.app.configRuns.current(f.task.Worktree, "test"); cur != "" && f.app.runs.Running(cur) {
			id = cur
		} else {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if id == "" {
		t.Fatal("the run never started")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || res.ID != id {
			t.Fatalf("RunConfiguration = %+v, %v", res, err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("a cancelled RunConfiguration did not return")
	}
	if f.app.runs.Running(id) {
		t.Error("the run goes on after its step was cancelled")
	}
}

func TestKeepEnd(t *testing.T) {
	for _, tc := range []struct {
		in   string
		n    int
		want string
	}{
		{"abc", 5, "abc"},
		{"abcdef", 3, "def"},
		{"aé", 1, ""},   // the cut lands inside é: drop the partial rune
		{"aéb", 2, "b"}, // "\xa9b" starts mid-rune
		{"aéb", 3, "éb"},
	} {
		if got := keepEnd(tc.in, tc.n); got != tc.want {
			t.Errorf("keepEnd(%q, %d) = %q, want %q", tc.in, tc.n, got, tc.want)
		}
	}
}

// PromptStepChat asks a step's chat again after its turn ended: the message
// comes from Flint (redacted), reaches the agent, and the next WaitChat waits
// for the turn it starts. A chat whose turn still runs gets nothing.
func TestPromptStepChatStartsTheNextTurn(t *testing.T) {
	f := newStepFixture(t)
	ctx := context.Background()
	f.driver.mu.Lock()
	orig := f.driver.hold // the fixture closes it when the test ends
	f.driver.mu.Unlock()
	t.Cleanup(func() { f.driver.mu.Lock(); f.driver.hold = orig; f.driver.mu.Unlock() })
	hold := func() chan struct{} {
		ch := make(chan struct{})
		f.driver.mu.Lock()
		f.driver.hold = ch
		f.driver.mu.Unlock()
		return ch
	}
	first := hold()
	chat := f.start("implement")
	f.driver.waitPrompts(t, chat, 1)
	if err := f.h.PromptStepChat(ctx, chat, "finish please"); err != nil {
		t.Fatalf("PromptStepChat on a busy chat = %v, want nil", err)
	}
	if got := f.driver.got(chat); len(got) != 1 {
		t.Fatalf("a busy chat got another prompt: %q", got)
	}
	endTurn := func(release chan struct{}) {
		close(release)
		for i := 0; i < 500 && f.app.agents.Busy(chat); i++ {
			time.Sleep(10 * time.Millisecond)
		}
		f.app.orch.Observe(as.Event{SessionID: chat, Kind: as.KindTurnEnd, StopReason: as.StopEndTurn})
	}
	endTurn(first)
	if w := waited(t, f.waitAsync(ctx, chat)); w.err != nil || w.res.State != agenttask.Finished {
		t.Fatalf("first turn = %+v, %v", w.res, w.err)
	}

	second := hold()
	if err := f.h.PromptStepChat(ctx, chat, "Your turn ended without a result line, token ghp_0123456789abcdefghijklmnopqrstuvwxyzAB"); err != nil {
		t.Fatal(err)
	}
	ch := f.waitAsync(ctx, chat)
	notYet(t, ch)
	msgs := f.rec.announced(chat)
	last := msgs[len(msgs)-1]
	if len(msgs) != 2 || last.Origin == nil || *last.Origin != stepOrigin || strings.Contains(last.Text, "ghp_0123") {
		t.Fatalf("messages = %+v", msgs)
	}
	if prompts := f.driver.waitPrompts(t, chat, 2); prompts[1] != last.Text {
		t.Errorf("the agent got %q", prompts[1])
	}
	endTurn(second)
	if w := waited(t, ch); w.err != nil || w.res.State != agenttask.Finished {
		t.Fatalf("second turn = %+v, %v", w.res, w.err)
	}
}

func TestPromptStepChatRefusals(t *testing.T) {
	f := newStepFixture(t)
	ctx := context.Background()
	if err := f.h.PromptStepChat(ctx, "no-such-chat", "finish"); err == nil || !strings.Contains(err.Error(), "was closed") {
		t.Errorf("unknown chat: %v", err)
	}
	chat := f.start("work")
	if err := f.h.PromptStepChat(ctx, chat, "  \n"); err == nil {
		t.Error("an empty message was sent")
	}
}
