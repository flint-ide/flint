package acpdriver

import (
	"context"
	"errors"
	"testing"

	as "flint/internal/agentsession"
)

// ACP's session/new has no additional folders: a start that asks for some is
// refused before the agent runs.
func TestRefusesAdditionalFolders(t *testing.T) {
	d := New(Options{Name: "my-acp", Command: "/nonexistent/agent"})
	ctx := as.WithFolders(context.Background(), []string{t.TempDir()})
	if _, err := d.Start(ctx, t.TempDir(), func(as.Event) {}); !errors.Is(err, as.ErrNoFolders) {
		t.Errorf("Start = %v", err)
	}
}
