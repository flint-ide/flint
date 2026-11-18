package backlog

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"flint/internal/feature"
)

// depFixture is a backlog where "w" (repository api) waits for "dep".
func depFixture(t *testing.T, extra map[string]string) *fixture {
	t.Helper()
	items := map[string]string{
		"dep": item("Dependency", "todo", "repos: api"),
		"w":   item("Waiter", "todo", "repos: api", "after: dep"),
	}
	for k, v := range extra {
		items[k] = v
	}
	f := newFixture(t, items)
	f.open()
	return f
}

func (f *fixture) waits() map[string]Waiting {
	f.t.Helper()
	var w waits
	if raw := f.h.State(stateWaiting); raw != nil {
		if err := json.Unmarshal(raw, &w); err != nil {
			f.t.Fatal(err)
		}
	}
	return w[f.plan]
}

func (f *fixture) startReady() { f.api.startReady(f.t.Context()) }

func (f *fixture) autoStarts() []AutoStart {
	var out []AutoStart
	for _, e := range f.h.Events() {
		if e.Name == EventAutoStart {
			out = append(out, e.Data[0].(AutoStart))
		}
	}
	return out
}

func TestLoadShowsDependencies(t *testing.T) {
	f := depFixture(t, map[string]string{
		"done":   item("Done", "done"),
		"ready":  item("Ready", "todo", "after: done"),
		"loop-a": item("Loop A", "todo", "after: loop-b"),
		"loop-b": item("Loop B", "todo", "after: loop-a"),
		"lost":   item("Lost", "todo", "after: nowhere"),
	})
	b := f.load()
	for _, tc := range []struct {
		id      string
		after   []string
		waiting []string
		ready   bool
		problem string
	}{
		{"dep", []string{}, []string{}, true, ""},
		{"w", []string{"dep"}, []string{"dep"}, false, ""},
		{"ready", []string{"done"}, []string{}, true, ""},
		{"loop-a", []string{"loop-b"}, []string{"loop-b"}, false, "after: a cycle: loop-a → loop-b → loop-a"},
		{"lost", []string{"nowhere"}, []string{}, false, "after: no item nowhere"},
	} {
		v := f.item(b, tc.id)
		if !slices.Equal(v.After, tc.after) || !slices.Equal(v.WaitingFor, tc.waiting) || v.Ready != tc.ready || !strings.HasPrefix(v.DependencyProblem, tc.problem) || v.Waiting != nil {
			t.Errorf("%s: %+v", tc.id, v)
		}
	}
	var problems []string
	for _, p := range b.Problems {
		problems = append(problems, p.File+": "+p.Reason)
	}
	for _, want := range []string{"items/loop-a.md: after: a cycle", "items/loop-b.md: after: a cycle", "items/lost.md: after: no item nowhere"} {
		if !slices.ContainsFunc(problems, func(p string) bool { return strings.HasPrefix(p, want) }) {
			t.Errorf("problem %q missing in %q", want, problems)
		}
	}
}

func TestDelegateNotReady(t *testing.T) {
	f := depFixture(t, map[string]string{"loop": item("Loop", "todo", "after: loop")})
	res, err := f.api.Delegate(f.plan, "w", f.project)
	if err != nil {
		t.Fatal(err)
	}
	if !res.NotReady || !slices.Equal(res.WaitingFor, []string{"dep"}) || len(res.Tasks) != 0 || res.Item.ID != "w" || res.Item.Ready {
		t.Errorf("result %+v", res)
	}
	if len(f.h.Requested()) != 0 || f.read("w") != item("Waiter", "todo", "repos: api", "after: dep") {
		t.Error("a not-ready item was started")
	}
	_, err = f.api.Delegate(f.plan, "loop", f.project)
	wantErr(t, err, "can't start: after: the item names itself")
	// Once its dependency is done, it starts.
	if _, err := f.api.SetStatus(f.plan, "dep", StatusDone); err != nil {
		t.Fatal(err)
	}
	if res := f.delegate("w"); res.NotReady || len(res.Tasks) != 1 || !res.Item.Ready || res.WaitingFor == nil {
		t.Errorf("ready result %+v", res)
	}
}

