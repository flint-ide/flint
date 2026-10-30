package workspace

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestWithin(t *testing.T) {
	cases := []struct {
		root, path string
		want       bool
	}{
		{"/p", "/p", true},
		{"/p", "/p/a/b.go", true},
		{"/p", "/p/../q", false},
		{"/p", "/pq/a", false},
		{"/p", "/", false},
		{"/p", "p/a", false},
		{"/p/", "/p/a/../b", true},
	}
	for _, c := range cases {
		if got := Within(c.root, c.path); got != c.want {
			t.Errorf("Within(%q, %q) = %v, want %v", c.root, c.path, got, c.want)
		}
	}
}

func TestValidName(t *testing.T) {
	for _, bad := range []string{"", "  ", ".", "..", "a/b", "a\x00"} {
		if ValidName(bad) == nil {
			t.Errorf("ValidName(%q) accepted", bad)
		}
	}
	for _, good := range []string{"a.go", ".env", "with space", "ąę.md"} {
		if err := ValidName(good); err != nil {
			t.Errorf("ValidName(%q): %v", good, err)
		}
	}
}

func TestCreateFileAndDir(t *testing.T) {
	root := t.TempDir()
	file, err := CreateFile(root, root, "main.go")
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(file); err != nil || info.Size() != 0 {
		t.Fatalf("created file: %v %v", info, err)
	}
	if _, err := CreateFile(root, root, "main.go"); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("second create should fail with ErrExist, got %v", err)
	}

	dir, err := CreateDir(root, root, "pkg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CreateFile(root, dir, "util.go"); err != nil {
		t.Fatalf("create in subfolder: %v", err)
	}
	if _, err := CreateDir(root, root, "pkg"); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("existing folder should fail with ErrExist, got %v", err)
	}
}

func TestCreateRejectsOutsideAndBadNames(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if _, err := CreateFile(root, outside, "x"); !errors.Is(err, ErrOutsideProject) {
		t.Fatalf("outside dir: %v", err)
	}
	if _, err := CreateFile(root, root, "../escape"); err == nil {
		t.Fatal("name with a slash accepted")
	}
	if _, err := CreateDir(root, "relative", "x"); err == nil {
		t.Fatal("relative dir accepted")
	}
	if _, err := os.Stat(filepath.Join(outside, "x")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("file created outside the project")
	}
}

func TestRename(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.go"), []byte("a"), 0o644)
	mustWrite(t, filepath.Join(root, "b.go"), []byte("b"), 0o644)
	mustWrite(t, filepath.Join(root, "dir/inner.go"), []byte("i"), 0o644)

	got, err := Rename(root, filepath.Join(root, "a.go"), "c.go")
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(root, "c.go") {
		t.Fatalf("new path %q", got)
	}
	if data, _ := os.ReadFile(got); string(data) != "a" {
		t.Fatalf("content after rename: %q", data)
	}

	if _, err := Rename(root, got, "b.go"); err == nil {
		t.Fatal("rename onto an existing file must fail")
	}
	if data, _ := os.ReadFile(filepath.Join(root, "b.go")); string(data) != "b" {
		t.Fatal("existing file was overwritten")
	}

	moved, err := Rename(root, filepath.Join(root, "dir"), "lib")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(moved, "inner.go")); err != nil {
		t.Fatalf("folder contents after rename: %v", err)
	}
}

func TestRenameCaseOnly(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "Readme.md"), []byte("r"), 0o644)
	got, err := Rename(root, filepath.Join(root, "Readme.md"), "README.md")
	if err != nil {
		t.Fatalf("case-only rename: %v", err)
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 1 || entries[0].Name() != "README.md" || filepath.Base(got) != "README.md" {
		t.Fatalf("entries after case-only rename: %v", entries)
	}
}

func TestRenameRejects(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.go"), []byte("a"), 0o644)
	if _, err := Rename(root, root, "other"); !errors.Is(err, ErrOutsideProject) {
		t.Fatalf("renaming the root: %v", err)
	}
	if _, err := Rename(root, filepath.Join(root, "a.go"), "sub/b.go"); err == nil {
		t.Fatal("rename into another folder accepted")
	}
	if _, err := Rename(root, filepath.Join(root, "missing.go"), "b.go"); err == nil {
		t.Fatal("renaming a missing file accepted")
	}
}

func TestTopLevel(t *testing.T) {
	got := TopLevel([]string{"/p/a/x.go", "/p/a", "/p/b.go", "/p/a/", "/p/ab"})
	want := []string{"/p/a", "/p/b.go", "/p/ab"}
	if !slices.Equal(got, want) {
		t.Fatalf("TopLevel = %v, want %v", got, want)
	}
}

func TestTrash(t *testing.T) {
	root := t.TempDir()
	var trashed []string
	fake := func(p string) error {
		trashed = append(trashed, p)
		return os.RemoveAll(p)
	}
	mustWrite(t, filepath.Join(root, "dir/x.go"), []byte("x"), 0o644)
	mustWrite(t, filepath.Join(root, "y.go"), []byte("y"), 0o644)

	moved, err := Trash(root, []string{filepath.Join(root, "dir/x.go"), filepath.Join(root, "dir"), filepath.Join(root, "y.go")}, fake)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(root, "dir"), filepath.Join(root, "y.go")}
	if !slices.Equal(moved, want) || !slices.Equal(trashed, want) {
		t.Fatalf("moved %v, trashed %v, want %v", moved, trashed, want)
	}
}

func TestTrashChecksEveryPathFirst(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.go"), []byte("a"), 0o644)
	called := false
	fake := func(string) error { called = true; return nil }

	for _, paths := range [][]string{
		{filepath.Join(root, "a.go"), "/etc/hosts"},
		{root},
		{filepath.Join(root, "a.go"), "relative.go"},
	} {
		if _, err := Trash(root, paths, fake); err == nil {
			t.Errorf("Trash(%v) accepted", paths)
		}
	}
	if called {
		t.Fatal("nothing may be trashed when one path is invalid")
	}
}

func TestTrashReportsPartialFailure(t *testing.T) {
	root := t.TempDir()
	a, b := filepath.Join(root, "a"), filepath.Join(root, "b")
	fake := func(p string) error {
		if p == b {
			return errors.New("locked")
		}
		return nil
	}
	moved, err := Trash(root, []string{a, b}, fake)
	if err == nil || !slices.Equal(moved, []string{a}) {
		t.Fatalf("moved %v, err %v", moved, err)
	}
}
