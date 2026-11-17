package main

import (
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"flint/internal/terminal"
)

// The window's side against a real host process: the test binary runs itself
// with --agent-host (TestMain), as Flint does. A shell started by one
// "window" survives its detach and is attached, with its output, by the next;
// Quit and Stop ends the host.
func TestBackgroundTerminalsEndToEnd(t *testing.T) {
	cfg, err := os.MkdirTemp("/tmp", "fb") // short: a socket path is limited to about 100 bytes
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(cfg) })
	t.Setenv("FLINT_CONFIG_DIR", cfg)
	t.Setenv("XDG_RUNTIME_DIR", cfg)
	t.Setenv("FLINT_TEST_AGENT_HOST", "1")

	window := func(events *termRecorder) *hostTerminals {
		h := newHostTerminals(hostTermConfig{
			ConfigDir: cfg, Version: "e2e", Events: events, Local: terminal.NewManager(events),
			Dial: dialHost, Spawn: spawnHost,
			Choose: func(string, string, []string) string { return "" },
		})
		h.connect()
		return h
	}

	first := newTermEventsRecorder()
	w1 := window(first)
	if !w1.connected() {
		t.Fatalf("no host: %s", w1.takeNotice())
	}
	pid := w1.current().Hello().Pid
	t.Cleanup(func() { // never leave a host behind
		if syscall.Kill(pid, 0) == nil {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
	if err := w1.Start("t1", terminal.Options{Cwd: cfg, Shell: "/bin/sh"}); err != nil {
		t.Fatal(err)
	}
	if err := w1.Write("t1", []byte("echo e2e-$((40+2))\n")); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the first window's output", func() bool { return strings.Contains(first.output("t1"), "e2e-42") })

	w1.detach(false) // the window closes
	time.Sleep(100 * time.Millisecond)
	if syscall.Kill(pid, 0) != nil {
		t.Fatal("the host exited when the window detached")
	}

	second := newTermEventsRecorder()
	w2 := window(second)
	if got := w2.current().Hello().Pid; got != pid {
		t.Fatalf("the second window found host %d, want %d", got, pid)
	}
	list, err := w2.live()
	if err != nil || len(list) != 1 || list[0].ID != "t1" || list[0].Exited {
		t.Fatalf("live = %+v, %v", list, err)
	}
	if err := w2.attach("t1"); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the replay", func() bool { return strings.Contains(second.output("t1"), "e2e-42") })
	if err := w2.Write("t1", []byte("echo still-$((1+1))\n")); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "live output after the replay", func() bool { return strings.Contains(second.output("t1"), "still-2") })

	// An agent's state reported in a shell the host runs reaches the window's
	// term:agent path (terminal.Events.AgentState) through term.agent.
	if err := w2.Write("t1", []byte(`printf '\033]6977;state=waiting\007'`+"\n")); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the agent state from the host", func() bool { return second.agentState("t1") == "waiting" })

	w2.detach(true) // Quit and Stop Background Sessions
	deadline := time.Now().Add(15 * time.Second)
	for syscall.Kill(pid, 0) == nil && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if syscall.Kill(pid, 0) == nil {
		t.Fatal("the host still runs after Quit and Stop")
	}
}
