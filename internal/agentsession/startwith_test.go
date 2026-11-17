package agentsession

import (
	"context"
	"errors"
	"testing"
)

// engineFake reports its own name, as the native drivers do.
type engineFake struct {
	*resumableFake
	name   string
	closes int
}

func (e *engineFake) Close() error {
	e.closes++
	return nil
}

func (e *engineFake) Start(ctx context.Context, cwd string, sink Sink) (SessionInfo, error) {
	info, err := e.resumableFake.Start(ctx, cwd, sink)
	info.Agent = e.name
	return info, err
}

func (e *engineFake) Resume(ctx context.Context, cwd, id string, sink Sink) (SessionInfo, error) {
	info, err := e.resumableFake.Resume(ctx, cwd, id, sink)
	info.Agent = e.name
	return info, err
}

// StartWith and ResumeWith run a session on the driver handed over, under
// the agent's name, even when nothing is registered under that name: a
// defined agent's engine driver.
func TestStartWithHandsSessionsToTheGivenDriver(t *testing.T) {
	m := NewManager(func(Event) {})
	claude := &engineFake{resumableFake: &resumableFake{fakeDriver: newFake("")}, name: "claude"}
	codex := &engineFake{resumableFake: &resumableFake{fakeDriver: newFake("")}, name: "codex"}
	close(claude.release)
	// A registration under the name is not what starts.
	other := &engineFake{resumableFake: &resumableFake{fakeDriver: newFake("")}, name: "other"}
	m.Register("reviewer", other)

	info, err := m.StartWith(context.Background(), "reviewer", t.TempDir(), claude)
	if err != nil || info.Agent != "reviewer" || info.Engine != "claude" {
		t.Fatalf("StartWith = %+v, %v", info, err)
	}
	// The session talks to that driver.
	if err := m.Respond(info.ID, "p1", "yes"); err != nil {
		t.Fatal(err)
	}
	if opt := <-claude.answered; opt != "yes" {
		t.Fatalf("claude got %q", opt)
	}
	info, err = m.ResumeWith(context.Background(), "reviewer", "/w", "thr-1", codex)
	if err != nil || info.Agent != "reviewer" || info.Engine != "codex" || len(codex.resumed) != 1 {
		t.Fatalf("ResumeWith = %+v, %v (codex resumed %v)", info, err, codex.resumed)
	}
	if _, err := m.ResumeWith(context.Background(), "reviewer", "/w", "x", newFake("")); !errors.Is(err, ErrNoResume) {
		t.Fatalf("resumed on a driver that can't: %v", err)
	}

	// Drivers that were never registered still reach Close.
	m.Unregister("reviewer")
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	if claude.closes != 1 || codex.closes != 1 {
		t.Fatalf("closed claude %d, codex %d times; want 1 each", claude.closes, codex.closes)
	}
	if _, err := m.StartWith(context.Background(), "reviewer", t.TempDir(), claude); !errors.Is(err, ErrClosed) {
		t.Fatalf("StartWith after Close = %v", err)
	}
}

func TestNativeSessionEngineIsItsName(t *testing.T) {
	m := NewManager(func(Event) {})
	m.Register("claude", &engineFake{resumableFake: &resumableFake{fakeDriver: newFake("")}, name: "claude"})
	if info, err := m.Start(context.Background(), "claude", t.TempDir()); err != nil || info.Agent != "claude" || info.Engine != "claude" {
		t.Fatalf("claude: %+v %v", info, err)
	}
	m2 := NewManager(func(Event) {})
	m2.Register("gemini", newFake("")) // a driver that reports no name
	if info, err := m2.Start(context.Background(), "gemini", t.TempDir()); err != nil || info.Agent != "gemini" || info.Engine != "gemini" {
		t.Fatalf("gemini: %+v %v", info, err)
	}
}
