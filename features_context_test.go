package main

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"flint/internal/agenttask"
	"flint/internal/feature"
	"flint/internal/features/projects"
)

// projectsT runs the real Projects module on the fixture's app and returns
// its service.
func projectsT(t *testing.T, a *App) *projects.API {
	t.Helper()
	a.features = feature.New([]feature.Module{projects.Module}, featureHost{a})
	for _, s := range a.features.Services() {
		if api, ok := s.Instance().(*projects.API); ok {
			return api
		}
	}
	t.Fatal("the Projects module has no service")
	return nil
}

// planT writes a trusted project folder next to the fixture's repository
// with these repositories (name → URL), items (id → file) and PROJECT.md,
// and opens it.
func planT(t *testing.T, a *App, api *projects.API, base string, repos [][2]string, items map[string]string, context string) string {
	t.Helper()
	plan := filepath.Join(base, "plan")
	var decl []string
	for _, r := range repos {
		decl = append(decl, `{"name":"`+r[0]+`","url":"`+r[1]+`"}`)
	}
	writeT(t, filepath.Join(plan, projects.ConfigFile), `{"name":"Acme","repositories":[`+strings.Join(decl, ",")+`]}`)
	for id, file := range items {
		writeT(t, filepath.Join(plan, projects.ItemsDir, id+".md"), file)
	}
	writeT(t, filepath.Join(plan, projects.ContextFile), context)
	if err := a.setTrust(plan, true); err != nil {
		t.Fatal(err)
	}
	if _, err := api.Open(plan); err != nil {
		t.Fatal(err)
	}
	return plan
}

// chatsOpened records the chats Go opens (chat:opened).
func chatsOpened(a *App) func() []ChatOpened {
	var mu sync.Mutex
	var opened []ChatOpened
	a.emitHook = func(name string, data ...any) {
		if name == eventChatOpened {
			mu.Lock()
			opened = append(opened, data[0].(ChatOpened))
			mu.Unlock()
		}
	}
	return func() []ChatOpened {
		mu.Lock()
		defer mu.Unlock()
		return append([]ChatOpened(nil), opened...)
	}
}

// A delegated task's chat gets the project context as the start of its first
// message, then the item's text; the task keeps the item's text as its
// description, and its chat is known as the item's to get_project.
func TestDelegatedTaskChatGetsTheProjectContext(t *testing.T) {
	f := queueFixture(t, 0)
	web := f.addRepoT("web", "git@github.com:acme/web.git")
	api := projectsT(t, f.app)
	opened := chatsOpened(f.app)
	plan := planT(t, f.app, api, filepath.Dir(f.repo), [][2]string{{"web", "git@github.com:acme/web.git"}},
		map[string]string{"rl": "---\ntitle: Rate limit\nagent: fake\nrepos: web\n---\nLimit it.\n"}, "Acme is a shop.\n")
	res, err := api.Delegate(plan, "rl", f.repo)
	if err != nil {
		t.Fatal(err)
	}
	f.waitStarts()
	chats := opened()
	if len(res.Tasks) != 1 || len(chats) != 1 || chats[0].TaskID != res.Tasks[0].ID {
		t.Fatalf("tasks %+v, chats %+v", res.Tasks, chats)
	}
	prompt := f.driver.waitPrompts(t, chats[0].Info.ID, 1)[0]
	for _, want := range []string{"## About the project (PROJECT.md)\n\nAcme is a shop.", "## Your item\n\nrl: Rate limit", "- web: git@github.com:acme/web.git (you work in it)"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("first message lacks %q:\n%s", want, prompt)
		}
	}
	if !strings.HasPrefix(prompt, "===== Project context (from Flint) =====\n") || !strings.HasSuffix(prompt, "\n\n===== Your task =====\n\nRate limit\n\nLimit it.") {
		t.Errorf("first message:\n%s", prompt)
	}
	_, task, _ := f.app.taskStore().Find(res.Tasks[0].ID)
	if task.Prompt != "Rate limit\n\nLimit it." || task.Title != "Rate limit" || !strings.HasPrefix(task.Origin, "projects:") || !within(task.Worktree, filepath.Dir(web)) {
		t.Errorf("task %+v", task)
	}
	if got := (featureHost{f.app}).TaskOrigin(chats[0].Info.ID); got != task.Origin {
		t.Errorf("TaskOrigin = %q", got)
	}
	if got := (featureHost{f.app}).TaskOrigin("no-such-chat"); got != "" {
		t.Errorf("TaskOrigin of no chat = %q", got)
	}
}

