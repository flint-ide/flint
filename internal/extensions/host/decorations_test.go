package host

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// write creates a file under dir and returns its path.
func write(t *testing.T, dir, name, text string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// goLanguage is Options.LanguageOf for the tests: .go files are "go".
func goLanguage(o *Options) {
	o.LanguageOf = func(path string) string {
		if strings.HasSuffix(path, ".go") {
			return "go"
		}
		return ""
	}
}

// decorationEvents decodes the EventDecorations events sent so far.
func (a *fakeApp) decorationEvents(t *testing.T) []DecorationsEvent {
	t.Helper()
	var out []DecorationsEvent
	for _, e := range a.eventsLike(EventDecorations + " ") {
		var list []DecorationsEvent
		if err := json.Unmarshal([]byte(strings.TrimPrefix(e, EventDecorations+" ")), &list); err != nil || len(list) != 1 {
			t.Fatalf("event %s: %v", e, err)
		}
		out = append(out, list[0])
	}
	return out
}

func (a *fakeApp) setSet(set string, on bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.sets == nil {
		a.sets = map[string]bool{}
	}
	a.sets[set] = on
}

func TestEditorInterestFollowsSetsTrustAndSecrets(t *testing.T) {
	f := setup(t, goLanguage)
	for _, tc := range []struct {
		name string
		path string
		want DocumentInterest
	}{
		{"a set with unsavedText", write(t, f.root, "a.txt", "x"), DocumentInterest{Tracked: true, Text: true}},
		{"a set by language, without text", write(t, f.root, "main.go", "x"), DocumentInterest{Tracked: true}},
		{"no set for the file", write(t, f.root, "a.md", "x"), DocumentInterest{}},
		{"an untrusted folder", write(t, f.other, "a.txt", "x"), DocumentInterest{}},
		{"inside .git", write(t, f.root, ".git/notes.txt", "x"), DocumentInterest{}},
		{"a file outside the folders", write(t, realDir(t), "a.txt", "x"), DocumentInterest{}},
		{"a file that doesn't exist", filepath.Join(f.root, "missing.txt"), DocumentInterest{}},
	} {
		if got := f.h.EditorInterest(tc.path); got != tc.want {
			t.Errorf("%s: interest = %+v, want %+v", tc.name, got, tc.want)
		}
	}
	// A credential file is never announced, whatever a set's glob says.
	if got := f.h.EditorInterest(write(t, f.root, ".env.txt", "x")); got.Tracked {
		t.Errorf("a credential file is tracked: %+v", got)
	}
	// A symbolic link to a file outside the trusted folder isn't either.
	link := filepath.Join(f.root, "link.txt")
	if err := os.Symlink(write(t, f.other, "target.txt", "x"), link); err != nil {
		t.Fatal(err)
	}
	if got := f.h.EditorInterest(link); got.Tracked {
		t.Errorf("a link out of the trusted folder is tracked: %+v", got)
	}
	// The user's switches: a set turned off, an extension turned off.
	txt := filepath.Join(f.root, "a.txt")
	f.app.setSet("test.ext.marks", false)
	if got := f.h.EditorInterest(txt); got.Tracked {
		t.Errorf("a set turned off is tracked: %+v", got)
	}
	f.app.setSet("test.ext.extra", true) // off by default, no unsavedText
	if got := f.h.EditorInterest(txt); got != (DocumentInterest{Tracked: true}) {
		t.Errorf("a set turned on: %+v", got)
	}
	f.app.mu.Lock()
	f.app.disabled["test.ext"] = true
	f.app.mu.Unlock()
	if got := f.h.EditorInterest(txt); got.Tracked {
		t.Errorf("an extension turned off is tracked: %+v", got)
	}
	if f.state() != StateDisabled || f.log(t) != "" {
		t.Errorf("asking about interest started the process: %s\n%s", f.state(), f.log(t))
	}
}

func TestEditorNotificationsStartAndKeepTheExtension(t *testing.T) {
	f := setup(t, func(o *Options) { goLanguage(o); o.IdleTimeout = 100 * time.Millisecond })
	md, txt, gofile := write(t, f.root, "a.md", "x"), write(t, f.root, "a.txt", "x"), write(t, f.root, "main.go", "x")

	// A file no set is for starts nothing.
	if err := f.h.EditorOpened(md, 1, "", false); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if f.state() != StateStopped {
		t.Fatalf("a file without a decoration set started the extension: %s", f.state())
	}

	// One with a set does; the set with unsavedText gets the text.
	if err := f.h.EditorOpened(txt, 1, "hello", true); err != nil {
		t.Fatal(err)
	}
	opened := `editor/opened {"language":"","path":"` + txt + `","sets":["test.ext.marks"],"text":"hello","version":1}`
	waitFor(t, "editor/opened", func() bool { return strings.Contains(f.log(t), opened) })
	if strings.Contains(f.log(t), "a.md") {
		t.Errorf("the extension heard of a file it has no set for:\n%s", f.log(t))
	}
	// A set without unsavedText gets none, even when the editor passes it.
	if err := f.h.EditorOpened(gofile, 3, "package main", true); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "editor/opened of the Go file", func() bool {
		return strings.Contains(f.log(t), `editor/opened {"language":"go","path":"`+gofile+`","sets":["test.ext.go"],"version":3}`)
	})
	f.h.EditorChanged(txt, 2, "hello!", true)
	f.h.EditorChanged(gofile, 4, "package main\n", true)
	f.h.EditorChanged(md, 2, "y", true)
	waitFor(t, "editor/changed", func() bool {
		log := f.log(t)
		return strings.Contains(log, `editor/changed {"path":"`+txt+`","text":"hello!","version":2}`) &&
			strings.Contains(log, `editor/changed {"path":"`+gofile+`","version":4}`)
	})
	// A document larger than the limit is announced without its text.
	f.h.EditorChanged(txt, 3, strings.Repeat("x", maxEditorText+1), true)
	waitFor(t, "editor/changed without text", func() bool {
		return strings.Contains(f.log(t), `editor/changed {"path":"`+txt+`","version":3}`)
	})

	// Open documents keep it running past the idle timeout.
	time.Sleep(300 * time.Millisecond)
	if f.state() != StateRunning {
		t.Fatalf("stopped while a document it decorates is open: %s", f.state())
	}
	f.h.EditorClosed(gofile)
	f.h.EditorClosed(txt)
	f.h.EditorClosed(md)
	waitFor(t, "the idle stop after the documents closed", func() bool { return f.state() == StateStopped })
	if log := f.log(t); !strings.Contains(log, `editor/closed {"path":"`+txt+`"}`) || !strings.Contains(log, `editor/closed {"path":"`+gofile+`"}`) || strings.Contains(log, "a.md") {
		t.Errorf("log:\n%s", log)
	}
}

