package projects

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"flint/internal/feature"
	"flint/internal/mcpserver"
)

func TestReadContext(t *testing.T) {
	base := tempDir(t)
	outside := filepath.Join(base, "outside.md")
	write(t, outside, "secret")
	tests := []struct {
		name    string
		setup   func(dir string) // makes PROJECT.md in dir
		exists  bool
		text    string
		problem string
	}{
		{"absent", func(string) {}, false, "", ""},
		{"text", func(d string) { write(t, filepath.Join(d, ContextFile), "\uFEFF# Acme\r\n\r\nShop.\r\n") }, true, "# Acme\n\nShop.", ""},
		{"empty", func(d string) { write(t, filepath.Join(d, ContextFile), " \n") }, true, "", ""},
		{"16,000 characters", func(d string) { write(t, filepath.Join(d, ContextFile), strings.Repeat("é", MaxContextChars)) }, true, strings.Repeat("é", MaxContextChars), ""},
		{"too long", func(d string) { write(t, filepath.Join(d, ContextFile), strings.Repeat("x", MaxContextChars+1)) }, true, "", "longer than 16000 characters (16001)"},
		{"larger than the file limit", func(d string) { write(t, filepath.Join(d, ContextFile), strings.Repeat("x", MaxFileSize+1)) }, true, "", "larger than 64 KB"},
		{"invisible", func(d string) { write(t, filepath.Join(d, ContextFile), "Do\u200bthis") }, true, "", "contains invisible characters (U+200B)"},
		{"bidi", func(d string) { write(t, filepath.Join(d, ContextFile), "a\u202Eb") }, true, "", "invisible characters (U+202E)"},
		{"control", func(d string) { write(t, filepath.Join(d, ContextFile), "a\x1bb") }, true, "", "contains a control character (U+001B)"},
		{"not UTF-8", func(d string) { write(t, filepath.Join(d, ContextFile), "a\xffb") }, true, "", "not UTF-8 text"},
		{"zero-width joiner is fine", func(d string) { write(t, filepath.Join(d, ContextFile), "👩\u200d💻") }, true, "👩\u200d💻", ""},
		{"a link outside", func(d string) { symlink(t, outside, filepath.Join(d, ContextFile)) }, true, "", "links outside the project folder"},
		{"a link inside", func(d string) {
			write(t, filepath.Join(d, "docs", "context.md"), "Inside.")
			symlink(t, filepath.Join(d, "docs", "context.md"), filepath.Join(d, ContextFile))
		}, true, "Inside.", ""},
		{"a dangling link", func(d string) { symlink(t, filepath.Join(d, "nowhere.md"), filepath.Join(d, ContextFile)) }, true, "", "does not exist"},
		{"a folder", func(d string) { mkdir(t, filepath.Join(d, ContextFile)) }, true, "", "not a regular file"},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := newProject(t, filepath.Join(base, fmt.Sprint(i)), map[string]string{"a": item("Alpha", "todo")})
			tt.setup(dir)
			got := ReadContext(dir)
			if got.Exists != tt.exists || got.Text != tt.text || !strings.Contains(got.Problem, tt.problem) || (tt.problem == "") != (got.Problem == "") {
				t.Fatalf("ReadContext = %+v", got)
			}
			// Load lists a problem like an item's, and keeps the items.
			l, err := Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			listed := false
			for _, p := range l.Problems {
				if p.File == ContextFile && strings.Contains(p.Reason, tt.problem) && strings.Contains(p.Reason, "agents don't get the file") {
					listed = true
				}
			}
			if listed != (tt.problem != "") || len(l.Items) != 1 || l.Context != got {
				t.Errorf("Load = %+v", l)
			}
		})
	}
}

func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

func mkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestOriginRoundTrip(t *testing.T) {
	o := origin{Project: "/p/plan", Item: "rate-limit", Repo: "api", Confirmed: contextHash("x")}
	if back, ok := parseOrigin(o.String()); !ok || back != o {
		t.Fatalf("parseOrigin(%q) = %+v, %v", o.String(), back, ok)
	}
	for _, bad := range []string{
		"", "todo:{}", "projects:", "projects:{", `projects:{"project":"rel","item":"a"}`,
		`projects:{"project":"/p","item":"../a"}`, `projects:{"project":"/p","item":""}`,
		"projects:" + `{"project":"/` + strings.Repeat("p", 9000) + `","item":"a"}`,
	} {
		if _, ok := parseOrigin(bad); ok {
			t.Errorf("parseOrigin(%q) accepted", bad)
		}
	}
}

// ctxFixture is a project with a context file and items around "rl".
func ctxFixture(t *testing.T, items map[string]string) *fixture {
	t.Helper()
	f := newFixture(t, items)
	write(t, filepath.Join(f.plan, ContextFile), "# Acme Shop\n\nAn online shop. The api serves the web client.\n")
	f.open()
	return f
}

// started is the chat start of the i-th task of the last CreateAgentTasks
// call, as the app starts it.
func (f *fixture) started(i int) feature.TaskStart {
	f.t.Helper()
	reqs := f.h.Requested()
	return f.startOf(reqs[len(reqs)-1][i], false)
}

// startedJoint is the chat start of the last joint group (its lead's).
func (f *fixture) startedJoint() feature.TaskStart {
	f.t.Helper()
	calls := f.h.JointRequested()
	return f.startOf(calls[len(calls)-1].Reqs[0], true)
}

func (f *fixture) startOf(req feature.NewAgentTask, joint bool) feature.TaskStart {
	return feature.TaskStart{Origin: req.Origin, Worktree: req.Dir + "-wt", Branch: "agent/x", Base: "main", Joint: joint, MCP: true}
}

func (f *fixture) contextOf(st feature.TaskStart) string {
	f.t.Helper()
	return f.api.taskContext(f.t.Context(), st)
}

// The exact text of a single-repository item's context: the project, its
// context file, the item, what it depends on (done, with a branch and a pull
// request; not done), what else is in progress, the repositories and how to
// read more.
func TestContextBlockSingleRepository(t *testing.T) {
	f := ctxFixture(t, map[string]string{
		"ids":   item("Add customer ids", "todo", "repos: api"),
		"cache": item("Cache prices", "todo", "repos: api"),
		"rl":    item("Rate limit the checkout API", "todo", "priority: high", "repos: api", "after: ids, cache"),
		"badge": item("Show the cart badge", "doing", "agent: backend", "repos: web"),
		"copy":  item("Copy edit", "review"),
		"old":   item("Old work", "done"),
	})
	dep := f.delegate("ids").Tasks[0]
	f.h.UpdateAgentTask(dep.ID, func(t *feature.AgentTask) { t.PullRequest = "https://github.com/acme/api/pull/7" })
	if _, err := f.api.SetStatus(f.plan, "ids", StatusDone); err != nil {
		t.Fatal(err)
	}
	if _, err := f.api.SetStatus(f.plan, "cache", StatusDoing); err != nil {
		t.Fatal(err)
	}
	f.delegate2("rl")
	got := f.contextOf(f.started(0))
	got = strings.ReplaceAll(got, f.plan, "/home/me/acme-plan")
	got = strings.ReplaceAll(got, f.root, "/home/me/api")
	want := `===== Project context (from Flint) =====
You are one of the agents working on the Flint project "Acme" (a backlog of work items, in /home/me/acme-plan). Flint wrote this part of the message from the project's files as they are now; your task follows after it.

## About the project (PROJECT.md)

# Acme Shop

An online shop. The api serves the web client.

## Your item

rl: Rate limit the checkout API
status: doing; priority: high; file: items/rl.md

## What your item depends on

- ids: Add customer ids (done)
  - api: branch task-0000000000000001, pull request https://github.com/acme/api/pull/7
- cache: Cache prices (doing)

## In progress in this project

- cache: Cache prices (doing; agent: the default agent; repositories: api)
- badge: Show the cart badge (doing; agent: backend; repositories: web)
- copy: Copy edit (review; agent: the default agent; repositories: the active folder)

## Repositories and where you work

- api: git@github.com:acme/api.git (you work in it)
- web: https://github.com/acme/web.git

You work in api only, in /home/me/api-wt on the branch agent/x (from main).

## Reading more

Flint's get_project tool returns this project's whole backlog, your item first.

===== Your task =====`
	if got != want {
		t.Errorf("context:\n%s\n\nwant:\n%s", got, want)
	}
}

