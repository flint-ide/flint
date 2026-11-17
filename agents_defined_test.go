package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"flint/internal/agent"
	"flint/internal/agentdefs"
	as "flint/internal/agentsession"
	"flint/internal/agentsession/claude"
	"flint/internal/agentsession/codex"
	"flint/internal/settings"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// nameDriver is a driver that reports its engine's name and can resume.
type nameDriver struct {
	name    string
	mu      sync.Mutex
	resumed []string
}

func (d *nameDriver) Start(_ context.Context, cwd string, _ as.Sink) (as.SessionInfo, error) {
	return as.SessionInfo{ID: d.name + "-new", Agent: d.name, Cwd: cwd}, nil
}
func (d *nameDriver) Resume(_ context.Context, cwd, id string, _ as.Sink) (as.SessionInfo, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.resumed = append(d.resumed, id)
	return as.SessionInfo{ID: id, Agent: d.name, Cwd: cwd}, nil
}
func (d *nameDriver) Prompt(context.Context, string, string, []as.Attachment) (string, error) {
	return as.StopEndTurn, nil
}
func (d *nameDriver) Cancel(string) error                  { return nil }
func (d *nameDriver) Respond(string, string, string) error { return nil }
func (d *nameDriver) Stop(string) error                    { return nil }
func (d *nameDriver) Close() error                         { return nil }

func TestSyncDefinedAgents(t *testing.T) {
	m := as.NewManager(func(as.Event) {})
	reviewer := agentdefs.Definition{Name: "reviewer", Engine: "claude", Instructions: "Review."}
	planner := agentdefs.Definition{Name: "planner", Instructions: "Plan."}
	built := []string{}
	newDriver := func(d agentdefs.Definition) as.Driver {
		built = append(built, d.Name)
		return &nameDriver{name: "claude"}
	}
	none := func(string) bool { return false }

	syncDefinedAgents(m, nil, []agentdefs.Definition{reviewer, planner}, none, newDriver)
	if got := m.Agents(); !slices.Equal(got, []string{"planner", "reviewer"}) {
		t.Fatalf("Agents() = %v", got)
	}
	// Only a definition that runs differently gets a new driver.
	changed := reviewer
	changed.Model = "opus"
	described := planner
	described.Description, described.Path = "new words", "/elsewhere/planner.md"
	built = nil
	syncDefinedAgents(m, []agentdefs.Definition{reviewer, planner}, []agentdefs.Definition{changed, described}, none, newDriver)
	if !slices.Equal(built, []string{"reviewer"}) {
		t.Fatalf("rebuilt %v, want only reviewer", built)
	}
	// Removed: planner goes; a name a custom agent took since stays its owner's.
	custom := &nameDriver{name: "reviewer"}
	m.Register("reviewer", custom) // syncCustomAgents registered a custom agent "reviewer"
	syncDefinedAgents(m, []agentdefs.Definition{changed, described}, nil, func(n string) bool { return n == "reviewer" }, newDriver)
	if got := m.Agents(); !slices.Equal(got, []string{"reviewer"}) {
		t.Fatalf("after removal: Agents() = %v", got)
	}
	if info, err := m.Start(context.Background(), "reviewer", t.TempDir()); err != nil || info.Engine != "reviewer" {
		t.Fatalf("the custom agent lost its name: %+v %v", info, err)
	}
}

func TestDefinedAgentRoute(t *testing.T) {
	def := agentdefs.Definition{Name: "reviewer", Instructions: "Review."}
	engine, failure := agent.Codex, error(nil)
	resolve := func(agentdefs.Definition) (agent.Kind, error) { return engine, failure }
	d := newDefinedAgent(def, nil, func(agent.Kind) agent.Config { return agent.Config{} }, nil, resolve)
	for _, tt := range []struct {
		engine string
		want   string
	}{{"", "codex"}, {"claude", "claude"}, {"codex", "codex"}, {"gemini", ""}} {
		drv, err := d.Route(tt.engine)
		var got string
		switch drv.(type) {
		case *claude.Driver:
			got = "claude"
		case *codex.Driver:
			got = "codex"
		}
		if got != tt.want || (tt.want == "") != (err != nil) {
			t.Errorf("Route(%q) = %T, %v; want %s", tt.engine, drv, err, tt.want)
		}
	}
	failure = agentdefs.ErrNoEngine
	if _, err := d.Route(""); !errors.Is(err, agentdefs.ErrNoEngine) {
		t.Errorf("Route with nothing installed = %v", err)
	}
}

