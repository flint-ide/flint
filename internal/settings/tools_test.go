package settings

import (
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
		{"refresh bounds", func(s *Settings) { s.Refresh = Refresh{KubernetesSeconds: 2, AWSSeconds: 3600} }, ""},
		{"kubernetes too fast", func(s *Settings) { s.Refresh.KubernetesSeconds = 1 }, "Kubernetes refresh"},
		{"aws too slow", func(s *Settings) { s.Refresh.AWSSeconds = 3601 }, "AWS refresh"},
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
	}`)
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
	if want := (Refresh{KubernetesSeconds: MinKubernetesSeconds, AWSSeconds: MaxAWSSeconds}); got.Refresh != want {
		t.Errorf("Refresh = %+v, want clamped %+v", got.Refresh, want)
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
	if got.Refresh != defaultRefresh() || len(got.Agents) != 0 || len(got.LanguageServers) != 0 {
		t.Errorf("tool settings = %+v %+v %+v, want defaults", got.Refresh, got.Agents, got.LanguageServers)
	}
}

func TestToolSettingsRoundtripAndViews(t *testing.T) {
	in := Default()
	in.Agents = map[string]AgentSettings{"claude": {Path: "/opt/claude", Model: "sonnet", Args: []string{"--add-dir", "/tmp"}}}
	in.LanguageServers = map[string]LanguageServerSettings{"python": {Command: "pylsp"}}
	in.Refresh = Refresh{KubernetesSeconds: 10, AWSSeconds: 60}
	path := filepath.Join(t.TempDir(), FileName)
	saved, err := Save(path, in)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	// Normalize fills missing argument lists in; compare what matters.
	if loaded.AgentConfig("claude").Model != "sonnet" || loaded.ServerConfig("python").Command != "pylsp" || loaded.Refresh != saved.Refresh {
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
	s.Agents["claude"] = AgentSettings{Args: []string{"a"}}
	c := s.Clone()
	c.Agents["claude"].Args[0] = "b"
	c.Agents["codex"] = AgentSettings{}
	if s.Agents["claude"].Args[0] != "a" || len(s.Agents) != 1 {
		t.Errorf("Clone shares state: %+v", s.Agents)
	}
}
