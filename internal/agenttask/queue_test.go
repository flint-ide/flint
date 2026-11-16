package agenttask

import (
	"encoding/json"
	"math"
	"testing"

	as "flint/internal/agentsession"
)

func TestSlots(t *testing.T) {
	for _, c := range []struct{ limit, active, want int }{
		{3, 0, 3},
		{3, 2, 1},
		{3, 3, 0},
		{3, 5, 0}, // tasks the user started by hand may exceed the limit
		{1, 0, 1},
		{0, 100, math.MaxInt}, // 0: no limit
		{-1, 2, math.MaxInt},
	} {
		if got := Slots(c.limit, c.active); got != c.want {
			t.Errorf("Slots(%d, %d) = %d, want %d", c.limit, c.active, got, c.want)
		}
	}
}

func queuedStore() Store {
	return Store{
		"/b/.git": {
			{ID: "b1", CreatedAt: 20, Queued: true},
			{ID: "b2", CreatedAt: 5},
		},
		"/a/.git": {
			{ID: "a1", CreatedAt: 30, Queued: true},
			{ID: "a2", CreatedAt: 30, Queued: true}, // same second: the order they were added in
			{ID: "a3", CreatedAt: 10, Queued: true},
		},
	}
}

func ids(tasks []Task) string {
	out := ""
	for _, t := range tasks {
		out += t.ID + " "
	}
	return out
}

func TestQueuedTasksOldestFirst(t *testing.T) {
	var got []Task
	for _, q := range queuedStore().QueuedTasks() {
		got = append(got, q.Task)
		if want := map[string]string{"a": "/a/.git", "b": "/b/.git"}[q.Task.ID[:1]]; q.Repo != want {
			t.Errorf("%s is kept under %q", q.Task.ID, q.Repo)
		}
	}
	if ids(got) != "a3 b1 a1 a2 " {
		t.Errorf("queue = %s", ids(got))
	}
	if len(Store{}.QueuedTasks()) != 0 {
		t.Error("an empty store has a queue")
	}
}