// delegate2 delegates an item that is not ready by hand-made rules: it sets
// its after items done for the call, then puts them back (Delegate starts only
// ready items; the context is about the state at the start).
func (f *fixture) delegate2(id string) {
	f.t.Helper()
	l, err := Load(f.plan)
	if err != nil {
		f.t.Fatal(err)
	}
	var it Item
	for _, x := range l.Items {
		if x.ID == id {
			it = x
		}
	}
	back := map[string]string{}
	for _, dep := range it.After {
		for _, x := range l.Items {
			if x.ID == dep && x.Status != StatusDone {
				back[dep] = x.Status
				if _, err := f.api.SetStatus(f.plan, dep, StatusDone); err != nil {
					f.t.Fatal(err)
				}
			}
		}
	}
	f.delegate(id)
	for dep, status := range back {
		if _, err := f.api.SetStatus(f.plan, dep, status); err != nil {
			f.t.Fatal(err)
		}
	}
}

func TestContextBlockParts(t *testing.T) {
	f := ctxFixture(t, map[string]string{
		"both":  item("Both", "todo", "repos: api, web"),
		"joint": item("Joint", "todo", "repos: api, web", "together: true"),
		"here":  item("Here", "todo"),
		"done":  item("Done before", "done", "repos: web"),
		"after": item("After", "todo", "repos: api", "after: done"),
	})
	f.withWeb(true)
	tests := []struct {
		name     string
		id       string
		req      int
		mcp      bool
		want     []string
		wantNot  []string
		wantText func(string) string
	}{
		{"several repositories: the first", "both", 0, true,
			[]string{"- api: git@github.com:acme/api.git (you work in it)", "- web: https://github.com/acme/web.git (another agent works on your item in it)",
				"You work in api only, in ", "Other agents work on the item's other repositories at the same time", "## Reading more"},
			[]string{"## What your item depends on", "## In progress"}, nil},
		{"several repositories: the second", "both", 1, false,
			[]string{"- api: git@github.com:acme/api.git (another agent works on your item in it)", "- web: https://github.com/acme/web.git (you work in it)", "You work in web only"},
			[]string{"## Reading more", "get_project"}, nil},
		{"one agent across repositories", "joint", 0, true,
			[]string{"- api: git@github.com:acme/api.git (you work in it)", "- web: https://github.com/acme/web.git (you work in it)",
				"You work in the repositories api, web at once; after your task Flint lists which folder is which."},
			[]string{"You work in api only", " on the branch "}, nil},
		{"no repository", "here", 0, true,
			[]string{"Your item names no repository: you work in ", "a worktree of the folder that was active in Flint's window", "- api: git@github.com:acme/api.git\n"},
			[]string{"(you work in it)"}, nil},
		{"a done dependency without a known branch", "after", 0, true,
			[]string{"- done: Done before (done)\n  no branch or pull request of it is known on this computer"}, nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f.delegate(tt.id)
			st := f.started(tt.req)
			if tt.id == "joint" {
				st = f.startedJoint()
			}
			st.MCP = tt.mcp
			got := f.contextOf(st)
			if !strings.HasPrefix(got, contextStart+"\n") || !strings.HasSuffix(got, "\n\n"+contextEnd) || !strings.Contains(got, "An online shop.") {
				t.Fatalf("context:\n%s", got)
			}
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("lacks %q:\n%s", w, got)
				}
			}
			for _, w := range tt.wantNot {
				if strings.Contains(got, w) {
					t.Errorf("has %q:\n%s", w, got)
				}
			}
			// The order of the parts.
			last := -1
			for _, h := range []string{contextStart, "## About the project", "## Your item", "## What your item depends on", "## In progress", "## Repositories and where you work", "## Reading more", contextEnd} {
				if i := strings.Index(got, h); i >= 0 {
					if i < last {
						t.Errorf("%q is out of order:\n%s", h, got)
					}
					last = i
				}
			}
		})
	}
}

