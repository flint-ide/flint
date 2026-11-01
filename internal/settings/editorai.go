package settings

import (
	"fmt"

	"flint/internal/completions"
)

// Completions configures inline suggestions from a code model ("completions").
// The API key, when the endpoint needs one, is in the Keychain, never here.
type Completions struct {
	// Enabled turns suggestions on (off by default: they send code to Endpoint).
	Enabled bool `json:"enabled"`
	// Provider is "ollama" (/api/generate) or "openai" (an OpenAI-compatible
	// /v1/completions).
	Provider string `json:"provider"`
	// Endpoint is the server's base URL; http only for localhost.
	Endpoint string `json:"endpoint"`
	// Model is a fill-in-the-middle capable code model.
	Model string `json:"model"`
	// DebounceMs is the typing pause before a suggestion is asked for.
	DebounceMs int `json:"debounceMs"`
	// MaxTokens bounds the length of a suggestion.
	MaxTokens int `json:"maxTokens"`
	// ContextLines is how many lines before and after the cursor are sent.
	ContextLines int `json:"contextLines"`
}

// InlineEdit configures "Edit with Agent" on the selection.
type InlineEdit struct {
	// ContextLines is how many lines before and after the selection the agent sees.
	ContextLines int `json:"contextLines"`
}

// Bounds of the completion and inline edit settings.
const (
	MinCompletionDebounceMs   = 50
	MaxCompletionDebounceMs   = 5000
	MinCompletionTokens       = 1
	MaxCompletionTokens       = 512
	MinCompletionContextLines = 1
	MaxCompletionContextLines = 1000
	MaxInlineEditContextLines = 500
)

func defaultCompletions() Completions {
	return Completions{
		Provider:     completions.Ollama,
		Endpoint:     "http://localhost:11434",
		Model:        "qwen2.5-coder:1.5b-base",
		DebounceMs:   300,
		MaxTokens:    64,
		ContextLines: 100,
	}
}

func defaultInlineEdit() InlineEdit { return InlineEdit{ContextLines: 60} }

// CompletionConfig is the completions package's view of the settings.
func (s Settings) CompletionConfig() completions.Config {
	c := s.Completions
	return completions.Config{Provider: c.Provider, Endpoint: c.Endpoint, Model: c.Model, MaxTokens: c.MaxTokens}
}

func validateCompletions(c Completions) error {
	if !completions.ValidProvider(c.Provider) {
		return fmt.Errorf("completions: unsupported provider %q", c.Provider)
	}
	if err := completions.CheckEndpoint(c.Endpoint); err != nil {
		return fmt.Errorf("completions: %w", err)
	}
	if err := completions.CheckModel(c.Model); err != nil {
		return fmt.Errorf("completions: %w", err)
	}
	for _, r := range []struct {
		name        string
		v, min, max int
	}{
		{"debounceMs", c.DebounceMs, MinCompletionDebounceMs, MaxCompletionDebounceMs},
		{"maxTokens", c.MaxTokens, MinCompletionTokens, MaxCompletionTokens},
		{"contextLines", c.ContextLines, MinCompletionContextLines, MaxCompletionContextLines},
	} {
		if r.v < r.min || r.v > r.max {
			return fmt.Errorf("completions.%s %d out of range %d–%d", r.name, r.v, r.min, r.max)
		}
	}
	return nil
}

func validateInlineEdit(e InlineEdit) error {
	if e.ContextLines < 0 || e.ContextLines > MaxInlineEditContextLines {
		return fmt.Errorf("inlineEdit.contextLines %d out of range 0–%d", e.ContextLines, MaxInlineEditContextLines)
	}
	return nil
}

// normalizeCompletions keeps Enabled and replaces each invalid value by its
// default; numbers are clamped (0 or missing is the default).
func normalizeCompletions(c Completions) Completions {
	def := defaultCompletions()
	out := c
	if !completions.ValidProvider(c.Provider) {
		out.Provider = def.Provider
	}
	if completions.CheckEndpoint(c.Endpoint) != nil {
		out.Endpoint = def.Endpoint
	}
	if completions.CheckModel(c.Model) != nil {
		out.Model = def.Model
	}
	clamp := func(v, lo, hi, d int) int {
		if v == 0 {
			return d
		}
		return max(lo, min(hi, v))
	}
	out.DebounceMs = clamp(c.DebounceMs, MinCompletionDebounceMs, MaxCompletionDebounceMs, def.DebounceMs)
	out.MaxTokens = clamp(c.MaxTokens, MinCompletionTokens, MaxCompletionTokens, def.MaxTokens)
	out.ContextLines = clamp(c.ContextLines, MinCompletionContextLines, MaxCompletionContextLines, def.ContextLines)
	return out
}

func normalizeInlineEdit(e InlineEdit) InlineEdit {
	return InlineEdit{ContextLines: max(0, min(MaxInlineEditContextLines, e.ContextLines))}
}
