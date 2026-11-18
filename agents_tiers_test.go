package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"flint/internal/agent"
	"flint/internal/agentdefs"
	as "flint/internal/agentsession"
	"flint/internal/settings"
)

// definedModel: a tier resolves on whichever engine runs the agent (settings
// over the defaults), a model name only on the engine it is pinned to.
func TestDefinedModel(t *testing.T) {
	claudeDefaults := settings.Default().AgentConfig("claude")
	codexPlain := agent.Config{Model: "gpt-5"}
	codexMapped := agent.Config{Model: "gpt-5", Tiers: map[string]string{"strong": "gpt-5-codex"}}
	tests := []struct {
		name     string
		def      agentdefs.Definition
		k        agent.Kind
		cfg      agent.Config
		model    string // definedModel
		cfgModel string // definedConfig(...).Model
	}{
		{name: "tier on claude", def: agentdefs.Definition{Model: "strong"}, k: agent.Claude, cfg: claudeDefaults, model: "opus", cfgModel: "opus"},
		{name: "fast on claude", def: agentdefs.Definition{Model: "fast"}, k: agent.Claude, cfg: claudeDefaults, model: "haiku", cfgModel: "haiku"},
		{name: "tier unmapped on codex", def: agentdefs.Definition{Model: "strong"}, k: agent.Codex, cfg: codexPlain, model: "", cfgModel: "gpt-5"},
		{name: "tier mapped on codex", def: agentdefs.Definition{Model: "strong"}, k: agent.Codex, cfg: codexMapped, model: "gpt-5-codex", cfgModel: "gpt-5-codex"},
		{name: "pinned tier", def: agentdefs.Definition{Engine: "codex", Model: "strong"}, k: agent.Codex, cfg: codexMapped, model: "gpt-5-codex", cfgModel: "gpt-5-codex"},
		{name: "model name on its engine", def: agentdefs.Definition{Engine: "claude", Model: "claude-opus-4-1"}, k: agent.Claude, cfg: claudeDefaults, model: "claude-opus-4-1", cfgModel: "claude-opus-4-1"},
		{name: "model name elsewhere", def: agentdefs.Definition{Engine: "claude", Model: "opus"}, k: agent.Codex, cfg: codexPlain, model: "", cfgModel: "gpt-5"},
		{name: "no model", def: agentdefs.Definition{}, k: agent.Claude, cfg: agent.Config{Model: "sonnet", Tiers: claudeDefaults.Tiers}, model: "", cfgModel: "sonnet"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := definedModel(tt.def, tt.k, tt.cfg); got != tt.model {
				t.Errorf("definedModel = %q, want %q", got, tt.model)
			}
			if got := definedConfig(tt.def, tt.k, tt.cfg).Model; got != tt.cfgModel {
				t.Errorf("definedConfig().Model = %q, want %q", got, tt.cfgModel)
			}
		})
	}
}

// tierApp is an App with claude and codex installed (both the test binary)
// and a definition per way of naming a model.
func tierApp(t *testing.T) *App {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	a := &App{configDir: t.TempDir(), settings: settings.Default()}
	a.settings.Agents = map[string]settings.AgentSettings{
		"claude": {Path: exe, Model: "sonnet"},
		"codex":  {Path: exe, Model: "gpt-5"},
	}
	personal := agentdefs.PersonalDir(a.configDir)
	writeFile(t, filepath.Join(personal, "strong.md"), "---\nmodel: strong\n---\nReview.")
	writeFile(t, filepath.Join(personal, "fast-codex.md"), "---\nengine: codex\nmodel: fast\n---\nTriage.")
	writeFile(t, filepath.Join(personal, "named.md"), "---\nengine: claude\nmodel: claude-opus-4-1\n---\nPlan.")
	return a
}

// Headless runs (review, commit message, inline edit) take the program behind
// a name from agentView.native: a tier resolves there, on the resolved engine.
func TestTierResolvesForHeadlessRuns(t *testing.T) {
	tests := []struct {
		name, defaultAgent string
		codexTiers         settings.ModelTiers
		claudeTiers        settings.ModelTiers
		engine, model      string
	}{
		{name: "strong", defaultAgent: "claude", engine: "claude", model: "opus"},
		{name: "strong", defaultAgent: "claude", claudeTiers: settings.ModelTiers{"strong": "claude-opus-4-1"}, engine: "claude", model: "claude-opus-4-1"},
		{name: "strong", defaultAgent: "claude", claudeTiers: settings.ModelTiers{"strong": ""}, engine: "claude", model: "sonnet"}, // default cleared
		{name: "strong", defaultAgent: "codex", engine: "codex", model: "gpt-5"},                                                    // unmapped: the engine's model
		{name: "strong", defaultAgent: "codex", codexTiers: settings.ModelTiers{"strong": "gpt-5-codex"}, engine: "codex", model: "gpt-5-codex"},
		{name: "fast-codex", defaultAgent: "claude", codexTiers: settings.ModelTiers{"fast": "gpt-5-mini"}, engine: "codex", model: "gpt-5-mini"},
		{name: "named", defaultAgent: "codex", engine: "claude", model: "claude-opus-4-1"},
	}
	for _, tt := range tests {
		a := tierApp(t)
		a.settings.DefaultAgent = tt.defaultAgent
		claude, codex := a.settings.Agents["claude"], a.settings.Agents["codex"]
		claude.Tiers, codex.Tiers = tt.claudeTiers, tt.codexTiers
		a.settings.Agents["claude"], a.settings.Agents["codex"] = claude, codex
		run, ok, err := a.agentView().native(tt.name)
		if err != nil || !ok || string(run.kind) != tt.engine || run.cfg.Model != tt.model {
			t.Errorf("%s (default %s, claude %v, codex %v) = %s %q %v %v; want %s %q",
				tt.name, tt.defaultAgent, tt.claudeTiers, tt.codexTiers, run.kind, run.cfg.Model, ok, err, tt.engine, tt.model)
		}
	}
}

