package agentsession

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"
)

// fakeDriver scripts a turn: optional permission request, then text and turn end.
type fakeDriver struct {
	mu        sync.Mutex
	sinks     map[string]Sink
	responses map[string]string // request id → option id
	permKind  string            // tool kind of the permission request ("" = none)
	paths     []string          // paths of that tool call
	release   chan struct{}     // closed to let the turn finish
	stopped   []string
	answered  chan string
}

func newFake(permKind string, paths ...string) *fakeDriver {
	return &fakeDriver{
		sinks: map[string]Sink{}, responses: map[string]string{}, permKind: permKind, paths: paths,
		release: make(chan struct{}), answered: make(chan string, 4),
	}
}

func (f *fakeDriver) Start(_ context.Context, cwd string, sink Sink) (SessionInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sinks["s1"] = sink
	return SessionInfo{ID: "s1", Cwd: cwd}, nil
}

func (f *fakeDriver) Prompt(ctx context.Context, id, text string, _ []Attachment) (string, error) {
	f.mu.Lock()
	sink := f.sinks[id]
	f.mu.Unlock()
	if f.permKind != "" {
		sink(Event{SessionID: id, Kind: KindPermission, Permission: &PermissionRequest{
			ID: "p1", ToolCall: ToolCall{ID: "t1", Kind: f.permKind, Paths: f.paths},
			Options: []PermissionOption{{ID: "no", Kind: RejectOnce}, {ID: "always", Kind: AllowAlways}, {ID: "yes", Kind: AllowOnce}},
		}})
	}
	sink(Event{SessionID: id, Kind: KindMessageChunk, Text: "echo: " + text})
	select {
	case <-f.release:
	case <-ctx.Done():
		sink(Event{SessionID: id, Kind: KindTurnEnd, StopReason: StopCancelled})
		return StopCancelled, nil
	}
	sink(Event{SessionID: id, Kind: KindTurnEnd, StopReason: StopEndTurn})
	return StopEndTurn, nil
}

func (f *fakeDriver) Cancel(string) error { return nil }

func (f *fakeDriver) Respond(_, req, opt string) error {
	f.mu.Lock()
	f.responses[req] = opt
	f.mu.Unlock()
	f.answered <- opt
	return nil
}

func (f *fakeDriver) Stop(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopped = append(f.stopped, id)
	return nil
}

func (f *fakeDriver) Close() error { return nil }

type recorder struct {
	mu     sync.Mutex
	events []Event
}

func (r *recorder) emit(e Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *recorder) kinds() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.events))
	for i, e := range r.events {
		out[i] = e.Kind
	}
	return out
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in time")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func TestEditsAreAutoApprovedByDefault(t *testing.T) {
	rec := &recorder{}
	m := NewManager(rec.emit)
	fake := newFake(ToolEdit, "/p/src/a.go")
	m.Register("fake", fake)
	info, err := m.Start(context.Background(), "fake", "/p")
	if err != nil || info.Agent != "fake" {
		t.Fatalf("start = %+v, %v", info, err)
	}
	if err := m.Prompt(info.ID, "hi", nil); err != nil {
		t.Fatal(err)
	}
	if opt := <-fake.answered; opt != "yes" {
		t.Errorf("auto-approve picked %q, want allow_once option", opt)
	}
	close(fake.release)
	waitFor(t, func() bool { return contains(rec.kinds(), KindTurnEnd) })
	if contains(rec.kinds(), KindPermission) {
		t.Error("auto-approved request must not reach the UI")
	}
	waitFor(t, func() bool { return !m.Busy(info.ID) })
}

func TestCommandsAskTheUser(t *testing.T) {
	rec := &recorder{}
	m := NewManager(rec.emit)
	fake := newFake(ToolExecute)
	m.Register("fake", fake)
	info, _ := m.Start(context.Background(), "fake", "/p")
	if err := m.Prompt(info.ID, "run tests", nil); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return contains(rec.kinds(), KindPermission) })
	if err := m.Prompt(info.ID, "again", nil); !errors.Is(err, ErrBusy) {
		t.Errorf("second prompt during a turn: %v, want ErrBusy", err)
	}
	if err := m.Respond(info.ID, "p1", "no"); err != nil {
		t.Fatal(err)
	}
	if opt := <-fake.answered; opt != "no" {
		t.Errorf("respond forwarded %q", opt)
	}
	close(fake.release)
	waitFor(t, func() bool { return !m.Busy(info.ID) })
}

