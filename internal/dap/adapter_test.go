package dap

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// The test binary doubles as a fake debug adapter on stdio when
// FLINT_FAKE_DAP is set: it answers launch only after configurationDone (as
// debugpy does), stops at the first breakpoint and ends on continue.
func TestMain(m *testing.M) {
	if mode := os.Getenv("FLINT_FAKE_DAP"); mode != "" {
		fakeStdioAdapter(mode)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func fakeStdioAdapter(mode string) {
	r := bufio.NewReader(os.Stdin)
	seq := 0
	send := func(m message) {
		seq++
		m.Seq = seq
		b, _ := json.Marshal(m)
		fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n%s", len(b), b)
	}
	ok := true
	reply := func(req message, body any) {
		send(message{Type: "response", RequestSeq: req.Seq, Command: req.Command, Success: &ok, Body: mustJSON(body)})
	}
	var launch message
	line := 0
	for {
		body, err := readMessage(r)
		if err != nil {
			return
		}
		var m message
		_ = json.Unmarshal(body, &m)
		switch m.Command {
		case "initialize":
			reply(m, map[string]any{"supportsConfigurationDoneRequest": true})
			send(message{Type: "event", Event: "initialized"})
		case "launch":
			if mode == "fail" {
				f := false
				fmt.Fprintln(os.Stderr, "rdbg: cannot load script")
				send(message{Type: "response", RequestSeq: m.Seq, Command: m.Command, Success: &f, Message: "no such file"})
				continue
			}
			launch = m // answered after configurationDone
			send(message{Type: "event", Event: "output", Body: mustJSON(OutputEvent{Category: "console", Output: string(m.Arguments)})})
		case "setBreakpoints":
			var args struct {
				Breakpoints []SourceBreakpoint `json:"breakpoints"`
			}
			_ = json.Unmarshal(m.Arguments, &args)
			out := []Breakpoint{}
			for _, b := range args.Breakpoints {
				out = append(out, Breakpoint{Verified: true, Line: b.Line})
				line = b.Line
			}
			reply(m, map[string]any{"breakpoints": out})
		case "configurationDone":
			reply(m, nil)
			reply(launch, nil)
			send(message{Type: "event", Event: "stopped", Body: mustJSON(StoppedEvent{Reason: "breakpoint", ThreadID: 1})})
		case "threads":
			reply(m, map[string]any{"threads": []Thread{{ID: 1, Name: "main"}}})
		case "stackTrace":
			reply(m, map[string]any{"stackFrames": []StackFrame{{ID: 7, Name: "main", Line: line, Source: &Source{Path: "/p/app.rb"}}}})
		case "continue":
			reply(m, nil)
			send(message{Type: "event", Event: "terminated"})
		case "disconnect":
			reply(m, nil)
			return
		default:
			f := false
			send(message{Type: "response", RequestSeq: m.Seq, Command: m.Command, Success: &f, Message: "unsupported"})
		}
	}
}

func fakeAdapterSpec(t *testing.T, mode string) Adapter {
	t.Helper()
	exe, err := os.Executable()
	must(t, err)
	return Adapter{Argv: []string{exe}, Env: append(os.Environ(), "FLINT_FAKE_DAP="+mode), Dir: t.TempDir(), ID: "fake"}
}

func TestStartAdapterOverStdio(t *testing.T) {
	events := make(chan Event, 64)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	args := json.RawMessage(`{"script":"/p/app.rb","n":12345678901234567}`)
	s, err := StartAdapter(ctx, fakeAdapterSpec(t, "ok"), args, map[string][]SourceBreakpoint{"/p/app.rb": {{Line: 3}}}, func(e Event) { events <- e })
	must(t, err)
	defer s.Stop()

	out := waitEvent[OutputEvent](t, events, "output")
	if out.Output != string(args) {
		t.Errorf("launch arguments = %s, want %s", out.Output, args)
	}
	stop := waitEvent[StoppedEvent](t, events, "stopped")
	frames, err := s.StackTrace(ctx, stop.ThreadID, 20)
	must(t, err)
	if len(frames) != 1 || frames[0].Line != 3 {
		t.Fatalf("frames = %+v", frames)
	}
	must(t, s.Continue(ctx, stop.ThreadID))
	select {
	case <-s.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("adapter still running after terminated")
	}
}

func TestStartAdapterReportsLaunchFailureWithStderr(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := StartAdapter(ctx, fakeAdapterSpec(t, "fail"), nil, nil, func(Event) {})
	if err == nil || !strings.Contains(err.Error(), "no such file") {
		t.Fatalf("err = %v", err)
	}
}

func TestExtensionDebuggersSkipGoAndFirstWins(t *testing.T) {
	SetExtensionDebuggers([]ExtensionDebugger{
		{Type: "a", Languages: []string{"ruby", "go"}, Source: "me.a"},
		{Type: "b", Languages: []string{"ruby", "python"}, Source: "me.b"},
	})
	t.Cleanup(func() { SetExtensionDebuggers(nil) })
	if d, ok := ExtensionDebuggerFor("ruby"); !ok || d.Type != "a" {
		t.Errorf("ruby = %+v, %v", d, ok)
	}
	if d, ok := ExtensionDebuggerFor("python"); !ok || d.Type != "b" {
		t.Errorf("python = %+v, %v", d, ok)
	}
	if _, ok := ExtensionDebuggerFor("go"); ok {
		t.Error("an extension took over Go debugging")
	}
}
