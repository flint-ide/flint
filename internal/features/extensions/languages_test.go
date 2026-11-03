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

func TestExtensionDebuggersAndRunners(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bin", "adapter"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	dbg := manifest.Debugger{
		Type: "rdbg", Label: "Ruby (rdbg)", Languages: []string{"ruby"}, Launch: []byte(`{"script":"${file}"}`),
		Adapter: &manifest.Server{Candidates: [][]string{{"rdbg", "--open"}, {"./bin/adapter"}, {"./bin/missing"}}, Install: "gem install debug"},
	}
	runners := []manifest.TestRunner{
		{ID: "rspec", Label: "RSpec", Languages: []string{"ruby"}, Command: []string{"rspec", "${file}"}, Format: "tap"},
		{ID: "gone", Label: "Gone", Languages: []string{"ruby"}, Command: []string{"./bin/missing"}},
	}
	infos := []host.Info{
		{ID: "me.ruby", Dir: dir, Enabled: true, State: host.StateStopped, Debuggers: []manifest.Debugger{dbg}, TestRunners: runners},
		{ID: "me.off", Dir: dir, Enabled: false, State: host.StateDisabled, Debuggers: []manifest.Debugger{dbg}, TestRunners: runners},
		{ID: "me.broken", Dir: dir, Enabled: true, State: host.StateInvalid, Debuggers: []manifest.Debugger{dbg}},
	}
	ds := extensionDebuggers(infos)
	if len(ds) != 1 || ds[0].Source != "me.ruby" || ds[0].Install != "gem install debug" || string(ds[0].Launch) != `{"script":"${file}"}` {
		t.Fatalf("debuggers = %+v", ds)
	}
	if c := ds[0].Candidates; len(c) != 2 || c[0][0] != "rdbg" || c[0][1] != "--open" || !filepath.IsAbs(c[1][0]) {
		t.Errorf("adapter candidates = %v", c)
	}
	rs := extensionRunners(infos)
	if len(rs) != 1 || rs[0].ID != "rspec" || rs[0].Format != "tap" || rs[0].Source != "me.ruby" || rs[0].Command[1] != "${file}" {
		t.Errorf("runners = %+v", rs)
	}
}
