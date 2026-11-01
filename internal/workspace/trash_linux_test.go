//go:build linux

package workspace

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTrashHomeFollowsTheSpec(t *testing.T) {
	dir := t.TempDir()
	trash := filepath.Join(dir, "Trash")
	now := time.Date(2026, 11, 1, 12, 30, 0, 0, time.Local)

	for i := range 2 { // the second one needs a new name
		path := filepath.Join(dir, "my notes.txt")
		mustWrite(t, path, []byte{byte('a' + i)}, 0o644)
		if err := trashHome(path, trash, now); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("file still there: %v", err)
		}
	}

	for _, name := range []string{"my notes.txt", "my notes.2.txt"} {
		if _, err := os.Stat(filepath.Join(trash, "files", name)); err != nil {
			t.Errorf("trashed file %s: %v", name, err)
		}
		info, err := os.ReadFile(filepath.Join(trash, "info", name+".trashinfo"))
		if err != nil {
			t.Fatal(err)
		}
		want := "[Trash Info]\nPath=" + strings.ReplaceAll(filepath.Join(dir, "my notes.txt"), " ", "%20") +
			"\nDeletionDate=2026-11-01T12:30:00\n"
		if string(info) != want {
			t.Errorf("%s.trashinfo = %q, want %q", name, info, want)
		}
	}
}

func TestTrashHomeMissingFileLeavesNoInfo(t *testing.T) {
	dir := t.TempDir()
	trash := filepath.Join(dir, "Trash")
	if err := trashHome(filepath.Join(dir, "gone"), trash, time.Now()); err == nil {
		t.Fatal("trashing a missing file should fail")
	}
	entries, _ := os.ReadDir(filepath.Join(trash, "info"))
	if len(entries) != 0 {
		t.Errorf("left %d .trashinfo files behind", len(entries))
	}
}

// A file in files/ without its .trashinfo (left by another tool or a crash)
// must not be overwritten: the new one takes the next name.
func TestTrashHomeKeepsOrphanedTrashFiles(t *testing.T) {
	dir := t.TempDir()
	trash := filepath.Join(dir, "Trash")
	orphan := filepath.Join(trash, "files", "a.txt")
	mustWrite(t, orphan, []byte("old"), 0o644)
	path := filepath.Join(dir, "a.txt")
	mustWrite(t, path, []byte("new"), 0o644)

	if err := trashHome(path, trash, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(orphan); string(got) != "old" {
		t.Errorf("orphaned trash file overwritten: %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(trash, "files", "a.2.txt")); string(got) != "new" {
		t.Errorf("trashed file not at a.2.txt: %q", got)
	}
	if _, err := os.Stat(filepath.Join(trash, "info", "a.txt.trashinfo")); err == nil {
		t.Error("an info file was written for the orphan's name")
	}
}

func TestRenameNoReplace(t *testing.T) {
	dir := t.TempDir()
	from, to := filepath.Join(dir, "from"), filepath.Join(dir, "to")
	mustWrite(t, from, []byte("a"), 0o644)
	mustWrite(t, to, []byte("b"), 0o644)
	if err := renameNoReplace(from, to); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("renameNoReplace onto a file = %v, want fs.ErrExist", err)
	}
	if got, _ := os.ReadFile(to); string(got) != "b" {
		t.Errorf("target replaced: %q", got)
	}
}

func TestHomeTrash(t *testing.T) {
	if got, err := homeTrash("/data", nil); err != nil || got != "/data/Trash" {
		t.Errorf("XDG_DATA_HOME: %q, %v", got, err)
	}
	if got, err := homeTrash("", func() (string, error) { return "/home/me", nil }); err != nil || got != "/home/me/.local/share/Trash" {
		t.Errorf("default: %q, %v", got, err)
	}
	if _, err := homeTrash("relative", func() (string, error) { return "", errors.New("no home") }); err == nil {
		t.Error("no home directory must be an error, not a relative trash path")
	}
}
