package depdirs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIs(t *testing.T) {
	root := t.TempDir()
	mk := func(rel string) string {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	touch := func(rel string) {
		if err := os.WriteFile(mk(rel), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	touch("web/node_modules/x/index.js")
	touch("py/env/pyvenv.cfg")
	touch("rust/Cargo.toml")
	touch("rust/target/debug/app")
	touch("java/target/notes.md") // no manifest: target is source here
	touch("pkg/foo.egg-info/PKG-INFO")
	touch("src/main.go")

	for rel, want := range map[string]bool{
		"web/node_modules": true,
		"py/env":           true,
		"rust/target":      true,
		"java/target":      false,
		"pkg/foo.egg-info": true,
		"src":              false,
	} {
		if got := Is(filepath.Join(root, filepath.FromSlash(rel))); got != want {
			t.Errorf("Is(%s) = %v, want %v", rel, got, want)
		}
	}
}

func TestByEntries(t *testing.T) {
	if !HasMarker([]string{"bin", "CACHEDIR.TAG"}) || HasMarker([]string{"bin"}) {
		t.Error("HasMarker")
	}
	if !BesideManifest("build", []string{"build.gradle"}) || BesideManifest("build", []string{"Makefile"}) {
		t.Error("BesideManifest")
	}
}
