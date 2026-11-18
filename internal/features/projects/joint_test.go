package projects

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"flint/internal/feature"
)

// Items with together: one agent across their repositories (a joint group).

func TestParseTogether(t *testing.T) {
	many := Config{Name: "Many"}
	var names []string
	for i := range 11 {
		name := fmt.Sprintf("r%d", i)
		many.Repositories = append(many.Repositories, RepositoryDecl{name, "https://example.com/" + name})
		names = append(names, name)
	}
	for _, tc := range []struct {
		name, data string
		c          Config
		together   bool
		err        string
	}{
		{"default", "---\ntitle: T\nrepos: api, web\n---\n", testConfig, false, ""},
		{"true", "---\ntitle: T\nrepos: api, web\ntogether: true\n---\n", testConfig, true, ""},
		{"before repos", "---\ntitle: T\ntogether: true\nrepos: api, web\n---\n", testConfig, true, ""},
		{"false", "---\ntitle: T\nrepos: api, web\ntogether: false\n---\n", testConfig, false, ""},
		{"false with one repository", "---\ntitle: T\nrepos: api\ntogether: false\n---\n", testConfig, false, ""},
		{"yes", "---\ntitle: T\nrepos: api, web\ntogether: yes\n---\n", testConfig, false, `together "yes": use true or false`},
		{"empty", "---\ntitle: T\nrepos: api, web\ntogether:\n---\n", testConfig, false, `together "": use true or false`},
		{"True", "---\ntitle: T\nrepos: api, web\ntogether: True\n---\n", testConfig, false, "use true or false"},
		{"one repository", "---\ntitle: T\nrepos: api\ntogether: true\n---\n", testConfig, false, "together: true needs two or more repositories"},
		{"no repository", "---\ntitle: T\ntogether: true\n---\n", testConfig, false, "two or more repositories"},
		{"ten", "---\ntitle: T\nrepos: " + strings.Join(names[:10], ", ") + "\ntogether: true\n---\n", many, true, ""},
		{"eleven", "---\ntitle: T\nrepos: " + strings.Join(names, ", ") + "\ntogether: true\n---\n", many, false, "at most 10 repositories"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it, err := ParseItem("n", []byte(tc.data), tc.c)
			if tc.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.err) {
					t.Fatalf("err = %v, want %q", err, tc.err)
				}
				return
			}
			if err != nil || it.Together != tc.together {
				t.Fatalf("together = %v, %v", it.Together, err)
			}
		})
	}
	// A file with a problem is listed as one, not as an item.
	base := tempDir(t)
	dir := newProject(t, filepath.Join(base, "b"), map[string]string{"one": item("One", "todo", "repos: api", "together: true")})
	write(t, filepath.Join(dir, ConfigFile), `{"name":"Acme","repositories":[{"name":"api","url":"git@github.com:acme/api.git"}]}`)
	l, err := Load(dir)
	if err != nil || len(l.Items) != 0 || len(l.Problems) != 1 || !strings.Contains(l.Problems[0].Reason, "two or more repositories") {
		t.Errorf("Load = %+v, %v", l, err)
	}
}

func TestTaskDescriptionTogether(t *testing.T) {
	it := Item{Title: "T", Body: "Do it.", Repos: []string{"api", "web"}, Together: true}
	// Flint's first message of the group's chat names the folders once: the
	// tasks get the item's description as is.
	if got := it.TaskDescription("api"); got != "T\n\nDo it." {
		t.Errorf("together: %q", got)
	}
}

// togetherFixture has "j" (api and web, together) and a trusted web checkout.
func togetherFixture(t *testing.T, extra ...string) (*fixture, string) {
	t.Helper()
	f := newFixture(t, map[string]string{"j": item("Joint", "todo", append([]string{"repos: api, web", "together: true"}, extra...)...)})
	web := f.withWeb(true)
	f.open()
	return f, web
}