// The item whose chat starts is in doing in its own context, through the
// real path: the chat starts inside CreateAgentTasks, before Delegate writes
// the status (reviewer's reproduction: "status: todo" 8 times out of 8).
func TestDelegatedTaskContextSaysDoing(t *testing.T) {
	f := queueFixture(t, 0)
	f.addRepoT("web", "git@github.com:acme/web.git")
	api := projectsT(t, f.app)
	opened := chatsOpened(f.app)
	plan := planT(t, f.app, api, filepath.Dir(f.repo), [][2]string{{"web", "git@github.com:acme/web.git"}},
		map[string]string{"rl": "---\ntitle: Rate limit\nagent: fake\nrepos: web\n---\nLimit it.\n"}, "Acme is a shop.\n")
	if _, err := api.Delegate(plan, "rl", f.repo); err != nil {
		t.Fatal(err)
	}
	f.waitStarts()
	chats := opened()
	if len(chats) != 1 {
		t.Fatalf("chats %+v", chats)
	}
	prompt := f.driver.waitPrompts(t, chats[0].Info.ID, 1)[0]
	if !strings.Contains(prompt, "## Your item\n\nrl: Rate limit\nstatus: doing;") {
		t.Errorf("the context does not say the item is doing:\n%s", prompt)
	}
}

// One agent across repositories: the context comes first, then the item's
// text and Flint's list of which folder is which (said once, after it).
func TestDelegatedJointGroupGetsTheProjectContext(t *testing.T) {
	j := newJointFixture(t, 2, 0)
	gitT(t, j.repos[0], "remote", "add", "origin", "git@github.com:acme/api.git")
	gitT(t, j.repos[1], "remote", "add", "origin", "git@github.com:acme/web.git")
	api := projectsT(t, j.app)
	plan := planT(t, j.app, api, filepath.Dir(j.repo), [][2]string{{"api", "git@github.com:acme/api.git"}, {"web", "git@github.com:acme/web.git"}},
		map[string]string{"both": "---\ntitle: Rename the field\nagent: claude\nrepos: api, web\ntogether: true\n---\nIn both.\n"}, "Acme is a shop.")
	res, err := api.Delegate(plan, "both", j.repo)
	if err != nil {
		t.Fatal(err)
	}
	j.waitStarts()
	prompt := j.claude.prompt(t)
	ctx := strings.Index(prompt, "Acme is a shop.")
	end := strings.Index(prompt, "===== Your task =====\n\nRename the field\n\nIn both.")
	folders := strings.Index(prompt, "This task spans 2 repositories.")
	if !strings.HasPrefix(prompt, "===== Project context (from Flint) =====\n") || ctx < 0 || end < ctx || folders < end ||
		!strings.Contains(prompt, "You work in the repositories api, web at once; after your task Flint lists which folder is which.") ||
		strings.Count(prompt, res.Tasks[1].Worktree) != 1 || strings.Count(prompt, "In both.") != 1 {
		t.Errorf("first message:\n%s", prompt)
	}
}

