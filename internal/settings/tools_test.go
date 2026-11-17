package settings

import (
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestValidateToolSettings(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*Settings)
		want   string // error substring; "" means valid
	}{
		{"agent defaults", func(s *Settings) { s.Agents = map[string]AgentSettings{"claude": {}} }, ""},
		{"agent configured", func(s *Settings) {
			s.Agents = map[string]AgentSettings{
				"claude": {Path: "/opt/homebrew/bin/claude", Model: "opus", Args: []string{"--add-dir", "/tmp/notes"}},
				"codex":  {Path: "codex", Model: "gpt-5-codex", Args: []string{"-c", "model_reasoning_effort=high"}},
			}
		}, ""},
		{"unknown agent", func(s *Settings) { s.Agents = map[string]AgentSettings{"gpt": {}} }, "agents.gpt: unsupported agent"},
		{"relative agent path", func(s *Settings) { s.Agents = map[string]AgentSettings{"claude": {Path: "node_modules/.bin/claude"}} }, "agents.claude: path"},
		{"model flag", func(s *Settings) {
			s.Agents = map[string]AgentSettings{"claude": {Model: "--dangerously-skip-permissions"}}
		}, "invalid model"},
		{"tiers", func(s *Settings) {
			s.Agents = map[string]AgentSettings{
				"claude": {Tiers: map[string]string{"fast": "", "strong": "claude-opus-4-1"}},
				"codex":  {Tiers: map[string]string{"balanced": "gpt-5-codex"}},
			}
		}, ""},
		{"unknown tier", func(s *Settings) {
			s.Agents = map[string]AgentSettings{"codex": {Tiers: map[string]string{"huge": "gpt-5"}}}
		}, `agents.codex: unknown model tier "huge"`},
		{"tier flag", func(s *Settings) {
			s.Agents = map[string]AgentSettings{"claude": {Tiers: map[string]string{"strong": "--yolo"}}}
		}, "agents.claude: tier strong: invalid model"},
		{"arg newline", func(s *Settings) { s.Agents = map[string]AgentSettings{"claude": {Args: []string{"a\nb"}}} }, "newline"},
		{"arg overrides setting sources", func(s *Settings) {
			s.Agents = map[string]AgentSettings{"claude": {Args: []string{"--setting-sources", "project"}}}
		}, "--setting-sources is set by Flint"},
		{"codex sandbox override", func(s *Settings) {
			s.Agents = map[string]AgentSettings{"codex": {Args: []string{"--sandbox", "danger-full-access"}}}
		}, "--sandbox"},
		{"too many args", func(s *Settings) {
			s.Agents = map[string]AgentSettings{"claude": {Args: slices.Repeat([]string{"--verbose"}, 33)}}
		}, "too many"},
		{"language server", func(s *Settings) {
			s.LanguageServers = map[string]LanguageServerSettings{
				"python": {Command: "pyright-langserver"}, "go": {Args: []string{"-remote=auto"}}, "rust": {Disabled: true},
			}
		}, ""},
		{"unknown language", func(s *Settings) { s.LanguageServers = map[string]LanguageServerSettings{"cobol": {}} }, "languageServers.cobol: unknown"},
		{"relative server", func(s *Settings) { s.LanguageServers = map[string]LanguageServerSettings{"go": {Command: "./gopls"}} }, "languageServers.go"},
		{"tsserver path", func(s *Settings) {
			s.LanguageServers = map[string]LanguageServerSettings{"typescript": {Args: []string{"--tsserver-path", "/x"}}}
		}, "own TypeScript"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := Default()
			tt.modify(&s)
			err := Validate(s)
			if tt.want == "" {
				if err != nil {
					t.Errorf("Validate() error = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Validate() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestLoadToolSettingsForgiving(t *testing.T) {
	path := write(t, `{
		"agents": {
			"claude": {"path": "/opt/claude", "model": "x y", "args": ["--add-dir", "/a", "--setting-sources", "project"]},
			"codex": {"model": "o3", "args": "not a list"},
			"gemini": {"path": "/opt/gemini"}
		},
		"languageServers": {"go": {"command": "./gopls", "args": ["-remote=auto"]}, "rust": {"disabled": true}, "cobol": {}},
		"refresh": {"kubernetesSeconds": 1, "awsSeconds": 99999}
	}`) // "refresh" was removed with the built-in Kubernetes and AWS panels
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	wantAgents := map[string]AgentSettings{
		// A bad model falls back to the default; bad arguments are dropped
		// as a whole so no flag value is left behind.
		"claude": {Path: "/opt/claude", Args: []string{}},
		"codex":  {Model: "o3", Args: []string{}},
	}
	if !reflect.DeepEqual(got.Agents, wantAgents) {
		t.Errorf("Agents = %+v, want %+v", got.Agents, wantAgents)
	}
	wantServers := map[string]LanguageServerSettings{
		"go":   {Args: []string{"-remote=auto"}},
		"rust": {Disabled: true, Args: []string{}},
	}
	if !reflect.DeepEqual(got.LanguageServers, wantServers) {
		t.Errorf("LanguageServers = %+v, want %+v", got.LanguageServers, wantServers)
	}
	if err := Validate(got); err != nil {
		t.Errorf("normalized settings fail Validate: %v", err)
	}
}

func TestLoadOlderFileGetsToolDefaults(t *testing.T) {
	got, err := Load(write(t, `{"version": 1, "theme": "light"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Agents) != 0 || len(got.LanguageServers) != 0 {
		t.Errorf("tool settings = %+v %+v, want defaults", got.Agents, got.LanguageServers)
	}
}

func TestToolSettingsRoundtripAndViews(t *testing.T) {
	in := Default()
	in.Agents = map[string]AgentSettings{"claude": {Path: "/opt/claude", Model: "sonnet", Args: []string{"--add-dir", "/tmp"}}}
	in.LanguageServers = map[string]LanguageServerSettings{"python": {Command: "pylsp"}}
	path := filepath.Join(t.TempDir(), FileName)
	if _, err := Save(path, in); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	// Normalize fills missing argument lists in; compare what matters.
	if loaded.AgentConfig("claude").Model != "sonnet" || loaded.ServerConfig("python").Command != "pylsp" {
		t.Errorf("Load() = %+v", loaded)
	}
	cfg := loaded.AgentConfig("claude")
	cfg.Args[0] = "changed"
	if loaded.Agents["claude"].Args[0] != "--add-dir" {
		t.Error("AgentConfig shares its argument slice with the settings")
	}
	if got := loaded.AgentConfigs()["claude"].Path; got != "/opt/claude" {
		t.Errorf("AgentConfigs()[claude].Path = %q", got)
	}
	if loaded.AgentConfig("codex").Path != "" {
		t.Error("unconfigured agent should be the zero Config")
	}
}

// Tiers: an unknown tier or a bad model is dropped (back to the default), an
// explicit "" survives a save and a load (it clears a default), and the
// agent's Config carries the tiers with the defaults filled in.
func TestToolSettingsTiers(t *testing.T) {
	got, err := Load(write(t, `{"agents": {
		"claude": {"tiers": {"fast": "", "balanced": "x y", "strong": "claude-opus-4-1", "huge": "opus"}},
		"codex": {"tiers": {"strong": "gpt-5-codex", "fast": 3}},
		"gemini": {"tiers": {"strong": "x"}}
	}, "theme": "light"}`))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]map[string]string{
		"claude": {"fast": "", "strong": "claude-opus-4-1"},
		"codex":  {"strong": "gpt-5-codex"},
	}
	for name, tiers := range want {
		if !maps.Equal(got.Agents[name].Tiers, tiers) {
			t.Errorf("%s tiers = %#v, want %#v", name, got.Agents[name].Tiers, tiers)
		}
	}
	if err := Validate(got); err != nil {
		t.Fatalf("normalized settings fail Validate: %v", err)
	}
	// Tiers of the wrong type lose only themselves (the rest of the agent
	// and of the file loads).
	for _, tiers := range []string{`"strong"`, `[1]`, `3`, `{"fast": 3}`} {
		bad, err := Load(write(t, `{"agents": {"claude": {"model": "opus", "tiers": `+tiers+`}}, "theme": "light", "watchExclude": ["zzz"]}`))
		if err != nil || bad.Theme != "light" || bad.Agents["claude"].Model != "opus" || len(bad.Agents["claude"].Tiers) != 0 ||
			!slices.Equal(bad.WatchExclude, []string{"zzz"}) {
			t.Errorf("tiers %s: %+v, %v", tiers, bad.Agents, err)
		}
	}
	path := filepath.Join(t.TempDir(), FileName)
	if _, err := Save(path, got); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name string
		want map[string]string
	}{
		{"claude", map[string]string{"balanced": "sonnet", "strong": "claude-opus-4-1"}}, // fast cleared
		{"codex", map[string]string{"strong": "gpt-5-codex"}},
	} {
		if cfg := loaded.AgentConfig(tt.name); !reflect.DeepEqual(cfg.Tiers, tt.want) {
			t.Errorf("AgentConfig(%s).Tiers = %v, want %v", tt.name, cfg.Tiers, tt.want)
		}
	}
	// Nothing configured: Claude Code's defaults, none for Codex.
	def := Default()
	if got := def.AgentConfig("claude").Tiers; !reflect.DeepEqual(got, map[string]string{"fast": "haiku", "balanced": "sonnet", "strong": "opus"}) {
		t.Errorf("default claude tiers = %v", got)
	}
	if got := def.AgentConfig("codex").Tiers; len(got) != 0 {
		t.Errorf("default codex tiers = %v", got)
	}
	// An agent without tiers is saved without the key.
	if _, err := Save(path, def); err != nil {
		t.Fatal(err)
	}
	def.Agents["codex"] = AgentSettings{Model: "o3"}
	if _, err := Save(path, def); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); strings.Contains(string(data), `"tiers"`) {
		t.Errorf("empty tiers saved:\n%s", data)
	}
}

func TestChangedLanguageServers(t *testing.T) {
	prev := Default()
	next := prev.Clone()
	next.LanguageServers["go"] = LanguageServerSettings{Args: []string{}}
	if got := ChangedLanguageServers(prev, next); len(got) != 0 {
		t.Errorf("empty settings vs defaults changed %v", got)
	}
	next.LanguageServers["python"] = LanguageServerSettings{Disabled: true}
	next.LanguageServers["rust"] = LanguageServerSettings{Args: []string{"x"}}
	if got := ChangedLanguageServers(prev, next); !reflect.DeepEqual(got, []string{"python", "rust"}) {
		t.Errorf("ChangedLanguageServers() = %v", got)
	}
}

func TestCloneIsDeep(t *testing.T) {
	s := Default()
	s.Agents["claude"] = AgentSettings{Args: []string{"a"}, Tiers: map[string]string{"fast": "haiku"}}
	c := s.Clone()
	c.Agents["claude"].Args[0] = "b"
	c.Agents["claude"].Tiers["fast"] = "opus"
	c.Agents["codex"] = AgentSettings{}
	if s.Agents["claude"].Args[0] != "a" || s.Agents["claude"].Tiers["fast"] != "haiku" || len(s.Agents) != 1 {
		t.Errorf("Clone shares state: %+v", s.Agents)
	}
}

// Keys of the built-ins that moved to extensions (Docker, Kubernetes, AWS
// Profiles) still load, and saving drops them.
func TestLoadRemovedKeys(t *testing.T) {
	path := write(t, `{"theme": "light", "refresh": {"kubernetesSeconds": 5, "awsSeconds": 30},
		"mcpServer": {"tools": {"kubernetes": true, "terminals": true}}, "docker": {"host": "unix:///x"}}`)
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Theme != "light" || !got.MCPServer.Tools.Terminals {
		t.Fatalf("Load() = %+v", got)
	}
	if _, err := Save(path, got); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"refresh"`, `"kubernetes"`, `"docker"`} {
		if strings.Contains(string(data), key) {
			t.Errorf("saved file still has %s:\n%s", key, data)
		}
	}
}