func TestDelegateTogether(t *testing.T) {
	f, web := togetherFixture(t, "agent: backend")
	res := f.delegate("j")
	if n := len(f.h.Requested()); n != 0 {
		t.Fatalf("%d CreateAgentTasks calls", n)
	}
	calls := f.h.JointRequested()
	if len(calls) != 1 || calls[0].Agent != "backend" || len(calls[0].Reqs) != 2 {
		t.Fatalf("joint calls %+v", calls)
	}
	// Every member gets the item's description as is, the lead first.
	want := []feature.NewAgentTask{
		{Dir: f.root, Description: "Joint\n\nDo Joint.", Agent: "backend"},
		{Dir: web, Description: "Joint\n\nDo Joint.", Agent: "backend"},
	}
	for i, r := range calls[0].Reqs {
		if r != want[i] {
			t.Errorf("request %d = %+v, want %+v", i, r, want[i])
		}
	}
	if res.Item.Status != StatusDoing || !res.Item.Together || len(res.Tasks) != 2 || res.Warning != "" {
		t.Fatalf("result %+v", res)
	}
	if res.Tasks[0].Repo != "api" || res.Tasks[1].Repo != "web" || res.Tasks[0].Joint == "" ||
		res.Tasks[0].Joint != res.Tasks[1].Joint || !res.Tasks[0].Lead || res.Tasks[1].Lead {
		t.Errorf("tasks %+v", res.Tasks)
	}
	if ls := f.links("j"); len(ls) != 2 || ls[0].Task != res.Tasks[0].ID || ls[0].Repo != "api" || ls[1].Task != res.Tasks[1].ID || ls[1].Repo != "web" {
		t.Errorf("links %+v", ls)
	}
	if !strings.Contains(f.read("j"), "status: doing") {
		t.Errorf("file %q", f.read("j"))
	}
	v := f.item(f.load(), "j")
	if !v.Together || len(v.Tasks) != 2 || v.Tasks[0].Joint == "" || !v.Tasks[0].Lead {
		t.Errorf("loaded %+v", v)
	}
}

// A refused joint group leaves nothing: no links, the status as it was.
func TestDelegateTogetherRefusals(t *testing.T) {
	f, web := togetherFixture(t)
	nothing := func(what string) {
		t.Helper()
		if ls := f.links("j"); len(ls) != 0 || !strings.Contains(f.read("j"), "status: todo") {
			t.Errorf("%s: links %+v, file %q", what, ls, f.read("j"))
		}
		if n := len(f.h.AllAgentTasks()); n != 0 {
			t.Errorf("%s: %d tasks", what, n)
		}
	}
	// The engine: Flint's reason, and what to change in the item.
	engine := "codex runs on codex, which cannot work in several folders at once: delegate per repository instead"
	f.h.SetCreateError(featureRefusal(engine, feature.ErrJointEngine))
	_, err := f.api.Delegate(f.plan, "j", f.root)
	wantErr(t, err, engine+" (the item says together: true; without that line each repository gets its own agent)")
	if !errors.Is(err, feature.ErrJointEngine) {
		t.Errorf("err %v is not ErrJointEngine", err)
	}
	nothing("engine")
	// A folder that is not trusted, by Flint's check.
	untrusted := "the worktree /w is not trusted (a joint agent task works only in trusted folders)"
	f.h.SetCreateError(featureRefusal(untrusted, feature.ErrJointUntrusted))
	_, err = f.api.Delegate(f.plan, "j", f.root)
	wantErr(t, err, untrusted)
	nothing("untrusted")
	// A detached HEAD.
	f.h.SetCreateError(&feature.DetachedHeadError{Dir: web})
	_, err = f.api.Delegate(f.plan, "j", f.root)
	wantErr(t, err, web+" is on a detached HEAD: check out a branch there, then Delegate again")
	nothing("detached HEAD")
	f.h.SetCreateError(nil)

	// Delegate's own checks come first: nothing is asked of Flint.
	g := newFixture(t, map[string]string{
		"j": item("Joint", "todo", "repos: api, web", "together: true"),
		"k": item("Other agent", "todo", "repos: api, web", "together: true", "agent: frontend"),
	})
	g.withWeb(false)
	g.open()
	_, err = g.api.Delegate(g.plan, "j", g.root)
	wantErr(t, err, "is in Restricted Mode: trust the folder first")
	_, err = g.api.Delegate(g.plan, "k", g.root)
	wantErr(t, err, `agent "frontend" is not available`)
	if len(g.h.JointRequested()) != 0 || len(g.h.Requested()) != 0 {
		t.Error("tasks were asked for although a check failed")
	}
}

// featureRefusal is an app error that is also a feature error, as the
// app's host returns it.
func featureRefusal(text string, kind error) error {
	return fmt.Errorf("%s%w", text, hidden{kind})
}

// hidden wraps an error without adding to the text.
type hidden struct{ err error }

func (h hidden) Error() string { return "" }
func (h hidden) Unwrap() error { return h.err }

// The untrusted-project confirmation says that one agent works in all the
// folders, and shows the description once.
func TestDelegateTogetherFromAnUntrustedProject(t *testing.T) {
	f := newFixture(t, nil)
	web := f.withWeb(true)
	f.open()
	other := newProject(t, filepath.Join(f.base, "other"), map[string]string{"j": item("Joint", "todo", "repos: api, web", "together: true")})
	if _, err := f.api.Open(other); err != nil {
		t.Fatal(err)
	}
	f.h.Answer = false
	_, err := f.api.Delegate(other, "j", f.root)
	wantErr(t, err, "Delegate was cancelled")
	asked := f.h.Asked()
	if len(asked) != 1 {
		t.Fatalf("asked %q", asked)
	}
	for _, want := range []string{
		"Works in: " + f.root + ", " + web,
		"One agent works in all of these folders together, in one chat (the item says together: true)",
		"Description:\nJoint\n\nDo Joint.",
	} {
		if !strings.Contains(asked[0], want) {
			t.Errorf("the question lacks %q:\n%s", want, asked[0])
		}
	}
	if strings.Contains(asked[0], "Each task also gets a note") {
		t.Errorf("the question speaks of separate tasks:\n%s", asked[0])
	}
	if len(f.h.JointRequested()) != 0 {
		t.Error("a group was created after No")
	}
}

