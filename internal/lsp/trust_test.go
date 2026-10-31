package lsp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTypeScriptServerIsPinnedToItsOwnTypeScript(t *testing.T) {
	prefix := t.TempDir()
	cli := filepath.Join(prefix, "lib", "node_modules", "typescript-language-server", "lib", "cli.mjs")
	ts := filepath.Join(prefix, "lib", "node_modules", "typescript", "lib", "tsserver.js")
	for _, f := range []string{cli, ts} {
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte("//"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	bin := filepath.Join(prefix, "bin", "typescript-language-server")
	_ = os.MkdirAll(filepath.Dir(bin), 0o755)
	if err := os.Symlink(cli, bin); err != nil {
		t.Fatal(err)
	}
	opts := initializationOptions(bin)
	got, _ := opts["tsserver"].(map[string]any)["path"].(string)
	want, _ := filepath.EvalSymlinks(ts)
	if got != want {
		t.Fatalf("tsserver path = %q, want %q", got, want)
	}
	if initializationOptions("/usr/local/bin/gopls") != nil {
		t.Fatal("options for another server")
	}
}
