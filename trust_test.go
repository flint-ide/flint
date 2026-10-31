package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"flint/internal/config"
)

func TestWorkspaceTrust(t *testing.T) {
	cfg := t.TempDir()
	base := t.TempDir()
	for _, d := range []string{"work/api", "work-other", "elsewhere"} {
		if err := os.MkdirAll(filepath.Join(base, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(filepath.Join(base, "elsewhere"), link); err != nil {
		t.Fatal(err)
	}
	a := &App{configDir: cfg}
	if a.isTrusted(filepath.Join(base, "work")) {
		t.Fatal("trusted before the user said so")
	}
	if err := a.setTrust(filepath.Join(base, "work"), true); err != nil {
		t.Fatal(err)
	}
	if !a.isTrusted(filepath.Join(base, "work", "api")) {
		t.Error("subfolder of a trusted folder not trusted")
	}
	if a.isTrusted(filepath.Join(base, "work-other")) {
		t.Error("sibling sharing the name prefix trusted")
	}
	if err := a.setTrust(link, true); err != nil {
		t.Fatal(err)
	}
	if !a.isTrusted(filepath.Join(base, "elsewhere")) {
		t.Error("trust through a symlink not applied to its target")
	}
	var saved appState
	if err := config.LoadJSON(a.statePath(), &saved); err != nil || len(saved.TrustedFolders) != 2 {
		t.Fatalf("saved %v err %v", saved.TrustedFolders, err)
	}
	if err := a.setTrust(filepath.Join(base, "work"), false); err != nil {
		t.Fatal(err)
	}
	if a.isTrusted(filepath.Join(base, "work", "api")) {
		t.Error("revoked trust still applies")
	}
}

func TestSessionScrollbackIsRedacted(t *testing.T) {
	in := `{"tabs":[{"kind":"terminal","cwd":"/Users/me/p","output":"$ export GITHUB_TOKEN=ghp_abcdefghijklmnopqrstuvwxyz0123456789\nok \"quoted\""}]}`
	out := redactJSONStrings(in)
	if strings.Contains(out, "ghp_abcdef") || !strings.Contains(out, `"cwd":"/Users/me/p"`) || !strings.Contains(out, `\"quoted\"`) {
		t.Fatalf("redacted session: %s", out)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("invalid JSON after redaction: %v", err)
	}
	if redactJSONStrings("not json") != "not json" {
		t.Error("non-JSON changed")
	}
}