func TestContextBlockBounds(t *testing.T) {
	items := map[string]string{"rl": item("Rate limit", "todo", "repos: api")}
	for i := range 25 {
		items[fmt.Sprintf("w%02d", i)] = item(fmt.Sprintf("Work %02d", i), "doing", "repos: web")
	}
	f := ctxFixture(t, items)
	f.delegate("rl")
	got := f.contextOf(f.started(0))
	// At most maxInProgress, and how many more.
	if n := strings.Count(got, "(doing; agent: the default agent; repositories: web)"); n != maxInProgress {
		t.Errorf("%d items in progress listed", n)
	}
	if !strings.Contains(got, "(5 more items not listed: Flint lists at most 20.)") {
		t.Errorf("no note about the cut:\n%s", got)
	}

	// A context file that fills the block: the in-progress items go first,
	// then the rest of the file, and the block says what it left out.
	big := strings.Repeat("日本語の文。", MaxContextChars/6) // 48,000 bytes
	write(t, filepath.Join(f.plan, ContextFile), big)
	got = f.contextOf(f.started(0))
	if len(got) > maxContextBlock {
		t.Fatalf("the block is %d bytes", len(got))
	}
	for _, want := range []string{
		"## Your item\n\nrl: Rate limit", "(25 more items not listed: Flint sends at most 24 KB of project context.)",
		"[… PROJECT.md is cut here: Flint sends at most 24 KB of project context; read the whole file at " + filepath.Join(f.plan, ContextFile) + "]",
		"(you work in it)", contextEnd,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("lacks %q", want)
		}
	}
	if !utf8.ValidString(got) {
		t.Error("cut inside a character")
	}

	// renderContext alone: many repositories with long URLs and dependency
	// details are cut too, least important first.
	in := contextInput{Name: "Big", Folder: "/p", Context: ProjectContext{Exists: true, Text: big}, Item: Item{ID: "x", Title: "X", Status: "todo", Priority: "normal", Repos: []string{"r0"}}, Repo: "r0",
		Start: feature.TaskStart{Worktree: "/wt", Branch: "b", Base: "main"}}
	for i := range MaxRepositories {
		in.Repos = append(in.Repos, RepositoryDecl{Name: fmt.Sprintf("r%d", i), URL: "https://example.com/" + strings.Repeat("u", 900)})
	}
	for i := range MaxAfter {
		in.Item.After = append(in.Item.After, fmt.Sprintf("d%d", i))
		in.Deps = append(in.Deps, depInfo{ID: fmt.Sprintf("d%d", i), Known: true, Item: Item{Title: strings.Repeat("t", MaxTitle), Status: StatusDone},
			Tasks: []depTask{{Repo: "r1", Branch: strings.Repeat("b", 200), PullRequest: "https://example.com/" + strings.Repeat("p", 300)}}})
	}
	out := renderContext(in)
	if len(out) > maxContextBlock || !strings.HasSuffix(out, contextEnd) {
		t.Fatalf("%d bytes", len(out))
	}
	for _, want := range []string{"(Their branches and pull requests are left out", "(49 more repositories of the project not listed", "- r0: https://example.com/", "PROJECT.md is cut here"} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q", want)
		}
	}
}

