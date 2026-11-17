package main

import (
	"fmt"
	"io"
	"strings"
	"testing"

	"flint/internal/terminal"
)

// fakeTerminals records calls and knows only the ids it was started with.
type fakeTerminals struct {
	calls  []string
	known  map[string]bool
	closed bool
}

func newFakeTerminals() *fakeTerminals { return &fakeTerminals{known: map[string]bool{}} }

func (f *fakeTerminals) check(op, id string) error {
	f.calls = append(f.calls, op+":"+id)
	if !f.known[id] {
		return fmt.Errorf("terminal %q not running", id)
	}
	return nil
}

func (f *fakeTerminals) Start(id string, _ terminal.Options) error {
	f.calls = append(f.calls, "start:"+id)
	f.known[id] = true
	return nil
}
func (f *fakeTerminals) StartStream(id string, _ terminal.Stream) error {
	f.calls = append(f.calls, "stream:"+id)
	f.known[id] = true
	return nil
}
func (f *fakeTerminals) Write(id string, _ []byte) error  { return f.check("write", id) }
func (f *fakeTerminals) Resize(id string, _, _ int) error { return f.check("resize", id) }
func (f *fakeTerminals) Cwd(id string) (string, error)    { return "", f.check("cwd", id) }
func (f *fakeTerminals) Resume(id string) (terminal.Resume, error) {
	return terminal.Resume{}, f.check("resume", id)
}
func (f *fakeTerminals) Close(id string) { f.calls = append(f.calls, "close:"+id) }
func (f *fakeTerminals) CloseAll()       { f.closed = true }

func (f *fakeTerminals) saw(call string) bool {
	for _, c := range f.calls {
		if c == call {
			return true
		}
	}
	return false
}

func TestTerminalsRouterRoutesByOwner(t *testing.T) {
	main, streams := newFakeTerminals(), newFakeTerminals()
	r := newTerminalsRouter(main, streams)

	if err := r.Start("shell", terminal.Options{}); err != nil {
		t.Fatal(err)
	}
	if err := r.StartStream("docker", terminal.Stream{Reader: io.NopCloser(strings.NewReader(""))}); err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{"shell", "docker"} {
		if err := r.Write(id, nil); err != nil {
			t.Fatalf("Write %s: %v", id, err)
		}
		if err := r.Resize(id, 80, 24); err != nil {
			t.Fatalf("Resize %s: %v", id, err)
		}
		if _, err := r.Cwd(id); err != nil {
			t.Fatalf("Cwd %s: %v", id, err)
		}
		if _, err := r.Resume(id); err != nil {
			t.Fatalf("Resume %s: %v", id, err)
		}
	}
	if !main.saw("write:shell") || main.saw("write:docker") {
		t.Errorf("main calls = %v", main.calls)
	}
	if !streams.saw("write:docker") || streams.saw("write:shell") || streams.saw("start:shell") {
		t.Errorf("streams calls = %v", streams.calls)
	}
}

func TestTerminalsRouterCloseGoesToOwnerAndForgets(t *testing.T) {
	main, streams := newFakeTerminals(), newFakeTerminals()
	r := newTerminalsRouter(main, streams)
	_ = r.StartStream("docker", terminal.Stream{})
	_ = r.Start("shell", terminal.Options{})

	r.Close("docker")
	r.Close("shell")
	if !streams.saw("close:docker") || main.saw("close:docker") {
		t.Errorf("docker close: main %v, streams %v", main.calls, streams.calls)
	}
	if !main.saw("close:shell") || streams.saw("close:shell") {
		t.Errorf("shell close: main %v, streams %v", main.calls, streams.calls)
	}
	// Once closed, the id is no longer a stream's.
	delete(main.known, "docker")
	if err := r.Write("docker", nil); err == nil || !main.saw("write:docker") {
		t.Errorf("Write after Close = %v, main calls %v", err, main.calls)
	}
}

func TestTerminalsRouterCloseAllReachesBoth(t *testing.T) {
	main, streams := newFakeTerminals(), newFakeTerminals()
	r := newTerminalsRouter(main, streams)
	r.CloseAll()
	if !main.closed || !streams.closed {
		t.Errorf("CloseAll: main %v, streams %v", main.closed, streams.closed)
	}
}

func TestTerminalsRouterUnknownIDErrorUnchanged(t *testing.T) {
	r := newTerminalsRouter(newFakeTerminals(), newFakeTerminals())
	err := r.Write("nope", nil)
	if err == nil || err.Error() != `terminal "nope" not running` {
		t.Fatalf("Write unknown = %v", err)
	}
}

func TestTerminalsRouterFailedStreamStartIsNotRemembered(t *testing.T) {
	main := newFakeTerminals()
	r := newTerminalsRouter(main, failingStreams{newFakeTerminals()})
	if err := r.StartStream("x", terminal.Stream{}); err == nil {
		t.Fatal("expected error")
	}
	_ = r.Write("x", nil)
	if !main.saw("write:x") {
		t.Errorf("id of a failed stream start went to the streams manager")
	}
}

type failingStreams struct{ *fakeTerminals }

func (failingStreams) StartStream(string, terminal.Stream) error { return fmt.Errorf("boom") }
