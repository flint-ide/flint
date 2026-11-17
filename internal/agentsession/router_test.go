package agentsession

import (
	"cmp"
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

// routerFake is a defined agent: new conversations go to def, resumed ones
// to the engine they started on.
type routerFake struct {
	*closeCounting
	def     string
	engines map[string]Driver
	asked   []string
}

func (r *routerFake) Route(engine string) (Driver, error) {
	r.asked = append(r.asked, engine)
	d := r.engines[cmp.Or(engine, r.def)]
	if d == nil {
		return nil, errors.New("no engine " + engine)
	}
	return d, nil
}

func TestRouterHandsSessionsToItsEngine(t *testing.T) {
	m := NewManager(func(Event) {})
	claude := &engineFake{resumableFake: &resumableFake{fakeDriver: newFake("")}, name: "claude"}
	codex := &engineFake{resumableFake: &resumableFake{fakeDriver: newFake("")}, name: "codex"}
	close(claude.release)
	r := &routerFake{closeCounting: &closeCounting{fakeDriver: newFake("")}, def: "claude", engines: map[string]Driver{"claude": claude, "codex": codex}}
	m.Register("reviewer", r)

	info, err := m.Start(context.Background(), "reviewer", t.TempDir())
	if err != nil || info.Agent != "reviewer" || info.Engine != "claude" {
		t.Fatalf("Start = %+v, %v", info, err)
	}
	// The session talks to the engine's driver, not to the router.
	if err := m.Respond(info.ID, "p1", "yes"); err != nil {
		t.Fatal(err)
	}
	if opt := <-claude.answered; opt != "yes" {
		t.Fatalf("claude got %q", opt)
	}

	// Whatever the default is now, a conversation that started on codex
	// resumes there.
	info, err = m.Resume(WithEngine(context.Background(), "codex"), "reviewer", "/w", "thr-1")
	if err != nil || info.Agent != "reviewer" || info.Engine != "codex" || len(codex.resumed) != 1 {
		t.Fatalf("Resume = %+v, %v (codex resumed %v)", info, err, codex.resumed)
	}
	if _, err := m.Resume(WithEngine(context.Background(), "gemini"), "reviewer", "/w", "x"); err == nil {
		t.Fatal("resumed on an unknown engine")
	}
	if got := r.asked; len(got) != 3 || got[0] != "" || got[1] != "codex" || got[2] != "gemini" {
		t.Fatalf("Route asked for %q", got)
	}

	// A removed definition's sessions keep their engine drivers, which
	// Close still reaches.
	m.Unregister("reviewer")
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	if claude.closes != 1 || codex.closes != 1 {
		t.Fatalf("closed claude %d, codex %d times; want 1 each", claude.closes, codex.closes)
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
