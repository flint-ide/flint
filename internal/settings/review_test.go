package settings

import (
	"strings"
	"testing"
)

func TestAgentReviewDefaults(t *testing.T) {
	r := Default().AgentReview
	if r.Scope != ReviewSelected || r.MaxDiffKB != 128 || r.TimeoutSeconds != 300 || r.Agent != "" {
		t.Errorf("default = %+v", r)
	}
	if err := Validate(Default()); err != nil {
		t.Fatal(err)
	}
}

func TestAgentReviewValidate(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*AgentReview)
		want   string
	}{
		{"scope", func(r *AgentReview) { r.Scope = "staged" }, "scope"},
		{"small diff", func(r *AgentReview) { r.MaxDiffKB = 1 }, "maxDiffKB"},
		{"large diff", func(r *AgentReview) { r.MaxDiffKB = 4096 }, "maxDiffKB"},
		{"timeout", func(r *AgentReview) { r.TimeoutSeconds = 5 }, "timeoutSeconds"},
		{"agent", func(r *AgentReview) { r.Agent = "nope" }, "agent"},
	} {
		s := Default()
		tc.change(&s.AgentReview)
		if err := Validate(s); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
	s := Default()
	s.AgentReview = AgentReview{Scope: ReviewAll, MaxDiffKB: 512, TimeoutSeconds: 30, Agent: "codex"}
	if err := Validate(s); err != nil {
		t.Errorf("valid: %v", err)
	}
	s.CustomAgents = []CustomAgent{{ID: "gemini", Name: "Gemini", Command: "/usr/local/bin/gemini"}}
	s.AgentReview.Agent = "gemini"
	if err := Validate(s); err != nil {
		t.Errorf("custom agent: %v", err)
	}
}

func TestAgentReviewNormalize(t *testing.T) {
	s, err := decode([]byte(`{"agentReview": {"scope": "bogus", "maxDiffKB": 99999, "timeoutSeconds": 1, "agent": "gone"}}`))
	if err != nil {
		t.Fatal(err)
	}
	want := AgentReview{Scope: ReviewSelected, MaxDiffKB: MaxReviewDiffKB, TimeoutSeconds: MinReviewTimeout}
	if s.AgentReview != want {
		t.Errorf("normalized = %+v", s.AgentReview)
	}
	old, err := decode([]byte(`{"theme": "dark"}`))
	if err != nil || old.AgentReview != defaultAgentReview() {
		t.Errorf("old file: %+v %v", old.AgentReview, err)
	}
}
