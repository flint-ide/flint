package backlog

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"flint/internal/feature"
)

// withWeb gives the fixture a checkout of acme/web inside the workspace,
// trusted unless untrusted.
func (f *fixture) withWeb(trusted bool) string {
	f.t.Helper()
	web := filepath.Join(f.base, "web")
	if err := os.MkdirAll(web, 0o755); err != nil {
		f.t.Fatal(err)
	}
	f.h.SetRepository(web, "git@github.com:acme/web.git")
	f.h.SetRoots(f.project, web)
	if trusted {
		f.h.SetTrusted(web)
	}
	return web
}

func (f *fixture) links(id string) []Link {
	f.t.Helper()
	var ls links
	if raw := f.h.State(stateTasks); raw != nil {
		if err := json.Unmarshal(raw, &ls); err != nil {
			f.t.Fatal(err)
		}
	}
	return ls[f.plan][id]
}

func (f *fixture) delegate(id string) DelegateResult {
	f.t.Helper()
	res, err := f.api.Delegate(f.plan, id, f.project)
	if err != nil {
		f.t.Fatal(err)
	}
	return res
}

func TestDelegateOneRepository(t *testing.T) {
	f := newFixture(t, map[string]string{"rl": "---\ntitle: Rate limit\nagent: backend\nrepos: api\nowner: me\n---\nLimit it.\n"})
	f.open()
	res := f.delegate("rl")
	reqs := f.h.Requested()
	if len(reqs) != 1 || len(reqs[0]) != 1 || reqs[0][0] != (feature.NewAgentTask{Dir: f.project, Description: "Rate limit\n\nLimit it.", Agent: "backend"}) {
		t.Fatalf("requests %+v", reqs)
	}
	if res.Warning != "" || res.Item.Status != StatusDoing || len(res.Tasks) != 1 || res.Tasks[0].Repo != "api" || res.Tasks[0].Worktree == "" || len(res.Item.Tasks) != 1 {
		t.Errorf("result %+v", res)
	}
	if got := f.read("rl"); got != "---\ntitle: Rate limit\nagent: backend\nrepos: api\nowner: me\nstatus: doing\n---\nLimit it.\n" {
		t.Errorf("file %q", got)
	}
	if ls := f.links("rl"); len(ls) != 1 || ls[0].Task != res.Tasks[0].ID || ls[0].Repo != "api" {
		t.Errorf("links %+v", ls)
	}
	if v := f.item(f.load(), "rl"); len(v.Tasks) != 1 || v.Tasks[0].State != "working" || v.Failed {
		t.Errorf("loaded %+v", v)
	}
}

func TestDelegateSeveralRepositoriesAndDefaultAgent(t *testing.T) {
	f := newFixture(t, map[string]string{"both": item("Both", "todo", "repos: api, web")})
	web := f.withWeb(true)
	f.open()
	res := f.delegate("both")
	reqs := f.h.Requested()
	if len(reqs) != 1 || len(reqs[0]) != 2 {
		t.Fatalf("requests %+v", reqs)
	}
	if reqs[0][0].Dir != f.project || reqs[0][1].Dir != web || reqs[0][0].Agent != "claude" ||
		!strings.Contains(reqs[0][0].Description, "works in api only; web is handled") ||
		!strings.Contains(reqs[0][1].Description, "works in web only; api is handled") {
		t.Errorf("requests %+v", reqs[0])
	}
	if len(res.Tasks) != 2 || res.Tasks[0].Repo != "api" || res.Tasks[1].Repo != "web" {
		t.Errorf("tasks %+v", res.Tasks)
	}
}

func TestDelegateNoRepositoryUsesTheProject(t *testing.T) {
	f := newFixture(t, map[string]string{"x": item("X", "todo")})
	f.open()
	for project, want := range map[string]string{
		"":                              "no project is open",
		f.base:                          "not an open project folder",
		filepath.Join(f.project, "sub"): "not an open project folder",
	} {
		_, err := f.api.Delegate(f.plan, "x", project)
		wantErr(t, err, want)
	}
	untrusted := filepath.Join(f.base, "untrusted")
	f.h.SetRoots(f.project, untrusted)
	_, err := f.api.Delegate(f.plan, "x", untrusted)
	wantErr(t, err, "Restricted Mode")
	if len(f.h.Requested()) != 0 {
		t.Fatal("tasks were requested")
	}
	res := f.delegate("x")
	if reqs := f.h.Requested(); len(reqs) != 1 || reqs[0][0].Dir != f.project || res.Tasks[0].Repo != "" {
		t.Errorf("requests %+v", reqs)
	}
}

