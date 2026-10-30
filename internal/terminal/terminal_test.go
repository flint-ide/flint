package terminal

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"
)

type recorder struct {
	mu     sync.Mutex
	out    bytes.Buffer
	exited chan int
}

func newRecorder() *recorder { return &recorder{exited: make(chan int, 1)} }

func (r *recorder) Output(_ string, data []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.out.Write(data)
}

func (r *recorder) Exited(_ string, code int) { r.exited <- code }

func (r *recorder) output() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.out.String()
}

func waitExit(t *testing.T, r *recorder) int {
	t.Helper()
	select {
	case code := <-r.exited:
		return code
	case <-time.After(5 * time.Second):
		t.Fatalf("terminal did not exit; output so far: %q", r.output())
		return 0
	}
}

func TestShellRoundTrip(t *testing.T) {
	rec := newRecorder()
	m := NewManager(rec)
	if err := m.Start("t1", Options{Cwd: t.TempDir(), Shell: "/bin/sh"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Write("t1", []byte("echo flint-$((40+2))\nexit 3\n")); err != nil {
		t.Fatal(err)
	}
	if code := waitExit(t, rec); code != 3 {
		t.Errorf("exit code = %d, want 3", code)
	}
	if !strings.Contains(rec.output(), "flint-42") {
		t.Errorf("output %q does not contain command result", rec.output())
	}
	if err := m.Write("t1", []byte("x")); err == nil {
		t.Error("write after exit should fail")
	}
}

func TestEnvironmentAndCwd(t *testing.T) {
	rec := newRecorder()
	m := NewManager(rec)
	dir := t.TempDir()
	err := m.Start("t2", Options{Cwd: dir, Shell: "/bin/sh", Args: []string{"-c", "echo $TERM; pwd"}})
	if err != nil {
		t.Fatal(err)
	}
	waitExit(t, rec)
	out := rec.output()
	if !strings.Contains(out, "xterm-256color") {
		t.Errorf("TERM not set: %q", out)
	}
	// macOS temp dirs live behind the /private symlink.
	if !strings.Contains(out, strings.TrimPrefix(dir, "/private")) {
		t.Errorf("cwd not applied: %q (want %s)", out, dir)
	}
}

func TestResizeAndClose(t *testing.T) {
	rec := newRecorder()
	m := NewManager(rec)
	if err := m.Start("t3", Options{Shell: "/bin/sh", Cols: 100, Rows: 30}); err != nil {
		t.Fatal(err)
	}
	if err := m.Start("t3", Options{Shell: "/bin/sh"}); err == nil {
		t.Error("duplicate id should fail")
	}
	if err := m.Resize("t3", 120, 40); err != nil {
		t.Errorf("resize: %v", err)
	}
	m.Close("t3")
	waitExit(t, rec)
	if err := m.Resize("t3", 1, 1); err == nil {
		t.Error("resize after close should fail")
	}
}

func TestCloseAll(t *testing.T) {
	rec := newRecorder()
	m := NewManager(rec)
	if err := m.Start("a", Options{Shell: "/bin/sh"}); err != nil {
		t.Fatal(err)
	}
	m.CloseAll()
	waitExit(t, rec)
}

func TestWinsizeDefaults(t *testing.T) {
	ws := winsize(0, -1)
	if ws.Cols != 80 || ws.Rows != 24 {
		t.Errorf("winsize defaults = %dx%d", ws.Cols, ws.Rows)
	}
}
