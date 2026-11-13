package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"flint/internal/extensions/manifest"
)

// extension writes a folder with a manifest and, when script is set, a shell
// script as its program.
func extension(t *testing.T, manifestJSON, script string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "flint-extension.json"), []byte(manifestJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if script != "" {
		if err := os.WriteFile(filepath.Join(dir, "run.sh"), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const panelManifest = `{"contract":1,"id":"test.ext","name":"T","version":"0.1.0","run":{"command":"run.sh"},
  "activation":["onView:test.ext.p"],
  "contributes":{"panels":[{"id":"test.ext.p","title":"P","icon":"files","order":1,"view":"list"}]}}`

// A tiny extension in shell: answers initialize and view/resolve with a
// fixed reply (resolve), then shutdown.
func shellExtension(resolve string) string {
	return `while IFS= read -r line; do
  id=$(printf '%s' "$line" | grep -o '"id":[0-9][0-9]*' | head -n 1 | cut -d: -f2)
  case "$line" in
    *'"method":"initialize"'*) printf '{"jsonrpc":"2.0","id":%s,"result":{"capabilities":{}}}\n' "$id" ;;
    *'"method":"view/resolve"'*) printf '{"jsonrpc":"2.0","id":%s,%s}\n' "$id" '` + resolve + `' ;;
    *'"method":"shutdown"'*) printf '{"jsonrpc":"2.0","id":%s,"result":null}\n' "$id"; exit 0 ;;
  esac
done
`
}

const setManifest = `{"contract":1,"id":"test.ext","name":"T","version":"0.1.0","run":{"command":"run.sh"},
  "permissions":["workspace.read"],
  "contributes":{"decorationSets":[{"id":"test.ext.marks","title":"Marks","globs":["*.txt"]}]}}`

// decorating answers initialize and shutdown, publishes publish (the params
// of editor/setDecorations, with %s for the path) when a file opens, and
// answers editor/action with action.
func decorating(publish, action string) string {
	return `while IFS= read -r line; do
  id=$(printf '%s' "$line" | grep -o '"id":[0-9][0-9]*' | head -n 1 | cut -d: -f2)
  case "$line" in
    *'"method":"initialize"'*) printf '{"jsonrpc":"2.0","id":%s,"result":{"capabilities":{}}}\n' "$id" ;;
    *'"method":"editor/opened"'*)
      path=$(printf '%s' "$line" | grep -o '"path":"[^"]*"' | head -n 1 | cut -d'"' -f4)
      printf '{"jsonrpc":"2.0","method":"editor/setDecorations","params":` + publish + `}\n' "$path" ;;
    *'"method":"editor/action"'*) printf '{"jsonrpc":"2.0","id":%s,%s}\n' "$id" '` + action + `' ;;
    *'"method":"shutdown"'*) printf '{"jsonrpc":"2.0","id":%s,"result":null}\n' "$id"; exit 0 ;;
  esac
done
`
}

func TestCheckDecorationSets(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell extensions")
	}
	good := `{"set":"test.ext.marks","path":"%s","version":1,"decorations":[{"kind":"gutter","line":1}]}`
	for name, tc := range map[string]struct {
		manifest, script, want string
	}{
		"good":               {setManifest, decorating(good, `"result":{"edits":[]}`), ""},
		"no quick fixes":     {setManifest, decorating(good, `"error":{"code":-32601,"message":"method not found"}`), ""},
		"foreign set":        {setManifest, decorating(strings.Replace(good, "test.ext.marks", "other.ext.marks", 1), `"result":{}`), "editor/setDecorations"},
		"another file":       {setManifest, decorating(strings.Replace(good, "%s", "%s.bak", 1), `"result":{}`), "editor/setDecorations"},
		"bad edits":          {setManifest, decorating(good, `"result":{"edits":[{"line":0,"text":"x"}]}`), "editor/action"},
		"no answer":          {setManifest, "read -r line; echo nope; sleep 5\n", "decoration set test.ext.marks"},
		"without permission": {strings.Replace(setManifest, `"workspace.read"`, ``, 1), decorating(good, `"result":{}`), "manifest"},
	} {
		t.Run(name, func(t *testing.T) {
			r := check(extension(t, tc.manifest, tc.script))
			got := strings.Join(r.problems, "\n")
			if tc.want == "" && got != "" {
				t.Errorf("problems:\n%s", got)
			}
			if tc.want != "" && !strings.Contains(got, tc.want) {
				t.Errorf("problems %q, want one with %q", got, tc.want)
			}
		})
	}
}

func TestSampleFile(t *testing.T) {
	for _, tc := range []struct {
		set  manifest.DecorationSet
		want string
	}{
		{manifest.DecorationSet{Extensions: []string{".md"}}, "extcheck.md"},
		{manifest.DecorationSet{Globs: []string{"Dockerfile*"}}, "Dockerfileextcheck"},
		{manifest.DecorationSet{Globs: []string{"*.test.t?"}}, "extcheck.test.tx"},
		{manifest.DecorationSet{Languages: []string{"ruby", "python"}}, "extcheck.py"},
		{manifest.DecorationSet{Languages: []string{"ruby"}}, ""},
		{manifest.DecorationSet{Globs: []string{"[a-c]*"}}, ""},
	} {
		if got := sampleFile(tc.set); got != tc.want {
			t.Errorf("sampleFile(%+v) = %q, want %q", tc.set, got, tc.want)
		}
	}
}

func TestCheck(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell extensions")
	}
	for name, tc := range map[string]struct {
		manifest, script, want string
	}{
		"good":                 {panelManifest, shellExtension(`"result":{"kind":"list","items":[{"id":"1","label":"one"}]}`), ""},
		"message view is fine": {panelManifest, shellExtension(`"result":{"kind":"message","text":"Sign in first."}`), ""},
		"bad manifest":         {`{"contract":1,"id":"Bad Id","name":"T","version":"1"}`, "", "manifest"},
		"no program":           {panelManifest, "", "manifest"},
		"resolve error":        {panelManifest, shellExtension(`"error":{"code":-32603,"message":"boom"}`), "view/resolve"},
		"wrong view kind":      {panelManifest, shellExtension(`"result":{"kind":"tree","items":[]}`), "list panel"},
		"not json-rpc":         {panelManifest, "echo hello; sleep 5\n", "panel test.ext.p"},
		"unknown contribution": {strings.Replace(panelManifest, `"contributes":{`, `"contributes":{"fileMenu":[],`, 1), shellExtension(`"result":{"kind":"list","items":[]}`), "doesn't offer"},
	} {
		t.Run(name, func(t *testing.T) {
			r := check(extension(t, tc.manifest, tc.script))
			got := strings.Join(r.problems, "\n")
			if tc.want == "" && got != "" {
				t.Errorf("problems:\n%s", got)
			}
			if tc.want != "" && !strings.Contains(got, tc.want) {
				t.Errorf("problems %q, want one with %q", got, tc.want)
			}
		})
	}
}