func TestStartWhenReadyRefusals(t *testing.T) {
	f := depFixture(t, map[string]string{
		"loop":  item("Loop", "todo", "after: loop"),
		"fin":   item("Finished", "done", "after: dep"),
		"local": item("Local", "todo", "after: dep"),
		"bad":   "---\nstatus: todo\n---\n",
	})
	for _, tc := range []struct{ id, project, want string }{
		{"loop", f.project, "names itself"},
		{"fin", f.project, "the item is done"},
		{"local", "", "no project is open"},
		{"local", f.base, "no project is open"},
		{"bad", f.project, "no usable item bad"},
		{"../x", f.project, "invalid item id"},
	} {
		_, err := f.api.StartWhenReady(f.plan, tc.id, tc.project)
		wantErr(t, err, tc.want)
	}
	_, err := f.api.StartWhenReady(f.base, "w", f.project)
	wantErr(t, err, "not an open backlog")
	if len(f.waits()) != 0 {
		t.Errorf("waits %+v", f.waits())
	}
	// The bound.
	w := waits{f.plan: {}}
	for i := range MaxWaiting {
		w[f.plan][strings.Repeat("x", i+1)] = Waiting{}
	}
	if err := f.h.SaveState(stateWaiting, w); err != nil {
		t.Fatal(err)
	}
	_, err = f.api.StartWhenReady(f.plan, "w", f.project)
	wantErr(t, err, "wait to start already")
}

// The waiting item starts once, when the user sets its last dependency to
// done; the project of an item without repositories is the one remembered.
func TestStartWhenReadyOnTheUsersStatus(t *testing.T) {
	f := depFixture(t, map[string]string{"local": item("Local", "todo", "after: dep, w")})
	v, err := f.api.StartWhenReady(f.plan, "w", "")
	if err != nil {
		t.Fatal(err)
	}
	if v.Waiting == nil || v.Waiting.Reason != "" || v.Ready {
		t.Errorf("view %+v", v)
	}
	if _, err := f.api.StartWhenReady(f.plan, "local", f.project); err != nil {
		t.Fatal(err)
	}
	if w := f.waits(); len(w) != 2 || w["w"].Project != "" || w["local"].Project != f.project {
		t.Errorf("waits %+v", w)
	}
	if lv := f.item(f.load(), "w"); lv.Waiting == nil || lv.Waiting.Since == "" {
		t.Errorf("loaded %+v", lv)
	}
	f.startReady() // nothing is ready
	if len(f.h.Requested()) != 0 {
		t.Fatal("started too early")
	}
	if _, err := f.api.SetStatus(f.plan, "dep", StatusDone); err != nil {
		t.Fatal(err)
	}
	f.startReady()
	f.startReady()
	reqs := f.h.Requested()
	if len(reqs) != 1 || reqs[0][0].Dir != f.project || !strings.HasPrefix(reqs[0][0].Description, "Waiter") {
		t.Fatalf("requests %+v", reqs)
	}
	if _, ok := f.waits()["w"]; ok {
		t.Error("the waiting start stayed")
	}
	if !strings.Contains(f.read("w"), "status: doing") || len(f.links("w")) != 1 {
		t.Errorf("file %q links %+v", f.read("w"), f.links("w"))
	}
	if ev := f.autoStarts(); len(ev) != 1 || ev[0].ID != "w" || ev[0].Tasks != 1 || ev[0].Error != "" || ev[0].Backlog != f.plan {
		t.Errorf("events %+v", ev)
	}
	if len(f.h.Asked()) != 0 {
		t.Errorf("a trusted backlog asked: %q", f.h.Asked())
	}
	// "local" waits for w too, which is doing now.
	f.startReady()
	if len(f.h.Requested()) != 1 {
		t.Fatal("local started before w was done")
	}
	if _, err := f.api.SetStatus(f.plan, "w", StatusDone); err != nil {
		t.Fatal(err)
	}
	f.startReady()
	if reqs := f.h.Requested(); len(reqs) != 2 || reqs[1][0].Dir != f.project || !strings.HasPrefix(reqs[1][0].Description, "Local") {
		t.Fatalf("requests %+v", reqs)
	}
}

// Flint moving the dependency to done (its tasks merged) starts the item.
func TestStartWhenReadyOnFlintsStatus(t *testing.T) {
	f := depFixture(t, nil)
	res := f.delegate("dep")
	if _, err := f.api.StartWhenReady(f.plan, "w", ""); err != nil {
		t.Fatal(err)
	}
	f.h.UpdateAgentTask(res.Tasks[0].ID, func(t *feature.AgentTask) { t.State, t.Merged = "finished", true })
	f.api.reconcileAll(t.Context())
	if !strings.Contains(f.read("dep"), "status: done") {
		t.Fatalf("dep %q", f.read("dep"))
	}
	f.startReady()
	if reqs := f.h.Requested(); len(reqs) != 2 || !strings.HasPrefix(reqs[1][0].Description, "Waiter") {
		t.Fatalf("requests %+v", reqs)
	}
}

