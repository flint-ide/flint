package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDroppedPaths(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.go")
	if err := os.WriteFile(file, []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := droppedPaths([]string{dir, file, filepath.Join(dir, "gone")})
	want := []DroppedPath{{Path: dir, Dir: true}, {Path: file}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("droppedPaths = %+v, want %+v", got, want)
	}
}

func TestFolderFromArgsResolvesAgainstTheCallersDirectory(t *testing.T) {
	parent := t.TempDir()
	if err := os.Mkdir(filepath.Join(parent, "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := folderFromArgs([]string{"-psn_0_123", "api"}, parent); got != filepath.Join(parent, "api") {
		t.Fatalf("folder = %q", got)
	}
	if got := folderFromArgs([]string{"missing"}, parent); got != "" {
		t.Fatalf("missing folder = %q", got)
	}
}

// `flint --install-extension <dir>` installs an extension: its folder is not
// a project to open, while a project folder next to it still is.
func TestFolderFromArgsSkipsTheExtensionToInstall(t *testing.T) {
	parent := t.TempDir()
	for _, name := range []string{"ext", "api"} {
		if err := os.Mkdir(filepath.Join(parent, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"--install-extension", "ext"}, {"--install-extension=ext"}, {"--install-extension"}} {
		if got := folderFromArgs(args, parent); got != "" {
			t.Errorf("folderFromArgs(%v) = %q, want none", args, got)
		}
	}
	if got := folderFromArgs([]string{"--install-extension", "ext", "api"}, parent); got != filepath.Join(parent, "api") {
		t.Errorf("folder = %q", got)
	}
}
