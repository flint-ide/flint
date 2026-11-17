package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	as "flint/internal/agentsession"
	"flint/internal/agenttask"
	"flint/internal/orchestrate"
	"flint/internal/settings"
)

// chatDriver is a fake agent: every Start is a new chat, every Prompt is
// recorded and, when hold is set, stays a running turn until released.
type chatDriver struct {
	mu      sync.Mutex
	next    int
	prompts map[string][]string
	hold    chan struct{}
	stopped []string
}

func newChatDriver() *chatDriver { return &chatDriver{prompts: map[string][]string{}} }

func (d *chatDriver) Start(_ context.Context, cwd string, _ as.Sink) (as.SessionInfo, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.next++
	return as.SessionInfo{ID: fmt.Sprintf("c%d", d.next), Cwd: cwd, Model: "m"}, nil
}

func (d *chatDriver) Prompt(ctx context.Context, id, text string, _ []as.Attachment) (string, error) {
	d.mu.Lock()
	d.prompts[id] = append(d.prompts[id], text)
	hold := d.hold
	d.mu.Unlock()
	if hold != nil {
		select {
		case <-hold:
		case <-ctx.Done():
		}
	}
	return as.StopEndTurn, nil
}

// waitPrompts waits until the agent has been handed n prompts (turns run in the
// background) and returns them.
func (d *chatDriver) waitPrompts(t *testing.T, id string, n int) []string {
	t.Helper()
	for i := 0; i < 500; i++ {
		if got := d.got(id); len(got) >= n {
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the agent was not handed %d prompts", n)
	return nil
}

func (d *chatDriver) got(id string) []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.prompts[id]...)
}

func (d *chatDriver) Cancel(string) error          { return nil }
func (d *chatDriver) Respond(_, _, _ string) error { return nil }
func (d *chatDriver) Close() error                 { return nil }
func (d *chatDriver) Stop(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.stopped = append(d.stopped, id)
	return nil
}

// chatApp is an App with a real session manager over a fake driver; it
// records the events the manager emits and the Wails events the App emits.
type chatApp struct {
	*App
	driver *chatDriver
	mu     sync.Mutex
	events []as.Event
	opened []ChatOpened
}

func newChatApp(t *testing.T) *chatApp {
	t.Helper()
	c := &chatApp{driver: newChatDriver()}
	a := &App{configDir: t.TempDir(), settings: settings.Default(), orch: orchestrate.New(), ctx: context.Background()}
	a.tasks = agenttask.NewTracker(a.taskByFolder, a.taskChanged)
	a.emitHook = func(name string, data ...any) {
		if name == eventChatOpened {
			c.mu.Lock()
			c.opened = append(c.opened, data[0].(ChatOpened))
			c.mu.Unlock()
		}
	}
	m := as.NewManager(func(e as.Event) {
		a.tasks.Observe(e) // a chat in a task's worktree makes the task work, as newAgentManager does
		a.observeChat(e)
		c.mu.Lock()
		c.events = append(c.events, e)
		c.mu.Unlock()
	})
	m.Register("fake", c.driver)
	a.agentMgr, a.agents = m, m
	c.App = a
	return c
}

func (c *chatApp) userMessages() []as.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []as.Event
	for _, e := range c.events {
		if e.Kind == as.KindUserMessage {
			out = append(out, e)
		}
	}
	return out
}

func TestOpenChatRegistersTellsTheUIAndPrompts(t *testing.T) {
	c := newChatApp(t)
	writeRoleFile(t, orchestrate.PersonalRolesDir(c.configDir), "fixer.md", "You fix bugs.")
	parent := as.Origin{SessionID: "p0", Agent: "claude", Role: "lead"}
	info, err := c.openChat(context.Background(), ChatRequest{
		Agent: "fake", Cwd: "/work/repo", Prompt: "fix it, token ghp_0123456789abcdefghijklmnopqrstuvwxyzAB",
		Role: "fixer", TaskID: "t1", Parent: "p0", Origin: &parent,
	})
	if err != nil {
		t.Fatal(err)
	}
	n, ok := c.orch.Get(info.ID)
	if !ok || n.Agent != "fake" || n.Role != "fixer" || n.Parent != "p0" || n.Cwd != "/work/repo" {
		t.Fatalf("registered node = %+v, %v", n, ok)
	}
	if _, ok := c.usageChats.get(info.ID); !ok {
		t.Error("the chat's usage was not bound")
	}
	if len(c.opened) != 1 || c.opened[0].Info.ID != info.ID || c.opened[0].Role != "fixer" ||
		c.opened[0].TaskID != "t1" || c.opened[0].Parent != "p0" || !c.opened[0].Background {
		t.Errorf("chat:opened = %+v", c.opened)
	}
	msgs := c.userMessages()
	if len(msgs) != 1 || msgs[0].Origin == nil || *msgs[0].Origin != parent || strings.Contains(msgs[0].Text, "ghp_0123") {
		t.Fatalf("user_message events = %+v", msgs)
	}
	got := c.driver.waitPrompts(t, info.ID, 1)
	if len(got) != 1 || got[0] != msgs[0].Text || !strings.HasPrefix(got[0], "You fix bugs.") {
		t.Errorf("the agent got %q, the transcript shows %q: they must be the same redacted text", got, msgs[0].Text)
	}
	if n, _ = c.orch.Get(info.ID); n.Live.State == "idle" {
		t.Error("the prompt must have started a turn in the registry")
	}
}

