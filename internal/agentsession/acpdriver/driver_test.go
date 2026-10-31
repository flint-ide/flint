package acpdriver

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"flint/internal/acp/acptest"
	as "flint/internal/agentsession"
)

// TestMain lets the test binary double as the fake ACP agent.
func TestMain(m *testing.M) {
	acptest.RunIfAgent()
	os.Exit(m.Run())
}

func fakeOpts(t *testing.T, mode string, extraEnv ...string) Options {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return Options{Name: "fake", Command: exe, Env: append(acptest.Env(mode), extraEnv...)}
}

func testCtx(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// recorder is a Sink that keeps every event.
type recorder struct {
	mu     sync.Mutex
	events []as.Event
	onPerm func(as.Event)
}

func (r *recorder) sink(e as.Event) {
	r.mu.Lock()
	r.events = append(r.events, e)
	onPerm := r.onPerm
	r.mu.Unlock()
	if e.Kind == as.KindPermission && onPerm != nil {
		go onPerm(e)
	}
}

func (r *recorder) text() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var b strings.Builder
	for _, e := range r.events {
		if e.Kind == as.KindMessageChunk {
			b.WriteString(e.Text)
		}
	}
	return b.String()
}

func TestStartRefusedInUntrustedFolder(t *testing.T) {
	opts := fakeOpts(t, "")
	opts.Untrusted = func(string) bool { return true }
	d := New(opts)
	_, err := d.Start(testCtx(t), t.TempDir(), func(as.Event) {})
	if !errors.Is(err, ErrRestricted) || !strings.Contains(err.Error(), "trust this folder to use fake") {
		t.Fatalf("Start in an untrusted folder = %v, want ErrRestricted naming the agent", err)
	}
	if _, err := Headless(testCtx(t), opts, t.TempDir(), "hi"); !errors.Is(err, ErrRestricted) {
		t.Fatalf("Headless in an untrusted folder = %v, want ErrRestricted", err)
	}
}

func TestChatTurnWithPermission(t *testing.T) {
	cwd := t.TempDir()
	file := filepath.Join(cwd, "a.txt")
	if err := os.WriteFile(file, []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := New(fakeOpts(t, ""))
	t.Cleanup(func() { _ = d.Close() })
	rec := &recorder{}
	var sessionID string
	rec.onPerm = func(e as.Event) {
		if err := d.Respond(sessionID, e.Permission.ID, "allow-once"); err != nil {
			t.Errorf("Respond: %v", err)
		}
	}
	info, err := d.Start(testCtx(t), cwd, rec.sink)
	if err != nil {
		t.Fatal(err)
	}
	sessionID = info.ID
	if info.Agent != "fake" || info.Cwd != cwd {
		t.Errorf("SessionInfo = %+v", info)
	}
	stop, err := d.Prompt(testCtx(t), info.ID, "full", []as.Attachment{{Path: file}})
	if err != nil || stop != as.StopEndTurn {
		t.Fatalf("Prompt = %q, %v", stop, err)
	}
	if got := rec.text(); got != "read:two\nwrote" {
		t.Errorf("agent said %q", got)
	}
	if data, _ := os.ReadFile(file); string(data) != "patched\n" {
		t.Errorf("file = %q, want the approved write", data)
	}
}

func TestPromptBlocksWithoutEmbeddedContext(t *testing.T) {
	att := []as.Attachment{{Path: "/p/a.go", Text: "x := 1", StartLine: 3, EndLine: 4}, {Path: "/p/b.go"}}
	embedded := promptBlocks("hi", att, true)
	if embedded[1].Type != "resource" || embedded[2].Type != "resource_link" {
		t.Errorf("with embedded context: %+v", embedded)
	}
	plain := promptBlocks("hi", att, false)
	if plain[1].Type != "text" || plain[1].Text != "/p/a.go (lines 3–4):\nx := 1" {
		t.Errorf("without embedded context: %+v", plain[1])
	}
}

func TestHeadlessDeclinesEverything(t *testing.T) {
	cwd := t.TempDir()
	answer, err := Headless(testCtx(t), fakeOpts(t, acptest.ModeHeadless), cwd, "describe the diff")
	if err != nil {
		t.Fatal(err)
	}
	// The commentary before the tool call is dropped; the permission was
	// rejected and the write refused (a JSON-RPC error code).
	if !strings.HasPrefix(answer, "perm:selected:reject-once write:-") {
		t.Errorf("answer = %q", answer)
	}
	if _, err := os.Stat(filepath.Join(cwd, "notes.txt")); !os.IsNotExist(err) {
		t.Errorf("headless run wrote a file: %v", err)
	}
}

func TestHeadlessAnswer(t *testing.T) {
	answer, err := Headless(testCtx(t), fakeOpts(t, acptest.ModeEcho), t.TempDir(), "write a message")
	if err != nil || answer != "echo:write a message" {
		t.Fatalf("Headless = %q, %v", answer, err)
	}
}

func TestHeadlessTimeoutStopsAgent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	_, err := Headless(ctx, fakeOpts(t, ""), t.TempDir(), "hang")
	if err == nil || !strings.Contains(err.Error(), "did not answer in time") {
		t.Fatalf("Headless = %v, want a timeout", err)
	}
	if time.Since(start) > 10*time.Second {
		t.Errorf("Headless took %v after its deadline", time.Since(start))
	}
}

func TestProbe(t *testing.T) {
	info, err := Probe(testCtx(t), fakeOpts(t, ""), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "fake-agent" || info.Version != "1.2.3" || len(info.AuthMethods) != 1 || info.AuthMethods[0] != "Log in" {
		t.Errorf("Probe = %+v", info)
	}
	opts := fakeOpts(t, "noinit")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := Probe(ctx, opts, t.TempDir()); err == nil || !strings.Contains(err.Error(), "in time") {
		t.Errorf("Probe of a silent agent = %v", err)
	}
}

func TestStopKillsProcessGroup(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pids")
	d := New(fakeOpts(t, "", acptest.PIDFileEnv(pidFile)))
	info, err := d.Start(testCtx(t), t.TempDir(), func(as.Event) {})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	var pids []int
	for _, f := range strings.Fields(string(data)) {
		pid, _ := strconv.Atoi(f)
		pids = append(pids, pid)
	}
	if len(pids) != 2 {
		t.Fatalf("pids = %q", data)
	}
	if err := d.Stop(info.ID); err != nil {
		t.Fatal(err)
	}
	for _, pid := range pids {
		waitGone(t, pid)
	}
}

// waitGone waits until pid no longer exists (a child of the killed agent is
// reaped by launchd/init shortly after).
func waitGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			t.Fatalf("process %d still runs after Stop", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
