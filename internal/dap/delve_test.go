package dap

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const program = `package main

import "fmt"

func main() {
	greeting := "hello"
	count := 3
	for i := 0; i < count; i++ {
		fmt.Println(greeting, i)
	}
}
`

// TestDelveStopsAtBreakpoint debugs a real program with dlv: breakpoint, stack,
// locals, step, continue to exit.
func TestDelveStopsAtBreakpoint(t *testing.T) {
	dlv, err := exec.LookPath("dlv")
	if err != nil {
		t.Skip("dlv not installed")
	}
	dir, err := filepath.EvalSymlinks(t.TempDir()) // macOS: /var → /private/var
	must(t, err)
	must(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module demo\n\ngo 1.22\n"), 0o644))
	main := filepath.Join(dir, "main.go")
	must(t, os.WriteFile(main, []byte(program), 0o644))

	events := make(chan Event, 64)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	s, err := StartDelve(ctx, dlv, os.Environ(), LaunchConfig{Mode: "debug", Program: dir, Cwd: dir},
		map[string][]SourceBreakpoint{main: {{Line: 9}}}, func(e Event) { events <- e })
	if err != nil && strings.Contains(err.Error(), "stub exited") {
		// macOS debugserver refused to attach (developer mode off or no permission).
		t.Skipf("dlv cannot debug on this machine: %v", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer s.Stop()

	stop := waitEvent[StoppedEvent](t, events, "stopped")
	if stop.Reason != "breakpoint" {
		t.Fatalf("stopped reason = %q", stop.Reason)
	}
	frames, err := s.StackTrace(ctx, stop.ThreadID, 20)
	must(t, err)
	if len(frames) == 0 || frames[0].Line != 9 || frames[0].Source == nil || frames[0].Source.Path != main {
		t.Fatalf("top frame = %+v", frames[0])
	}
	scopes, err := s.Scopes(ctx, frames[0].ID)
	must(t, err)
	vars, err := s.Variables(ctx, scopes[0].VariablesReference)
	must(t, err)
	values := map[string]string{}
	for _, v := range vars {
		values[v.Name] = v.Value
	}
	if values["greeting"] != `"hello"` || values["count"] != "3" || values["i"] != "0" {
		t.Fatalf("locals = %v", values)
	}
	res, err := s.Evaluate(ctx, "count * 2", frames[0].ID)
	must(t, err)
	if res.Result != "6" {
		t.Fatalf("count * 2 = %q", res.Result)
	}

	// Clearing the breakpoint lets the program run to the end.
	_, err = s.SetBreakpoints(ctx, main, nil)
	must(t, err)
	must(t, s.Continue(ctx, stop.ThreadID))
	var out strings.Builder
	for {
		e := next(t, events)
		if e.Name == "output" {
			var o OutputEvent
			_ = json.Unmarshal(e.Body, &o)
			out.WriteString(o.Output)
		}
		if e.Name == "terminated" || e.Name == "exited" {
			break
		}
	}
	if !strings.Contains(out.String(), "hello 2") {
		t.Fatalf("program output = %q", out.String())
	}
	// The session ends by itself once the program is done: dlv must not linger.
	select {
	case <-s.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("dlv still running after the program terminated")
	}
	// The debug binary is built outside the project.
	if leftovers, _ := filepath.Glob(filepath.Join(dir, "__debug_bin*")); len(leftovers) > 0 {
		t.Fatalf("debug binary left in the project: %v", leftovers)
	}
}

func waitEvent[T any](t *testing.T, events <-chan Event, name string) T {
	t.Helper()
	for {
		e := next(t, events)
		if e.Name != name {
			continue
		}
		var body T
		must(t, json.Unmarshal(e.Body, &body))
		return body
	}
}

func next(t *testing.T, events <-chan Event) Event {
	t.Helper()
	select {
	case e := <-events:
		return e
	case <-time.After(time.Minute):
		t.Fatal("timed out waiting for a debug event")
		return Event{}
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
