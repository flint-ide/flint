package main

import (
	"path/filepath"
	"sync"
	"testing"

	"flint/internal/feature"
	"flint/internal/features/workflows"
)

// workflowsAPI is the Workflows feature's bound API in a. Its state is
// saved until the test ends: the fake chats' turns end in a cleanup
// (installChats), and the run that follows must not write state.json into
// the configuration folder while the test removes it.
func workflowsAPI(t *testing.T, a *App) *workflows.API {
	t.Helper()
	if err := registerFeatures(); err != nil {
		t.Fatal(err)
	}
	h := &endingHost{featureHost: featureHost{a}}
	a.features = feature.New([]feature.Module{workflows.Module}, h)
	a.features.Start(t.Context())
	t.Cleanup(func() {
		h.mu.Lock()
		h.ended = true
		h.mu.Unlock()
		for _, c := range a.features.Closers() {
			c()
		}
	})
	for _, s := range a.features.Services() {
		if w, ok := s.Instance().(*workflows.API); ok {
			return w
		}
	}
	t.Fatal("no workflows API")
	return nil
}

// endingHost is the feature host until the test ends; then it saves no
// more state (a save under way is waited for).
type endingHost struct {
	featureHost
	mu    sync.RWMutex
	ended bool
}

func (h *endingHost) SaveState(key string, v any) error {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.ended {
		return nil
	}
	return h.featureHost.SaveState(key, v)
}

// A run stopped while its agent task waits in the queue never starts an
// agent: when a slot frees, the task is taken off the queue without a chat.
func TestStoppedRunsQueuedTaskNeverStarts(t *testing.T) {
	f := queueFixture(t, 1)
	api := workflowsAPI(t, f.app)
	writeT(t, filepath.Join(workflows.PersonalDir(f.app.configDir), "fix.md"), "## Fix\n\nFix it.\n\n## Approve\ntype: gate\n")
	busy := f.create("busy")
	f.work(busy, "s-busy")
	run, err := api.Start(workflows.StartRequest{Dir: f.repo, Workflow: "fix", Description: "Fix the bug", Agent: "fake"})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.states(TaskView{ID: run.Task}); got != "queued" {
		t.Fatalf("the run's task is %s, want queued", got)
	}
	if err := api.Stop(run.ID); err != nil {
		t.Fatal(err)
	}
	opened := chatsOpened(f.app)
	f.settle("s-busy") // a slot frees: the queue moves
	f.waitStarts()
	f.app.startQueuedTasks()
	f.waitStarts()
	for _, c := range opened() {
		if c.TaskID == run.Task {
			t.Fatalf("a chat started for the stopped run's task: %+v", c)
		}
	}
	if got := f.states(TaskView{ID: run.Task}); got == "queued" || got == "working" {
		t.Errorf("the stopped run's task is %s", got)
	}
}

// The first step's chat is the one Flint opened for the task's start,
// recorded before its first message (Instance.TaskChatStarted).
func TestRunRecordsItsFirstChat(t *testing.T) {
	f := queueFixture(t, 0)
	api := workflowsAPI(t, f.app)
	writeT(t, filepath.Join(workflows.PersonalDir(f.app.configDir), "fix.md"), "## Fix\n\nFix it.\n\n## Approve\ntype: gate\n")
	opened := chatsOpened(f.app)
	run, err := api.Start(workflows.StartRequest{Dir: f.repo, Workflow: "fix", Description: "Fix the bug", Agent: "fake"})
	if err != nil {
		t.Fatal(err)
	}
	f.waitStarts()
	chats := opened()
	if len(chats) != 1 || chats[0].TaskID != run.Task {
		t.Fatalf("chats %+v", chats)
	}
	for _, v := range api.Runs() {
		if v.ID == run.ID && v.Steps[0].Chat != chats[0].Info.ID {
			t.Errorf("the first step's chat is %q, want %q", v.Steps[0].Chat, chats[0].Info.ID)
		}
	}
}