// A task that waits in the queue gets the context of when its chat starts:
// what changed after Delegate is in it.
func TestQueuedDelegatedTaskGetsTheContextAtItsStart(t *testing.T) {
	f := queueFixture(t, 1)
	f.addRepoT("web", "git@github.com:acme/web.git")
	api := projectsT(t, f.app)
	busy := f.create("first")
	f.work(busy, "sa")
	plan := planT(t, f.app, api, filepath.Dir(f.repo), [][2]string{{"web", "git@github.com:acme/web.git"}},
		map[string]string{"rl": "---\ntitle: Rate limit\nagent: fake\nrepos: web\n---\nLimit it.\n"}, "Old context.")
	res, err := api.Delegate(plan, "rl", f.repo)
	if err != nil {
		t.Fatal(err)
	}
	opened := chatsOpened(f.app)
	if res.Tasks[0].State != agenttask.Queued {
		t.Fatalf("task %+v", res.Tasks[0])
	}
	writeT(t, filepath.Join(plan, projects.ContextFile), "New context.")
	f.settle("sa") // a slot frees: the queue starts it
	f.waitStarts()
	chats := opened()
	if len(chats) != 1 || chats[0].TaskID != res.Tasks[0].ID {
		t.Fatalf("chats %+v", chats)
	}
	prompt := f.driver.waitPrompts(t, chats[0].Info.ID, 1)[0]
	if !strings.Contains(prompt, "New context.") || strings.Contains(prompt, "Old context.") {
		t.Errorf("first message:\n%s", prompt)
	}
}

// Start now on a queued task with an origin: Go starts its chat (with the
// context), and the view tells the UI not to start one.
func TestStartNowOnADelegatedTask(t *testing.T) {
	f := queueFixture(t, 1)
	f.addRepoT("web", "git@github.com:acme/web.git")
	api := projectsT(t, f.app)
	busy := f.create("first")
	f.work(busy, "sa")
	plan := planT(t, f.app, api, filepath.Dir(f.repo), [][2]string{{"web", "git@github.com:acme/web.git"}},
		map[string]string{"rl": "---\ntitle: Rate limit\nagent: fake\nrepos: web\n---\nLimit it.\n"}, "Acme is a shop.")
	res, err := api.Delegate(plan, "rl", f.repo)
	if err != nil {
		t.Fatal(err)
	}
	opened := chatsOpened(f.app)
	v, err := f.api.StartNow(res.Tasks[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if !v.FlintStarts || busy.FlintStarts {
		t.Errorf("FlintStarts: delegated %v, ordinary %v", v.FlintStarts, busy.FlintStarts)
	}
	f.waitStarts()
	chats := opened()
	if len(chats) != 1 || chats[0].TaskID != res.Tasks[0].ID {
		t.Fatalf("chats %+v", chats)
	}
	if prompt := f.driver.waitPrompts(t, chats[0].Info.ID, 1)[0]; !strings.Contains(prompt, "Acme is a shop.") {
		t.Errorf("first message:\n%s", prompt)
	}
}

// Tasks without an origin (New Agent Task, the queue) never ask the features.
func TestOrdinaryTasksGetNoContext(t *testing.T) {
	f := queueFixture(t, 1)
	var asked atomic.Int32
	f.app.features = feature.New([]feature.Module{{ID: "probe", New: func(feature.Host) feature.Instance {
		return feature.Instance{TaskContext: func(context.Context, feature.TaskStart) string { asked.Add(1); return "never" }}
	}}}, featureHost{f.app})
	opened := chatsOpened(f.app)
	a := f.create("first")
	f.work(a, "sa")
	b := f.create("second")
	f.settle("sa")
	f.waitStarts()
	chats := opened()
	if len(chats) != 1 || chats[0].TaskID != b.ID {
		t.Fatalf("chats %+v", chats)
	}
	if prompt := f.driver.waitPrompts(t, chats[0].Info.ID, 1)[0]; prompt != "second" || asked.Load() != 0 {
		t.Errorf("first message %q, features asked %d times", prompt, asked.Load())
	}
}