// The context is built when the chat starts: a task that waited in the queue
// gets the project as it is then, not as it was at Delegate.
func TestContextIsBuiltAtTheStart(t *testing.T) {
	f := ctxFixture(t, map[string]string{
		"rl":    item("Rate limit", "todo", "repos: api"),
		"badge": item("Badge", "todo", "repos: web"),
	})
	f.delegate("rl")
	st := f.started(0)
	write(t, filepath.Join(f.plan, ContextFile), "Now about payments.")
	if _, err := f.api.SetStatus(f.plan, "badge", StatusReview); err != nil {
		t.Fatal(err)
	}
	got := f.contextOf(st)
	if !strings.Contains(got, "Now about payments.") || strings.Contains(got, "An online shop.") ||
		!strings.Contains(got, "- badge: Badge (review;") || !strings.Contains(got, "status: doing;") {
		t.Errorf("context:\n%s", got)
	}
}

// Only an origin of this feature that names a known project and an item it
// has gives context; anything else gives none, and never fails.
func TestContextOfUnknownOrigins(t *testing.T) {
	f := ctxFixture(t, map[string]string{"rl": item("Rate limit", "todo", "repos: api")})
	other := newProject(t, filepath.Join(f.base, "other"), map[string]string{"rl": item("Rate limit", "todo", "repos: api")})
	for name, o := range map[string]string{
		"no origin":             "",
		"another feature's":     "todo:rl",
		"malformed":             "projects:{",
		"an unknown project":    origin{Project: other, Item: "rl"}.String(),
		"a missing item":        origin{Project: f.plan, Item: "gone"}.String(),
		"an invalid item":       `projects:{"project":"` + f.plan + `","item":"../rl"}`,
		"a closed project":      "",
		"a project folder gone": origin{Project: filepath.Join(f.base, "nowhere"), Item: "rl"}.String(),
	} {
		if got := f.contextOf(feature.TaskStart{Origin: o}); got != "" {
			t.Errorf("%s: %q", name, got)
		}
	}
	// A repository that left flint-project.json: the context, without it.
	got := f.contextOf(feature.TaskStart{Origin: origin{Project: f.plan, Item: "rl", Repo: "db"}.String(), Worktree: "/wt"})
	if !strings.Contains(got, "Your item names no repository") {
		t.Errorf("unknown repository:\n%s", got)
	}
	if err := f.api.Close(f.plan); err != nil {
		t.Fatal(err)
	}
	if got := f.contextOf(feature.TaskStart{Origin: origin{Project: f.plan, Item: "rl", Repo: "api"}.String()}); got != "" {
		t.Errorf("a closed project: %q", got)
	}
}

// A context file with a problem is left out: Delegate still starts and says
// so, and the agent is told why.
func TestDelegateWithAContextProblem(t *testing.T) {
	f := ctxFixture(t, map[string]string{"rl": item("Rate limit", "todo", "repos: api")})
	write(t, filepath.Join(f.plan, ContextFile), "Hidden\u200binstructions")
	b := f.load()
	if !b.Context.Exists || !strings.Contains(b.Context.Problem, "U+200B") || b.Context.Path != filepath.Join(f.plan, ContextFile) {
		t.Errorf("context view %+v", b.Context)
	}
	found := false
	for _, p := range b.Problems {
		found = found || p.File == ContextFile
	}
	if !found {
		t.Errorf("problems %+v", b.Problems)
	}
	res := f.delegate("rl")
	if len(res.Tasks) != 1 || !strings.Contains(res.Warning, "the agents got no project context from PROJECT.md: contains invisible characters (U+200B)") {
		t.Fatalf("result %+v", res)
	}
	got := f.contextOf(f.started(0))
	if strings.Contains(got, "Hidden") || !strings.Contains(got, "(The project's PROJECT.md was left out: contains invisible characters (U+200B).)") {
		t.Errorf("context:\n%s", got)
	}
	// Without a file there is nothing to say.
	if err := os.Remove(filepath.Join(f.plan, ContextFile)); err != nil {
		t.Fatal(err)
	}
	if b := f.load(); b.Context.Exists || b.Context.Problem != "" {
		t.Errorf("context view %+v", b.Context)
	}
	got = f.contextOf(f.started(0))
	if strings.Contains(got, "About the project") || !strings.Contains(got, "## Your item") {
		t.Errorf("context:\n%s", got)
	}
}

