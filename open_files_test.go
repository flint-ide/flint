package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeTestFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("# notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFilesFromArgsKeepsExistingFilesOnly(t *testing.T) {
	parent := t.TempDir()
	if err := os.Mkdir(filepath.Join(parent, "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(parent, "README.md"))
	got := filesFromArgs([]string{"-psn_0_123", "api", "README.md", "missing.md"}, parent)
	want := []string{filepath.Join(parent, "README.md")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("filesFromArgs = %v, want %v", got, want)
	}
}

func TestFilesFromArgsSkipsTheExtensionToInstall(t *testing.T) {
	parent := t.TempDir()
	writeTestFile(t, filepath.Join(parent, "ext.zip"))
	if got := filesFromArgs([]string{"--install-extension", "ext.zip"}, parent); len(got) != 0 {
		t.Fatalf("filesFromArgs = %v, want none", got)
	}
}

// Files opened before the frontend asked for them wait; once it has, they are
// sent like files dragged into the window.
func TestOpenFilesWaitsForTheFrontendThenEmits(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "a.md")
	second := filepath.Join(dir, "b.md")
	writeTestFile(t, first)
	writeTestFile(t, second)

	var emitted []any
	a := &App{emitHook: func(name string, data ...any) {
		if name == eventFilesDropped {
			emitted = append(emitted, data...)
		}
	}}

	a.openFiles([]string{first})
	if len(emitted) != 0 {
		t.Fatalf("emitted before the frontend asked: %v", emitted)
	}
	ws := &WorkspaceAPI{app: a}
	if got, want := ws.InitialFiles(), []DroppedPath{{Path: first}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("InitialFiles = %+v, want %+v", got, want)
	}
	if got := ws.InitialFiles(); len(got) != 0 {
		t.Fatalf("InitialFiles again = %+v, want none", got)
	}

	a.openFiles([]string{second})
	want := []any{[]DroppedPath{{Path: second}}}
	if !reflect.DeepEqual(emitted, want) {
		t.Fatalf("emitted = %+v, want %+v", emitted, want)
	}
}

func TestOpenFilesIgnoresNothing(t *testing.T) {
	called := false
	a := &App{emitHook: func(string, ...any) { called = true }}
	(&WorkspaceAPI{app: a}).InitialFiles()
	a.openFiles(nil)
	if called {
		t.Fatal("emitted for no files")
	}
}
