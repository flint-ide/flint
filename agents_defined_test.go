package main

import (
	"context"
	"errors"
	"maps"
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
	"flint/internal/orchestrate"
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
	reviewer := agentdefs.Definition{Name: "reviewer", Engine: "claude", Instructions: "Review."}
	planner := agentdefs.Definition{Name: "planner", Instructions: "Plan."}
	built := []string{}
	newDriver := func(d agentdefs.Definition) *definedAgent {
		built = append(built, d.Name)
		return &definedAgent{def: d}
	}
	first := syncDefinedAgents(nil, []agentdefs.Definition{reviewer, planner}, newDriver)
	if len(first) != 2 || !slices.Equal(built, []string{"reviewer", "planner"}) {
		t.Fatalf("built %v", built)
	}
	// Only a definition that runs differently gets a new driver.
	changed := reviewer
	changed.Model = "opus"
	described := planner
	described.Description, described.Path = "new words", "/elsewhere/planner.md"
	built = nil
	second := syncDefinedAgents(first, []agentdefs.Definition{changed, described}, newDriver)
	if !slices.Equal(built, []string{"reviewer"}) || second[1] != first[1] || second[0] == first[0] {
		t.Fatalf("rebuilt %v, want only reviewer", built)
	}
	// Removed: gone from the next read.
	if got := syncDefinedAgents(second, nil, newDriver); len(got) != 0 {
		t.Fatalf("after removal: %v", got)
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
		drv, k, err := d.route(tt.engine)
		var got string
		switch drv.(type) {
		case *claude.Driver:
			got = "claude"
		case *codex.Driver:
			got = "codex"
		}
		if got != tt.want || string(k) != tt.want || (tt.want == "") != (err != nil) {
			t.Errorf("route(%q) = %T, %q, %v; want %s", tt.engine, drv, k, err, tt.want)
		}
	}
	failure = agentdefs.ErrNoEngine
	if _, _, err := d.route(""); !errors.Is(err, agentdefs.ErrNoEngine) {
		t.Errorf("route with nothing installed = %v", err)
	}
}

// fakeEngines replaces the engine drivers of defined agent name in loaded by
// fakes that report their engine's name.
func fakeEngines(t *testing.T, loaded loadedAgents, name string) (claudeFake, codexFake *nameDriver) {
	t.Helper()
	d := loaded.drivers[name]
	if d == nil {
		t.Fatalf("%s was not loaded", name)
	}
	claudeFake, codexFake = &nameDriver{name: "claude"}, &nameDriver{name: "codex"}
	d.engines = map[agent.Kind]as.Driver{agent.Claude: claudeFake, agent.Codex: codexFake}
	return claudeFake, codexFake
}

// A conversation resumes on the engine it started on, whatever the default
// engine is by then; a new one starts on the default.
func TestDefinedAgentResumesOnItsEngine(t *testing.T) {
	a, _, _ := definedApp(t)
	a.agents = a.agentMgr
	loaded := a.loadDefinedAgents()
	_, codexFake := fakeEngines(t, loaded, "planner")
	drv, k, err := loaded.route("planner", "")
	if err != nil || k != "claude" {
		t.Fatalf("route = %q, %v", k, err)
	}
	info, err := a.startChat(context.Background(), drv, "planner", t.TempDir())
	if err != nil || info.Agent != "planner" || info.Engine != "claude" {
		t.Fatalf("Start = %+v, %v", info, err)
	}
	info, err = a.resumeChatWith(context.Background(), loaded, "planner", "/w", "thr-1", "codex")
	if err != nil || info.Agent != "planner" || info.Engine != "codex" {
		t.Fatalf("Resume = %+v, %v", info, err)
	}
	if got := codexFake.resumed; !slices.Equal(got, []string{"thr-1"}) {
		t.Fatalf("codex resumed %v", got)
	}
	// Unknown engine (a chat saved before engines were recorded): the default.
	if info, err = a.resumeChatWith(context.Background(), loaded, "planner", "/w", "old", ""); err != nil || info.Engine != "claude" {
		t.Fatalf("Resume without engine = %+v, %v", info, err)
	}
	if _, err = a.resumeChatWith(context.Background(), loaded, "planner", "/w", "x", "gemini"); err == nil {
		t.Fatal("resumed on an unknown engine")
	}
}