func TestPolicyAllAndStop(t *testing.T) {
	rec := &recorder{}
	m := NewManager(rec.emit)
	fake := newFake(ToolExecute)
	m.Register("fake", fake)
	m.SetPolicy(Policy{AutoApproveAll: true})
	info, _ := m.Start(context.Background(), "fake", "/p")
	_ = m.Prompt(info.ID, "x", nil)
	if opt := <-fake.answered; opt != "yes" {
		t.Errorf("AutoApproveAll picked %q", opt)
	}
	if err := m.Stop(info.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return contains(rec.kinds(), KindTurnEnd) }) // cancelled by Stop
	if err := m.Prompt(info.ID, "x", nil); err == nil {
		t.Error("prompt on a stopped session must fail")
	}
	if _, err := m.Start(context.Background(), "nope", "/p"); err == nil {
		t.Error("unknown agent must fail")
	}
	if got := m.Agents(); len(got) != 1 || got[0] != "fake" {
		t.Errorf("agents = %v", got)
	}
}

func TestPolicyMatrix(t *testing.T) {
	cwd := t.TempDir()
	in := cwd + "/src/a.go"
	def := DefaultPolicy()
	tests := []struct {
		name string
		tc   ToolCall
		want bool
	}{
		{"edit inside", ToolCall{Kind: ToolEdit, Paths: []string{in}}, true},
		{"relative edit inside", ToolCall{Kind: ToolEdit, Paths: []string{"src/b.go"}}, true},
		{"delete inside", ToolCall{Kind: ToolDelete, Paths: []string{in}}, true},
		{"read inside", ToolCall{Kind: ToolRead, Paths: []string{in}}, true},
		{"edit without paths", ToolCall{Kind: ToolEdit}, false},
		{"edit shell rc", ToolCall{Kind: ToolEdit, Paths: []string{"/Users/me/.zshrc"}}, false},
		{"edit escaping with ..", ToolCall{Kind: ToolEdit, Paths: []string{cwd + "/../evil"}}, false},
		{"edit git hook", ToolCall{Kind: ToolEdit, Paths: []string{cwd + "/.git/hooks/pre-commit"}}, false},
		{"edit agent config", ToolCall{Kind: ToolEdit, Paths: []string{cwd + "/.claude/settings.json"}}, false},
		{"edit nested git config", ToolCall{Kind: ToolEdit, Paths: []string{cwd + "/vendor/x/.git/config"}}, false},
		{"create a repository", ToolCall{Kind: ToolEdit, Paths: []string{"x/.git/HEAD"}}, false},
		{"edit upper-case git dir", ToolCall{Kind: ToolEdit, Paths: []string{cwd + "/.GIT/config"}}, false},
		{"edit nested editor config", ToolCall{Kind: ToolEdit, Paths: []string{cwd + "/app/.VSCode/tasks.json"}}, false},
		{"edit a .gitignore", ToolCall{Kind: ToolEdit, Paths: []string{cwd + "/app/.gitignore"}}, true},
		{"one path outside", ToolCall{Kind: ToolEdit, Paths: []string{in, "/etc/hosts"}}, false},
		{"read ssh key", ToolCall{Kind: ToolRead, Paths: []string{"/Users/me/.ssh/id_ed25519"}}, false},
		{"execute", ToolCall{Kind: ToolExecute, Paths: []string{in}}, false},
		{"fetch", ToolCall{Kind: ToolFetch}, false},
	}
	for _, tt := range tests {
		if got := def.autoApproves(tt.tc, cwd); got != tt.want {
			t.Errorf("%s: autoApproves = %v, want %v", tt.name, got, tt.want)
		}
	}
	if (Policy{}).autoApproves(ToolCall{Kind: ToolEdit, Paths: []string{in}}, cwd) {
		t.Error("empty policy must ask for everything")
	}
	if !(Policy{AutoApproveAll: true}).autoApproves(ToolCall{Kind: ToolExecute}, cwd) {
		t.Error("AutoApproveAll must allow everything")
	}
}

func TestSymlinkOutsideProjectIsNotInside(t *testing.T) {
	cwd := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, cwd+"/link"); err != nil {
		t.Fatal(err)
	}
	if allInside([]string{cwd + "/link/x.go"}, cwd) {
		t.Error("a symlink pointing outside the project must not count as inside")
	}
}