func TestNextQueued(t *testing.T) {
	s := queuedStore()
	all := func(QueuedTask) bool { return true }
	for _, c := range []struct {
		name     string
		free     int
		eligible func(QueuedTask) bool
		want     string
	}{
		{"no slot", 0, all, ""},
		{"one slot: the oldest", 1, all, "a3 "},
		{"two slots", 2, all, "a3 b1 "},
		{"more slots than tasks", 9, all, "a3 b1 a1 a2 "},
		{"a closed repository is skipped, not waited for", 2, func(q QueuedTask) bool { return q.Repo == "/b/.git" }, "b1 "},
		{"nothing may start", 3, func(QueuedTask) bool { return false }, ""},
		{"no limit", math.MaxInt, all, "a3 b1 a1 a2 "},
	} {
		if got := ids(NextQueued(s, c.free, c.eligible)); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestStoreGroup(t *testing.T) {
	s := Store{
		"/a/.git": {{ID: "a1", Group: "g1"}, {ID: "a2"}, {ID: "a3", Group: "g1"}, {ID: "a4", Group: "g2"}},
		"/b/.git": {{ID: "b1"}},
	}
	if got := ids(s.Group("g1")); got != "a1 a3 " {
		t.Errorf("group g1 = %q", got)
	}
	if got := s.Group(""); got != nil {
		t.Errorf("tasks on their own are no group: %v", got)
	}
	if got := s.Group("nope"); got != nil {
		t.Errorf("unknown group = %v", got)
	}
}

// A state.json written before groups and the queue has neither key.
func TestStoreLoadsOldStateFiles(t *testing.T) {
	old := `{"/r/.git":[{"id":"0123456789abcdef","title":"T","prompt":"p","agent":"claude","branch":"agent/t",
		"base":"main","baseCommit":"abc","worktree":"/wt/t","checkout":"/r","createdAt":7,"state":"finished"}]}`
	var s Store
	if err := json.Unmarshal([]byte(old), &s); err != nil {
		t.Fatal(err)
	}
	_, task, ok := s.Find("0123456789abcdef")
	if !ok || task.Group != "" || task.Queued || task.State != Finished || task.Branch != "agent/t" {
		t.Errorf("old task = %+v", task)
	}
	if len(s.QueuedTasks()) != 0 {
		t.Error("an old task is queued")
	}
	// And a task on its own writes neither key, so older versions read the file.
	data, err := json.Marshal(Store{"/r/.git": {task}})
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string][]map[string]any
	_ = json.Unmarshal(data, &raw)
	for _, k := range []string{"group", "queued"} {
		if _, has := raw["/r/.git"][0][k]; has {
			t.Errorf("%q written for a plain task", k)
		}
	}
	// A grouped, queued task survives a round trip.
	data, _ = json.Marshal(Store{"/r/.git": {{ID: "a1", Group: "g1", Queued: true}}})
	var back Store
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if _, task, _ := back.Find("a1"); task.Group != "g1" || !task.Queued {
		t.Errorf("round trip = %+v", task)
	}
}

func TestBranchName(t *testing.T) {
	for _, c := range []struct {
		prefix, desc, agent string
		grouped             bool
		want                string
	}{
		{"agent/", "Add a Retry button", "claude", false, "agent/add-a-retry-button"},
		{"agent/", "Add a Retry button", "claude", true, "agent/add-a-retry-button-claude"},
		{"", "Fix it", "My Agent", true, "fix-it-my-agent"},
		{"x/", "Fix it", "@@", true, "x/fix-it-task"}, // an id with nothing usable still gives a name
	} {
		if got := BranchName(c.prefix, c.desc, c.agent, c.grouped); got != c.want {
			t.Errorf("BranchName(%q, %q, %q, %v) = %q, want %q", c.prefix, c.desc, c.agent, c.grouped, got, c.want)
		}
	}
	// Two attempts of one agent share the name; Unique tells them apart.
	taken := map[string]bool{}
	var names []string
	for range 3 {
		n, err := Unique(BranchName("agent/", "Fix it", "codex", true), func(b string) bool { return taken[b] })
		if err != nil {
			t.Fatal(err)
		}
		taken[n] = true
		names = append(names, n)
	}
	if names[0] != "agent/fix-it-codex" || names[1] != "agent/fix-it-codex-2" || names[2] != "agent/fix-it-codex-3" {
		t.Errorf("attempts = %v", names)
	}
}

func TestTrackerActive(t *testing.T) {
	folders := map[string]string{"/wt/a": "a", "/wt/b": "b", "/wt/c": "c"}
	tr := NewTracker(func(cwd string) string { return folders[cwd] }, func(string, Live) {})
	tr.Bind("s1", "/wt/a")
	tr.Bind("s2", "/wt/b")
	tr.Bind("s3", "/wt/c")
	tr.Bind("s4", "/elsewhere")
	if got := tr.Active(); len(got) != 0 {
		t.Errorf("idle chats are active: %v", got)
	}
	tr.Prompted("s1")
	tr.Prompted("s2")
	tr.Observe(as.Event{SessionID: "s2", Kind: as.KindPermission}) // waiting still holds its slot
	if got := tr.Active(); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("active = %v", got)
	}
	tr.Observe(as.Event{SessionID: "s1", Kind: as.KindTurnEnd, StopReason: as.StopEndTurn})
	if got := tr.Active(); len(got) != 1 || got[0] != "b" {
		t.Errorf("after a turn ended: %v", got)
	}
	tr.Unbind("s2")
	if got := tr.Active(); len(got) != 0 {
		t.Errorf("after the chat closed: %v", got)
	}
	var none *Tracker
	if none.Active() != nil {
		t.Error("a nil tracker has active tasks")
	}
}
