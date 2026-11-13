package host

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// The conformance test of the example extension (examples/extensions/stats):
// it is built from source, installed like a user would, and driven through
// every contribution point it uses. It is the contract check for level 2:
// a change to the host that breaks a real extension fails here.

func buildExample(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is not installed")
	}
	src, err := filepath.Abs("../../../examples/extensions/stats")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	cmd := exec.Command("go", "build", "-o", filepath.Join(dir, "stats"), ".")
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

func TestExampleExtension(t *testing.T) {
	src := buildExample(t)
	root := realDir(t)
	for name, size := range map[string]int{"a.go": 300, "b.go": 100, "notes.md": 50, "node_modules/x.js": 10, ".hidden/y.txt": 5} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(strings.Repeat("x", size)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	app := &fakeApp{roots: []string{root}, trusted: map[string]bool{root: true}, confirm: true, disabled: map[string]bool{}, settings: map[string]json.RawMessage{}}
	h := New(Options{Dir: filepath.Join(t.TempDir(), "extensions"), App: app, Environ: MinimalEnv, StopTimeout: time.Second})
	defer h.Close()
	info, err := h.Install(src)
	if err != nil {
		t.Fatal(err)
	}
	if info.ID != "example.stats" || info.State != StateStopped || len(info.Commands) != 3 || len(info.Panels) != 2 || len(info.TabViews) != 1 || info.Settings == nil {
		t.Fatalf("installed: %+v", info)
	}
	ctx := context.Background()

	// The panel: file types, counted on first use; the status item follows.
	v, err := h.Resolve(ctx, "example.stats", "example.stats.files", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Items) != 2 || v.Items[0].ID != "type:.go" || v.Items[0].Description != "2 files · 400 B" || !v.Items[0].HasChildren {
		t.Fatalf("types = %+v", v.Items)
	}
	waitFor(t, "the status item", func() bool { return len(app.eventsLike(EventStatus)) > 0 })
	if s := app.eventsLike(EventStatus)[0]; !strings.Contains(s, `"text":"3 files"`) || !strings.Contains(s, `"command":"example.stats.refresh"`) {
		t.Errorf("status = %s", s)
	}

	// A file type's largest files, which open in Flint.
	files, err := h.Resolve(ctx, "example.stats", "example.stats.files", "type:.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(files.Items) != 2 || files.Items[0].Label != "a.go" || files.Items[0].Command == nil || len(files.Items[0].Actions) != 1 {
		t.Fatalf("files = %+v", files.Items)
	}
	if err := h.Action(ctx, "example.stats", "example.stats.files", files.Items[0].ID, "open"); err != nil {
		t.Fatal(err)
	}
	if err := h.Execute(ctx, "example.stats", files.Items[1].Command.ID, files.Items[1].Command.Args); err != nil {
		t.Fatal(err)
	}
	open := app.eventsLike(EventOpenFile)
	if len(open) != 2 || !strings.Contains(open[0], filepath.Join(root, "a.go")) || !strings.Contains(open[1], filepath.Join(root, "b.go")) {
		t.Errorf("opened = %v", open)
	}

	// The table panel: a row per file type with typed cells, sorted by Flint.
	tv, err := h.ResolveView(ctx, "example.stats", "example.stats.types", "", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if tv.Kind != "table" || tv.Table == nil || len(tv.Table.Columns) != 5 || len(tv.Table.Rows) != 2 || tv.Truncated {
		t.Fatalf("table = %+v", tv)
	}
	if c := tv.Table.Columns; c[1].Type != "number" || c[1].Align != "end" || c[3].Badges["large"] != "warning" {
		t.Errorf("columns = %+v", c)
	}
	row := tv.Table.Rows[0]
	if row.ID != "type:.go" || !reflect.DeepEqual(row.Cells, []any{".go", float64(2), float64(400), "large", "a.go"}) || row.Command == nil || len(row.Actions) != 1 {
		t.Errorf("row = %+v", row)
	}
	if tv.Table.Sort == nil || tv.Table.Sort.Column != "files" || !tv.Table.Sort.Descending {
		t.Errorf("sort = %+v", tv.Table.Sort)
	}
	if err := h.ViewAction(ctx, "example.stats", "example.stats.types", "", row.ID, "open"); err != nil {
		t.Fatal(err)
	}
	if open := app.eventsLike(EventOpenFile); len(open) != 3 || !strings.Contains(open[2], filepath.Join(root, "a.go")) {
		t.Errorf("opened from the table = %v", open)
	}

	// The chart: a command opens its tab, and the tab's key picks the metric.
	if err := h.Execute(ctx, "example.stats", "example.stats.showChart", nil); err != nil {
		t.Fatal(err)
	}
	if tabs := app.eventsLike(EventOpenTab); len(tabs) != 1 || !strings.Contains(tabs[0], `"view":"example.stats.chart"`) || !strings.Contains(tabs[0], `"key":"files"`) {
		t.Errorf("opened tabs = %v", tabs)
	}
	for key, want := range map[string]Point{"files": {X: ".go", Y: 2}, "bytes": {X: ".go", Y: 400}} {
		cv, err := h.ResolveView(ctx, "example.stats", "example.stats.chart", key, nil, "")
		if err != nil {
			t.Fatal(err)
		}
		if cv.Kind != "chart" || cv.Chart == nil || cv.Chart.Type != "bar" || cv.Chart.X.Type != "category" || len(cv.Chart.Series) != 1 {
			t.Fatalf("chart %s = %+v", key, cv)
		}
		if pts := cv.Chart.Series[0].Points; len(pts) != 2 || pts[0] != want {
			t.Errorf("chart %s points = %+v", key, pts)
		}
	}

	// The command from the palette counts again and says so.
	if err := h.Execute(ctx, "example.stats", "example.stats.refresh", nil); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the message", func() bool { return len(app.eventsLike(EventMessage)) > 0 })
	if m := app.eventsLike(EventMessage)[0]; !strings.Contains(m, "Counted 3 files (450 B).") {
		t.Errorf("message = %s", m)
	}

	// A setting: the extension hears about it and the panel follows.
	refreshes := len(app.eventsLike(EventRefresh))
	app.mu.Lock()
	app.settings["example.stats"] = json.RawMessage(`{"topFiles": 1, "countHidden": true}`)
	app.mu.Unlock()
	h.SettingsChanged("example.stats")
	waitFor(t, "the refresh after the settings change", func() bool { return len(app.eventsLike(EventRefresh)) > refreshes })
	files, err = h.Resolve(ctx, "example.stats", "example.stats.files", "type:.go")
	if err != nil || len(files.Items) != 1 {
		t.Errorf("with topFiles 1: %+v, %v", files.Items, err)
	}
	if v, _ := h.Resolve(ctx, "example.stats", "example.stats.files", ""); len(v.Items) != 3 {
		t.Errorf("with hidden files counted: %+v", v.Items)
	}

	// The agent tool, for the agent's project.
	tools := h.Tools(func(string) bool { return true })
	if len(tools) != 1 || tools[0].Name != "stats_summary" {
		t.Fatalf("tools = %+v", tools)
	}
	text, err := tools[0].Call(ctx, root, json.RawMessage(`{"top": 1}`))
	if err != nil || !strings.Contains(text, ".go\t2\t400 B\ta.go") || !strings.Contains(text, "… and 2 more types") {
		t.Errorf("summary = %q, %v", text, err)
	}

	// Its stderr is in its log.
	if log, err := h.Log("example.stats"); err != nil || !strings.Contains(log, "[stderr] stats: counted 3 files") {
		t.Errorf("log:\n%s", log)
	}

	// Quit: the process stops.
	h.Close()
	if st := h.all()[0].state(); st != StateStopped {
		t.Errorf("after Close: %s", st)
	}
}
