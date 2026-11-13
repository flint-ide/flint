package host

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The conformance test of the decorations example (examples/extensions/todos):
// built from source, installed like a user would, and driven through the
// editor decoration contract: a decoration set, editor/opened and
// editor/changed, editor/setDecorations with versions, and a quick fix.

func buildTodosExample(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is not installed")
	}
	src, err := filepath.Abs("../../../examples/extensions/todos")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	cmd := exec.Command("go", "build", "-o", filepath.Join(dir, "todos"), ".")
	cmd.Dir = src
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build the example: %v\n%s", err, out)
	}
	data, err := os.ReadFile(filepath.Join(src, "flint-extension.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "flint-extension.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestDecorationsExampleExtension(t *testing.T) {
	src := buildTodosExample(t)
	root := realDir(t)
	app := &fakeApp{roots: []string{root}, trusted: map[string]bool{root: true}, confirm: true, disabled: map[string]bool{}, settings: map[string]json.RawMessage{}}
	h := New(Options{Dir: filepath.Join(t.TempDir(), "extensions"), App: app, Environ: MinimalEnv, StopTimeout: time.Second})
	defer h.Close()
	info, err := h.Install(src)
	if err != nil {
		t.Fatal(err)
	}
	if info.State != StateStopped || len(info.DecorationSets) != 1 || !info.DecorationSets[0].Enabled || !info.DecorationSets[0].UnsavedText {
		t.Fatalf("installed: %+v", info)
	}
	const ext, set = "example.todos", "example.todos.marks"
	events := func(n int) []DecorationsEvent {
		t.Helper()
		waitFor(t, "the decorations", func() bool { return len(app.eventsLike(EventDecorations+" ")) >= n })
		return app.decorationEvents(t)
	}

	// Opening a file starts it; it marks the TODO lines of the editor's text.
	text := "package main\n\n// TODO: tidy up\nfunc main() {} // ünï TODO\n"
	path := write(t, root, "main.go", "package main\n")
	if got := h.EditorInterest(path); got != (DocumentInterest{Tracked: true, Text: true}) {
		t.Fatalf("interest = %+v", got)
	}
	if err := h.EditorOpened(path, 1, text, true); err != nil {
		t.Fatal(err)
	}
	ev := events(1)[0]
	if ev.Extension != ext || ev.Set != set || ev.Path != path || ev.Version != 1 || len(ev.Decorations) != 2 {
		t.Fatalf("event = %+v", ev)
	}
	first, second := ev.Decorations[0], ev.Decorations[1]
	if first.Kind != "diagnostic" || first.Line != 3 || first.From != 3 || first.To != 7 || first.Message != "To do: tidy up" || len(first.Actions) != 1 || first.Actions[0].Label != "Remove TODO marker" {
		t.Errorf("first = %+v", first)
	}
	// Columns count UTF-16 units, not bytes: "func main() {} // ünï " is 22 long.
	if second.Line != 4 || second.From != 22 || second.To != 26 {
		t.Errorf("second = %+v", second)
	}

	// The quick fix: an edit for the version it was asked about.
	req := ActionRequest{Set: set, Path: path, Version: 1, Action: first.Actions[0].ID, Line: 3, EndLine: 3, From: 3, To: 7}
	res, err := h.EditorAction(context.Background(), ext, req)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Edits) != 1 || res.Edits[0] != (TextEdit{Line: 3, From: 3, EndLine: 3, To: 9, Text: ""}) || res.Version != 1 {
		t.Errorf("edits = %+v", res)
	}
	req.Version = 9 // a text it hasn't seen: nothing to apply
	if res, err := h.EditorAction(context.Background(), ext, req); err != nil || len(res.Edits) != 0 {
		t.Errorf("a fix for an unknown version: %+v, %v", res, err)
	}

	// After the edit the editor reports the change; the marks follow.
	h.EditorChanged(path, 2, strings.Replace(text, "TODO: ", "", 1), true)
	if all := events(2); all[1].Version != 2 || len(all[1].Decorations) != 1 || all[1].Decorations[0].Line != 4 {
		t.Errorf("after the fix: %+v", all[1])
	}

	// A document reported without its text is read from disk.
	saved := write(t, root, "notes.md", "- TODO buy milk\n")
	if err := h.EditorOpened(saved, 1, "", false); err != nil {
		t.Fatal(err)
	}
	if all := events(3); all[2].Path != saved || len(all[2].Decorations) != 1 || all[2].Decorations[0].Message != "To do: buy milk" {
		t.Errorf("from disk: %+v", all[2])
	}

	h.EditorClosed(path)
	h.EditorClosed(saved)
	h.Close()
	if log, _ := h.Log(ext); strings.Contains(log, "protocol error") || strings.Contains(log, "panic") {
		t.Errorf("log:\n%s", log)
	}
	if st := h.all()[0].state(); st != StateStopped {
		t.Errorf("after Close: %s", st)
	}
}