func TestDelegateRefusals(t *testing.T) {
	f := newFixture(t, map[string]string{
		"fe":   item("FE", "todo", "agent: frontend"),
		"web":  item("Web", "todo", "repos: web"),
		"both": item("Both", "todo", "repos: api, web"),
		"bad":  "---\ntitle: Bad\nstatus: nope\n---\n",
	})
	_, err := f.api.Delegate(f.plan, "web", f.project)
	wantErr(t, err, "not an open backlog")
	f.open()
	_, err = f.api.Delegate(f.plan, "fe", f.project)
	wantErr(t, err, `agent "frontend" is not available`)
	_, err = f.api.Delegate(f.plan, "web", f.project)
	wantErr(t, err, "no checkout of web was found")
	_, err = f.api.Delegate(f.plan, "both", f.project)
	wantErr(t, err, "no checkout of web")
	_, err = f.api.Delegate(f.plan, "bad", f.project)
	wantErr(t, err, "status")
	_, err = f.api.Delegate(f.plan, "../web", f.project)
	wantErr(t, err, "invalid item id")
	_, err = f.api.Delegate(f.plan, "absent", f.project)
	wantErr(t, err, "no such file")

	// Located, but not attached to the workspace.
	web := filepath.Join(f.base, "elsewhere", "web")
	if err := os.MkdirAll(web, 0o755); err != nil {
		t.Fatal(err)
	}
	f.h.SetRepository(web, "https://github.com/acme/web")
	if _, err := f.api.Locate(f.plan, "web", web); err != nil {
		t.Fatal(err)
	}
	_, err = f.api.Delegate(f.plan, "both", f.project)
	wantErr(t, err, "to the workspace first")
	// Attached, but in Restricted Mode.
	f.h.SetRoots(f.project, filepath.Join(f.base, "elsewhere"))
	_, err = f.api.Delegate(f.plan, "both", f.project)
	wantErr(t, err, "Restricted Mode")
	if len(f.h.Requested()) != 0 {
		t.Fatalf("tasks were requested: %+v", f.h.Requested())
	}
	// Creating fails: nothing is remembered and the status stays.
	f.h.SetTrusted(filepath.Join(f.base, "elsewhere"))
	f.h.SetCreateError(errors.New("worktree failed"))
	_, err = f.api.Delegate(f.plan, "both", f.project)
	wantErr(t, err, "worktree failed")
	if len(f.links("both")) != 0 || !strings.Contains(f.read("both"), "status: todo") {
		t.Error("a failed Delegate left links or a status")
	}
	f.h.SetCreateError(nil)
	if res := f.delegate("both"); len(res.Tasks) != 2 {
		t.Errorf("tasks %+v", res.Tasks)
	}
}

func TestDelegateAgainAddsTasks(t *testing.T) {
	f := newFixture(t, map[string]string{"x": item("X", "todo", "repos: api")})
	f.open()
	first := f.delegate("x")
	f.h.UpdateAgentTask(first.Tasks[0].ID, func(t *feature.AgentTask) { t.State, t.Error = "failed", "boom" })
	v := f.item(f.load(), "x")
	if v.Status != StatusDoing || !v.Failed || v.Tasks[0].Error != "boom" {
		t.Errorf("failed task: %+v", v)
	}
	second := f.delegate("x")
	if len(second.Item.Tasks) != 2 || len(second.Tasks) != 1 || second.Tasks[0].ID == first.Tasks[0].ID {
		t.Errorf("second Delegate: %+v", second)
	}
	// The failed attempt is removed; the new one finishes: review.
	f.h.RemoveAgentTask(first.Tasks[0].ID)
	f.h.UpdateAgentTask(second.Tasks[0].ID, func(t *feature.AgentTask) { t.State = "finished" })
	v = f.item(f.load(), "x")
	if v.Status != StatusReview || v.Failed || !v.Tasks[0].Gone || v.Tasks[0].State != "failed" {
		t.Errorf("after the retry: %+v", v)
	}

	// The item's tasks are bounded.
	ls := links{f.plan: {"x": make([]Link, MaxTasksPerItem)}}
	if err := f.h.SaveState(stateTasks, ls); err != nil {
		t.Fatal(err)
	}
	_, err := f.api.Delegate(f.plan, "x", f.project)
	wantErr(t, err, "remove some")
}

