package main

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"flint/internal/acp/acptest"
	as "flint/internal/agentsession"
	"flint/internal/agentsession/acpdriver"
	"flint/internal/settings"
)

// TestMain lets the test binary double as a fake ACP agent (custom agents
// in these tests run os.Executable()).
func TestMain(m *testing.M) {
	acptest.RunIfAgent()
	os.Exit(m.Run())
}

// useFakeAgent makes processes started by this test the fake agent in mode
// (custom agents inherit Flint's environment) and returns its command.
func useFakeAgent(t *testing.T, mode string) string {
	t.Helper()
	for _, kv := range acptest.Env(mode) {
		k, v, _ := strings.Cut(kv, "=")
		t.Setenv(k, v)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return exe
}

func TestSyncCustomAgents(t *testing.T) {
	m := as.NewManager(func(as.Event) {})
	gemini := settings.CustomAgent{ID: "gemini", Command: "gemini", Args: []string{"--experimental-acp"}}
	opencode := settings.CustomAgent{ID: "opencode", Command: "opencode", Args: []string{"acp"}}
	never := func(string) bool { return false }

	syncCustomAgents(m, nil, []settings.CustomAgent{gemini, opencode}, never)
	if got := m.Agents(); !slices.Equal(got, []string{"gemini", "opencode"}) {
		t.Fatalf("Agents() = %v", got)
	}
	changed := opencode
	changed.Args = []string{"acp", "--pure"}
	syncCustomAgents(m, []settings.CustomAgent{gemini, opencode}, []settings.CustomAgent{changed}, never)
	if got := m.Agents(); !slices.Equal(got, []string{"opencode"}) {
		t.Fatalf("after removing gemini: Agents() = %v", got)
	}
	syncCustomAgents(m, []settings.CustomAgent{changed}, nil, never)
	if got := m.Agents(); len(got) != 0 {
		t.Fatalf("after removing all: Agents() = %v", got)
	}
}

func TestCustomAgentNeedsTrustedFolder(t *testing.T) {
	cmd := useFakeAgent(t, acptest.ModeEcho)
	dir := t.TempDir()
	trusted := false
	untrusted := func(string) bool { return !trusted }
	m := as.NewManager(func(as.Event) {})
	t.Cleanup(func() { _ = m.Close() })
	syncCustomAgents(m, nil, []settings.CustomAgent{{ID: "fake", Command: cmd}}, untrusted)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := m.Start(ctx, "fake", dir); !errors.Is(err, acpdriver.ErrRestricted) {
		t.Fatalf("Start in an untrusted folder = %v, want ErrRestricted", err)
	}
	trusted = true
	info, err := m.Start(ctx, "fake", dir)
	if err != nil {
		t.Fatalf("Start in a trusted folder: %v", err)
	}
	if info.Agent != "fake" {
		t.Errorf("SessionInfo.Agent = %q, want the custom agent's id", info.Agent)
	}
}

func TestHeadlessThroughCustomAgent(t *testing.T) {
	cmd := useFakeAgent(t, acptest.ModeEcho)
	dir := t.TempDir()
	a := &App{configDir: t.TempDir(), settings: settings.Default()}
	a.settings.CustomAgents = []settings.CustomAgent{{ID: "fake", Command: cmd}}
	api := &AgentAPI{app: a}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if _, err := api.headless(ctx, "fake", dir, "Write a message.", "diff"); !errors.Is(err, acpdriver.ErrRestricted) {
		t.Fatalf("headless run in an untrusted folder = %v, want ErrRestricted", err)
	}
	if err := a.setTrust(dir, true); err != nil {
		t.Fatal(err)
	}
	got, err := api.headless(ctx, "fake", dir, "Write a message.", "diff")
	if err != nil || got != "echo:Write a message.\n\ndiff" {
		t.Fatalf("headless = %q, %v", got, err)
	}
	if _, err := api.headless(ctx, "gone", dir, "x", "y"); err == nil {
		t.Error("an agent missing from settings ran")
	}
}

func TestInstalledAgentsAndConflictAgent(t *testing.T) {
	s := settings.Default()
	// Native CLIs at paths that don't exist, so the machine's own don't count.
	s.Agents = map[string]settings.AgentSettings{"claude": {Path: "/nonexistent/claude"}, "codex": {Path: "/nonexistent/codex"}}
	s.CustomAgents = []settings.CustomAgent{{ID: "gemini", Command: "gemini"}, {ID: "missing", Command: "missing"}}
	lookPath := func(name string) (string, error) {
		if name == "gemini" {
			return "/bin/gemini", nil
		}
		return "", errors.New("not found")
	}
	if got := installedAgents(s, lookPath); !slices.Equal(got, []string{"gemini"}) {
		t.Errorf("installedAgents = %v, want [gemini]", got)
	}

	tests := []struct {
		preferred string
		available []string
		want      string
	}{
		{"claude", []string{"claude", "codex", "gemini"}, "claude"},
		{"codex", []string{"claude", "codex"}, "codex"},
		{"gemini", []string{"codex", "gemini"}, "codex"},
		{"gemini", []string{"gemini"}, ""},
		{"", nil, ""},
	}
	for _, tt := range tests {
		if got := conflictAgent(tt.preferred, tt.available); got != tt.want {
			t.Errorf("conflictAgent(%q, %v) = %q, want %q", tt.preferred, tt.available, got, tt.want)
		}
	}
}