// Automatic triggers at once start the item once.
func TestStartWhenReadyStartsOnce(t *testing.T) {
	for range 20 {
		f := depFixture(t, map[string]string{"dep": item("Dependency", "done", "repos: api")})
		if _, err := f.api.StartWhenReady(f.plan, "w", ""); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		for range 3 {
			wg.Add(1)
			go func() { defer wg.Done(); f.startReady() }()
		}
		wg.Wait()
		if n := len(f.h.Requested()); n != 1 {
			t.Fatalf("started %d times", n)
		}
		if len(f.waits()) != 0 {
			t.Fatalf("waits %+v", f.waits())
		}
		for _, ev := range f.autoStarts() {
			if ev.Error != "" {
				t.Errorf("event %+v", ev)
			}
		}
	}
}

// While an item is being started, a click on Delegate is refused (and an
// automatic start skips it without recording a failure).
func TestDelegateWhileStarting(t *testing.T) {
	f := depFixture(t, map[string]string{"dep": item("Dependency", "done", "repos: api")})
	if _, err := f.api.StartWhenReady(f.plan, "w", ""); err != nil {
		t.Fatal(err)
	}
	f.api.starting[f.plan+"\x00w"] = true
	_, err := f.api.Delegate(f.plan, "w", f.project)
	wantErr(t, err, "being started already")
	f.startReady()
	if len(f.h.Requested()) != 0 || f.waits()["w"].Reason != "" || len(f.autoStarts()) != 0 {
		t.Fatalf("requests %+v waits %+v", f.h.Requested(), f.waits())
	}
	delete(f.api.starting, f.plan+"\x00w")
	f.startReady()
	if len(f.h.Requested()) != 1 {
		t.Fatal("not started once free")
	}
}

// A start that fails keeps the item waiting with the reason; it is tried
// again only once something it depends on changed.
func TestRefusedStartKeepsWaiting(t *testing.T) {
	f := depFixture(t, map[string]string{"dep": item("Dependency", "done", "repos: api"), "w": item("Waiter", "todo", "repos: api", "after: dep", "agent: planner")})
	if _, err := f.api.StartWhenReady(f.plan, "w", ""); err != nil {
		t.Fatal(err)
	}
	f.startReady()
	w := f.waits()["w"]
	if !strings.Contains(w.Reason, `the agent "planner" is not available`) || w.Tried == "" || len(f.h.Requested()) != 0 {
		t.Fatalf("waiting %+v", w)
	}
	if v := f.item(f.load(), "w"); v.Waiting == nil || v.Waiting.Reason != w.Reason {
		t.Errorf("view %+v", v)
	}
	if ev := f.autoStarts(); len(ev) != 1 || ev[0].Error != w.Reason || ev[0].Tasks != 0 {
		t.Errorf("events %+v", ev)
	}
	// Nothing changed: not tried again (no loop through backlog:changed).
	for range 3 {
		f.startReady()
	}
	if len(f.autoStarts()) != 1 {
		t.Fatalf("tried again: %+v", f.autoStarts())
	}
	// The agent appears: it starts.
	f.h.SetAgents("claude", "claude", "backend", "planner")
	f.startReady()
	if len(f.h.Requested()) != 1 || len(f.waits()) != 0 {
		t.Fatalf("requests %+v waits %+v", f.h.Requested(), f.waits())
	}
}

