package extensions

import (
	"os"
	"path/filepath"
	"testing"

	"flint/internal/extensions/host"
	"flint/internal/extensions/manifest"
)

func TestExtensionLanguages(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bin", "server"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	ruby := manifest.Language{
		ID: "ruby", Label: "Ruby", Extensions: []string{".RB"}, Filenames: []string{"Gemfile"},
		Server: &manifest.Server{Candidates: [][]string{{"ruby-lsp"}, {"./bin/server", "--stdio"}, {"./bin/missing"}}, Install: "gem install ruby-lsp"},
	}
	infos := []host.Info{
		{ID: "me.ruby", Name: "Ruby", Dir: dir, Enabled: true, State: host.StateStopped, Languages: []manifest.Language{ruby}},
		{ID: "me.off", Enabled: false, State: host.StateDisabled, Languages: []manifest.Language{{ID: "off", Extensions: []string{".off"}}}},
		{ID: "me.broken", Enabled: true, State: host.StateInvalid, Languages: []manifest.Language{{ID: "broken", Extensions: []string{".b"}}}},
		{ID: "me.plain", Enabled: true, State: host.StateStopped, Languages: []manifest.Language{{ID: "plain", LanguageID: "plaintext", Extensions: []string{".pl1"}}}},
	}
	got := extensionLanguages(infos)
	if len(got) != 2 {
		t.Fatalf("languages = %+v, want ruby and plain", got)
	}
	r := got[0]
	if r.Language != "ruby" || r.LanguageID != "ruby" || r.Extensions[0] != ".rb" || r.Source != "me.ruby" || r.Install != "gem install ruby-lsp" {
		t.Errorf("ruby = %+v", r)
	}
	if len(r.Candidates) != 2 || r.Candidates[0][0] != "ruby-lsp" || !filepath.IsAbs(r.Candidates[1][0]) || r.Candidates[1][1] != "--stdio" {
		t.Errorf("candidates = %v", r.Candidates)
	}
	if p := got[1]; p.LanguageID != "plaintext" || len(p.Candidates) != 0 {
		t.Errorf("plain = %+v", p)
	}
}
