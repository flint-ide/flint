package backlog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"flint/internal/feature/featuretest"
)

// fixture is the API over a fake host with one open, trusted project (a
// checkout of acme/api) and a backlog folder that is not opened yet.
type fixture struct {
	t       *testing.T
	h       *featuretest.Host
	api     *API
	base    string
	project string
	plan    string
}

func newFixture(t *testing.T, items map[string]string) *fixture {
	t.Helper()
	h := featuretest.New(t.Context())
	h.Dir = tempDir(t)
	base := tempDir(t)
	project := filepath.Join(base, "api")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	h.SetRoots(project)
	h.SetRepository(project, "git@github.com:acme/api.git")
	h.SetTrusted(project)
	h.SetAgents("claude", "claude", "backend")
	plan := newBacklog(t, filepath.Join(base, "plan"), items)
	return &fixture{t: t, h: h, api: newAPI(h), base: base, project: project, plan: plan}
}

func (f *fixture) open() {
	f.t.Helper()
	if _, err := f.api.Open(f.plan); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) load() Backlog {
	f.t.Helper()
	b, err := f.api.Load(f.plan)
	if err != nil {
		f.t.Fatal(err)
	}
	return b
}

func (f *fixture) item(b Backlog, id string) ItemView {
	f.t.Helper()
	i := slices.IndexFunc(b.Items, func(v ItemView) bool { return v.ID == id })
	if i < 0 {
		f.t.Fatalf("no item %s in %+v", id, b.Items)
	}
	return b.Items[i]
}

func (f *fixture) read(id string) string {
	f.t.Helper()
	data, err := os.ReadFile(ItemFile(f.plan, id))
	if err != nil {
		f.t.Fatal(err)
	}
	return string(data)
}

func wantErr(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("err = %v, want %q", err, want)
	}
}