// An untrusted backlog's waiting item is confirmed natively at the time it
// starts, by a question that names the item and why it starts now; a no
// keeps it waiting and is not asked again until something changes.
func TestAutoStartFromAnUntrustedBacklog(t *testing.T) {
	f := newFixture(t, nil)
	f.open()
	other := newBacklog(t, filepath.Join(f.base, "other"), map[string]string{"dep": item("Dependency", "todo", "repos: api"), "w": item("Waiter", "todo", "repos: api", "after: dep")})
	if _, err := f.api.Open(other); err != nil {
		t.Fatal(err)
	}
	if _, err := f.api.StartWhenReady(other, "w", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.api.SetStatus(other, "dep", StatusDone); err != nil {
		t.Fatal(err)
	}
	f.h.Answer = false
	f.startReady()
	asked := f.h.Asked()
	if len(asked) != 1 || !strings.HasPrefix(asked[0], "Start a waiting item of an untrusted backlog?") ||
		!strings.Contains(asked[0], `the backlog item "Waiter" (w) by itself`) || !strings.Contains(asked[0], "(dep) are done now") ||
		!strings.Contains(asked[0], "Description:\nWaiter") {
		t.Fatalf("asked %q", asked)
	}
	var w waits
	_ = f.h.LoadState(stateWaiting, &w)
	if !strings.Contains(w[other]["w"].Reason, "declined") || len(f.h.Requested()) != 0 {
		t.Fatalf("waiting %+v", w)
	}
	f.startReady()
	if len(f.h.Asked()) != 1 {
		t.Fatal("asked again although nothing changed")
	}
	// Trusting the backlog changes what the start depends on: it starts.
	f.h.SetTrusted(other)
	f.startReady()
	if len(f.h.Requested()) != 1 || len(f.h.Asked()) != 1 {
		t.Fatalf("requests %+v asked %d", f.h.Requested(), len(f.h.Asked()))
	}
	// Yes to the question starts it too.
	g := newFixture(t, nil)
	g.open()
	third := newBacklog(t, filepath.Join(g.base, "third"), map[string]string{"dep": item("Dependency", "done", "repos: api"), "w": item("Waiter", "todo", "repos: api", "after: dep")})
	if _, err := g.api.Open(third); err != nil {
		t.Fatal(err)
	}
	g.h.Answer = true
	if _, err := g.api.StartWhenReady(third, "w", ""); err != nil {
		t.Fatal(err)
	}
	g.startReady()
	if len(g.h.Requested()) != 1 || len(g.h.Asked()) != 1 {
		t.Fatalf("requests %+v asked %q", g.h.Requested(), g.h.Asked())
	}
}

func TestCancelStart(t *testing.T) {
	f := depFixture(t, nil)
	wantErr(t, f.api.CancelStart(f.plan, "w"), "not waiting")
	if _, err := f.api.StartWhenReady(f.plan, "w", ""); err != nil {
		t.Fatal(err)
	}
	if err := f.api.CancelStart(f.plan, "w"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.api.SetStatus(f.plan, "dep", StatusDone); err != nil {
		t.Fatal(err)
	}
	f.startReady()
	if len(f.h.Requested()) != 0 || len(f.waits()) != 0 {
		t.Fatal("a cancelled start ran")
	}
	wantErr(t, f.api.CancelStart(f.plan, "../w"), "invalid item id")
}

// An item set to done by hand while waiting is not started.
func TestWaitingItemDoneIsDropped(t *testing.T) {
	f := depFixture(t, nil)
	if _, err := f.api.StartWhenReady(f.plan, "w", ""); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"w", "dep"} {
		if _, err := f.api.SetStatus(f.plan, id, StatusDone); err != nil {
			t.Fatal(err)
		}
	}
	f.startReady()
	if len(f.h.Requested()) != 0 || len(f.waits()) != 0 {
		t.Fatal("a done item started")
	}
}

func TestCloseForgetsWaitingStarts(t *testing.T) {
	f := depFixture(t, nil)
	if _, err := f.api.StartWhenReady(f.plan, "w", ""); err != nil {
		t.Fatal(err)
	}
	if err := f.api.Close(f.plan); err != nil {
		t.Fatal(err)
	}
	if len(f.waits()) != 0 {
		t.Fatalf("waits %+v", f.waits())
	}
	// Opened again: nothing waits, nothing starts.
	f.open()
	if _, err := f.api.SetStatus(f.plan, "dep", StatusDone); err != nil {
		t.Fatal(err)
	}
	f.startReady()
	if len(f.h.Requested()) != 0 {
		t.Fatal("a forgotten start ran")
	}
}

// The starter goroutine runs when the user sets a status.
func TestStarterRunsOnStatusChanges(t *testing.T) {
	f := depFixture(t, nil)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go f.api.watch(ctx)
	go f.api.starter(ctx)
	if _, err := f.api.StartWhenReady(f.plan, "w", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.api.SetStatus(f.plan, "dep", StatusDone); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(f.h.Requested()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(f.h.Requested()) != 1 {
		t.Fatal("the starter did not start the item")
	}
}
