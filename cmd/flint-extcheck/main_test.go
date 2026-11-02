package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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