func TestStatusFollowsTasks(t *testing.T) {
	f := newFixture(t, map[string]string{"x": "---\ntitle: X\r\nrepos: api, web\r\n---\r\nText\r\n"})
	f.withWeb(true)
	f.open()
	res := f.delegate("x")
	a, w := res.Tasks[0].ID, res.Tasks[1].ID
	status := func() string { return f.item(f.load(), "x").Status }

	f.h.UpdateAgentTask(a, func(t *feature.AgentTask) { t.State = "finished" })
	if got := status(); got != StatusDoing {
		t.Fatalf("one of two finished: %s", got)
	}
	f.h.UpdateAgentTask(w, func(t *feature.AgentTask) { t.State = "finished" })
	if got := status(); got != StatusReview {
		t.Fatalf("both finished: %s", got)
	}
	if got := f.read("x"); got != "---\ntitle: X\r\nrepos: api, web\r\nstatus: review\r\n---\r\nText\r\n" {
		t.Errorf("file %q", got)
	}
	// One is merged; the other gets a pull request and is removed.
	f.h.UpdateAgentTask(a, func(t *feature.AgentTask) { t.Merged = true })
	f.h.UpdateAgentTask(w, func(t *feature.AgentTask) { t.PullRequest = "https://github.com/acme/web/pull/7" })
	if got := status(); got != StatusReview {
		t.Fatalf("pull request still open: %s", got)
	}
	f.h.RemoveAgentTask(w)
	if got := status(); got != StatusDone {
		t.Fatalf("merged + removed after its pull request: %s", got)
	}
	v := f.item(f.load(), "x")
	if !v.Tasks[1].Gone || v.Tasks[1].PullRequest == "" || !v.Tasks[0].Merged {
		t.Errorf("tasks %+v", v.Tasks)
	}
	// The user moves it back: nothing moves it again by itself.
	if _, err := f.api.SetStatus(f.plan, "x", StatusTodo); err != nil {
		t.Fatal(err)
	}
	if got := status(); got != StatusTodo {
		t.Errorf("after the user's change: %s", got)
	}
	// An item whose file is gone loses its links.
	if err := os.Remove(ItemFile(f.plan, "x")); err != nil {
		t.Fatal(err)
	}
	f.load()
	if raw := f.h.State(stateTasks); strings.Contains(string(raw), a) {
		t.Errorf("links kept: %s", raw)
	}
}

// Agent tasks that change while the panel is not loading move items too:
// TasksChanged wakes the watcher, which tells the UI.
func TestWatcherMovesItems(t *testing.T) {
	f := newFixture(t, map[string]string{"x": item("X", "todo", "repos: api")})
	f.open()
	res := f.delegate("x")
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { f.api.watch(ctx); close(done) }()
	defer func() { cancel(); <-done }()

	f.h.UpdateAgentTask(res.Tasks[0].ID, func(t *feature.AgentTask) { t.State = "finished" })
	before := len(f.h.Events())
	f.api.tasksChanged()
	f.api.tasksChanged() // a burst wakes it once more at most, never blocks
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(f.read("x"), "status: review") {
		if time.Now().After(deadline) {
			t.Fatal("the watcher did not move the item")
		}
		time.Sleep(10 * time.Millisecond)
	}
	for time.Now().Before(deadline) {
		ev := f.h.Events()
		if len(ev) > before && ev[len(ev)-1].Name == EventChanged && ev[len(ev)-1].Data[0] == f.plan {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("no backlog:changed event")
}