// A review with a defined agent whose model is a tier passes the tier's
// model on the command line.
func TestReviewChangesResolvesATier(t *testing.T) {
	claude, out := fakeClaude(t, `echo '{"findings": []}'`)
	api, root := reviewRepo(t, claude, true)
	writeFile(t, filepath.Join(agentdefs.PersonalDir(api.app.configDir), "reviewer.md"), "---\nmodel: strong\n---\nFind bugs.")
	api.app.settings.AgentReview.Agent = "reviewer"
	if _, err := api.ReviewChanges(context.Background(), ReviewRequest{Root: root, Files: reviewFiles[:1]}); err != nil {
		t.Fatal(err)
	}
	if args, _ := os.ReadFile(filepath.Join(out, "args")); !strings.Contains(string(args), "\n--model\nopus\n") {
		t.Errorf("claude args\n%s", args)
	}
}

// recordingClaude is a fake claude that writes its arguments to a new file
// in dir on every start, answers the initialize request and then waits for
// its input to close.
func recordingClaude(t *testing.T) (path, dir string) {
	t.Helper()
	dir = t.TempDir()
	path = filepath.Join(dir, "claude")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"" + dir + "/args.$$\"\n" +
		"read -r line\n" +
		"id=$(printf '%s' \"$line\" | sed 's/.*\"request_id\":\"\\([^\"]*\\)\".*/\\1/')\n" +
		"printf '{\"type\":\"control_response\",\"response\":{\"subtype\":\"success\",\"request_id\":\"%s\",\"response\":{}}}\\n' \"$id\"\n" +
		"cat > /dev/null\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path, dir
}

// takeArgs returns the arguments of the one start of recordingClaude since
// the last call (its file is written before the handshake Start waits for).
func takeArgs(t *testing.T, dir string) string {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(dir, "args.*"))
	if len(files) != 1 {
		t.Fatalf("claude starts: %v", files)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(files[0]); err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// A chat resolves its tier when it starts, and a resumed chat when it
// resumes: from the settings of that moment, on the engine it runs on.
func TestTierResolvesWhenAChatStartsAndResumes(t *testing.T) {
	a := tierApp(t)
	claude, dir := recordingClaude(t)
	a.settings.Agents = map[string]settings.AgentSettings{"claude": {Path: claude, Model: "sonnet"}}
	a.agentMgr = as.NewManager(func(as.Event) {})
	t.Cleanup(func() { _ = a.agentMgr.Close() })
	loaded := a.loadDefinedAgents()
	drv, k, err := loaded.route("strong", "")
	if err != nil || k != "claude" {
		t.Fatalf("route = %q, %v", k, err)
	}
	info, err := drv.Start(context.Background(), t.TempDir(), func(as.Event) {})
	if err != nil {
		t.Fatal(err)
	}
	if args := takeArgs(t, dir); !strings.Contains(args, "\n--model\nopus\n") {
		t.Errorf("start: claude args\n%s", args)
	}
	_ = drv.Stop(info.ID)

	// The mapping changed after the chat started: the resume uses it.
	a.settingsMu.Lock()
	a.settings.Agents["claude"] = settings.AgentSettings{Path: claude, Model: "sonnet", Tiers: settings.ModelTiers{"strong": "claude-opus-4-1"}}
	a.settingsMu.Unlock()
	drv, _, err = loaded.route("strong", "claude")
	if err != nil {
		t.Fatal(err)
	}
	info, err = drv.(as.Resumer).Resume(context.Background(), t.TempDir(), info.ID, func(as.Event) {})
	if err != nil {
		t.Fatal(err)
	}
	if args := takeArgs(t, dir); !strings.Contains(args, "\n--model\nclaude-opus-4-1\n") || !strings.Contains(args, "--resume") {
		t.Errorf("resume: claude args\n%s", args)
	}
	_ = drv.Stop(info.ID)
}

// The list shows the model as the file names it and what it means now.
func TestAgentDefsAPIResolvedModel(t *testing.T) {
	a := tierApp(t)
	a.settings.DefaultAgent = "codex"
	api := &AgentDefsAPI{app: a}
	got := map[string]string{}
	for _, d := range api.List().Agents {
		got[d.Name] = d.ResolvedEngine + ":" + d.Model + ":" + d.ResolvedModel
	}
	want := map[string]string{
		"strong":     "codex:strong:", // unmapped on codex
		"fast-codex": "codex:fast:",
		"named":      "claude:claude-opus-4-1:claude-opus-4-1",
	}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("%s = %q, want %q", name, got[name], w)
		}
	}
	a.settings.DefaultAgent = "claude"
	codex := a.settings.Agents["codex"]
	codex.Tiers = settings.ModelTiers{"fast": "gpt-5-mini"}
	a.settings.Agents["codex"] = codex
	for _, d := range api.List().Agents {
		got[d.Name] = d.ResolvedEngine + ":" + d.Model + ":" + d.ResolvedModel
	}
	if got["strong"] != "claude:strong:opus" || got["fast-codex"] != "codex:fast:gpt-5-mini" {
		t.Errorf("after the change: %v", got)
	}
	// No engine installed: nothing resolves.
	a.settings.Agents = map[string]settings.AgentSettings{"claude": {Path: "/nonexistent/claude"}, "codex": {Path: "/nonexistent/codex"}}
	for _, d := range api.List().Agents {
		if d.ResolvedEngine != "" || d.ResolvedModel != "" {
			t.Errorf("%s with nothing installed: %+v", d.Name, d)
		}
	}
}
