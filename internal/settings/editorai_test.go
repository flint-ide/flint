package settings

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestEditorAIDefaults(t *testing.T) {
	d := Default()
	if d.Completions.Enabled {
		t.Error("completions must be off by default")
	}
	if d.Completions.Provider != "ollama" || d.Completions.Endpoint != "http://localhost:11434" || d.Completions.Model == "" {
		t.Errorf("Completions = %+v", d.Completions)
	}
	if d.InlineEdit.ContextLines == 0 {
		t.Errorf("InlineEdit = %+v", d.InlineEdit)
	}
	if err := Validate(d); err != nil {
		t.Fatal(err)
	}
}

func TestValidateEditorAI(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*Settings)
		want   string
	}{
		{"openai over https", func(s *Settings) {
			s.Completions = Completions{Enabled: true, Provider: "openai", Endpoint: "https://api.example.com/v1", Model: "m", DebounceMs: 50, MaxTokens: 512, ContextLines: 1}
		}, ""},
		{"unknown provider", func(s *Settings) { s.Completions.Provider = "gemini" }, "completions: unsupported provider"},
		{"http remote", func(s *Settings) { s.Completions.Endpoint = "http://example.com" }, "completions: endpoint"},
		{"bad model", func(s *Settings) { s.Completions.Model = "a b" }, "completions: invalid model"},
		{"debounce", func(s *Settings) { s.Completions.DebounceMs = 10 }, "debounce"},
		{"max tokens", func(s *Settings) { s.Completions.MaxTokens = 0 }, "maxTokens"},
		{"context lines", func(s *Settings) { s.Completions.ContextLines = 5000 }, "contextLines"},
		{"inline edit context", func(s *Settings) { s.InlineEdit.ContextLines = -1 }, "inlineEdit.contextLines"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := Default()
			tt.modify(&s)
			err := Validate(s)
			if tt.want == "" {
				if err != nil {
					t.Errorf("Validate() = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Validate() = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestLoadEditorAIForgiving(t *testing.T) {
	got, err := Load(write(t, `{
		"completions": {"enabled": true, "provider": "gemini", "endpoint": "http://example.com", "model": "", "debounceMs": 1, "maxTokens": 99999, "contextLines": 0},
		"inlineEdit": {"contextLines": 100000}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	def := defaultCompletions()
	want := Completions{
		Enabled: true, Provider: def.Provider, Endpoint: def.Endpoint, Model: def.Model,
		DebounceMs: MinCompletionDebounceMs, MaxTokens: MaxCompletionTokens, ContextLines: def.ContextLines,
	}
	if got.Completions != want {
		t.Errorf("Completions = %+v, want %+v", got.Completions, want)
	}
	if got.InlineEdit.ContextLines != MaxInlineEditContextLines {
		t.Errorf("InlineEdit = %+v", got.InlineEdit)
	}
	if err := Validate(got); err != nil {
		t.Errorf("normalized settings fail Validate: %v", err)
	}
	// An older file gets the defaults.
	old, err := Load(write(t, `{"version": 1}`))
	if err != nil || old.Completions != defaultCompletions() || old.InlineEdit != defaultInlineEdit() {
		t.Errorf("old file: %+v %+v %v", old.Completions, old.InlineEdit, err)
	}
}

func TestEditorAIRoundtrip(t *testing.T) {
	in := Default()
	in.Completions = Completions{Enabled: true, Provider: "openai", Endpoint: "http://127.0.0.1:8080", Model: "starcoder2", DebounceMs: 500, MaxTokens: 100, ContextLines: 40}
	in.InlineEdit.ContextLines = 0
	path := filepath.Join(t.TempDir(), FileName)
	if _, err := Save(path, in); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Completions != in.Completions || got.InlineEdit != in.InlineEdit {
		t.Errorf("Load() = %+v %+v", got.Completions, got.InlineEdit)
	}
}