func TestOpenChatFromTheUIIsForegroundAndSilent(t *testing.T) {
	c := newChatApp(t)
	info, err := c.openChat(context.Background(), ChatRequest{Agent: "fake", Cwd: "/work/repo"})
	if err != nil {
		t.Fatal(err)
	}
	if len(c.opened) != 1 || c.opened[0].Background || c.opened[0].Info.ID != info.ID {
		t.Errorf("chat:opened = %+v", c.opened)
	}
	if len(c.driver.got(info.ID)) != 0 || len(c.userMessages()) != 0 {
		t.Error("no prompt was asked for")
	}
}

func TestOpenChatUnknownAgentRegistersNothing(t *testing.T) {
	c := newChatApp(t)
	if _, err := c.openChat(context.Background(), ChatRequest{Agent: "nope", Cwd: "/x"}); err == nil {
		t.Fatal("an unknown agent must fail")
	}
	if len(c.opened) != 0 {
		t.Errorf("chat:opened for a chat that did not start: %+v", c.opened)
	}
}

func TestWebPromptIsNotAnnouncedOrRedacted(t *testing.T) {
	c := newChatApp(t)
	info, _ := c.openChat(context.Background(), ChatRequest{Agent: "fake", Cwd: "/x"})
	if err := c.promptChat(info.ID, "hello", nil, true, nil); err != nil {
		t.Fatal(err)
	}
	if len(c.userMessages()) != 0 {
		t.Error("the web view shows its own messages: no user_message event")
	}
}

func TestPromptFromAnAgentIsRefusedWhileBusyAndNotAnnounced(t *testing.T) {
	c := newChatApp(t)
	c.driver.hold = make(chan struct{})
	t.Cleanup(func() { close(c.driver.hold) })
	info, _ := c.openChat(context.Background(), ChatRequest{Agent: "fake", Cwd: "/x"})
	if err := c.promptChat(info.ID, "first", nil, true, nil); err != nil {
		t.Fatal(err)
	}
	c.driver.waitPrompts(t, info.ID, 1)
	err := c.promptChat(info.ID, "second", nil, false, &as.Origin{SessionID: "p", Agent: "claude"})
	if !errors.Is(err, as.ErrBusy) {
		t.Fatalf("err = %v, want ErrBusy", err)
	}
	if len(c.userMessages()) != 0 || len(c.driver.got(info.ID)) != 1 {
		t.Error("a refused message must not be announced or delivered")
	}
}

func TestStopForgetsTheChat(t *testing.T) {
	c := newChatApp(t)
	info, _ := c.openChat(context.Background(), ChatRequest{Agent: "fake", Cwd: "/x"})
	if err := (&AgentChatAPI{app: c.App}).Stop(info.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.orch.Get(info.ID); ok {
		t.Error("a stopped chat stays registered")
	}
	if _, ok := c.usageChats.get(info.ID); ok {
		t.Error("a stopped chat stays bound to usage")
	}
}

func TestOpenChatPrependsTheRoleToThePrompt(t *testing.T) {
	c := newChatApp(t)
	roles := orchestrate.PersonalRolesDir(c.configDir)
	writeRoleFile(t, roles, "reviewer.md", "---\nagent: fake\n---\nYou review code.")
	writeRoleFile(t, roles, "codexonly.md", "---\nagent: codex\n---\nCodex things.")

	info, err := c.openChat(context.Background(), ChatRequest{Agent: "fake", Cwd: "/x", Role: "reviewer", Prompt: "Look at main.go"})
	if err != nil {
		t.Fatal(err)
	}
	if got := c.driver.waitPrompts(t, info.ID, 1); got[0] != "You review code.\n\nLook at main.go" {
		t.Errorf("prompt = %q", got[0])
	}
	if n, _ := c.orch.Get(info.ID); n.Role != "reviewer" {
		t.Errorf("node role = %q", n.Role)
	}

	// The role names the agent when the request does not.
	if _, err := c.openChat(context.Background(), ChatRequest{Cwd: "/x", Role: "reviewer"}); err != nil {
		t.Errorf("role should supply the agent: %v", err)
	}
	// A role for another agent is refused, and so is an unknown one: before
	// any agent starts.
	started := c.driver.next
	for _, req := range []ChatRequest{
		{Agent: "fake", Cwd: "/x", Role: "codexonly", Prompt: "hi"},
		{Agent: "fake", Cwd: "/x", Role: "nope", Prompt: "hi"},
		{Agent: "fake", Cwd: "/x", Role: "../reviewer", Prompt: "hi"},
	} {
		if _, err := c.openChat(context.Background(), req); err == nil {
			t.Errorf("openChat(%+v) must fail", req)
		}
	}
	if c.driver.next != started {
		t.Error("a refused role must not start an agent")
	}
}

func TestOpenChatIgnoresProjectRolesInRestrictedMode(t *testing.T) {
	c := newChatApp(t)
	project := t.TempDir()
	writeRoleFile(t, orchestrate.ProjectRolesDir(project), "theirs.md", "From the repository.")

	if _, err := c.openChat(context.Background(), ChatRequest{Agent: "fake", Cwd: project, Role: "theirs", Prompt: "hi"}); !errors.Is(err, orchestrate.ErrUnknownRole) {
		t.Fatalf("untrusted project role = %v, want ErrUnknownRole", err)
	}
	c.state.TrustedFolders = []string{cleanFolder(project)}
	info, err := c.openChat(context.Background(), ChatRequest{Agent: "fake", Cwd: project, Role: "theirs", Prompt: "hi"})
	if err != nil {
		t.Fatalf("trusted project role: %v", err)
	}
	if got := c.driver.waitPrompts(t, info.ID, 1); got[0] != "From the repository.\n\nhi" {
		t.Errorf("prompt = %q", got[0])
	}
}

func writeRoleFile(t *testing.T, dir, file, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