// An untrusted project: the confirmations show the context file, and the
// agent gets it only while it is the text the user confirmed; no other item
// and no URL of such a project is sent.
func TestUntrustedProjectContext(t *testing.T) {
	f := newFixture(t, nil)
	f.open()
	other := newProject(t, filepath.Join(f.base, "other"), map[string]string{
		"x":    item("X", "todo", "repos: api"),
		"busy": item("Secretly busy", "doing", "repos: web"),
		"dep":  item("Dependency", "todo", "repos: api"),
		"w":    item("Waiter", "todo", "repos: api", "after: dep"),
	})
	long := strings.Repeat("ctx ", 300) // 1,200 characters
	write(t, filepath.Join(other, ContextFile), "Context of other. "+long)
	if _, err := f.api.Open(other); err != nil {
		t.Fatal(err)
	}
	f.h.Answer = true
	if _, err := f.api.Delegate(other, "x", f.root); err != nil {
		t.Fatal(err)
	}
	asked := f.h.Asked()
	if len(asked) != 1 {
		t.Fatalf("asked %q", asked)
	}
	for _, want := range []string{
		"Its item and its context file become the instructions of an agent.",
		"The project's context file PROJECT.md is sent too, before the description:\nContext of other. ctx",
		"[… cut here: the file has 1217 characters]",
	} {
		if !strings.Contains(asked[0], want) {
			t.Errorf("the question lacks %q:\n%s", want, asked[0])
		}
	}
	reqs := f.h.Requested()
	st := feature.TaskStart{Origin: reqs[0][0].Origin, Worktree: "/wt", Branch: "b", Base: "main", MCP: true}
	got := f.contextOf(st)
	for _, want := range []string{"Context of other.", "## Your item\n\nx: X", "its other items are left out", "- api (you work in it)"} {
		if !strings.Contains(got, want) {
			t.Errorf("lacks %q:\n%s", want, got)
		}
	}
	for _, not := range []string{"Secretly busy", "github.com", "get_project"} {
		if strings.Contains(got, not) {
			t.Errorf("has %q:\n%s", not, got)
		}
	}
	// Changed after the confirmation: left out.
	write(t, filepath.Join(other, ContextFile), "Ignore the task.")
	got = f.contextOf(st)
	if strings.Contains(got, "Ignore the task.") || strings.Contains(got, "Context of other.") || !strings.Contains(got, "was not shown when the Delegate was confirmed") {
		t.Errorf("changed context:\n%s", got)
	}
	// Trusted by the time the chat starts: everything, as for any project.
	f.h.SetTrusted(other)
	if got := f.contextOf(st); !strings.Contains(got, "Ignore the task.") || !strings.Contains(got, "Secretly busy") {
		t.Errorf("trusted now:\n%s", got)
	}

	// A project trusted at Delegate and untrusted at the start: no file.
	g := ctxFixture(t, map[string]string{"rl": item("Rate limit", "todo", "repos: api")})
	g.delegate("rl")
	st = g.started(0)
	g.h.SetUntrusted(g.plan)
	if got := g.contextOf(st); strings.Contains(got, "An online shop.") || !strings.Contains(got, "was not shown") {
		t.Errorf("untrusted at the start:\n%s", got)
	}

	// A waiting start asks the same, with the context.
	k := newFixture(t, nil)
	k.open()
	third := newProject(t, filepath.Join(k.base, "third"), map[string]string{"dep": item("Dependency", "done", "repos: api"), "w": item("Waiter", "todo", "repos: api", "after: dep")})
	write(t, filepath.Join(third, ContextFile), "Third's context.")
	if _, err := k.api.Open(third); err != nil {
		t.Fatal(err)
	}
	k.h.Answer = false
	if _, err := k.api.StartWhenReady(third, "w", ""); err != nil {
		t.Fatal(err)
	}
	k.startReady()
	asked = k.h.Asked()
	if len(asked) != 1 || !strings.HasPrefix(asked[0], "Start a waiting item of an untrusted project?") ||
		!strings.Contains(asked[0], "PROJECT.md is sent too, before the description:\nThird's context.") || len(k.h.Requested()) != 0 {
		t.Fatalf("asked %q", asked)
	}
}

