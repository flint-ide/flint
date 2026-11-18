package projects

import (
	"slices"
	"testing"

	"flint/internal/feature"
)

func TestRefresh(t *testing.T) {
	links := []Link{
		{Task: "a", Repo: "api", State: "working"},
		{Task: "b", Repo: "web", State: "finished", PullRequest: "https://x/pr/1"},
		{Task: "c", State: "failed", Gone: true},
	}
	tasks := map[string]feature.AgentTask{
		"a": {ID: "a", State: "finished", Merged: true},
		"c": {ID: "c", State: "working"}, // a gone link stays gone
	}
	got, changed := Refresh(links, tasks)
	want := []Link{
		{Task: "a", Repo: "api", State: "finished", Merged: true},
		{Task: "b", Repo: "web", State: "finished", PullRequest: "https://x/pr/1", Gone: true},
		{Task: "c", State: "failed", Gone: true},
	}
	if !changed || !slices.Equal(got, want) {
		t.Errorf("Refresh = %+v, %v", got, changed)
	}
	if links[0].State != "working" {
		t.Error("Refresh changed its argument")
	}
	if _, changed := Refresh(got, tasks); changed {
		t.Error("a second Refresh changed something")
	}
}

func TestNextStatus(t *testing.T) {
	working := Link{Task: "w", State: "working"}
	queued := Link{Task: "q", State: "queued"}
	finished := Link{Task: "f", State: "finished"}
	failed := Link{Task: "x", State: "failed"}
	merged := Link{Task: "m", State: "finished", Merged: true}
	mergedWorking := Link{Task: "mw", State: "working", Merged: true}
	prRemoved := Link{Task: "p", State: "finished", PullRequest: "https://x/pr/2", Gone: true}
	prLive := Link{Task: "pl", State: "finished", PullRequest: "https://x/pr/3"}
	abandoned := Link{Task: "a", State: "failed", Gone: true}
	mergedRemoved := Link{Task: "mr", Merged: true, Gone: true}
	for _, tc := range []struct {
		name   string
		status string
		links  []Link
		want   string
	}{
		{"no tasks", "doing", nil, "doing"},
		{"still working", "doing", []Link{working, finished}, "doing"},
		{"queued", "doing", []Link{queued}, "doing"},
		{"all finished", "doing", []Link{finished, finished}, "review"},
		{"failed keeps doing", "doing", []Link{finished, failed}, "doing"},
		{"failed attempt removed", "doing", []Link{finished, abandoned}, "review"},
		{"only abandoned", "doing", []Link{abandoned}, "doing"},
		{"finished with a live pull request", "doing", []Link{prLive}, "review"},
		{"review waits for merge", "review", []Link{finished}, "review"},
		{"review: live pull request is not done", "review", []Link{prLive}, "review"},
		{"review: merged", "review", []Link{merged, merged}, "done"},
		{"review: one merged, one not", "review", []Link{merged, finished}, "review"},
		{"review: pull request then removed", "review", []Link{prRemoved}, "done"},
		{"review: merged and removed", "review", []Link{mergedRemoved, prRemoved}, "done"},
		{"review: abandoned ignored", "review", []Link{merged, abandoned}, "done"},
		{"doing to done in one step", "doing", []Link{merged, prRemoved}, "done"},
		{"merged although working again", "review", []Link{mergedWorking}, "done"},
		{"todo never moves", "todo", []Link{merged}, "todo"},
		{"done never moves", "done", []Link{working}, "done"},
		{"review never goes back", "review", []Link{working}, "review"},
	} {
		if got := NextStatus(tc.status, tc.links); got != tc.want {
			t.Errorf("%s: NextStatus(%s) = %s, want %s", tc.name, tc.status, got, tc.want)
		}
	}
	if !Failed([]Link{finished, failed}) || Failed([]Link{finished, abandoned}) || Failed([]Link{{State: "failed", Merged: true}}) {
		t.Error("Failed is wrong")
	}
}

func TestStateBookkeeping(t *testing.T) {
	var list []string
	for _, f := range []string{"/a", "/b", "/a", "/c"} {
		list = remember(list, f)
	}
	if !slices.Equal(list, []string{"/c", "/a", "/b"}) {
		t.Errorf("remember: %v", list)
	}
	if got := forget(list, "/a"); !slices.Equal(got, []string{"/c", "/b"}) || len(list) != 3 {
		t.Errorf("forget: %v (list %v)", got, list)
	}
	list = nil
	for i := range MaxRemembered + 5 {
		list = remember(list, string(rune('a'+i)))
	}
	if len(list) != MaxRemembered || list[0] != string(rune('a'+MaxRemembered+4)) {
		t.Errorf("remember keeps %d, first %q", len(list), list[0])
	}

	var cs []Checkout
	cs = setCheckout(cs, "github.com/acme/api", "/x")
	cs = setCheckout(cs, "github.com/acme/web", "/w")
	cs = setCheckout(cs, "github.com/acme/api", "/y")
	if len(cs) != 2 || checkoutOf(cs, "github.com/acme/api") != "/y" || checkoutOf(cs, "github.com/acme/web") != "/w" || checkoutOf(cs, "x") != "" {
		t.Errorf("checkouts: %+v", cs)
	}
	for i := range maxCheckouts + 3 {
		cs = setCheckout(cs, string(rune(1000+i)), "/f")
	}
	if len(cs) != maxCheckouts {
		t.Errorf("%d checkouts kept", len(cs))
	}

	full := make([]Link, MaxTasksPerItem)
	if _, ok := addLinks(full, []Link{{Task: "n"}}); ok {
		t.Error("a full item took a task")
	}
	full[3].Gone, full[3].Task = true, "old"
	got, ok := addLinks(full, []Link{{Task: "n"}})
	if !ok || len(got) != MaxTasksPerItem || got[len(got)-1].Task != "n" || slices.ContainsFunc(got, func(l Link) bool { return l.Task == "old" }) {
		t.Errorf("addLinks dropped the wrong link: %v", ok)
	}

	ls := links{}
	for i := range maxLinkedProjects + 1 {
		ls[string(rune('A'+i))] = map[string][]Link{"x": {{Task: "t"}}}
	}
	ls.pruneProjects(func(f string) bool { return f == "A" })
	if len(ls) != 1 || ls["A"] == nil {
		t.Errorf("pruneProjects kept %d", len(ls))
	}
	if ids := taskIDs(map[string][]Link{"a": {{Task: "1"}, {Task: "2", Gone: true}}, "b": {{Task: "3"}}}); len(ids) != 2 {
		t.Errorf("taskIDs = %v", ids)
	}
}
