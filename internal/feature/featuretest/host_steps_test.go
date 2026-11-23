package featuretest

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"flint/internal/agenttask"
	"flint/internal/feature"
)

func TestStepChatsAreScripted(t *testing.T) {
	h := New(context.Background())
	h.SetAgentTask(feature.AgentTask{ID: "t1"})
	ctx := context.Background()

	if _, err := h.StartStepChat(ctx, "nope", "", "go", feature.StepChatOptions{}); err == nil {
		t.Error("a step chat of an unknown task opened")
	}
	h.SetStepChatError(errors.New("paused"))
	if chat, err := h.StartStepChat(ctx, "t1", "", "go", feature.StepChatOptions{}); err == nil || chat != "" {
		t.Errorf("with an error: %q, %v", chat, err)
	}
	h.SetStepChatError(nil)
	chat, err := h.StartStepChat(ctx, "t1", "codex", "plan it", feature.StepChatOptions{})
	if err != nil || chat != "chat-3" || h.TaskChat("t1") != chat {
		t.Fatalf("StartStepChat = %q, %v; TaskChat %q", chat, err, h.TaskChat("t1"))
	}
	if calls := h.StepChats(); len(calls) != 3 || calls[2] != (StepChat{Task: "t1", Agent: "codex", Prompt: "plan it", Chat: chat}) {
		t.Errorf("StepChats = %+v", calls)
	}

	done := make(chan feature.ChatResult, 1)
	go func() {
		res, _ := h.WaitChat(ctx, chat)
		done <- res
	}()
	select {
	case res := <-done:
		t.Fatalf("WaitChat returned before FinishChat: %+v", res)
	case <-time.After(20 * time.Millisecond):
	}
	h.FinishChat(chat, feature.ChatResult{State: agenttask.Finished})
	if res := <-done; res.State != agenttask.Finished {
		t.Errorf("WaitChat = %+v", res)
	}

	h.SetChatOutput(chat, "a\nb\nRESULT: DONE")
	if out := h.ChatOutput(chat, 2); out != "b\nRESULT: DONE" {
		t.Errorf("ChatOutput = %q", out)
	}

	h.SetTaskChat("t1", "first")
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := h.WaitChat(cctx, "first"); !errors.Is(err, context.Canceled) {
		t.Errorf("WaitChat with a cancelled context: %v", err)
	}
	if err := h.StopChat("first"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.WaitChat(ctx, "first"); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Errorf("WaitChat on a stopped chat: %v", err)
	}
	if _, err := h.WaitChat(ctx, "never"); err == nil {
		t.Error("WaitChat on an unknown chat")
	}
	_ = h.StopChat("never")
	if got := h.Stopped(); len(got) != 1 || got[0] != "first" {
		t.Errorf("Stopped = %v", got)
	}
}

func TestRunConfigurationIsScripted(t *testing.T) {
	h := New(context.Background())
	h.SetAgentTask(feature.AgentTask{ID: "t1"})
	h.SetRunResult("test", feature.RunResult{ID: "r1", Code: 1, Output: "FAIL"}, nil)
	ctx := context.Background()
	if res, err := h.RunConfiguration(ctx, "t1", "test", feature.RunOptions{}); err != nil || res.Code != 1 || res.Output != "FAIL" {
		t.Errorf("RunConfiguration = %+v, %v", res, err)
	}
	sb := &feature.Sandbox{Network: true, Writable: []string{"/cache"}}
	if _, err := h.RunConfiguration(ctx, "t1", "lint", feature.RunOptions{Sandbox: sb}); err == nil {
		t.Error("an unscripted configuration ran")
	}
	got := h.Runs()
	if len(got) != 2 || got[0] != (RunCall{Task: "t1", Name: "test"}) {
		t.Fatalf("Runs = %+v", got)
	}
	sb.Writable[0] = "/changed"
	if s := got[1].Sandbox; s == nil || s == sb || !s.Network || s.Writable[0] != "/cache" {
		t.Errorf("the sandbox was not recorded as asked: %+v", s)
	}
}

func TestBudgetPause(t *testing.T) {
	h := New(context.Background())
	if err := h.BudgetPause(); err != nil {
		t.Fatal(err)
	}
	h.SetBudgetPause(errors.New("paused: today's budget is spent"))
	if err := h.BudgetPause(); err == nil {
		t.Error("no pause")
	}
}
