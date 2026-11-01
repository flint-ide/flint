package agenttask

import (
	"strings"
	"testing"

	as "flint/internal/agentsession"
)

func ev(kind string) as.Event { return as.Event{SessionID: "s", Kind: kind} }

func turnEnd(reason string) as.Event {
	return as.Event{SessionID: "s", Kind: as.KindTurnEnd, StopReason: reason}
}

func run(l Live, events ...as.Event) Live {
	for _, e := range events {
		l = Next(l, e)
	}
	return l
}

func TestNextFollowsATurn(t *testing.T) {
	l := Prompted(Live{})
	if l.State != Working {
		t.Fatalf("after a prompt: %s", l.State)
	}
	l = run(l, ev(as.KindMessageChunk), ev(as.KindToolCall))
	if l.State != Working {
		t.Fatalf("while working: %s", l.State)
	}
	l = run(l, ev(as.KindPermission))
	if l.State != Waiting {
		t.Fatalf("permission asked: %s", l.State)
	}
	l = run(l, ev(as.KindPermission), ev(as.KindPermissionSettled))
	if l.State != Waiting || l.Pending != 1 {
		t.Fatalf("one of two answered: %+v", l)
	}
	l = run(l, ev(as.KindPermissionSettled))
	if l.State != Working {
		t.Fatalf("all answered: %s", l.State)
	}
	l = run(l, turnEnd(as.StopEndTurn))
	if l.State != Finished || l.Turn {
		t.Fatalf("turn ended: %+v", l)
	}
}

func TestNextStopReasons(t *testing.T) {
	for reason, want := range map[string]string{
		as.StopEndTurn:   Finished,
		as.StopCancelled: Idle,
		as.StopError:     Failed,
		as.StopRefusal:   Failed,
		as.StopMaxTokens: Failed,
	} {
		if got := run(Prompted(Live{}), turnEnd(reason)).State; got != want {
			t.Errorf("%s: %s, want %s", reason, got, want)
		}
	}
}

func TestNextErrors(t *testing.T) {
	// An early failure ends the turn with only an error event.
	l := run(Prompted(Live{}), as.Event{Kind: as.KindError, Text: "claude: not logged in"})
	if l.State != Failed || l.Error != "claude: not logged in" {
		t.Fatalf("early failure: %+v", l)
	}
	// A non-fatal error mid-turn is overtaken by further work.
	l = run(Prompted(Live{}), as.Event{Kind: as.KindError, Text: "auto-approve failed"}, ev(as.KindToolCallUpdate))
	if l.State != Working || l.Error != "" {
		t.Fatalf("recovered: %+v", l)
	}
	// The kept error text is short and has no secrets.
	l = run(Prompted(Live{}), as.Event{Kind: as.KindError, Text: "token ghp_abcdefghijklmnopqrstuvwxyz0123456789 " + strings.Repeat("x", 1000)})
	if strings.Contains(l.Error, "ghp_abc") || len(l.Error) > maxError+len("…") {
		t.Errorf("error kept as %q", l.Error)
	}
}

func TestNextIgnoresReplayOutsideATurn(t *testing.T) {
	// A resumed chat replays its history; that is no work in progress.
	l := run(Live{State: Finished}, ev(as.KindUserMessage), ev(as.KindMessageChunk), ev(as.KindToolCall))
	if l.State != Finished {
		t.Errorf("replay changed the state to %s", l.State)
	}
}

func TestCombine(t *testing.T) {
	if got := Combine(nil, ""); got.State != Idle {
		t.Errorf("no chats: %s", got.State)
	}
	if got := Combine(nil, Finished); got.State != Finished {
		t.Errorf("no chats, finished before: %s", got.State)
	}
	got := Combine([]Live{{State: Finished}, {State: Waiting}, {State: Working}}, "")
	if got.State != Waiting {
		t.Errorf("combined: %s", got.State)
	}
	got = Combine([]Live{{State: Failed, Error: "boom"}, {State: Finished}}, "")
	if got.State != Failed || got.Error != "boom" {
		t.Errorf("failed wins over finished: %+v", got)
	}
}