// A conversation resumes on the engine it started on, whatever the default
// engine is by then; a new one starts on the default.
func TestDefinedAgentResumesOnItsEngine(t *testing.T) {
	m := as.NewManager(func(as.Event) {})
	d := &definedAgent{
		def:     agentdefs.Definition{Name: "reviewer", Instructions: "Review."},
		engines: map[agent.Kind]as.Driver{agent.Claude: &nameDriver{name: "claude"}, agent.Codex: &nameDriver{name: "codex"}},
		resolve: func(agentdefs.Definition) (agent.Kind, error) { return agent.Claude, nil },
	}
	m.Register("reviewer", d)
	info, err := m.Start(context.Background(), "reviewer", t.TempDir())
	if err != nil || info.Agent != "reviewer" || info.Engine != "claude" {
		t.Fatalf("Start = %+v, %v", info, err)
	}
	info, err = m.Resume(as.WithEngine(context.Background(), "codex"), "reviewer", "/w", "thr-1")
	if err != nil || info.Agent != "reviewer" || info.Engine != "codex" {
		t.Fatalf("Resume = %+v, %v", info, err)
	}
	if got := d.engines[agent.Codex].(*nameDriver).resumed; !slices.Equal(got, []string{"thr-1"}) {
		t.Fatalf("codex resumed %v", got)
	}
	// Unknown engine (a chat saved before engines were recorded): the default.
	if info, err = m.Resume(context.Background(), "reviewer", "/w", "old"); err != nil || info.Engine != "claude" {
		t.Fatalf("Resume without engine = %+v, %v", info, err)
	}
}

// definedApp is an App with claude installed (codex not), a custom agent
// gemini, a trusted and an untrusted workspace root and some definitions.
func definedApp(t *testing.T) (*App, string, string) {
	t.Helper()
	config, trusted, untrusted := t.TempDir(), t.TempDir(), t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	a := &App{configDir: config, settings: settings.Default(), roots: []string{trusted, untrusted}}
	a.settings.Agents = map[string]settings.AgentSettings{"claude": {Path: exe, Model: "sonnet"}, "codex": {Path: "/nonexistent/codex"}}
	a.settings.CustomAgents = []settings.CustomAgent{{ID: "gemini", Command: exe}}
	a.state.TrustedFolders = []string{trusted}
	a.agentMgr = as.NewManager(func(as.Event) {})
	t.Cleanup(func() { _ = a.agentMgr.Close() })
	personal := agentdefs.PersonalDir(config)
	writeFile(t, filepath.Join(personal, "opus-reviewer.md"), "---\nengine: claude\nmodel: opus\n---\nReview.")
	writeFile(t, filepath.Join(personal, "planner.md"), "Plan.")
	writeFile(t, filepath.Join(personal, "backend.md"), "---\nengine: codex\n---\nBuild.")
	writeFile(t, filepath.Join(personal, "gemini.md"), "Taken by a custom agent.")
	writeFile(t, filepath.Join(agentdefs.ProjectDir(trusted), "proj.md"), "Project agent.")
	writeFile(t, filepath.Join(agentdefs.ProjectDir(untrusted), "evil.md"), "Untrusted.")
	return a, trusted, untrusted
}

func TestAgentEngine(t *testing.T) {
	a, _, _ := definedApp(t)
	tests := []struct {
		name, defaultAgent string
		engine, model      string
		ok                 bool
		err                string
	}{
		{name: "claude", engine: "claude", model: "sonnet", ok: true},
		{name: "codex", engine: "codex", ok: true},
		{name: "gemini"},
		{name: "opus-reviewer", engine: "claude", model: "opus", ok: true},
		{name: "planner", engine: "claude", model: "sonnet", ok: true},
		{name: "planner", defaultAgent: "codex", engine: "claude", model: "sonnet", ok: true}, // codex is not installed
		{name: "planner", defaultAgent: "gemini", engine: "claude", model: "sonnet", ok: true},
		{name: "planner", defaultAgent: "planner", engine: "claude", model: "sonnet", ok: true}, // no loop
		{name: "backend", err: "codex, which is not installed"},
		{name: "proj", engine: "claude", model: "sonnet", ok: true},
		{name: "evil"},
		{name: "nobody"},
	}
	for _, tt := range tests {
		a.settings.DefaultAgent = tt.defaultAgent
		k, cfg, ok, err := a.agentEngine(tt.name)
		if tt.err != "" {
			if err == nil || !strings.Contains(err.Error(), tt.err) {
				t.Errorf("%s: err %v, want %q", tt.name, err, tt.err)
			}
			continue
		}
		if err != nil || string(k) != tt.engine || cfg.Model != tt.model || ok != tt.ok {
			t.Errorf("%s (default %q) = %q %+v %v %v; want %q model %q ok %v", tt.name, tt.defaultAgent, k, cfg, ok, err, tt.engine, tt.model, tt.ok)
		}
	}
}

