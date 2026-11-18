package codex

import (
	"context"
	"errors"
	"testing"

	"flint/internal/agentsession"
)

// Codex threads get no additional folders: a start or resume that asks for
// some is refused before any process runs, never started in cwd alone.
func TestRefusesAdditionalFolders(t *testing.T) {
	d := New(Options{Binary: "/nonexistent/codex"})
	ctx := agentsession.WithFolders(context.Background(), []string{t.TempDir()})
	sink := func(agentsession.Event) {}
	if _, err := d.Start(ctx, t.TempDir(), sink); !errors.Is(err, agentsession.ErrNoFolders) {
		t.Errorf("Start = %v", err)
	}
	if _, err := d.Resume(ctx, t.TempDir(), "thread", sink); !errors.Is(err, agentsession.ErrNoFolders) {
		t.Errorf("Resume = %v", err)
	}
}