// A waiting item with together starts as one joint group, and the question
// of an untrusted project says so.
func TestStartWhenReadyTogether(t *testing.T) {
	f := depFixture(t, map[string]string{"j": item("Joint", "todo", "repos: api, web", "together: true", "after: dep")})
	f.withWeb(true)
	if _, err := f.api.StartWhenReady(f.plan, "j", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.api.SetStatus(f.plan, "dep", StatusDone); err != nil {
		t.Fatal(err)
	}
	f.startReady()
	if calls := f.h.JointRequested(); len(calls) != 1 || len(calls[0].Reqs) != 2 || len(f.h.Requested()) != 0 {
		t.Fatalf("joint %+v, per repository %+v", calls, f.h.Requested())
	}
	if ev := f.autoStarts(); len(ev) != 1 || ev[0].Tasks != 2 || ev[0].Error != "" {
		t.Errorf("auto starts %+v", ev)
	}
	if ls := f.links("j"); len(ls) != 2 || len(f.waits()) != 0 || !strings.Contains(f.read("j"), "status: doing") {
		t.Errorf("links %+v waits %+v", ls, f.waits())
	}

	// From an untrusted project: the question names the repositories; an
	// engine that can't do it keeps the item waiting with Flint's reason.
	g := newFixture(t, nil)
	g.withWeb(true)
	g.open()
	other := newProject(t, filepath.Join(g.base, "other"), map[string]string{
		"dep": item("Dependency", "done"),
		"j":   item("Joint", "todo", "repos: api, web", "together: true", "after: dep"),
	})
	if _, err := g.api.Open(other); err != nil {
		t.Fatal(err)
	}
	if _, err := g.api.StartWhenReady(other, "j", ""); err != nil {
		t.Fatal(err)
	}
	g.h.Answer = true
	g.h.SetCreateError(featureRefusal("codex runs on codex, which cannot work in several folders at once", feature.ErrJointEngine))
	g.startReady()
	asked := g.h.Asked()
	if len(asked) != 1 || !strings.Contains(asked[0], "It runs as one agent across the repositories api, web.") ||
		!strings.Contains(asked[0], "One agent works in all of these folders together") {
		t.Fatalf("asked %q", asked)
	}
	var w waits
	_ = g.h.LoadState(stateWaiting, &w)
	if r := w[other]["j"].Reason; !strings.Contains(r, "cannot work in several folders at once") || !strings.Contains(r, "together: true") {
		t.Errorf("waiting %+v", w[other]["j"])
	}
	if ev := g.autoStarts(); len(ev) != 1 || ev[0].Tasks != 0 || ev[0].Error == "" {
		t.Errorf("auto starts %+v", ev)
	}
	if !strings.Contains(g.readIn(other, "j"), "status: todo") {
		t.Error("a refused start moved the item")
	}
}

// readIn reads item id of the project in folder.
func (f *fixture) readIn(folder, id string) string {
	f.t.Helper()
	data, _, err := ReadItemFile(folder, id)
	if err != nil {
		f.t.Fatal(err)
	}
	return string(data)
}

// The members of a joint group share one chat: they finish together, and
// the item follows them like any tasks; removing one stops the chat for the
// others (they go idle), and an abandoned member no longer counts.
func TestStatusFollowsAJointGroup(t *testing.T) {
	f, _ := togetherFixture(t)
	res := f.delegate("j")
	a, w := res.Tasks[0].ID, res.Tasks[1].ID
	status := func() string { return f.item(f.load(), "j").Status }
	set := func(state string, ids ...string) {
		for _, id := range ids {
			f.h.UpdateAgentTask(id, func(t *feature.AgentTask) { t.State = state })
		}
	}
	set("finished", a, w)
	if got := status(); got != StatusReview {
		t.Fatalf("the group finished: %s", got)
	}
	// Rework: the user moves it back and the chat works again.
	if _, err := f.api.SetStatus(f.plan, "j", StatusDoing); err != nil {
		t.Fatal(err)
	}
	set("working", a, w)
	if got := status(); got != StatusDoing {
		t.Fatalf("working: %s", got)
	}
	// The web member is removed without a pull request while the chat
	// runs: the chat stops, the lead is idle (it is no group any more).
	f.h.RemoveAgentTask(w)
	f.h.UpdateAgentTask(a, func(t *feature.AgentTask) { t.State, t.Joint, t.Lead = "idle", "", false })
	if got := status(); got != StatusDoing {
		t.Fatalf("after removing a member: %s", got)
	}
	v := f.item(f.load(), "j")
	if !v.Tasks[1].Gone || v.Tasks[0].Joint != "" || v.Failed {
		t.Errorf("tasks %+v", v.Tasks)
	}
	// The lead finishes and is merged: the abandoned member does not hold it.
	f.h.UpdateAgentTask(a, func(t *feature.AgentTask) { t.State, t.Merged = "finished", true })
	if got := status(); got != StatusDone {
		t.Fatalf("lead merged: %s", got)
	}
}

func TestNextStatusJointGroup(t *testing.T) {
	// One chat: every member has its state.
	both := func(state string) []Link {
		return []Link{{Task: "a", Repo: "api", State: state}, {Task: "b", Repo: "web", State: state}}
	}
	for state, want := range map[string]string{"queued": StatusDoing, "working": StatusDoing, "idle": StatusDoing, "failed": StatusDoing, "finished": StatusReview} {
		if got := NextStatus(StatusDoing, both(state)); got != want {
			t.Errorf("%s: %s, want %s", state, got, want)
		}
	}
	if !Failed(both("failed")) {
		t.Error("a failed group is not marked failed")
	}
	// One member delivered by a pull request and removed, the other merged.
	ls := []Link{{Task: "a", State: "finished", Merged: true}, {Task: "b", State: "finished", PullRequest: "https://x/pr/1", Gone: true}}
	if got := NextStatus(StatusReview, ls); got != StatusDone {
		t.Errorf("delivered: %s", got)
	}
}

func TestAddItemTogether(t *testing.T) {
	f := newFixture(t, nil)
	f.open()
	f.allowAdd()
	out, err := f.add(`{"project":"Acme","title":"Rename the field","text":"Both sides.","repos":["api","web"],"together":true}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "rename-the-field") {
		t.Errorf("answer %q", out)
	}
	if got := f.read("rename-the-field"); got != "---\ntitle: Rename the field\nstatus: todo\npriority: normal\nrepos: api, web\ntogether: true\n---\nBoth sides.\n" {
		t.Errorf("file %q", got)
	}
	if v := f.item(f.load(), "rename-the-field"); !v.Together {
		t.Errorf("item %+v", v)
	}
	// false writes nothing; with one repository it is refused with the reason.
	if _, err := f.add(`{"project":"Acme","title":"Plain","text":"","repos":["api","web"],"together":false}`); err != nil {
		t.Fatal(err)
	}
	if got := f.read("plain"); strings.Contains(got, "together") {
		t.Errorf("file %q", got)
	}
	_, err = f.add(`{"project":"Acme","title":"Lonely","text":"","repos":["api"],"together":true}`)
	wantErr(t, err, "together: true needs two or more repositories")
	_, err = f.add(`{"project":"Acme","title":"Bad","text":"","repos":["api","web"],"together":"yes"}`)
	wantErr(t, err, "invalid arguments")
	// The schema offers it; the summary the user is asked about says so.
	tools := f.api.agentTools()
	if !strings.Contains(string(tools[1].InputSchema), `"together":{"type":"boolean"`) {
		t.Errorf("schema %s", tools[1].InputSchema)
	}
	if s := addSummary("Acme", f.plan, "x", ItemSpec{Title: "X", Repos: []string{"api", "web"}, Together: true}); !strings.Contains(s, "Together: one agent in all of its repositories") {
		t.Errorf("summary %q", s)
	}
}

func TestGetProjectShowsTogether(t *testing.T) {
	f := newFixture(t, map[string]string{
		"j": item("Joint", "todo", "repos: api, web", "together: true"),
		"s": item("Split", "todo", "repos: api, web"),
	})
	f.open()
	out := f.api.describe("", "")
	for _, want := range []string{
		"## j: Joint\nstatus: todo; priority: normal; repositories: api, web (together: one agent works in all of them)\n",
		"## s: Split\nstatus: todo; priority: normal; repositories: api, web\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("get_project lacks %q:\n%s", want, out)
		}
	}
}

func TestPlanPromptDescribesTogether(t *testing.T) {
	f := newFixture(t, nil)
	f.open()
	p, err := f.api.PlanPrompt(f.plan, "Rename a field everywhere")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.Prompt, "- together (optional, only with 2 to 10 repos)") || !strings.Contains(p.Prompt, "unless together is true") {
		t.Errorf("prompt:\n%s", p.Prompt)
	}
}