func TestAvailableAgentsAndRegistration(t *testing.T) {
	a, _, _ := definedApp(t)
	if got := a.availableAgents(); !slices.Equal(got, []string{"claude", "gemini", "opus-reviewer", "planner", "proj"}) {
		t.Fatalf("availableAgents = %v", got)
	}
	// Registered for chats, backend too (it fails to start with a clear
	// message); gemini stays unregistered here (syncCustomAgents owns it).
	if got := a.agentMgr.Agents(); !slices.Equal(got, []string{"backend", "opus-reviewer", "planner", "proj"}) {
		t.Fatalf("registered %v", got)
	}
	// A removed file is gone for the next conversation.
	if err := os.Remove(filepath.Join(agentdefs.PersonalDir(a.configDir), "planner.md")); err != nil {
		t.Fatal(err)
	}
	a.loadDefinedAgents()
	if _, err := a.agentMgr.Start(context.Background(), "planner", t.TempDir()); err == nil || !strings.Contains(err.Error(), "planner") {
		t.Fatalf("a removed definition started: %v", err)
	}
	// With no native agent installed, no definition can run.
	a.settings.Agents = map[string]settings.AgentSettings{"claude": {Path: "/nonexistent/claude"}, "codex": {Path: "/nonexistent/codex"}}
	if got := a.availableAgents(); !slices.Equal(got, []string{"gemini"}) {
		t.Fatalf("nothing installed: availableAgents = %v", got)
	}
}

func TestAgentDefsAPI(t *testing.T) {
	a, trusted, untrusted := definedApp(t)
	api := &AgentDefsAPI{app: a}
	list := api.List()
	got := []string{}
	for _, d := range list.Agents {
		got = append(got, d.Name+":"+string(d.Source)+":"+d.Engine+":"+d.ResolvedEngine+":"+d.Model)
	}
	want := []string{"backend:personal:codex::", "opus-reviewer:personal:claude:claude:opus", "planner:personal::claude:", "proj:project::claude:"}
	if !slices.Equal(got, want) {
		t.Fatalf("agents %v\nwant %v", got, want)
	}
	if len(list.Ignored) != 1 || list.Ignored[0].Dir != agentdefs.ProjectDir(untrusted) || list.Ignored[0].Count != 1 {
		t.Errorf("ignored %+v", list.Ignored)
	}
	if !strings.Contains(strings.Join(list.Problems, "\n"), "gemini.md: gemini is a custom agent's id") {
		t.Errorf("problems %v", list.Problems)
	}
	if list.PersonalDir != agentdefs.PersonalDir(a.configDir) {
		t.Errorf("personal dir %s", list.PersonalDir)
	}

	path, err := api.Create("", true, "writer")
	if err != nil || path != filepath.Join(agentdefs.PersonalDir(a.configDir), "writer.md") {
		t.Fatalf("Create personal = %s, %v", path, err)
	}
	path, err = api.Create(filepath.Join(trusted, "sub"), false, "tester")
	if err != nil || path != filepath.Join(agentdefs.ProjectDir(trusted), "tester.md") {
		t.Fatalf("Create project = %s, %v", path, err)
	}
	for _, tt := range []struct {
		root     string
		personal bool
		name     string
		err      string
	}{
		{"", true, "Bad Name", "invalid agent name"},
		{"", true, "codex", "built-in"},
		{"", true, "gemini", "custom agent"},
		{"", true, "planner", "already defined"},
		{trusted, false, "proj", "already defined"},
		{untrusted, false, "x", "Restricted Mode"},
		{t.TempDir(), false, "x", "outside"},
		{"", false, "x", "open a folder"},
	} {
		if _, err := api.Create(tt.root, tt.personal, tt.name); err == nil || !strings.Contains(err.Error(), tt.err) {
			t.Errorf("Create(%q, %v, %q) = %v, want %q", tt.root, tt.personal, tt.name, err, tt.err)
		}
	}
}
