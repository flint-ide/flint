package client_test

import (
	"context"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"flint/internal/agenthost"
	"flint/internal/agenthost/client"
	as "flint/internal/agentsession"
	"flint/internal/sessions"
	"flint/internal/terminal"
)

// env is what one implementation of the session interfaces offers the
// contract: the interfaces themselves, and what they reported.
type env struct {
	terms  sessions.Terminals
	agents sessions.Agents
	rec    *termRecorder
	events chan as.Event
	driver *fakeDriver
}

type factory func(t *testing.T) *env

// inProcess is today's setup: the managers called directly.
func inProcess(t *testing.T) *env {
	e := &env{rec: newTermRecorder(), events: make(chan as.Event, 1024), driver: newFakeDriver()}
	m := as.NewManager(func(ev as.Event) { e.events <- ev })
	m.Register("fake", e.driver)
	e.terms, e.agents = terminal.NewManager(e.rec), m
	t.Cleanup(func() { e.terms.CloseAll(); _ = e.agents.Close() })
	return e
}

// overThePipe is the background host: the same managers behind a server, a
// client over net.Pipe in front.
func overThePipe(t *testing.T) *env {
	e := &env{rec: newTermRecorder(), events: make(chan as.Event, 1024), driver: newFakeDriver()}
	const token = "0123456789abcdef0123456789abcdef"
	srv, err := agenthost.New(agenthost.Config{
		Token: token, Version: "test",
		Terminals: func(ev terminal.Events) sessions.Terminals { return terminal.NewManager(ev) },
		Agents: func(emit func(as.Event)) sessions.Agents {
			m := as.NewManager(emit)
			m.Register("fake", e.driver)
			return m
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	a, b := net.Pipe()
	go func() { _ = srv.Serve(b) }()
	c, err := client.New(a, client.Config{Token: token, Handlers: client.Handlers{
		Terminal: e.rec, Agent: func(ev as.Event) { e.events <- ev },
	}})
	if err != nil {
		t.Fatal(err)
	}
	e.terms, e.agents = c.Terminals(), c.Agents()
	t.Cleanup(func() {
		e.terms.CloseAll()
		_ = e.agents.Close()
		_ = c.Close()
		srv.Close()
	})
	return e
}

func TestContract(t *testing.T) {
	for name, mk := range map[string]factory{"in-process": inProcess, "client+server": overThePipe} {
		t.Run(name, func(t *testing.T) { runContract(t, mk) })
	}
}

// sh runs a script in /bin/sh.
func sh(script string) terminal.Options {
	return terminal.Options{Shell: "/bin/sh", Args: []string{"-c", script}}
}

func runContract(t *testing.T, mk factory) {
	t.Run("terminal output arrives in order, then the exit", func(t *testing.T) {
		e := mk(t)
		o := sh("for i in 1 2 3 4 5 6 7 8; do echo line$i; done; exit 7")
		o.Cwd = t.TempDir()
		if err := e.terms.Start("t1", o); err != nil {
			t.Fatal(err)
		}
		if code := e.rec.waitExit(t, "t1"); code != 7 {
			t.Errorf("exit code = %d, want 7", code)
		}
		out := strings.ReplaceAll(e.rec.output("t1"), "\r", "")
		if want := "line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\n"; out != want {
			t.Errorf("output = %q, want %q", out, want)
		}
	})

	t.Run("input reaches the shell", func(t *testing.T) {
		e := mk(t)
		if err := e.terms.Start("t1", sh("read x; echo got:$x; exit 5")); err != nil {
			t.Fatal(err)
		}
		if err := e.terms.Write("t1", []byte("ping\n")); err != nil {
			t.Fatal(err)
		}
		e.rec.waitOutput(t, "t1", "got:ping")
		if code := e.rec.waitExit(t, "t1"); code != 5 {
			t.Errorf("exit code = %d, want 5", code)
		}
	})

	t.Run("resize changes the terminal's size", func(t *testing.T) {
		e := mk(t)
		if err := e.terms.Start("t1", sh("read x; stty size; exit 0")); err != nil {
			t.Fatal(err)
		}
		if err := e.terms.Resize("t1", 120, 50); err != nil {
			t.Fatal(err)
		}
		if err := e.terms.Write("t1", []byte("\n")); err != nil {
			t.Fatal(err)
		}
		e.rec.waitOutput(t, "t1", "50 120")
	})

	t.Run("cwd is where the shell is", func(t *testing.T) {
		e := mk(t)
		dir := t.TempDir()
		o := sh("read x")
		o.Cwd = dir
		if err := e.terms.Start("t1", o); err != nil {
			t.Fatal(err)
		}
		got, err := e.terms.Cwd("t1")
		if err != nil {
			t.Fatal(err)
		}
		want, _ := filepath.EvalSymlinks(dir)
		if real, _ := filepath.EvalSymlinks(got); real != want {
			t.Errorf("cwd = %q, want %q", got, want)
		}
	})

	t.Run("resume says nothing is running in a plain shell", func(t *testing.T) {
		e := mk(t)
		if err := e.terms.Start("t1", sh("read x")); err != nil {
			t.Fatal(err)
		}
		if r, err := e.terms.Resume("t1"); err != nil || r != (terminal.Resume{}) {
			t.Errorf("resume = %+v, %v", r, err)
		}
		if _, err := e.terms.Resume("nope"); err == nil {
			t.Error("resume of an unknown terminal succeeded")
		}
	})

	t.Run("close hangs up and the exit is reported", func(t *testing.T) {
		e := mk(t)
		if err := e.terms.Start("t1", sh("read x")); err != nil {
			t.Fatal(err)
		}
		e.terms.Close("t1")
		e.rec.waitExit(t, "t1")
		if err := e.terms.Write("t1", []byte("x")); err == nil {
			t.Error("write to a closed terminal succeeded")
		}
		e.terms.Close("t1") // closing again, or an unknown id, is not an error
		e.terms.Close("never-existed")
	})

	t.Run("close all hangs up every terminal", func(t *testing.T) {
		e := mk(t)
		for _, id := range []string{"a", "b", "c"} {
			if err := e.terms.Start(id, sh("read x")); err != nil {
				t.Fatal(err)
			}
		}
		e.terms.CloseAll()
		for _, id := range []string{"a", "b", "c"} {
			e.rec.waitExit(t, id)
		}
	})

	t.Run("a terminal can be started again once its shell ended", func(t *testing.T) {
		e := mk(t)
		for range 2 {
			if err := e.terms.Start("t1", sh("echo again; exit 0")); err != nil {
				t.Fatal(err)
			}
			e.rec.until(t, "the shell to end", func() bool {
				e.rec.mu.Lock()
				defer e.rec.mu.Unlock()
				_, done := e.rec.exits["t1"]
				return done && strings.Count(e.rec.out["t1"].String(), "again") >= 1
			})
			e.rec.mu.Lock()
			delete(e.rec.exits, "t1")
			e.rec.out["t1"].Reset()
			e.rec.mu.Unlock()
		}
	})

	t.Run("terminal errors", func(t *testing.T) {
		e := mk(t)
		if err := e.terms.Write("nope", []byte("x")); err == nil {
			t.Error("write to an unknown terminal succeeded")
		}
		if err := e.terms.Resize("nope", 80, 24); err == nil {
			t.Error("resize of an unknown terminal succeeded")
		}
		if _, err := e.terms.Cwd("nope"); err == nil {
			t.Error("cwd of an unknown terminal succeeded")
		}
		if err := e.terms.Start("t1", sh("read x")); err != nil {
			t.Fatal(err)
		}
		if err := e.terms.Start("t1", sh("read x")); err == nil {
			t.Error("a second terminal under the same id started")
		}
		if err := e.terms.Start("t2", terminal.Options{Shell: "/nonexistent/shell"}); err == nil {
			t.Error("a missing shell started")
		}
	})

	t.Run("agents lists the registered drivers", func(t *testing.T) {
		e := mk(t)
		if got := e.agents.Agents(); len(got) != 1 || got[0] != "fake" {
			t.Errorf("agents = %v", got)
		}
	})

	t.Run("agent prompt streams events in order", func(t *testing.T) {
		e := mk(t)
		info := startChat(t, e)
		if info.Agent != "fake" || info.Cwd != "/work" {
			t.Errorf("info = %+v", info)
		}
		if err := e.agents.Prompt(info.ID, "hi", nil); err != nil {
			t.Fatal(err)
		}
		got := e.collectUntilTurnEnd(t, info.ID)
		if len(got) != 2 || got[0].Kind != as.KindMessageChunk || got[0].Text != "echo:hi" || got[0].Seq != 1 || got[1].Seq != 2 {
			t.Errorf("events = %+v", got)
		}
		if err := e.agents.Prompt("nope", "hi", nil); err == nil {
			t.Error("prompt to an unknown chat succeeded")
		}
		if _, err := e.agents.Start(context.Background(), "nobody", "/work"); err == nil {
			t.Error("start with an unknown agent succeeded")
		}
	})

	t.Run("busy while a turn runs, and a second prompt is refused", func(t *testing.T) {
		e := mk(t)
		info := startChat(t, e)
		if e.agents.Busy(info.ID) {
			t.Error("busy before any prompt")
		}
		if err := e.agents.Prompt(info.ID, "block", nil); err != nil {
			t.Fatal(err)
		}
		if !e.agents.Busy(info.ID) {
			t.Error("not busy during a turn")
		}
		if err := e.agents.Prompt(info.ID, "again", nil); err == nil {
			t.Error("a second prompt during a turn was accepted")
		}
		close(e.driver.release)
		e.collectUntilTurnEnd(t, info.ID)
		eventually(t, "the chat to be idle", func() bool { return !e.agents.Busy(info.ID) })
	})

	t.Run("cancel ends the turn", func(t *testing.T) {
		e := mk(t)
		info := startChat(t, e)
		if err := e.agents.Prompt(info.ID, "block", nil); err != nil {
			t.Fatal(err)
		}
		if err := e.agents.Cancel(info.ID); err != nil {
			t.Fatal(err)
		}
		got := e.collectUntilTurnEnd(t, info.ID)
		if last := got[len(got)-1]; last.StopReason != as.StopCancelled {
			t.Errorf("stop reason = %q", last.StopReason)
		}
		if err := e.agents.Cancel("nope"); err == nil {
			t.Error("cancel of an unknown chat succeeded")
		}
	})

	t.Run("a permission request waits for respond", func(t *testing.T) {
		e := mk(t)
		info := startChat(t, e)
		if err := e.agents.Prompt(info.ID, "ask", nil); err != nil {
			t.Fatal(err)
		}
		got := e.collectUntil(t, as.KindPermission)
		perm := got[len(got)-1].Permission
		if perm == nil || perm.ID != "req1" || len(perm.Options) != 2 {
			t.Fatalf("permission = %+v", perm)
		}
		if err := e.agents.Respond(info.ID, perm.ID, "yes"); err != nil {
			t.Fatal(err)
		}
		if opt := <-e.driver.answered; opt != "yes" {
			t.Errorf("the agent heard %q", opt)
		}
		e.collectUntilTurnEnd(t, info.ID)
	})

	t.Run("the policy decides what is answered without asking", func(t *testing.T) {
		e := mk(t)
		if got := e.agents.Policy(); got != as.DefaultPolicy() {
			t.Errorf("default policy = %+v", got)
		}
		want := as.Policy{AutoApproveAll: true}
		e.agents.SetPolicy(want)
		if got := e.agents.Policy(); got != want {
			t.Fatalf("policy = %+v, want %+v", got, want)
		}
		info := startChat(t, e)
		if err := e.agents.Prompt(info.ID, "ask", nil); err != nil {
			t.Fatal(err)
		}
		if opt := <-e.driver.answered; opt != "yes" {
			t.Errorf("auto answer = %q", opt)
		}
		for _, ev := range e.collectUntilTurnEnd(t, info.ID) {
			if ev.Kind == as.KindPermission {
				t.Error("a request that the policy answers was shown")
			}
		}
	})

	t.Run("resume reopens a conversation", func(t *testing.T) {
		e := mk(t)
		info, err := e.agents.Resume(context.Background(), "fake", "/work", "old-1")
		if err != nil || info.ID != "old-1" {
			t.Fatalf("resume = %+v, %v", info, err)
		}
		if _, err := e.agents.Resume(context.Background(), "fake", "/work", "gone"); err == nil {
			t.Error("resume of a missing conversation succeeded")
		}
	})

	t.Run("stop ends the chat", func(t *testing.T) {
		e := mk(t)
		info := startChat(t, e)
		if err := e.agents.Stop(info.ID); err != nil {
			t.Fatal(err)
		}
		if got := <-e.driver.stopped; got != info.ID {
			t.Errorf("stopped %q", got)
		}
		if err := e.agents.Prompt(info.ID, "hi", nil); err == nil {
			t.Error("prompt to a stopped chat succeeded")
		}
	})

	t.Run("close stops the open chats", func(t *testing.T) {
		e := mk(t)
		info := startChat(t, e)
		if err := e.agents.Close(); err != nil {
			t.Fatal(err)
		}
		select {
		case got := <-e.driver.stopped:
			if got != info.ID {
				t.Errorf("stopped %q", got)
			}
		case <-time.After(wait):
			t.Fatal("the chat was not stopped")
		}
	})
}

func startChat(t *testing.T, e *env) as.SessionInfo {
	t.Helper()
	info, err := e.agents.Start(context.Background(), "fake", "/work")
	if err != nil {
		t.Fatal(err)
	}
	return info
}

// collectUntil reads events up to and including the first of kind.
func (e *env) collectUntil(t *testing.T, kind string) []as.Event {
	t.Helper()
	var got []as.Event
	for {
		select {
		case ev := <-e.events:
			got = append(got, ev)
			if ev.Kind == kind {
				return got
			}
		case <-time.After(wait):
			t.Fatalf("no %s event; got %+v", kind, got)
		}
	}
}

func (e *env) collectUntilTurnEnd(t *testing.T, id string) []as.Event {
	t.Helper()
	got := e.collectUntil(t, as.KindTurnEnd)
	for _, ev := range got {
		if ev.SessionID != id {
			t.Errorf("event of chat %q, want %q", ev.SessionID, id)
		}
	}
	return got
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(wait)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
