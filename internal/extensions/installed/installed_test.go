package installed

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"flint/internal/extensions/manifest"
)

const nord = `{"contract":1,"id":"me.nord","name":"Nord","version":"1.0.0",
 "contributes":{"themes":[{"id":"me.nord.dark","label":"Nord","base":"dark","colors":{"--bg-editor":"#2e3440"}}]}}`

// writeExt makes an extension folder with the given manifest and files.
func writeExt(t *testing.T, dir, man string, files map[string]string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, manifest.FileName), []byte(man), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func mustLoad(t *testing.T, dir string) manifest.Manifest {
	t.Helper()
	m, err := manifest.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestDiscover(t *testing.T) {
	root := t.TempDir()
	if exts, err := Discover(filepath.Join(root, "missing")); err != nil || exts != nil {
		t.Errorf("Discover(missing) = %v, %v", exts, err)
	}
	writeExt(t, filepath.Join(root, "me.nord"), nord, nil)
	writeExt(t, filepath.Join(root, "wrong.name"), nord, nil)
	writeExt(t, filepath.Join(root, "me.broken"), `{"contract":1}`, nil)
	writeExt(t, filepath.Join(root, ".install-123"), nord, nil)
	exts, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(exts) != 3 {
		t.Fatalf("Discover() = %d extensions, want 3 (hidden skipped)", len(exts))
	}
	byID := map[string]Extension{}
	for _, e := range exts {
		byID[e.ID()] = e
	}
	if e := byID["me.nord"]; e.Err != nil || e.Manifest.Name != "Nord" {
		t.Errorf("me.nord = %+v", e)
	}
	if e := byID["wrong.name"]; e.Err == nil || !strings.Contains(e.Err.Error(), "folder is named") {
		t.Errorf("wrong.name = %v, want a name mismatch", e.Err)
	}
	if e := byID["me.broken"]; e.Err == nil {
		t.Error("me.broken has no error")
	}
}

func TestInstallAndReplace(t *testing.T) {
	root := filepath.Join(t.TempDir(), "extensions")
	src := writeExt(t, filepath.Join(t.TempDir(), "src"), nord, map[string]string{"bin/tool": "#!/bin/sh\n", "README.md": "hi", ".git/HEAD": "x"})
	ext, err := Install(src, root, mustLoad(t, src))
	if err != nil {
		t.Fatalf("Install() = %v", err)
	}
	if ext.Dir != filepath.Join(root, "me.nord") {
		t.Errorf("Dir = %s", ext.Dir)
	}
	info, err := os.Stat(filepath.Join(ext.Dir, "bin", "tool"))
	if err != nil || info.Mode().Perm()&0o100 == 0 {
		t.Errorf("bin/tool = %v, %v; want an executable copy", info, err)
	}
	if _, err := os.Stat(filepath.Join(ext.Dir, ".git")); err == nil {
		t.Error(".git was copied")
	}
	// Installing again replaces the folder: files gone from src disappear.
	if err := os.Remove(filepath.Join(src, "README.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(src, root, mustLoad(t, src)); err != nil {
		t.Fatalf("Install(again) = %v", err)
	}
	if _, err := os.Stat(filepath.Join(ext.Dir, "README.md")); err == nil {
		t.Error("the old version's files survived the update")
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 1 {
		t.Errorf("extensions folder has %d entries, want only me.nord (no temporary folders)", len(entries))
	}
}

func TestInstallRefusesChangedManifest(t *testing.T) {
	root := t.TempDir()
	src := writeExt(t, filepath.Join(t.TempDir(), "src"), nord, nil)
	confirmed := mustLoad(t, src)
	confirmed.Name = "Something Else"
	if _, err := Install(src, root, confirmed); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Errorf("Install() = %v, want a changed-manifest error", err)
	}
	if _, err := os.Stat(filepath.Join(root, "me.nord")); err == nil {
		t.Error("an unconfirmed manifest was installed")
	}
}

func TestInstallRefusesSymlinks(t *testing.T) {
	root := t.TempDir()
	src := writeExt(t, filepath.Join(t.TempDir(), "src"), nord, nil)
	if err := os.Symlink("/etc/passwd", filepath.Join(src, "passwd")); err != nil {
		t.Skip("symlinks unsupported:", err)
	}
	if _, err := Install(src, root, mustLoad(t, src)); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Errorf("Install() = %v, want a symlink error", err)
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 0 {
		t.Errorf("a failed install left %d entries", len(entries))
	}
}

func TestInstallRefusesItsOwnFolder(t *testing.T) {
	root := t.TempDir()
	src := writeExt(t, filepath.Join(root, "me.nord"), nord, nil)
	if _, err := Install(src, root, mustLoad(t, src)); err == nil {
		t.Error("Install() copied a folder from inside the extensions folder")
	}
}

func TestRemove(t *testing.T) {
	root := t.TempDir()
	writeExt(t, filepath.Join(root, "me.nord"), nord, nil)
	for _, bad := range []string{"", "..", "../x", ".old-me.nord", "a/b"} {
		if Remove(root, bad) == nil {
			t.Errorf("Remove(%q) succeeded", bad)
		}
	}
	if err := Remove(root, "me.nord"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "me.nord")); err == nil {
		t.Error("me.nord is still there")
	}
	if Remove(root, "me.nord") == nil {
		t.Error("removing twice succeeded")
	}
}
