package agent

import (
	"maps"
	"strings"
	"testing"
)

func TestIsTier(t *testing.T) {
	for model, want := range map[string]bool{"fast": true, "balanced": true, "strong": true, "Strong": false, "opus": false, "": false} {
		if IsTier(model) != want {
			t.Errorf("IsTier(%q) = %v", model, !want)
		}
	}
}

func TestCheckTiers(t *testing.T) {
	tests := []struct {
		name  string
		tiers map[string]string
		err   string
	}{
		{name: "none", tiers: nil},
		{name: "all", tiers: map[string]string{"fast": "haiku", "balanced": "claude-sonnet-4-5", "strong": "us.anthropic.claude-opus-4-1[1m]"}},
		{name: "cleared", tiers: map[string]string{"strong": ""}},
		{name: "unknown tier", tiers: map[string]string{"huge": "opus"}, err: `unknown model tier "huge"`},
		{name: "flag", tiers: map[string]string{"fast": "--yolo"}, err: "tier fast: invalid model"},
		{name: "spaces", tiers: map[string]string{"strong": "a b"}, err: "tier strong"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckTiers(tt.tiers)
			if tt.err == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.err) {
				t.Fatalf("err %v, want %q", err, tt.err)
			}
		})
	}
}

func TestTierModels(t *testing.T) {
	tests := []struct {
		name     string
		k        Kind
		settings map[string]string
		want     map[string]string
	}{
		{name: "claude defaults", k: Claude, want: map[string]string{"fast": "haiku", "balanced": "sonnet", "strong": "opus"}},
		{name: "claude override", k: Claude, settings: map[string]string{"strong": "claude-opus-4-1"},
			want: map[string]string{"fast": "haiku", "balanced": "sonnet", "strong": "claude-opus-4-1"}},
		{name: "claude default cleared", k: Claude, settings: map[string]string{"fast": ""},
			want: map[string]string{"balanced": "sonnet", "strong": "opus"}},
		{name: "codex has no defaults", k: Codex, want: map[string]string{}},
		{name: "codex mapped", k: Codex, settings: map[string]string{"strong": "gpt-5-codex", "fast": ""},
			want: map[string]string{"strong": "gpt-5-codex"}},
		{name: "unknown tier ignored", k: Codex, settings: map[string]string{"huge": "x"}, want: map[string]string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TierModels(tt.k, tt.settings); !maps.Equal(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
	// The defaults are a fresh map each time: changing one changes nothing else.
	DefaultTiers(Claude)["fast"] = "x"
	if DefaultTiers(Claude)["fast"] != "haiku" {
		t.Fatal("DefaultTiers shares its map")
	}
}