// A chat starts on exactly the definition its own call read, whatever another
// call read in between: openChatWith gets the read, and nothing looks the name
// up again. The interleaving is spelled out (no goroutines): read, a second
// read after the file changed (and after it is gone), then the start.
func TestOpenChatStartsWhatItsCallRead(t *testing.T) {
	c := newChatApp(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	c.settings.Agents = map[string]settings.AgentSettings{"claude": {Path: exe}, "codex": {Path: "/nonexistent/codex"}}
	file := filepath.Join(agentdefs.PersonalDir(c.configDir), "planner.md")
	writeFile(t, file, "Plan v1.")

	first := c.loadDefinedAgents()
	fakeEngines(t, first, "planner")
	writeFile(t, file, "Plan v2.")
	second := c.loadDefinedAgents()
	d := second.drivers["planner"]
	if d == nil || d == first.drivers["planner"] || d.def.Instructions != "Plan v2." {
		t.Fatalf("the second read did not replace the definition: %+v", d)
	}
	d.engines = map[agent.Kind]as.Driver{agent.Claude: &nameDriver{name: "v2"}, agent.Codex: &nameDriver{name: "v2"}}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if third := c.loadDefinedAgents(); third.drivers["planner"] != nil {
		t.Fatal("a removed definition is still loaded")
	}

	// Started on the first read's claude driver (the fake that names its
	// sessions "claude-new"), not the second's ("v2-new").
	info, err := c.openChatWith(context.Background(), ChatRequest{Agent: "planner", Cwd: t.TempDir()}, first)
	if err != nil || info.ID != "claude-new" || info.Agent != "planner" || info.Engine != "claude" {
		t.Fatalf("openChatWith = %+v, %v", info, err)
	}
	// The current read has no planner: by name, it is unknown.
	if _, err := c.openChat(context.Background(), ChatRequest{Agent: "planner", Cwd: t.TempDir()}); err == nil || !strings.Contains(err.Error(), "planner") {
		t.Fatalf("a removed definition started: %v", err)
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
	a.state.TrustedFolders = []string{cleanFolder(trusted)}
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
		run, ok, err := a.agentView().native(tt.name)
		k, cfg := run.kind, run.cfg
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
	// Kept for chats, backend too (it fails to start with a clear message);
	// never registered with the manager by name.
	got := slices.Sorted(maps.Keys(a.loadDefinedAgents().drivers))
	if !slices.Equal(got, []string{"backend", "opus-reviewer", "planner", "proj"}) || len(a.agentMgr.Agents()) != 0 {
		t.Fatalf("drivers %v, registered %v", got, a.agentMgr.Agents())
	}
	// A removed file is gone for the next conversation.
	if err := os.Remove(filepath.Join(agentdefs.PersonalDir(a.configDir), "planner.md")); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.loadDefinedAgents().drivers["planner"]; ok {
		t.Fatal("a removed definition is still loaded")
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

// A review agent that is a defined agent no longer there falls back to the
// default agent; a native or custom agent that can't start is still an error.
func TestReviewAgentFallsBack(t *testing.T) {
	a, _, _ := definedApp(t)
	api := &AgentAPI{app: a}
	tests := []struct{ set, want, err string }{
		{set: "", want: "claude"},
		{set: "opus-reviewer", want: "opus-reviewer"},
		{set: "removed-agent", want: "claude"},
		{set: "backend", want: "claude"}, // pinned to codex, which is not installed
		{set: "codex", err: "not installed"},
	}
	for _, tt := range tests {
		a.settings.AgentReview.Agent = tt.set
		got, err := api.reviewAgent(a.agentView())
		if tt.err != "" {
			if err == nil || !strings.Contains(err.Error(), tt.err) {
				t.Errorf("%q: err = %v, want %q", tt.set, err, tt.err)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Errorf("%q: got %q, %v; want %q", tt.set, got, err, tt.want)
		}
	}
}

// New Agent… may shadow a project definition with a personal one, not the
// other way round.
func TestCreatePersonalOverProject(t *testing.T) {
	a, trusted, _ := definedApp(t)
	api := &AgentDefsAPI{app: a}
	if _, err := api.Create("", true, "proj"); err != nil {
		t.Errorf("personal over project: %v", err)
	}
	if _, err := api.Create(trusted, false, "planner"); err == nil {
		t.Error("a project definition under a personal one's name was created")
	}
}

// A role for an engine fits a defined agent on that engine; a role naming a
// defined (or custom) agent fits that agent only.
func TestRolesAndDefinedAgents(t *testing.T) {
	a, _, _ := definedApp(t) // claude installed, codex not: planner runs on claude
	roles := orchestrate.PersonalRolesDir(a.configDir)
	writeRoleFile(t, roles, "rv.md", "---\nagent: claude\n---\nReview for Claude.")
	writeRoleFile(t, roles, "cx.md", "---\nagent: codex\n---\nCodex things.")
	writeRoleFile(t, roles, "pl.md", "---\nagent: planner\n---\nPlan things.")
	loaded := a.loadDefinedAgents()
	cwd := t.TempDir()
	for _, tt := range []struct {
		agent, role string
		want        string // the agent that starts; "" when refused
		engine      bool   // started on a defined agent's driver
	}{
		{agent: "planner", role: "rv", want: "planner", engine: true},
		{agent: "opus-reviewer", role: "rv", want: "opus-reviewer", engine: true},
		{agent: "claude", role: "rv", want: "claude"},
		{agent: "planner", role: "cx"},
		{agent: "planner", role: "pl", want: "planner", engine: true},
		{role: "pl", want: "planner", engine: true},
		{agent: "opus-reviewer", role: "pl"},
		{agent: "claude", role: "pl"},
		{agent: "gemini", role: "rv"},
	} {
		r, drv, err := a.applyRole(ChatRequest{Agent: tt.agent, Cwd: cwd, Role: tt.role, Prompt: "go"}, loaded)
		if tt.want == "" {
			if err == nil {
				t.Errorf("%s with role %s: allowed", tt.agent, tt.role)
			}
			continue
		}
		if err != nil || r.Agent != tt.want || (drv != nil) != tt.engine || !strings.HasSuffix(r.Prompt, "\n\ngo") {
			t.Errorf("%s with role %s = %+v, %T, %v", tt.agent, tt.role, r, drv, err)
		}
	}
	_, _, err := a.applyRole(ChatRequest{Agent: "planner", Cwd: cwd, Role: "cx"}, loaded)
	if err == nil || err.Error() != "role cx is for codex, not planner (claude)" {
		t.Errorf("refusal = %v", err)
	}
	if r, _, err := a.applyRole(ChatRequest{Agent: "planner", Cwd: cwd, Prompt: "go"}, loaded); err != nil || r.Prompt != "go" {
		t.Errorf("no role = %+v, %v", r, err)
	}
}

func TestRoleFits(t *testing.T) {
	for _, tt := range []struct {
		role, name, engine string
		want               bool
	}{
		{"", "anything", "anything", true},
		{"claude", "claude", "claude", true},
		{"claude", "reviewer", "claude", true},
		{"claude", "reviewer", "codex", false},
		{"reviewer", "reviewer", "claude", true},
		{"reviewer", "planner", "claude", false},
		{"reviewer", "claude", "claude", false},
		{"gemini", "gemini", "gemini", true},
		// A role for a custom agent is not an engine: a defined agent never
		// runs on one.
		{"gemini", "planner", "gemini", false},
	} {
		if got := roleFits(tt.role, tt.name, tt.engine); got != tt.want {
			t.Errorf("roleFits(%q, %q, %q) = %v", tt.role, tt.name, tt.engine, got)
		}
	}
}
