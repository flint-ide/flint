package agenttask

import (
	"encoding/json"
	"slices"
	"testing"
)

// jointStore holds a joint group j of three repositories (lead a1), a task
// on its own (a2) and a queued task on its own (c1, older than the group).
func jointStore(queued bool) Store {
	return Store{
		"/a/.git": {{ID: "a1", Joint: "j", Lead: true, Worktree: "/wt/a1", CreatedAt: 10, Queued: queued}, {ID: "a2", Worktree: "/wt/a2", CreatedAt: 11}},
		"/b/.git": {{ID: "b1", Joint: "j", Worktree: "/wt/b1", CreatedAt: 10, Queued: queued}},
		"/c/.git": {{ID: "c1", Worktree: "/wt/c1", CreatedAt: 5, Queued: true}, {ID: "c2", Joint: "j", Worktree: "/wt/c2", CreatedAt: 10, Queued: queued}},
	}
}

func TestStoreJoint(t *testing.T) {
	s := jointStore(false)
	if got := ids(s.Joint("j")); got != "a1 b1 c2 " {
		t.Errorf("Joint = %q", got)
	}
	if s.Joint("") != nil || s.Joint("nope") != nil {
		t.Error("no group has members")
	}
	for _, c := range []struct{ path, want string }{
		{"/wt/a1", "a1 b1 c2"}, // the lead's folder: the whole group
		{"/wt/b1", "b1"},       // a member's own folder: that task
		{"/wt/a2", "a2"},
		{"/elsewhere", ""},
	} {
		if got := s.ChatTasks(c.path); joinIDs(got) != c.want {
			t.Errorf("ChatTasks(%s) = %v, want %s", c.path, got, c.want)
		}
	}
}

func joinIDs(ids []string) string {
	out := ""
	for i, id := range ids {
		if i > 0 {
			out += " "
		}
		out += id
	}
	return out
}

func TestStoreNormalizeJoint(t *testing.T) {
	// The lead was removed: the next one leads.
	s := jointStore(false).Remove("a1").NormalizeJoint("j")
	if g := s.Joint("j"); len(g) != 2 || g[0].ID != "b1" || !g[0].Lead || g[1].Lead {
		t.Errorf("after removing the lead: %+v", g)
	}
	// One left: no group any more.
	s = s.Remove("c2").NormalizeJoint("j")
	if _, b1, _ := s.Find("b1"); b1.Joint != "" || b1.Lead || s.Joint("j") != nil {
		t.Errorf("a group of one = %+v", b1)
	}
	// A member that is not the lead: nothing changes.
	s = jointStore(false).Remove("b1").NormalizeJoint("j")
	if got := ids(s.Joint("j")); got != "a1 c2 " {
		t.Errorf("after removing a member: %s", got)
	}
}

func TestNextQueuedJointGroupIsOneUnit(t *testing.T) {
	s := jointStore(true)
	all := func(QueuedTask) bool { return true }
	for _, c := range []struct {
		name     string
		store    Store
		free     int
		eligible func(QueuedTask) bool
		want     string
	}{
		{"one slot: the oldest", s, 1, all, "c1 "},
		{"two slots: the group takes one, as its lead", s, 2, all, "c1 a1 "},
		{"more slots than units", s, 9, all, "c1 a1 "},
		{"a member that may not start holds the group back", s, 2, func(q QueuedTask) bool { return q.Repo != "/b/.git" }, "c1 "},
		{"the members' repositories are passed", s, 2, func(q QueuedTask) bool {
			return q.Task.ID != "c2" || q.Repo == "/c/.git"
		}, "c1 a1 "},
		{"one member queued is enough", s.Update("a1", func(t Task) Task { t.Queued = false; return t }).
			Update("c2", func(t Task) Task { t.Queued = false; return t }), 2, all, "c1 a1 "},
		{"not queued", jointStore(false), 2, all, "c1 "},
	} {
		if got := ids(NextQueued(c.store, c.free, c.eligible)); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestJointTaskRoundTrip(t *testing.T) {
	data, err := json.Marshal(jointStore(true))
	if err != nil {
		t.Fatal(err)
	}
	var back Store
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	g := back.Joint("j")
	if len(g) != 3 || !g[0].Lead || !slices.ContainsFunc(g, func(t Task) bool { return t.ID == "c2" && t.Queued && !t.Lead }) {
		t.Errorf("round trip = %+v", g)
	}
}
