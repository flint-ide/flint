package cli

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// humanRegistry has one human decision, "run approve <run>", whose Run
// counts its calls, and a command that waits.
func humanRegistry(t *testing.T, key *atomic.Value, ran *atomic.Int32) *Registry {
	t.Helper()
	r, err := NewRegistry([]Command{
		{
			Path: "run approve", Summary: "Approve", Kind: Acts, Human: true, Args: []Arg{{Name: "run"}},
			Ask: func(_ context.Context, c *Call) (Question, error) {
				if c.Arg(0) == "gone" {
					return Question{}, errors.New("no such workflow run")
				}
				return Question{Title: "Approve this step?", Message: "Run " + c.Arg(0), OK: "Approve", Key: key.Load().(string)}, nil
			},
			Run: func(context.Context, *Call) (Result, error) { ran.Add(1); return Result{Text: "approved"}, nil },
		},
		{
			Path: "run wait", Summary: "Wait", Kind: Reads, Waits: true,
			Run: func(ctx context.Context, _ *Call) (Result, error) { <-ctx.Done(); return Result{}, nil },
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// A human decision runs only after a yes in Flint's dialog, for every
// caller; an agent's, a detached or an unknown caller is refused before any
// dialog; the dialog names the process that asked.
func TestHumanDecisionAsksInFlint(t *testing.T) {
	callers := []struct {
		name   string
		caller Caller
		asked  bool // the dialog is shown
	}{
		{"a person in a terminal", Caller{PID: 42, Program: "zsh"}, true},
		{"an agent", Caller{Agent: true, What: "claude", PID: 43}, false},
		{"a detached process", Caller{Detached: true, What: "it has no terminal", PID: 44}, false},
		{"unknown", Caller{Unknown: true, What: "?", PID: 45}, false},
	}
	for _, c := range callers {
		for _, yes := range []bool{false, true} {
			t.Run(c.name, func(t *testing.T) {
				var key atomic.Value
				key.Store("r1/2/1")
				var ran atomic.Int32
				reg := humanRegistry(t, &key, &ran)
				var shown []Question
				env := Env{
					Who: func() Caller { return c.caller },
					Confirm: func(_ context.Context, q Question) bool {
						shown = append(shown, q)
						return yes
					},
				}
				resp := DispatchWith(t.Context(), reg, Request{Args: []string{"run", "approve", "r1"}}, env)
				if (len(shown) == 1) != c.asked {
					t.Fatalf("dialogs %+v", shown)
				}
				if c.asked {
					q := shown[0]
					if q.Title != "Approve this step?" || q.OK != "Approve" ||
						!strings.Contains(q.Message, "Run r1") || !strings.Contains(q.Message, "Asked from the command line (pid 42, zsh).") {
						t.Errorf("dialog %+v", q)
					}
				}
				wantRan := int32(0)
				if c.asked && yes {
					wantRan = 1
				}
				if ran.Load() != wantRan {
					t.Fatalf("ran %d times (yes %v)", ran.Load(), yes)
				}
				switch {
				case wantRan == 1 && (resp.Exit != ExitOK || resp.Stdout != "approved\n"):
					t.Errorf("approved: %+v", resp)
				case c.asked && !yes && (resp.Exit != ExitFailed || resp.Stderr != "flint: not approved in Flint\n"):
					t.Errorf("declined: %+v", resp)
				case !c.asked && (resp.Exit != ExitFailed || !strings.HasPrefix(resp.Stderr, "flint: ")):
					t.Errorf("refused: %+v", resp)
				}
			})
		}
	}
}

// Without a way to ask (Dispatch), a human decision never runs; when what
// it decides changed while the dialog was open, nothing is done; Ask's
// error is the command's.
func TestHumanDecisionNeverRunsUnasked(t *testing.T) {
	var key atomic.Value
	key.Store("r1/2/1")
	var ran atomic.Int32
	reg := humanRegistry(t, &key, &ran)
	resp := Dispatch(t.Context(), reg, Request{Args: []string{"run", "approve", "r1"}}, func() Caller { return Caller{PID: 1} })
	if ran.Load() != 0 || resp.Stderr != "flint: not approved in Flint\n" {
		t.Fatalf("without a dialog: %+v, ran %d", resp, ran.Load())
	}
	env := Env{Who: func() Caller { return Caller{PID: 1} }, Confirm: func(context.Context, Question) bool {
		key.Store("r1/3/1") // the run moved on while the user read
		return true
	}}
	resp = DispatchWith(t.Context(), reg, Request{Args: []string{"run", "approve", "r1"}}, env)
	if ran.Load() != 0 || !strings.Contains(resp.Stderr, "it changed while Flint asked you") {
		t.Fatalf("changed meanwhile: %+v, ran %d", resp, ran.Load())
	}
	resp = DispatchWith(t.Context(), reg, Request{Args: []string{"run", "approve", "gone"}}, env)
	if ran.Load() != 0 || resp.Stderr != "flint: no such workflow run\n" {
		t.Fatalf("Ask's error: %+v", resp)
	}
}

// A human decision needs an Ask and must act; Ask needs Human.
func TestRegistryChecksHumanDecisions(t *testing.T) {
	run := func(context.Context, *Call) (Result, error) { return Result{}, nil }
	ask := func(context.Context, *Call) (Question, error) { return Question{}, nil }
	for name, c := range map[string]Command{
		"no Ask":        {Path: "run approve", Summary: "x", Kind: Acts, Human: true, Run: run},
		"reads":         {Path: "run approve", Summary: "x", Kind: Reads, Human: true, Ask: ask, Run: run},
		"Ask, unmarked": {Path: "run approve", Summary: "x", Kind: Acts, Ask: ask, Run: run},
	} {
		if _, err := NewRegistry([]Command{c}); err == nil {
			t.Errorf("%s: registered", name)
		}
	}
	reg, err := NewRegistry([]Command{{Path: "run approve", Summary: "x", Kind: Acts, Human: true, Ask: ask, Run: run}})
	if err != nil {
		t.Fatal(err)
	}
	if h := Dispatch(t.Context(), reg, Request{Args: []string{"run", "approve", "--help"}}, nil); !strings.Contains(h.Stdout, "Flint asks you first in a dialog") {
		t.Errorf("help %q", h.Stdout)
	}
}

// Commands that wait take at most MaxWaiting connections; another is
// refused at once, and one that ended frees its place.
func TestWaitingCommandsAreBounded(t *testing.T) {
	var key atomic.Value
	key.Store("k")
	var ran atomic.Int32
	reg := humanRegistry(t, &key, &ran)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan Response, MaxWaiting)
	for range MaxWaiting {
		go func() { done <- Dispatch(ctx, reg, Request{Args: []string{"run", "wait"}}, nil) }()
	}
	waitFull(t, reg)
	resp := Dispatch(t.Context(), reg, Request{Args: []string{"run", "wait"}}, nil)
	if resp.Exit != ExitFailed || !strings.Contains(resp.Stderr, "commands are waiting already") {
		t.Fatalf("one more waiter: %+v", resp)
	}
	cancel()
	for range MaxWaiting {
		<-done
	}
	if len(reg.waiting) != 0 {
		t.Errorf("%d places still taken", len(reg.waiting))
	}
}

func waitFull(t *testing.T, reg *Registry) {
	t.Helper()
	for range 1000 {
		if len(reg.waiting) == MaxWaiting {
			return
		}
		select {
		case <-t.Context().Done():
			return
		default:
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("%d places taken, want %d", len(reg.waiting), MaxWaiting)
}
