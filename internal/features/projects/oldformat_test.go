package projects

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"flint/internal/feature"
)

// oldFixture is the fixture with its project in the old format: the folder
// has flint-backlog.json instead of flint-project.json.
func oldFixture(t *testing.T, items map[string]string) *fixture {
	t.Helper()
	f := newFixture(t, items)
	if err := os.Rename(filepath.Join(f.plan, ConfigFile), filepath.Join(f.plan, OldConfigFile)); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestOldFormatProjectIsListedAndReadOnly(t *testing.T) {
	f := oldFixture(t, map[string]string{"a": item("Alpha", "todo"), "b": item("Bravo", "done")})
	// Remembered (after the state migration) or opened with Open Project….
	info, err := f.api.Open(f.plan)
	if err != nil || !info.OldFormat || info.Name != "Acme" {
		t.Fatalf("Open = %+v, %v", info, err)
	}
	if list := f.api.Projects(); len(list) != 1 || !list[0].OldFormat || list[0].Error != "" {
		t.Errorf("Projects = %+v", list)
	}
	p := f.load()
	if !p.OldFormat || len(p.Items) != 2 || len(p.Problems) != 0 {
		t.Errorf("Load = %+v", p)
	}
	before := f.read("a")

	_, err = f.api.NewItem(f.plan, "New")
	wantErr(t, err, "old format")
	_, err = f.api.SetStatus(f.plan, "a", StatusDoing)
	wantErr(t, err, "Rename flint-backlog.json to flint-project.json")
	_, err = f.api.Delegate(f.plan, "a", f.root)
	wantErr(t, err, "old format")
	_, err = f.api.StartWhenReady(f.plan, "a", f.root)
	wantErr(t, err, "old format")
	_, err = f.api.AddContext(f.plan)
	wantErr(t, err, "old format")
	_, err = f.api.PlanPrompt(f.plan, "Ship it")
	wantErr(t, err, "old format")
	if len(f.h.Requested()) != 0 || f.read("a") != before {
		t.Errorf("something was started or written: %v", f.h.Requested())
	}
	if _, err := os.Stat(filepath.Join(f.plan, ContextFile)); err == nil {
		t.Error("PROJECT.md was written")
	}

	// The agent tools: get_project names it without its items, and
	// add_project_item refuses it.
	out := f.api.describe("", "")
	if !strings.Contains(out, "old format") || strings.Contains(out, "Alpha") {
		t.Errorf("get_project:\n%s", out)
	}
	f.allowAdd()
	_, err = f.add(`{"project":"Acme","title":"New","text":""}`)
	wantErr(t, err, "old format")
	if _, err := os.Stat(ItemFile(f.plan, "new")); err == nil {
		t.Error("add_project_item wrote an item")
	}

	// Automatic starts and status changes wait for the rename: a waiting
	// start (from a project that had both files once) and a linked task
	// (from main's backlogTasks) change nothing.
	w := waits{f.plan: {"a": {Since: time.Now(), Hash: itemHash([]byte(before))}}}
	if err := f.h.SaveState(stateWaiting, w); err != nil {
		t.Fatal(err)
	}
	f.h.SetAgentTask(feature.AgentTask{ID: "t1", State: "finished"})
	if err := f.h.SaveState(stateTasks, links{f.plan: {"a": {{Task: "t1", State: "working"}}}}); err != nil {
		t.Fatal(err)
	}
	f.startReady()
	f.api.reconcileAll(t.Context())
	if v := f.item(f.load(), "a"); v.Status != StatusTodo || len(f.h.Requested()) != 0 || f.read("a") != before {
		t.Errorf("after the starter and reconcile: %+v, %v", v, f.h.Requested())
	}

	// New Project… on the folder points to the rename instead of writing a
	// second file.
	_, err = f.api.Create(f.plan, "Other")
	wantErr(t, err, "old format")
	if _, err := os.Stat(filepath.Join(f.plan, ConfigFile)); err == nil {
		t.Error("Create wrote flint-project.json")
	}
}

func TestOldFormatWorkspaceFolderIsListed(t *testing.T) {
	f := oldFixture(t, map[string]string{"a": item("Alpha", "todo")})
	f.h.SetRoots(f.root, f.plan)
	list := f.api.Projects()
	if len(list) != 1 || !list[0].Workspace || !list[0].OldFormat {
		t.Fatalf("Projects = %+v", list)
	}
}

func TestRenameOldConfig(t *testing.T) {
	f := oldFixture(t, map[string]string{"a": item("Alpha", "todo")})
	f.open()
	old, err := os.ReadFile(filepath.Join(f.plan, OldConfigFile))
	if err != nil {
		t.Fatal(err)
	}

	// Declined: nothing changes.
	_, err = f.api.RenameOldConfig(f.plan)
	wantErr(t, err, "cancelled")
	if _, err := os.Stat(filepath.Join(f.plan, OldConfigFile)); err != nil {
		t.Fatal("the old file is gone after a declined rename")
	}
	if asked := f.h.Asked(); len(asked) != 1 || !strings.Contains(asked[0], "Rename flint-backlog.json to flint-project.json") {
		t.Errorf("asked %q", asked)
	}

	// Confirmed: the file is renamed, its content untouched, and the project
	// is writable.
	f.h.Answer = true
	info, err := f.api.RenameOldConfig(f.plan)
	if err != nil || info.OldFormat || info.Name != "Acme" {
		t.Fatalf("RenameOldConfig = %+v, %v", info, err)
	}
	now, err := os.ReadFile(filepath.Join(f.plan, ConfigFile))
	if err != nil || string(now) != string(old) {
		t.Errorf("flint-project.json = %q, %v; want %q", now, err, old)
	}
	if _, err := os.Lstat(filepath.Join(f.plan, OldConfigFile)); err == nil {
		t.Error("the old file is still there")
	}
	if p := f.load(); p.OldFormat {
		t.Error("still in the old format")
	}
	if _, err := f.api.SetStatus(f.plan, "a", StatusDoing); err != nil {
		t.Errorf("SetStatus after the rename: %v", err)
	}

	// Nothing to rename any more.
	_, err = f.api.RenameOldConfig(f.plan)
	wantErr(t, err, "already")
}

func TestRenameOldConfigRefusesAnExistingNewFile(t *testing.T) {
	f := oldFixture(t, map[string]string{"a": item("Alpha", "todo")})
	f.open()
	f.h.Answer = true
	// flint-project.json appears while the question is open.
	newFile := filepath.Join(f.plan, ConfigFile)
	f.h.OnConfirm = func() { write(t, newFile, `{"name":"New"}`) }
	_, err := f.api.RenameOldConfig(f.plan)
	wantErr(t, err, "already")
	if data, _ := os.ReadFile(newFile); string(data) != `{"name":"New"}` {
		t.Errorf("flint-project.json was replaced: %s", data)
	}
	if _, err := os.Stat(filepath.Join(f.plan, OldConfigFile)); err != nil {
		t.Error("the old file is gone")
	}
	// renameNoReplace itself never replaces a file.
	err = renameNoReplace(filepath.Join(f.plan, OldConfigFile), newFile)
	wantErr(t, err, "exists already")
}

func TestBothConfigFilesTheNewOneWins(t *testing.T) {
	f := newFixture(t, map[string]string{"a": item("Alpha", "todo")})
	write(t, filepath.Join(f.plan, OldConfigFile), `{"name":"Old"}`)
	f.open()
	p := f.load()
	if p.OldFormat || p.Name != "Acme" {
		t.Errorf("Load = %+v", p)
	}
	if len(p.Problems) != 1 || p.Problems[0].File != OldConfigFile || !strings.Contains(p.Problems[0].Reason, "left over") {
		t.Errorf("problems = %+v", p.Problems)
	}
	if _, err := f.api.SetStatus(f.plan, "a", StatusDoing); err != nil {
		t.Errorf("SetStatus: %v", err)
	}
	f.h.Answer = true
	_, err := f.api.RenameOldConfig(f.plan)
	wantErr(t, err, "already")
}
