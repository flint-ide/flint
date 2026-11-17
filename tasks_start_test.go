package main

import (
	"context"
	"strings"
	"sync"
	"testing"

	"flint/internal/agenttask"
)

func TestTaskQueueStartsTheChatFromGoAndBindsItToTheTask(t *testing.T) {
	f := queueFixture(t, 1)
	var opened []ChatOpened
	var mu sync.Mutex
	f.app.emitHook = func(name string, data ...any) {
		if name == eventChatOpened {
			mu.Lock()
			opened = append(opened, data[0].(ChatOpened))
			mu.Unlock()
		}
	}
	a := f.create("first")
	f.work(a, "sa")
	b := f.create("second task\n\nwith details")
	if got := f.states(b); got != "queued" {
		t.Fatalf("states = %s", got)
	}
	f.settle("sa") // a slot frees

	if got := f.states(b); got != "working" {
		t.Fatalf("the queued task did not start from Go: %s", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(opened) != 1 || opened[0].TaskID != b.ID || opened[0].Info.Cwd != b.Worktree {
		t.Fatalf("chat:opened = %+v, want one chat of task %s in %s", opened, b.ID, b.Worktree)
	}
	// The description is the first prompt, and the chat is bound to the task.
	prompts := f.driver.waitPrompts(t, opened[0].Info.ID, 1)
	if len(prompts) != 1 || !strings.Contains(prompts[0], "second task") {
		t.Errorf("first prompt = %q", prompts)
	}
	if f.app.tasks.State(b.ID, "").State != agenttask.Working {
		t.Error("the chat is not bound to the task")
	}
	// Looking at the queue again does not start the chat a second time.
	f.api.StartQueued()
	f.waitStarts()
	if n := len(f.app.orch.All()); n != 1 {
		t.Errorf("%d chats are open, want 1", n)
	}
}

func TestTaskQueueDoesNotStartASecondChatInAFolderThatHasOne(t *testing.T) {
	f := queueFixture(t, 1)
	a := f.create("first")
	f.work(a, "sa")
	b := f.create("second")
	// The user opened a chat in the queued task's folder by hand.
	if _, err := f.app.openChat(context.Background(), ChatRequest{Agent: "fake", Cwd: b.Worktree}); err != nil {
		t.Fatal(err)
	}
	f.settle("sa")
	f.waitStarts()
	if n := len(f.driver.prompts); n != 0 {
		t.Errorf("a second chat was started and prompted: %v", f.driver.prompts)
	}
	if got := f.states(b); got != "idle" {
		t.Errorf("states = %s", got)
	}
}
