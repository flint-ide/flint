package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// rubyLanguage is a language-only extension: files, comments, a server and a grammar.
const rubyLanguage = `{
  "contract": 1, "id": "me.ruby", "name": "Ruby", "version": "1.0.0",
  "contributes": { "languages": [ {
    "id": "ruby", "label": "Ruby", "extensions": [".rb", ".rake"], "filenames": ["Gemfile"],
    "lineComment": "#", "blockComment": ["=begin", "=end"],
    "server": { "candidates": [["ruby-lsp"], ["solargraph", "stdio"], ["./bin/server", "--stdio"]], "install": "gem install ruby-lsp" },
    "grammar": { "states": {
      "start": [
        { "regex": "#.*", "token": "comment" },
        { "regex": "\"", "token": "string", "push": "string" },
        { "regex": "\\b(?:def|end|class|module|if|else|do)\\b", "token": "keyword" },
        { "regex": "\\d+", "token": "number" }
      ],
      "string": [
        { "regex": "\\\\.", "token": "string" },
        { "regex": "\"", "token": "string", "pop": true },
        { "regex": "[^\"\\\\]+", "token": "string" }
      ]
    } }
  } ] }
}`

func TestParseLanguage(t *testing.T) {
	m, err := Parse([]byte(rubyLanguage))
	if err != nil {
		t.Fatalf("Parse() = %v", err)
	}
	if m.HasProgram() || m.NeedsProgram() {
		t.Error("a language-only extension has or needs a program")
	}
	l := m.Contributes.Languages[0]
	if l.ID != "ruby" || len(l.Extensions) != 2 || l.Server == nil || len(l.Server.Candidates) != 3 || len(l.Grammar.States) != 2 {
		t.Errorf("language = %+v", l)
	}
	if len(m.Contributes.Unsupported) != 0 {
		t.Errorf("languages listed as unsupported: %v", m.Contributes.Unsupported)
	}
}

func TestLanguageChecks(t *testing.T) {
	for _, tc := range []struct{ name, from, to, want string }{
		{"bad id", `"id": "ruby"`, `"id": "Ruby Lang"`, "language id"},
		{"no files", `"extensions": [".rb", ".rake"], "filenames": ["Gemfile"],`, ``, "no extensions or filenames"},
		{"bad extension", `".rake"`, `"rake"`, `not like ".rb"`},
		{"path as filename", `"Gemfile"`, `"../Gemfile"`, "plain file name"},
		{"block comment", `["=begin", "=end"]`, `["=begin"]`, "blockComment"},
		{"server path", `["ruby-lsp"]`, `["/usr/bin/ruby-lsp"]`, "neither a program name"},
		{"server escapes", `"./bin/server"`, `"./../server"`, "server program"},
		{"no start state", `"start": [`, `"begin": [`, `no "start" state`},
		{"unknown token", `"token": "number"`, `"token": "color: red"`, "token"},
		{"missing state", `"push": "string"`, `"push": "nope"`, `state "nope"`},
		{"catastrophic regex", `"\\d+"`, `"(a+)+$"`, "repeats a repeated group"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := strings.Replace(rubyLanguage, tc.from, tc.to, 1)
			if src == rubyLanguage {
				t.Fatalf("replacement %q not found", tc.from)
			}
			_, err := Parse([]byte(src))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Parse() = %v, want an error with %q", err, tc.want)
			}
		})
	}
}

func TestLanguageTwice(t *testing.T) {
	src := strings.Replace(rubyLanguage, `"languages": [ {`, `"languages": [ { "id": "ruby", "label": "Again", "extensions": [".x"] }, {`, 1)
	if _, err := Parse([]byte(src)); err == nil || !strings.Contains(err.Error(), "listed twice") {
		t.Fatalf("Parse() = %v", err)
	}
}

func TestServerProgram(t *testing.T) {
	dir := t.TempDir()
	if p, err := ServerProgram(dir, "ruby-lsp"); err != nil || p != "ruby-lsp" {
		t.Errorf("PATH name: %q, %v", p, err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bin", "server"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bin", "data"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "evil")
	if err := os.WriteFile(outside, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "bin", "link")); err != nil {
		t.Fatal(err)
	}
	p, err := ServerProgram(dir, "./bin/server")
	if err != nil || filepath.Base(p) != "server" || !filepath.IsAbs(p) {
		t.Errorf("inside: %q, %v", p, err)
	}
	for _, bad := range []string{"./bin/data", "./bin/link", "./bin/missing", "./../x"} {
		if p, err := ServerProgram(dir, bad); err == nil {
			t.Errorf("%s: resolved to %q", bad, p)
		}
	}
}
