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

// The conformance test of the webview example (examples/extensions/notes):
// built from source, installed like a user would, and driven through the
// webview contract: HTML from the folder, webview/request, webview/post and
// window/openTab.

func buildNotesExample(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is not installed")
	}
	src, err := filepath.Abs("../../../examples/extensions/notes")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	cmd := exec.Command("go", "build", "-o", filepath.Join(dir, "notes"), ".")
	cmd.Dir = src
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build the example: %v\n%s", err, out)
	}
	for _, name := range []string{"flint-extension.json", "ui/list.html", "ui/note.html"} {
		data, err := os.ReadFile(filepath.Join(src, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestWebviewExampleExtension(t *testing.T) {
	src := buildNotesExample(t)
	app := &fakeApp{trusted: map[string]bool{}, confirm: true, disabled: map[string]bool{}, settings: map[string]json.RawMessage{}}
	h := New(Options{Dir: filepath.Join(t.TempDir(), "extensions"), App: app, Environ: MinimalEnv, StopTimeout: time.Second})
	defer h.Close()
	info, err := h.Install(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Panels) != 1 || info.Panels[0].View != "webview" || len(info.TabViews) != 1 || !info.TabViews[0].Restore {
		t.Fatalf("installed: %+v", info)
	}
	const id, list, note = "example.notes", "example.notes.list", "example.notes.note"
	ctx := context.Background()

	// The pages come from the folder; nothing else does.
	html, err := h.WebviewHTML(id, list)
	if err != nil || !strings.Contains(html, "flint.request('list')") {
		t.Fatalf("list.html = %.60q, %v", html, err)
	}
	if _, err := h.WebviewHTML(id, "example.notes.nope"); err == nil {
		t.Error("an undeclared view has HTML")
	}

	// A page's request reaches the process (starting it) and the answer comes back.
	out, err := h.WebviewRequest(ctx, id, list, "", "list", nil)
	if err != nil || !strings.Contains(string(out), `"title":"Welcome"`) {
		t.Fatalf("list = %s, %v", out, err)
	}
	if _, err := h.WebviewRequest(ctx, id, "example.notes.nope", "", "list", nil); err == nil {
		t.Error("a request for an undeclared view went through")
	}
	if _, err := h.WebviewRequest(ctx, id, list, "", "bad method\n", nil); err == nil {
		t.Error("a malformed method went through")
	}

	// Saving in a tab: the process posts to the list panel.
	if _, err := h.WebviewRequest(ctx, id, note, "1", "save", json.RawMessage(`{"Title":"Renamed","Text":"x"}`)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the webview message", func() bool { return len(app.eventsLike(EventWebview)) > 0 })
	if m := app.eventsLike(EventWebview)[0]; !strings.Contains(m, `"view":"example.notes.list"`) || !strings.Contains(m, `"changed":"1"`) {
		t.Errorf("webview event = %s", m)
	}
	got, _ := h.WebviewRequest(ctx, id, note, "1", "get", nil)
	if !strings.Contains(string(got), `"title":"Renamed"`) {
		t.Errorf("get = %s", got)
	}

	// The palette command opens a new note's tab through window/openTab.
	if err := h.Execute(ctx, id, "example.notes.new", nil); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the tab", func() bool { return len(app.eventsLike(EventOpenTab)) > 0 })
	if tab := app.eventsLike(EventOpenTab)[0]; !strings.Contains(tab, `"view":"example.notes.note"`) || !strings.Contains(tab, `"key":"2"`) {
		t.Errorf("open tab = %s", tab)
	}

	// Showing a tab is reported to the process (and keeps it from idling).
	if err := h.SetWebviewVisible(id, note, "2", true); err != nil {
		t.Error(err)
	}
	if err := h.SetWebviewVisible(id, "example.notes.nope", "", true); err == nil {
		t.Error("visibility of an undeclared view accepted")
	}
}