func TestSymlinkIntoGitDirIsProtected(t *testing.T) {
	cwd := t.TempDir()
	if err := os.MkdirAll(cwd+"/.git", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(cwd+"/.git", cwd+"/gitdir"); err != nil {
		t.Fatal(err)
	}
	if got := ProtectedDir(cwd+"/gitdir/config", cwd); got != ".git" {
		t.Errorf("ProtectedDir through a link into .git = %q", got)
	}
	if got := ProtectedDir(cwd+"/src/a.go", cwd); got != "" {
		t.Errorf("ProtectedDir of a source file = %q", got)
	}
}

func TestEditOutsideProjectAsks(t *testing.T) {
	rec := &recorder{}
	m := NewManager(rec.emit)
	fake := newFake(ToolEdit, "/Users/me/.zshrc")
	m.Register("fake", fake)
	info, _ := m.Start(context.Background(), "fake", "/p")
	_ = m.Prompt(info.ID, "x", nil)
	waitFor(t, func() bool { return contains(rec.kinds(), KindPermission) })
	close(fake.release)
	_ = m.Respond(info.ID, "p1", "no")
	waitFor(t, func() bool { return !m.Busy(info.ID) })
}
func TestEventsAreNumberedPerSession(t *testing.T) {
	rec := &recorder{}
	m := NewManager(rec.emit)
	m.emit(Event{SessionID: "a"})
	m.emit(Event{SessionID: "b"})
	m.emit(Event{SessionID: "a"})
	rec.mu.Lock()
	defer rec.mu.Unlock()
	got := []uint64{rec.events[0].Seq, rec.events[1].Seq, rec.events[2].Seq}
	if got[0] != 1 || got[1] != 1 || got[2] != 2 {
		t.Errorf("seq = %v, want [1 1 2]", got)
	}
}

// resumableFake is a fakeDriver that can also reopen a session.
type resumableFake struct {
	*fakeDriver
	resumed []string
}

func (r *resumableFake) Resume(_ context.Context, cwd, id string, sink Sink) (SessionInfo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if id == "gone" {
		return SessionInfo{}, errors.New("no such conversation")
	}
	r.resumed = append(r.resumed, id)
	r.sinks[id] = sink
	return SessionInfo{ID: id, Cwd: cwd}, nil
}

func TestResumeRegistersTheSession(t *testing.T) {
	rec := &recorder{}
	m := NewManager(rec.emit)
	f := &resumableFake{fakeDriver: newFake("")}
	close(f.release)
	m.Register("fake", f)
	m.Register("plain", newFake(""))

	info, err := m.Resume(context.Background(), "fake", "/w", "old-1")
	if err != nil || info.ID != "old-1" || info.Agent != "fake" {
		t.Fatalf("Resume = %+v, %v", info, err)
	}
	if err := m.Prompt("old-1", "again", nil); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return contains(rec.kinds(), KindTurnEnd) })
	if _, err := m.Resume(context.Background(), "fake", "/w", "gone"); err == nil {
		t.Error("resuming a missing conversation should fail")
	}
	if _, err := m.Resume(context.Background(), "plain", "/w", "x"); !errors.Is(err, ErrNoResume) {
		t.Errorf("driver without Resume: %v", err)
	}
	if _, err := m.Resume(context.Background(), "nope", "/w", "x"); err == nil {
		t.Error("unknown agent should fail")
	}
}

// slowStart blocks Start until release is closed.
type slowStart struct {
	*closeCounting
	started, release chan struct{}
}

func (s *slowStart) Start(ctx context.Context, cwd string, sink Sink) (SessionInfo, error) {
	close(s.started)
	<-s.release
	return s.closeCounting.Start(ctx, cwd, sink)
}

func TestDriverReplacedWhileStartingIsClosed(t *testing.T) {
	m := NewManager(func(Event) {})
	old := &slowStart{closeCounting: &closeCounting{fakeDriver: newFake("")}, started: make(chan struct{}), release: make(chan struct{})}
	m.Register("gemini", old)
	done := make(chan error, 1)
	go func() {
		_, err := m.Start(context.Background(), "gemini", t.TempDir())
		done <- err
	}()
	<-old.started
	m.Unregister("gemini") // settings saved while the chat starts
	close(old.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	if old.closes != 1 {
		t.Errorf("driver replaced during Start closed %d times on Close, want 1", old.closes)
	}
}

// closeCounting records Close calls.
type closeCounting struct {
	*fakeDriver
	closes int
}

func (c *closeCounting) Close() error {
	c.closes++
	return nil
}

func TestReplacedDriverKeepsItsSessions(t *testing.T) {
	m := NewManager(func(Event) {})
	old := &closeCounting{fakeDriver: newFake("")}
	idle := &closeCounting{fakeDriver: newFake("")}
	m.Register("gemini", old)
	m.Register("other", idle)
	if _, err := m.Start(context.Background(), "gemini", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	next := &closeCounting{fakeDriver: newFake("")}
	m.Register("gemini", next) // settings changed
	m.Unregister("other")      // removed from settings
	if got := m.Agents(); len(got) != 1 || got[0] != "gemini" {
		t.Errorf("Agents() = %v, want [gemini]", got)
	}
	// The running session still talks to the driver that started it.
	if err := m.Respond("s1", "p1", "yes"); err != nil {
		t.Fatal(err)
	}
	if opt := <-old.answered; opt != "yes" {
		t.Errorf("old driver got %q", opt)
	}
	if _, err := m.Start(context.Background(), "other", t.TempDir()); err == nil {
		t.Error("an unregistered agent still starts")
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	if old.closes != 1 || next.closes != 1 {
		t.Errorf("Close: old closed %d times, new %d; want both once", old.closes, next.closes)
	}
	if idle.closes != 0 {
		t.Errorf("a removed driver without sessions was kept and closed %d times", idle.closes)
	}
}