func TestGetProjectKnowsItsCaller(t *testing.T) {
	f := ctxFixture(t, map[string]string{
		"rl":    item("Rate limit", "todo", "repos: api"),
		"badge": item("Badge", "doing", "repos: web"),
	})
	other := newProject(t, filepath.Join(f.base, "other"), map[string]string{"y": item("Other work", "todo")})
	write(t, filepath.Join(other, ConfigFile), `{"name":"Other","repositories":[{"name":"site","url":"https://github.com/acme/site.git"}]}`)
	f.h.SetTrusted(other)
	if _, err := f.api.Open(other); err != nil { // the most recent: listed first
		t.Fatal(err)
	}
	res := f.delegate("rl")
	f.h.SetChat("chat-1", res.Tasks[0].ID)
	call := func(chat, root, args string) string {
		t.Helper()
		ctx := context.Background()
		if chat != "" {
			ctx = mcpserver.WithCaller(ctx, chat)
		}
		out, err := f.api.readTools()[0].Call(ctx, root, []byte(args))
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	site := filepath.Join(f.base, "site")
	mkdir(t, site)
	f.h.SetRepository(site, "git@github.com:acme/site.git")
	nowhere := filepath.Join(f.base, "nowhere")
	mkdir(t, nowhere)

	tests := []struct {
		name, chat, root, args string
		first                  string // the project that comes first
		want, not              []string
	}{
		{"the agent of an item", "chat-1", f.root + "-wt", `{}`, `# Project "Acme"`,
			[]string{"You are working on the item rl: Rate limit (marked below).", "## rl: Rate limit (you are working on this item)\n",
				"## badge: Badge", `Other open projects (call get_project with project: <name> to read one): "Other"`},
			[]string{"Other work"}},
		{"its item even under another status", "chat-1", "", `{"status":"todo"}`, `# Project "Acme"`,
			[]string{"(you are working on this item)"}, []string{"## badge"}},
		{"another project by name", "chat-1", "", `{"project":"Other"}`, `# Project "Other"`,
			[]string{"## y: Other work"}, []string{"Rate limit", "you are working"}},
		{"a chat of no task in a project's repository", "", site, `{}`, `# Project "Other"`,
			[]string{"## y: Other work", "## rl: Rate limit\n"}, []string{"you are working", "Other open projects"}},
		{"a chat of no task in the api checkout", "chat-2", f.root, `{}`, `# Project "Acme"`,
			[]string{"## y: Other work"}, []string{"you are working"}},
		{"a chat elsewhere: the known order", "", nowhere, `{}`, `# Project "Other"`,
			[]string{"## rl: Rate limit\n"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := call(tt.chat, tt.root, tt.args)
			if !strings.HasPrefix(out, tt.first) {
				t.Errorf("does not start with %q:\n%s", tt.first, out)
			}
			for _, w := range tt.want {
				if !strings.Contains(out, w) {
					t.Errorf("lacks %q:\n%s", w, out)
				}
			}
			for _, w := range tt.not {
				if strings.Contains(out, w) {
					t.Errorf("has %q:\n%s", w, out)
				}
			}
		})
	}
	// The caller's project is left out when it is not trusted, like any.
	f.h.SetUntrusted(f.plan)
	if out := call("chat-1", "", `{}`); strings.Contains(out, "Rate limit") || !strings.HasPrefix(out, `# Project "Other"`) ||
		!strings.Contains(out, "1 project(s) left out") {
		t.Errorf("untrusted:\n%s", out)
	}
}

func TestPlanPromptHasTheContext(t *testing.T) {
	f := ctxFixture(t, nil)
	p, err := f.api.PlanPrompt(f.plan, "Take payments")
	if err != nil {
		t.Fatal(err)
	}
	goal := strings.Index(p.Prompt, "Take payments")
	ctx := strings.Index(p.Prompt, "An online shop. The api serves the web client.")
	tool := strings.Index(p.Prompt, "First read the project's backlog")
	if goal < 0 || ctx < goal || tool < ctx || !strings.Contains(p.Prompt, "Split the goal in the project's terms") {
		t.Errorf("prompt:\n%s", p.Prompt)
	}
	write(t, filepath.Join(f.plan, ContextFile), strings.Repeat("a", maxPlanContext+5))
	p, _ = f.api.PlanPrompt(f.plan, "Take payments")
	if strings.Count(p.Prompt, "a") < maxPlanContext || !strings.Contains(p.Prompt, "[… cut here: the file has 8005 characters; read the rest in "+filepath.Join(f.plan, ContextFile)+"]") {
		t.Errorf("a long file is not cut")
	}
	write(t, filepath.Join(f.plan, ContextFile), "bad\x00")
	p, _ = f.api.PlanPrompt(f.plan, "Take payments")
	if !strings.Contains(p.Prompt, "(The project's PROJECT.md is left out: contains a control character (U+0000).)") {
		t.Errorf("prompt:\n%s", p.Prompt)
	}
	if err := os.Remove(filepath.Join(f.plan, ContextFile)); err != nil {
		t.Fatal(err)
	}
	p, _ = f.api.PlanPrompt(f.plan, "Take payments")
	if strings.Contains(p.Prompt, ContextFile) {
		t.Errorf("prompt:\n%s", p.Prompt)
	}
}

func TestAddContext(t *testing.T) {
	f := newFixture(t, nil)
	_, err := f.api.AddContext(f.plan)
	wantErr(t, err, "not an open project")
	f.open()
	v, err := f.api.AddContext(f.plan)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.plan, ContextFile)
	if v.Path != path || !v.Exists || v.Problem != "" {
		t.Errorf("view %+v", v)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# Acme\n", "## What this project is for", "## Repositories\n\n- api: ", "- web: ", "## Conventions", "## Definition of done"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("template lacks %q:\n%s", want, data)
		}
	}
	if pc := ReadContext(f.plan); pc.Problem != "" || pc.Text == "" {
		t.Errorf("the template is not a usable context: %+v", pc)
	}
	// Never overwrites: a file, or a link (not followed).
	write(t, path, "Mine.")
	_, err = f.api.AddContext(f.plan)
	wantErr(t, err, "has a PROJECT.md already")
	if data, _ := os.ReadFile(path); string(data) != "Mine." {
		t.Errorf("overwritten: %q", data)
	}
	target := filepath.Join(f.base, "target.md")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	symlink(t, target, path)
	_, err = f.api.AddContext(f.plan)
	wantErr(t, err, "already")
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Error("the link was followed")
	}
	// An untrusted project is refused.
	other := newProject(t, filepath.Join(f.base, "other"), nil)
	if _, err := f.api.Open(other); err != nil {
		t.Fatal(err)
	}
	_, err = f.api.AddContext(other)
	wantErr(t, err, "Restricted Mode")
	if _, err := os.Lstat(filepath.Join(other, ContextFile)); err == nil {
		t.Error("written into an untrusted project")
	}
}
