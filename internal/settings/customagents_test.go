package settings

import (
	"slices"
	"strings"
	"testing"
)

func gemini() CustomAgent {
	return CustomAgent{ID: "gemini", Name: "Gemini CLI", Command: "gemini", Args: []string{"--experimental-acp"}}
}

func TestValidateCustomAgents(t *testing.T) {
	many := make([]CustomAgent, MaxCustomAgents+1)
	for i := range many {
		many[i] = CustomAgent{ID: "a" + strings.Repeat("x", i), Command: "x"}
	}
	tests := []struct {
		name   string
		modify func(*Settings)
		want   string // error substring; "" means valid
	}{
		{"none", func(*Settings) {}, ""},
		{"preset", func(s *Settings) { s.CustomAgents = []CustomAgent{gemini()} }, ""},
		{"absolute command, no name", func(s *Settings) {
			s.CustomAgents = []CustomAgent{{ID: "opencode", Command: "/Users/me/.opencode/bin/opencode", Args: []string{"acp"}}}
		}, ""},
		{"default is custom", func(s *Settings) { s.CustomAgents = []CustomAgent{gemini()}; s.DefaultAgent = "gemini" }, ""},
		{"default is unknown", func(s *Settings) { s.DefaultAgent = "gemini" }, "unsupported agent"},
		{"empty id", func(s *Settings) { s.CustomAgents = []CustomAgent{{Command: "x"}} }, "id is empty"},
		{"uppercase id", func(s *Settings) { s.CustomAgents = []CustomAgent{{ID: "Gemini", Command: "x"}} }, "lowercase"},
		{"id with space", func(s *Settings) { s.CustomAgents = []CustomAgent{{ID: "my agent", Command: "x"}} }, "lowercase"},
		{"id too long", func(s *Settings) { s.CustomAgents = []CustomAgent{{ID: strings.Repeat("a", 33), Command: "x"}} }, "at most"},
		{"id claude", func(s *Settings) { s.CustomAgents = []CustomAgent{{ID: "claude", Command: "x"}} }, "built-in"},
		{"id codex", func(s *Settings) { s.CustomAgents = []CustomAgent{{ID: "codex", Command: "x"}} }, "built-in"},
		{"duplicate id", func(s *Settings) { s.CustomAgents = []CustomAgent{gemini(), gemini()} }, "used twice"},
		{"too many", func(s *Settings) { s.CustomAgents = many }, "too many custom agents"},
		{"name newline", func(s *Settings) { s.CustomAgents = []CustomAgent{{ID: "a", Name: "x\ny", Command: "x"}} }, "invalid name"},
		{"name too long", func(s *Settings) {
			s.CustomAgents = []CustomAgent{{ID: "a", Name: strings.Repeat("n", 65), Command: "x"}}
		}, "name longer"},
		{"no command", func(s *Settings) { s.CustomAgents = []CustomAgent{{ID: "a"}} }, "command is empty"},
		{"relative command", func(s *Settings) { s.CustomAgents = []CustomAgent{{ID: "a", Command: "bin/agent"}} }, "must be absolute"},
		{"flag as command", func(s *Settings) { s.CustomAgents = []CustomAgent{{ID: "a", Command: "-x"}} }, "may not start with -"},
		{"arg newline", func(s *Settings) { s.CustomAgents = []CustomAgent{{ID: "a", Command: "x", Args: []string{"a\nb"}}} }, "newline"},
		{"arg control char", func(s *Settings) { s.CustomAgents = []CustomAgent{{ID: "a", Command: "x", Args: []string{"a\x1bb"}}} }, "control"},
		{"empty arg", func(s *Settings) { s.CustomAgents = []CustomAgent{{ID: "a", Command: "x", Args: []string{""}}} }, "empty argument"},
		{"too many args", func(s *Settings) {
			s.CustomAgents = []CustomAgent{{ID: "a", Command: "x", Args: slices.Repeat([]string{"-v"}, 33)}}
		}, "too many arguments"},
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
				t.Errorf("Validate() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestLoadCustomAgentsForgiving(t *testing.T) {
	data := `{
		"defaultAgent": "broken",
		"customAgents": [
			{"id": "gemini", "name": "Gemini CLI", "command": "gemini", "args": ["--experimental-acp"]},
			{"id": "gemini", "command": "other"},
			{"id": "broken", "command": "./run-me"},
			{"id": "claude", "command": "claude"},
			{"id": "opencode", "command": "opencode", "args": ["acp"], "env": {"PATH": "/tmp"}}
		]
	}`
	s, err := decode([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, c := range s.CustomAgents {
		ids = append(ids, c.ID)
	}
	if !slices.Equal(ids, []string{"gemini", "opencode"}) {
		t.Errorf("custom agents = %v, want gemini and opencode", ids)
	}
	if got, _ := s.CustomAgent("gemini"); !got.Equal(gemini()) {
		t.Errorf("gemini = %+v", got)
	}
	if s.DefaultAgent != "" {
		t.Errorf("default agent %q points at a dropped agent; want empty", s.DefaultAgent)
	}
	if err := Validate(s); err != nil {
		t.Errorf("normalized settings fail Validate: %v", err)
	}
}

func TestCustomAgentsCloned(t *testing.T) {
	s := Default()
	s.CustomAgents = []CustomAgent{gemini()}
	c := s.Clone()
	c.CustomAgents[0].Args[0] = "changed"
	if s.CustomAgents[0].Args[0] != "--experimental-acp" {
		t.Error("Clone shares the custom agents' arguments")
	}
	got, ok := s.CustomAgent("gemini")
	if !ok {
		t.Fatal("CustomAgent(gemini) not found")
	}
	got.Args[0] = "changed"
	if s.CustomAgents[0].Args[0] != "--experimental-acp" {
		t.Error("CustomAgent shares the arguments")
	}
	if _, ok := s.CustomAgent("nope"); ok {
		t.Error("CustomAgent(nope) found")
	}
}