func TestSetDecorationsIsCleanedBoundedAndVersioned(t *testing.T) {
	f := setup(t, nil)
	txt, other := write(t, f.root, "a.txt", "x"), write(t, f.root, "b.txt", "x")
	if err := f.h.EditorOpened(txt, 5, "x", true); err != nil {
		t.Fatal(err)
	}
	publish := func(args map[string]any) {
		t.Helper()
		if err := f.exec(t, "decorate", args); err != nil {
			t.Fatal(err)
		}
	}
	events := func(n int) []DecorationsEvent {
		t.Helper()
		waitFor(t, "the decorations", func() bool { return len(f.app.eventsLike(EventDecorations+" ")) >= n })
		return f.app.decorationEvents(t)
	}

	// What the extension sends is cleaned.
	publish(map[string]any{"set": "test.ext.marks", "path": txt, "version": 5})
	ev := events(1)[0]
	if ev.Extension != "test.ext" || ev.Set != "test.ext.marks" || ev.Path != txt || ev.Version != 5 || ev.Truncated || len(ev.Decorations) != 5 {
		t.Fatalf("event = %+v", ev)
	}
	d := ev.Decorations
	if d[0].Kind != "diagnostic" || d[0].ID != "d1" || d[0].Line != 2 || d[0].EndLine != 2 || d[0].From != 1 || d[0].To != 4 || d[0].Severity != "warning" || d[0].Message != "shadowed[31m\nsecond line" {
		t.Errorf("diagnostic = %+v", d[0])
	}
	if a := d[0].Actions; len(a) != 3 || a[0].ID != "fix" || a[0].Command != nil || a[1].Command == nil || a[1].Command.ID != "test.ext.status" || a[2].Command != nil {
		t.Errorf("actions keep the extension's own commands only: %+v", a)
	}
	if d[1].Kind != "gutter" || d[1].Icon != "check" || d[1].Severity != "ok" || d[1].Message != "Covered" || d[1].To != -1 {
		t.Errorf("gutter = %+v", d[1])
	}
	if d[2].Icon != "" || d[2].Severity != "info" {
		t.Errorf("an unknown icon and severity are dropped: %+v", d[2])
	}
	if d[3].Kind != "inline" || len([]rune(d[3].Text)) != maxInlineText || d[3].Command != nil {
		t.Errorf("inline = %+v", d[3])
	}
	if d[4].Kind != "highlight" || d[4].Line != 6 || d[4].EndLine != 6 || d[4].From != 0 || d[4].To != 2 {
		t.Errorf("highlight = %+v", d[4])
	}

	// A version older than the editor's is dropped; none, or the current one, is not.
	publish(map[string]any{"set": "test.ext.marks", "path": txt, "version": 4, "count": 1})
	publish(map[string]any{"set": "test.ext.marks", "path": txt, "count": 2})
	if all := events(2); len(all) != 2 || all[1].Version != 0 || len(all[1].Decorations) != 2 {
		t.Fatalf("after a stale and an unversioned publish: %+v", all)
	}
	f.h.EditorChanged(txt, 6, "xy", true)
	publish(map[string]any{"set": "test.ext.marks", "path": txt, "version": 5, "count": 3})
	publish(map[string]any{"set": "test.ext.marks", "path": txt, "version": 6, "count": 4})
	if all := events(3); len(all) != 3 || all[2].Version != 6 || len(all[2].Decorations) != 4 {
		t.Fatalf("after the document changed: %+v", all)
	}

	// At most 5,000 decorations per set and file.
	publish(map[string]any{"set": "test.ext.marks", "path": txt, "count": maxDecorations + 100})
	if all := events(4); len(all[3].Decorations) != maxDecorations || !all[3].Truncated {
		t.Errorf("%d decorations, truncated %v", len(all[3].Decorations), all[3].Truncated)
	}

	// Dropped: a file that isn't open, a set that is off or isn't for the
	// file, a set of nobody's.
	publish(map[string]any{"set": "test.ext.marks", "path": other, "count": 1})
	publish(map[string]any{"set": "test.ext.extra", "path": txt, "count": 1})
	publish(map[string]any{"set": "test.ext.go", "path": txt, "count": 1})
	publish(map[string]any{"set": "git.marks", "path": txt, "count": 1})
	if err := f.exec(t, "clear", map[string]any{"set": "git.marks"}); err != nil {
		t.Fatal(err)
	}
	// Clearing: one file, or all.
	if err := f.exec(t, "clear", map[string]any{"set": "test.ext.marks", "path": txt}); err != nil {
		t.Fatal(err)
	}
	if err := f.exec(t, "clear", map[string]any{"set": "test.ext.marks"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the clears", func() bool { return len(f.app.eventsLike(EventDecorationsCleared)) >= 2 })
	cleared := f.app.eventsLike(EventDecorationsCleared)
	if !strings.Contains(cleared[0], `"set":"test.ext.marks","path":"`+txt+`"`) || !strings.HasSuffix(cleared[1], `[{"extension":"test.ext","set":"test.ext.marks"}]`) {
		t.Errorf("cleared = %v", cleared)
	}
	if n := len(f.app.eventsLike(EventDecorations + " ")); n != 4 {
		t.Errorf("%d decoration events, want 4: a dropped publish got through", n)
	}
	log := f.log(t)
	for _, want := range []string{
		"dropped: " + other + " is not an open document of test.ext.marks",
		"dropped: " + txt + " is not an open document of test.ext.extra",
		`no decoration set "git.marks" in the manifest`,
	} {
		if !strings.Contains(log, want) {
			t.Errorf("the log lacks %q:\n%s", want, log)
		}
	}
}

func TestQuickFixRoundTrip(t *testing.T) {
	f := setup(t, nil)
	txt := write(t, f.root, "a.txt", "one\ntwo\n")
	ctx := context.Background()
	req := ActionRequest{Set: "test.ext.marks", Path: txt, Version: 7, Action: "fix", Decoration: "d1", Line: 2, EndLine: 2, From: 1, To: 3}
	if _, err := f.h.EditorAction(ctx, "test.ext", req); err == nil || !strings.Contains(err.Error(), "does not decorate this file") {
		t.Fatalf("a quick fix in a file that isn't open: %v", err)
	}
	if err := f.h.EditorOpened(txt, 7, "one\ntwo\n", true); err != nil {
		t.Fatal(err)
	}
	res, err := f.h.EditorAction(ctx, "test.ext", req)
	if err != nil {
		t.Fatal(err)
	}
	want := []TextEdit{{Line: 2, From: 1, EndLine: 2, To: 3, Text: "fixed d1"}, {Line: 3, From: 2, EndLine: 3, To: 2, Text: "inserted\n"}}
	if res.Version != 7 || len(res.Edits) != 2 || res.Edits[0] != want[0] || res.Edits[1] != want[1] {
		t.Errorf("result = %+v", res)
	}
	if log := f.log(t); !strings.Contains(log, `editor/action {"set":"test.ext.marks","path":"`+txt+`","version":7,"action":"fix","decoration":"d1","line":2,"endLine":2,"from":1,"to":3}`) {
		t.Errorf("log:\n%s", log)
	}
	// No edits is an answer too.
	req.Action = "none"
	if res, err := f.h.EditorAction(ctx, "test.ext", req); err != nil || len(res.Edits) != 0 {
		t.Errorf("an answer without edits: %+v, %v", res, err)
	}
	for action, wantErr := range map[string]string{"bad": "invalid position", "many": "more than 1000 edits", "": "no action"} {
		req.Action = action
		if _, err := f.h.EditorAction(ctx, "test.ext", req); err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Errorf("action %q: error %v, want one with %q", action, err, wantErr)
		}
	}
	req.Action, req.Set = "fix", "test.ext.go"
	if _, err := f.h.EditorAction(ctx, "test.ext", req); err == nil {
		t.Error("a quick fix of a set that isn't for the file was sent")
	}
	req.Set = "git.marks"
	if _, err := f.h.EditorAction(ctx, "test.ext", req); err == nil || !strings.Contains(err.Error(), "no decoration set") {
		t.Errorf("a foreign set: %v", err)
	}
}

func TestSetTogglesAndStopsClearDecorations(t *testing.T) {
	f := setup(t, nil)
	txt := write(t, f.root, "a.txt", "x")
	if err := f.h.EditorOpened(txt, 1, "x", true); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the start", func() bool { return strings.Contains(f.log(t), "editor/opened") })
	clears := func() []string { return f.app.eventsLike(EventDecorationsCleared) }

	// Another set turned on: the document is announced again with both.
	f.app.setSet("test.ext.extra", true)
	f.h.SetsChanged()
	waitFor(t, "editor/opened with both sets", func() bool {
		return strings.Contains(f.log(t), `"sets":["test.ext.marks","test.ext.extra"]`)
	})
	if err := f.exec(t, "decorate", map[string]any{"set": "test.ext.extra", "path": txt, "count": 1}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "decorations of the set turned on", func() bool { return len(f.app.eventsLike(EventDecorations+" ")) == 1 })

	// Both turned off: the document closes for the extension and its
	// decorations there are removed.
	f.app.setSet("test.ext.extra", false)
	f.app.setSet("test.ext.marks", false)
	f.h.SetsChanged()
	waitFor(t, "editor/closed", func() bool { return strings.Contains(f.log(t), `editor/closed {"path":"`+txt+`"}`) })
	if c := clears(); len(c) != 1 || !strings.HasSuffix(c[0], `[{"extension":"test.ext","path":"`+txt+`"}]`) {
		t.Errorf("cleared = %v", c)
	}
	if err := f.exec(t, "decorate", map[string]any{"set": "test.ext.marks", "path": txt, "count": 1}); err != nil {
		t.Fatal(err)
	}

	// Back on, then a crash: the decorations go, and the extension is
	// started again for the open document.
	f.app.setSet("test.ext.marks", true)
	f.h.SetsChanged()
	_ = f.exec(t, "crash", nil)
	waitFor(t, "the clear after the crash", func() bool {
		c := clears()
		return len(c) == 2 && strings.HasSuffix(c[1], `[{"extension":"test.ext"}]`)
	})
	waitFor(t, "the restart for the open document", func() bool {
		return strings.Count(f.log(t), "initialize ") == 2 && strings.Count(f.log(t), `editor/opened {"language":"","path":"`+txt+`","sets":["test.ext.marks"]`) >= 2
	})

	// Turned off in Settings: stopped, decorations removed.
	f.app.mu.Lock()
	f.app.disabled["test.ext"] = true
	f.app.mu.Unlock()
	f.h.SettingsChanged("test.ext")
	waitFor(t, "the clear after turning it off", func() bool { return len(clears()) >= 3 })
	if f.state() != StateDisabled {
		t.Errorf("state = %s", f.state())
	}
	if n := len(f.app.eventsLike(EventDecorations + " ")); n != 1 {
		t.Errorf("%d decoration events, want 1: a set that is off was drawn", n)
	}

	// On again: it starts for the document that is still open.
	f.app.mu.Lock()
	f.app.disabled["test.ext"] = false
	f.app.mu.Unlock()
	f.h.SettingsChanged("test.ext")
	waitFor(t, "the start after turning it on", func() bool { return strings.Count(f.log(t), "initialize ") == 3 })

	// Uninstalled: stopped and cleared.
	before := len(clears())
	if err := f.h.Uninstall("test.ext"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the clear after uninstalling", func() bool { return len(clears()) > before })
}

func TestCapabilitiesAndInstallMessageNameDecorations(t *testing.T) {
	f := setup(t, nil)
	if err := f.exec(t, "status", nil); err != nil {
		t.Fatal(err)
	}
	if log := f.log(t); !strings.Contains(log, `"decorations":["diagnostic","gutter","inline","highlight"]`) || !strings.Contains(log, `"quickFixes":true`) {
		t.Errorf("initialize doesn't announce decorations:\n%s", log)
	}
	asked := f.app.asked[0]
	for _, want := range []string{
		"Marks lines in editors: Marks, Extra marks, Go marks (it learns which matching files are open in trusted folders, and sees their text as you type, unsaved edits included)",
		"Starts: when you first use it, or open a file it marks",
	} {
		if !strings.Contains(asked, want) {
			t.Errorf("the confirmation lacks %q:\n%s", want, asked)
		}
	}
	sets := f.h.List()[0].DecorationSets
	if len(sets) != 3 || sets[0] != (DecorationSetInfo{ID: "test.ext.marks", Title: "Marks", Default: true, Enabled: true, Files: "*.txt", UnsavedText: true}) || sets[1].Default || sets[1].Enabled {
		t.Errorf("sets = %+v", sets)
	}
}
