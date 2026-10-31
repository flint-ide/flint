package codex

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"flint/internal/agent"
	"flint/internal/agentsession"
)

// The user's extra arguments precede app-server, Flint's overrides follow,
// and Restricted Mode drops the extra arguments.
func TestBuildArgs(t *testing.T) {
	opts := Options{Config: []string{"model_verbosity=low"}, ExtraArgs: []string{"-c", "model_reasoning_effort=high"}}
	got, err := buildArgs(opts, false)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"-c", "model_reasoning_effort=high", "app-server", "-c", "model_verbosity=low"}; !reflect.DeepEqual(got, want) {
		t.Errorf("buildArgs() = %q, want %q", got, want)
	}
	if got, err := buildArgs(opts, true); err != nil || !reflect.DeepEqual(got, []string{"app-server", "-c", "model_verbosity=low"}) {
		t.Errorf("untrusted buildArgs() = %q, %v", got, err)
	}
	for _, bad := range [][]string{{"--dangerously-bypass-approvals-and-sandbox"}, {"-c", "sandbox_mode=danger-full-access"}, {"--"}} {
		if _, err := buildArgs(Options{ExtraArgs: bad}, false); err == nil {
			t.Errorf("buildArgs() accepted %q", bad)
		}
	}
}

// Settings changes reach new sessions through Options.Settings.
func TestSettingsAreReadOnStart(t *testing.T) {
	cfg := agent.Config{Path: "/nonexistent/codex-from-settings"}
	d := New(Options{Binary: "/bin/sh", Settings: func() agent.Config { return cfg }})
	defer d.Close()
	sink := func(agentsession.Event) {}
	if _, err := d.Start(context.Background(), t.TempDir(), sink); err == nil || !strings.Contains(err.Error(), "codex-from-settings") {
		t.Errorf("Start with configured path: %v", err)
	}
	cfg = agent.Config{Path: "/bin/sh", Args: []string{"--yolo"}}
	if _, err := d.Start(context.Background(), t.TempDir(), sink); err == nil || !strings.Contains(err.Error(), "--yolo") {
		t.Errorf("Start with reserved extra argument: %v", err)
	}
}