func TestKnownBacklogs(t *testing.T) {
	f := newFixture(t, map[string]string{"a": item("Alpha", "todo")})
	if got := f.api.Backlogs(); len(got) != 0 {
		t.Fatalf("Backlogs = %+v", got)
	}
	// Only folders Flint knows are read.
	_, err := f.api.Load(f.plan)
	wantErr(t, err, "not an open backlog")
	_, err = f.api.Load("plan")
	wantErr(t, err, "choose a backlog")
	_, err = f.api.Load("")
	wantErr(t, err, "choose a backlog")

	// Open takes existing folders with flint-backlog.json only.
	empty := filepath.Join(f.base, "empty")
	if err := os.Mkdir(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{
		"relative/plan":                   "choose a folder",
		"":                                "choose a folder",
		filepath.Join(f.base, "missing"):  "no such file",
		filepath.Join(f.plan, ConfigFile): "not a folder",
		empty:                             "has no flint-backlog.json",
		filepath.Join(f.plan, ItemsDir, "..", "nope"): "no such file",
	} {
		_, err := f.api.Open(path)
		wantErr(t, err, want)
	}
	bad := filepath.Join(f.base, "bad")
	write(t, filepath.Join(bad, ConfigFile), `{"repositories":[{"name":"X","url":"u"}]}`)
	_, err = f.api.Open(bad)
	wantErr(t, err, "lowercase")

	// Through a symlink, the backlog is known by its real path.
	link := filepath.Join(f.base, "link")
	if err := os.Symlink(f.plan, link); err != nil {
		t.Fatal(err)
	}
	info, err := f.api.Open(link)
	if err != nil || info.Path != f.plan || info.Name != "Acme" || info.Workspace {
		t.Fatalf("Open = %+v, %v", info, err)
	}
	if b, err := f.api.Load(link); err != nil || b.Path != f.plan {
		t.Errorf("Load through the link = %+v, %v", b.Path, err)
	}
	other := newBacklog(t, filepath.Join(f.base, "other"), nil)
	if _, err := f.api.Open(other); err != nil {
		t.Fatal(err)
	}
	if got := f.api.Backlogs(); len(got) != 2 || got[0].Path != other || got[1].Path != f.plan {
		t.Errorf("most recent first: %+v", got)
	}
	f.open()
	var remembered []string
	if err := json.Unmarshal(f.h.State(stateBacklogs), &remembered); err != nil || !slices.Equal(remembered, []string{f.plan, other}) {
		t.Errorf("state %s", f.h.State(stateBacklogs))
	}

	// A workspace folder with flint-backlog.json is listed without being opened.
	ws := newBacklog(t, filepath.Join(f.base, "ws"), map[string]string{"w": item("W", "todo")})
	f.h.SetRoots(f.project, ws)
	got := f.api.Backlogs()
	if len(got) != 3 || got[2].Path != ws || !got[2].Workspace {
		t.Errorf("workspace backlog: %+v", got)
	}
	if b, err := f.api.Load(ws); err != nil || !b.Workspace || len(b.Items) != 1 {
		t.Errorf("Load(workspace) = %+v, %v", b, err)
	}
	wantErr(t, f.api.Close(ws), "workspace folder")
	f.h.SetRoots(f.project)
	_, err = f.api.Load(ws)
	wantErr(t, err, "not an open backlog")

	// Close forgets the folder; a folder that disappeared can still be closed.
	if err := f.api.Close(other); err != nil {
		t.Fatal(err)
	}
	_, err = f.api.Load(other)
	wantErr(t, err, "not an open backlog")
	wantErr(t, f.api.Close(other), "not an open backlog")
	if err := os.RemoveAll(f.plan); err != nil {
		t.Fatal(err)
	}
	if got := f.api.Backlogs(); len(got) != 1 || got[0].Error == "" {
		t.Errorf("a gone backlog: %+v", got)
	}
	_, err = f.api.Load(f.plan)
	wantErr(t, err, "is gone")
	if err := f.api.Close(f.plan); err != nil {
		t.Fatal(err)
	}
	if got := f.api.Backlogs(); len(got) != 0 {
		t.Errorf("after Close: %+v", got)
	}
	if n := len(f.h.Events()); n < 4 {
		t.Errorf("%d events", n)
	}
}

func TestCreateBacklog(t *testing.T) {
	f := newFixture(t, nil)
	dir := filepath.Join(f.base, "new")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	info, err := f.api.Create(dir, " My Plan ")
	if err != nil || info.Name != "My Plan" || info.Path != dir {
		t.Fatalf("Create = %+v, %v", info, err)
	}
	if st, err := os.Stat(filepath.Join(dir, ItemsDir)); err != nil || !st.IsDir() {
		t.Errorf("items folder: %v", err)
	}
	if got := f.api.Backlogs(); len(got) != 1 || got[0].Path != dir {
		t.Errorf("Backlogs = %+v", got)
	}
	_, err = f.api.Create(dir, "")
	wantErr(t, err, "already a backlog")

	dir2 := filepath.Join(f.base, "new2")
	if err := os.Mkdir(dir2, 0o755); err != nil {
		t.Fatal(err)
	}
	if info, err := f.api.Create(dir2, ""); err != nil || info.Name != "new2" {
		t.Errorf("default name: %+v, %v", info, err)
	}
	_, err = f.api.Create(filepath.Join(f.base, "absent"), "x")
	wantErr(t, err, "no such file")
	_, err = f.api.Create("rel", "x")
	wantErr(t, err, "choose a folder")
	dir3 := filepath.Join(f.base, "new3")
	if err := os.Mkdir(dir3, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err = f.api.Create(dir3, strings.Repeat("n", 81))
	wantErr(t, err, "at most 80")
	// A dangling link in place of flint-backlog.json is not followed.
	if err := os.Symlink(filepath.Join(f.base, "elsewhere.json"), filepath.Join(dir3, ConfigFile)); err != nil {
		t.Fatal(err)
	}
	_, err = f.api.Create(dir3, "x")
	wantErr(t, err, "already a backlog")
	if _, err := os.Stat(filepath.Join(f.base, "elsewhere.json")); err == nil {
		t.Error("Create wrote through a link")
	}
}

func TestNewItemAndSetStatus(t *testing.T) {
	f := newFixture(t, map[string]string{
		"crlf":  "---\r\ntitle: Windows\r\nowner: me\r\nstatus: todo\r\n---\r\nText\r\n",
		"plain": "---\ntitle: Plain\n# keep me\nestimate: 2d\n---\nText\n",
		"bad":   "---\ntitle: Bad\nstatus: todo\nstatus: doing\n---\n",
	})
	_, err := f.api.NewItem(f.plan, "x")
	wantErr(t, err, "not an open backlog")
	f.open()
	it, err := f.api.NewItem(f.plan, "Rate limit: checkout")
	if err != nil || it.ID != "rate-limit-checkout" || it.Title != "Rate limit: checkout" || it.Status != StatusTodo || it.Path != ItemFile(f.plan, it.ID) {
		t.Fatalf("NewItem = %+v, %v", it, err)
	}
	if it2, err := f.api.NewItem(f.plan, "Rate limit checkout"); err != nil || it2.ID != "rate-limit-checkout-2" {
		t.Errorf("second item = %+v, %v", it2, err)
	}
	for _, bad := range []string{"", "a\nb", strings.Repeat("t", 121)} {
		if _, err := f.api.NewItem(f.plan, bad); err == nil {
			t.Errorf("NewItem(%q) accepted", bad)
		}
	}
	if b := f.load(); len(b.Items) != 5 || len(b.Problems) != 0 {
		t.Errorf("after NewItem: %d items, problems %+v", len(b.Items), b.Problems)
	}

	// SetStatus changes the status line only.
	v, err := f.api.SetStatus(f.plan, "crlf", "review")
	if err != nil || v.Status != "review" {
		t.Fatalf("SetStatus = %+v, %v", v, err)
	}
	if got := f.read("crlf"); got != "---\r\ntitle: Windows\r\nowner: me\r\nstatus: review\r\n---\r\nText\r\n" {
		t.Errorf("crlf file: %q", got)
	}
	if _, err := f.api.SetStatus(f.plan, "plain", "done"); err != nil {
		t.Fatal(err)
	}
	if got := f.read("plain"); got != "---\ntitle: Plain\n# keep me\nestimate: 2d\nstatus: done\n---\nText\n" {
		t.Errorf("plain file: %q", got)
	}
	for _, tc := range []struct{ folder, id, status, want string }{
		{f.plan, "../plain", "done", "invalid item id"},
		{f.plan, "", "done", "invalid item id"},
		{f.plan, "plain", "started", "unknown status"},
		{f.plan, "missing", "done", "no such file"},
		{f.plan, "bad", "done", "two status lines"},
		{f.base, "plain", "done", "not an open backlog"},
	} {
		_, err := f.api.SetStatus(tc.folder, tc.id, tc.status)
		wantErr(t, err, tc.want)
	}
	if f.read("bad") != "---\ntitle: Bad\nstatus: todo\nstatus: doing\n---\n" {
		t.Error("a refused file was changed")
	}
	if ev := f.h.Events(); len(ev) == 0 || ev[len(ev)-1].Name != EventChanged || ev[len(ev)-1].Data[0] != f.plan {
		t.Errorf("events %+v", ev)
	}
}

func TestNewItemLimit(t *testing.T) {
	f := newFixture(t, nil)
	f.open()
	for i := range MaxItems {
		write(t, filepath.Join(f.plan, ItemsDir, fmt.Sprintf("i%03d.md", i)), item("I", "todo"))
	}
	_, err := f.api.NewItem(f.plan, "One more")
	wantErr(t, err, "500 items")
}

func TestRepositoryResolution(t *testing.T) {
	f := newFixture(t, nil)
	f.open()
	// A task worktree of acme/web is open: it is not a checkout to delegate to.
	worktree := filepath.Join(f.base, "web-task")
	if err := os.Mkdir(worktree, 0o755); err != nil {
		t.Fatal(err)
	}
	f.h.SetRepository(worktree, "https://github.com/acme/web")
	f.h.SetTaskWorktree(worktree)
	f.h.SetRoots(f.project, worktree)
	b := f.load()
	if len(b.Repositories) != 2 {
		t.Fatalf("repositories %+v", b.Repositories)
	}
	api, web := b.Repositories[0], b.Repositories[1]
	if api.Folder != f.project || !api.InWorkspace || !api.Trusted || api.Located {
		t.Errorf("api = %+v", api)
	}
	if web.Folder != "" || web.InWorkspace {
		t.Errorf("web = %+v", web)
	}

	// Locate… takes a checkout of that repository only.
	web1 := filepath.Join(f.base, "src", "web")
	if err := os.MkdirAll(filepath.Join(web1, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	f.h.SetRepository(web1, "git@github.com:other/web.git", "git@GitHub.com:acme/web.git")
	for _, tc := range []struct{ folder, repo, dir, want string }{
		{f.plan, "web", f.project, "no remote with this repository's URL"},
		{f.plan, "web", filepath.Join(f.base, "src"), "not a git checkout"},
		{f.plan, "web", "src/web", "choose a folder"},
		{f.plan, "web", filepath.Join(f.base, "nope"), "no such file"},
		{f.plan, "web", worktree, "agent task's worktree"},
		{f.plan, "db", web1, `no repository "db"`},
		{f.base, "web", web1, "not an open backlog"},
	} {
		_, err := f.api.Locate(tc.folder, tc.repo, tc.dir)
		wantErr(t, err, tc.want)
	}
	v, err := f.api.Locate(f.plan, "web", web1)
	if err != nil || v.Folder != web1 || !v.Located || v.InWorkspace || v.Trusted || v.Problem != "" {
		t.Fatalf("Locate = %+v, %v", v, err)
	}
	var cs []Checkout
	if err := json.Unmarshal(f.h.State(stateCheckouts), &cs); err != nil || len(cs) != 1 || cs[0] != (Checkout{"github.com/acme/web", web1}) {
		t.Errorf("checkouts %s", f.h.State(stateCheckouts))
	}
	// The UI attaches the parent folder: the checkout is in the workspace now.
	f.h.SetRoots(f.project, filepath.Join(f.base, "src"))
	f.h.SetTrusted(filepath.Join(f.base, "src", "web"))
	web = f.load().Repositories[1]
	if web.Folder != web1 || !web.InWorkspace || !web.Trusted || !web.Located {
		t.Errorf("attached: %+v", web)
	}
	// Opening the checkout itself in the workspace wins over the located one.
	web2 := filepath.Join(f.base, "web2")
	if err := os.Mkdir(web2, 0o755); err != nil {
		t.Fatal(err)
	}
	f.h.SetRepository(web2, "https://github.com/acme/web.git")
	f.h.SetRoots(f.project, web2)
	if web = f.load().Repositories[1]; web.Folder != web2 || web.Located {
		t.Errorf("workspace checkout: %+v", web)
	}
	// A located folder that is gone is a problem.
	f.h.SetRoots(f.project)
	if err := os.RemoveAll(web1); err != nil {
		t.Fatal(err)
	}
	if web = f.load().Repositories[1]; web.Problem == "" || !strings.Contains(web.Problem, "gone") {
		t.Errorf("gone: %+v", web)
	}
}

func TestLoadReportsAgents(t *testing.T) {
	f := newFixture(t, map[string]string{
		"a": item("A", "todo", "agent: backend"),
		"b": item("B", "todo", "agent: frontend"),
		"c": item("C", "todo"),
	})
	f.open()
	b := f.load()
	if b.DefaultAgent != "claude" || f.item(b, "a").AgentProblem != "" || f.item(b, "c").AgentProblem != "" ||
		!strings.Contains(f.item(b, "b").AgentProblem, `"frontend" is not available`) {
		t.Errorf("agents: %+v", b)
	}
	if len(b.Problems) != 1 || b.Problems[0].File != "items/b.md" {
		t.Errorf("problems %+v", b.Problems)
	}
	f.h.SetAgents("")
	if b := f.load(); !strings.Contains(f.item(b, "c").AgentProblem, "no agent can start") {
		t.Errorf("no agent: %+v", f.item(b, "c"))
	}
}
