package terminal

import (
	"io"
	"strings"
	"testing"
)

func TestStreamSessionForwardsOutputInputAndExit(t *testing.T) {
	rec := newRecorder()
	m := NewManager(rec)
	pr, pw := io.Pipe()
	var got strings.Builder
	var sizes [][2]int
	err := m.StartStream("s1", Stream{
		Reader:   pr,
		Writer:   &got,
		Resize:   func(c, r int) error { sizes = append(sizes, [2]int{c, r}); return nil },
		Close:    func() { _ = pw.Close() },
		ExitCode: func() int { return 7 },
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.StartStream("s1", Stream{Reader: pr}) == nil {
		t.Error("duplicate id accepted")
	}
	_, _ = pw.Write([]byte("hello\r\n"))
	if err := m.Write("s1", []byte("ls\n")); err != nil || got.String() != "ls\n" {
		t.Fatalf("write %v %q", err, got.String())
	}
	if err := m.Resize("s1", 100, 30); err != nil || sizes[0] != [2]int{100, 30} {
		t.Fatalf("resize %v %v", err, sizes)
	}
	if _, err := m.Cwd("s1"); err == nil {
		t.Error("stream has no cwd")
	}
	m.Close("s1")
	if code := waitExit(t, rec); code != 7 {
		t.Errorf("exit %d", code)
	}
	if !strings.Contains(rec.output(), "hello") {
		t.Errorf("out=%q", rec.output())
	}
	if err := m.Write("s1", []byte("x")); err == nil {
		t.Error("write after close should fail")
	}
}

func TestReadOnlyStreamRefusesInput(t *testing.T) {
	rec := newRecorder()
	m := NewManager(rec)
	pr, pw := io.Pipe()
	_ = m.StartStream("s2", Stream{Reader: pr, Close: func() { _ = pw.Close() }})
	if err := m.Write("s2", []byte("x")); err == nil {
		t.Error("read-only stream accepted input")
	}
	m.CloseAll()
	waitExit(t, rec)
}
