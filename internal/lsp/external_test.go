package lsp

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func setLanguages(t *testing.T, list ...ExtensionLanguage) {
	t.Helper()
	SetExtensionLanguages(list)
	t.Cleanup(func() { SetExtensionLanguages(nil) })
}

func TestExtensionLanguagesComeAfterFlintsOwn(t *testing.T) {
	setLanguages(t,
		ExtensionLanguage{Language: "ruby", LanguageID: "ruby", Extensions: []string{".rb"}, Filenames: []string{"Gemfile"}, Candidates: [][]string{{"ruby-lsp"}}, Source: "me.ruby"},
		ExtensionLanguage{Language: "other-ruby", LanguageID: "ruby", Extensions: []string{".rb"}, Candidates: [][]string{{"x"}}, Source: "you.ruby"},
		ExtensionLanguage{Language: "go", LanguageID: "go", Extensions: []string{".go2"}, Candidates: [][]string{{"evil"}}, Source: "me.go"},
		ExtensionLanguage{Language: "mygo", LanguageID: "go", Extensions: []string{".go"}, Candidates: [][]string{{"evil"}}, Source: "me.mygo"},
		ExtensionLanguage{Language: "nolsp", LanguageID: "nolsp", Extensions: []string{".nl"}, Source: "me.nolsp"},
	)
	for _, tc := range []struct {
		path, language string
		ok             bool
	}{
		{"/p/a.rb", "ruby", true},
		{"/p/A.RB", "ruby", true},
		{"/p/Gemfile", "ruby", true},
		{"/p/main.go", "go", true}, // Flint's own, not me.mygo's
		{"/p/x.go2", "", false},    // "go" is Flint's: me.go's language is skipped
		{"/p/x.nl", "", false},     // no server: nothing for LSP
		{"/p/x.txt", "", false},
	} {
		language, _, ok := languageFor(tc.path)
		if ok != tc.ok || language != tc.language {
			t.Errorf("languageFor(%s) = %q, %v; want %q, %v", tc.path, language, ok, tc.language, tc.ok)
		}
	}
	if spec, ok := specFor("go"); !ok || spec.Candidates[0][0] != "gopls" {
		t.Errorf("specFor(go) = %+v", spec)
	}
	if KnownLanguage("ruby") {
		t.Error("an extension's language is configurable in settings.json")
	}
	names := []string{}
	for _, l := range ExtensionLanguages() {
		names = append(names, l.Language)
	}
	if !slices.Equal(names, []string{"mygo", "nolsp", "other-ruby", "ruby"}) {
		t.Errorf("ExtensionLanguages() = %v", names)
	}
}

func TestChangedLanguagesRestartServers(t *testing.T) {
	var got [][]string
	onExtensionLanguagesChanged(func(l []string) { got = append(got, l) })
	ruby := ExtensionLanguage{Language: "ruby", Extensions: []string{".rb"}, Candidates: [][]string{{"ruby-lsp"}}}
	setLanguages(t, ruby)
	SetExtensionLanguages([]ExtensionLanguage{ruby}) // unchanged: no restart
	moved := ruby
	moved.Candidates = [][]string{{"solargraph", "stdio"}}
	SetExtensionLanguages([]ExtensionLanguage{moved})
	SetExtensionLanguages(nil)
	want := [][]string{{"ruby"}, {"ruby"}, {"ruby"}}
	if !slices.EqualFunc(got, want, slices.Equal[[]string]) {
		t.Errorf("restarts = %v, want %v", got, want)
	}
}

func TestExtensionServerNeedsATrustedFolder(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "started")
	srv := filepath.Join(dir, "server")
	if err := os.WriteFile(srv, []byte("#!/bin/sh\ntouch "+marker+"\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	setLanguages(t, ExtensionLanguage{Language: "ruby", LanguageID: "ruby", Extensions: []string{".rb"}, Candidates: [][]string{{srv, "--stdio"}}})
	root := t.TempDir()
	trusted := false
	m := NewManager()
	m.Trusted = func(string) bool { return trusted }
	defer m.Shutdown()

	m.Open(root, filepath.Join(root, "a.rb"), "puts 1")
	waitFor(t, func() bool { return hasState(m, "restricted") })
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("an extension's server started in an untrusted folder")
	}
	trusted = true
	m.Retrust(root)
	waitFor(t, func() bool { _, err := os.Stat(marker); return err == nil })
}

func TestLanguageOfCoversLanguagesWithoutServer(t *testing.T) {
	setLanguages(t,
		ExtensionLanguage{Language: "ruby", Label: "Ruby", Extensions: []string{".rb"}, Source: "me.ruby"},
		ExtensionLanguage{Language: "mygo", Label: "My Go", Extensions: []string{".go"}, Source: "me.mygo"},
	)
	for _, tc := range []struct{ path, language, label string }{
		{"/p/a.rb", "ruby", "Ruby"}, // no server, still a language
		{"/p/main.go", "go", "Go"},  // Flint's own wins
		{"/p/x.py", "python", "Python"},
		{"/p/x.txt", "", ""},
	} {
		language, label, ok := LanguageOf(tc.path)
		if language != tc.language || label != tc.label || ok != (tc.language != "") {
			t.Errorf("LanguageOf(%s) = %q, %q, %v", tc.path, language, label, ok)
		}
	}
}
