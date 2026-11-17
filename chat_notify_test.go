package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	as "flint/internal/agentsession"
	"flint/internal/orchestrate"
)

// family is a chat app with a parent chat and one child it started (the
// child is working on its first prompt).
type family struct {
	*chatApp
	parent, child string
}

func newFamily(t *testing.T) *family {
	t.Helper()
	c := newChatApp(t)
	c.settings.Orchestration.Enabled = true
	p, err := c.openChat(context.Background(), ChatRequest{Agent: "fake", Cwd: "/x"})
	if err != nil {
		t.Fatal(err)
	}
	ch, err := c.openChat(context.Background(), ChatRequest{Agent: "fake", Cwd: "/x", Role: "", Prompt: "work", Parent: p.ID,
		Origin: &as.Origin{SessionID: p.ID, Agent: "fake"}})
	if err != nil {
		t.Fatal(err)
	}
	return &family{chatApp: c, parent: p.ID, child: ch.ID}
}

func (f *family) say(id, text string) {
	f.observeChat(as.Event{SessionID: id, Kind: as.KindMessageChunk, Text: text})
}

func (f *family) end(id, reason string) {
	f.observeChat(as.Event{SessionID: id, Kind: as.KindTurnEnd, StopReason: reason})
}

// idle waits until the fake driver has finished the chat's prompt, so the
// session manager no longer calls it busy.
func (f *family) idle(t *testing.T, id string) {
	t.Helper()
	for i := 0; i < 500 && f.agents.Busy(id); i++ {
		time.Sleep(10 * time.Millisecond)
	}
}

// noticesFor returns the prompts of a chat that are Flint's notices.
func (f *family) noticesFor(id string) []string {
	var out []string
	for _, p := range f.driver.got(id) {
		if strings.HasPrefix(p, "[Flint] child") {
			out = append(out, p)
		}
	}
	return out
}

func (f *family) waitNotices(t *testing.T, id string, n int) []string {
	t.Helper()
	for i := 0; i < 300; i++ {
		if got := f.noticesFor(id); len(got) >= n {
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s got %d notices, want %d", id, len(f.noticesFor(id)), n)
	return nil
}

// quiet checks that no notice arrives (after the delivery goroutines had time).
func (f *family) quiet(t *testing.T, id string) {
	t.Helper()
	time.Sleep(250 * time.Millisecond)
	if got := f.noticesFor(id); len(got) != 0 {
		t.Fatalf("%s was told %q", id, got)
	}
}

func TestParentIsToldWhenTheChildFinishes(t *testing.T) {
	f := newFamily(t)
	writeRoleFile(t, orchestrate.PersonalRolesDir(f.configDir), "x.md", "")
	for i := 0; i < 30; i++ {
		f.say(f.child, fmt.Sprintf("line %d\n", i))
	}
	f.say(f.child, "token ghp_0123456789abcdefghijklmnopqrstuvwxyzAB\n")
	f.end(f.child, as.StopEndTurn)

	got := f.waitNotices(t, f.parent, 1)
	if len(got) != 1 || !strings.HasPrefix(got[0], "[Flint] child "+f.child+" (no role) is finished:\n") {
		t.Fatalf("notice = %q", got)
	}
	lines := strings.Split(got[0], "\n")
	if len(lines) != 1+orchestrate.NoticeLines || !strings.Contains(got[0], "line 29") || strings.Contains(got[0], "line 5\n") || strings.Contains(got[0], "ghp_0123") {
		t.Errorf("notice has %d lines or the wrong ones: %q", len(lines), got[0])
	}
	// It shows in the parent's chat with an origin, so the user sees it.
	var shown *as.Event
	for _, e := range f.userMessages() {
		if e.SessionID == f.parent {
			shown = &e
		}
	}
	if shown == nil || shown.Origin == nil || shown.Origin.Agent != "Flint" || shown.Origin.SessionID != f.child || shown.Text != got[0] {
		t.Errorf("transcript shows %+v", shown)
	}
}

func TestNoticeForEveryStateTheParentActsOn(t *testing.T) {
	for _, tc := range []struct {
		name  string
		event as.Event
		want  string
	}{
		{"failed", as.Event{Kind: as.KindError, Text: "boom"}, "is failed:"},
		{"waiting for approval", as.Event{Kind: as.KindPermission, Permission: &as.PermissionRequest{ID: "p"}}, "is waiting:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFamily(t)
			tc.event.SessionID = f.child
			f.observeChat(tc.event)
			if got := f.waitNotices(t, f.parent, 1); !strings.Contains(got[0], tc.want) {
				t.Errorf("notice = %q", got[0])
			}
		})
	}
}

func TestNoNoticeWithoutAChangeOrWhenSwitchedOff(t *testing.T) {
	f := newFamily(t)
	f.end(f.child, as.StopEndTurn)
	f.waitNotices(t, f.parent, 1)
	f.idle(t, f.parent)
	f.end(f.parent, as.StopEndTurn) // the parent finishes the turn the notice started
	// The child saying the same thing again is no change.
	f.end(f.child, as.StopEndTurn)
	f.end(f.child, as.StopEndTurn)
	time.Sleep(250 * time.Millisecond)
	if got := f.noticesFor(f.parent); len(got) != 1 {
		t.Fatalf("%d notices for one change: %q", len(got), got)
	}

	off := newFamily(t)
	off.settings.Orchestration.NotifyParent = false
	off.end(off.child, as.StopEndTurn)
	off.quiet(t, off.parent)
}

func TestNoticeWaitsForTheParentsTurnToEnd(t *testing.T) {
	f := newFamily(t)
	f.driver.mu.Lock()
	f.driver.hold = make(chan struct{})
	f.driver.mu.Unlock()
	if err := f.promptChat(f.parent, "my own work", nil, true, nil); err != nil {
		t.Fatal(err)
	}
	f.driver.waitPrompts(t, f.parent, 1)

	f.end(f.child, as.StopEndTurn)
	f.quiet(t, f.parent) // the parent is working: held

	close(f.driver.hold) // its turn ends
	f.end(f.parent, as.StopEndTurn)
	if got := f.waitNotices(t, f.parent, 1); len(got) != 1 {
		t.Fatalf("notices = %q", got)
	}
}

func TestNoticeWaitsForTheDraftToClose(t *testing.T) {
	f := newFamily(t)
	api := &AgentChatAPI{app: f.App}
	if err := api.DraftChanged(f.parent, true); err != nil {
		t.Fatal(err)
	}
	f.end(f.child, as.StopEndTurn)
	f.quiet(t, f.parent)
	f.idle(t, f.parent)
	if err := api.DraftChanged(f.parent, false); err != nil {
		t.Fatal(err)
	}
	f.waitNotices(t, f.parent, 1)
}

func TestNoticeWaitsWhileTheParentWaitsForApproval(t *testing.T) {
	f := newFamily(t)
	f.orch.Prompted(f.parent)
	f.observeChat(as.Event{SessionID: f.parent, Kind: as.KindPermission, Permission: &as.PermissionRequest{ID: "p"}})
	f.end(f.child, as.StopEndTurn)
	f.quiet(t, f.parent)
}

func TestNoticesMergePerChildWhileHeld(t *testing.T) {
	f := newFamily(t)
	api := &AgentChatAPI{app: f.App}
	_ = api.DraftChanged(f.parent, true)
	f.observeChat(as.Event{SessionID: f.child, Kind: as.KindPermission, Permission: &as.PermissionRequest{ID: "p"}}) // waiting
	f.observeChat(as.Event{SessionID: f.child, Kind: as.KindPermissionSettled, RequestID: "p"})
	f.observeChat(as.Event{SessionID: f.child, Kind: as.KindError, Text: "boom"}) // then failed
	other, _ := f.openChat(context.Background(), ChatRequest{Agent: "fake", Cwd: "/x", Prompt: "w", Parent: f.parent, Origin: &as.Origin{}})
	f.end(other.ID, as.StopEndTurn)
	f.quiet(t, f.parent)

	f.idle(t, f.parent)
	_ = api.DraftChanged(f.parent, false)
	got := f.waitNotices(t, f.parent, 1)
	if len(got) != 1 || strings.Contains(got[0], "is waiting") || !strings.Contains(got[0], "is failed") || !strings.Contains(got[0], "child "+other.ID) {
		t.Fatalf("one merged notice with the newest state per child expected: %q", got)
	}
}

func TestStoppingTheParentDropsItsNotices(t *testing.T) {
	f := newFamily(t)
	_ = (&AgentChatAPI{app: f.App}).DraftChanged(f.parent, true)
	f.end(f.child, as.StopEndTurn)
	if !f.notices.Pending(f.parent) {
		t.Fatal("the notice was not queued")
	}
	if err := (&AgentChatAPI{app: f.App}).Stop(f.parent); err != nil {
		t.Fatal(err)
	}
	if f.notices.Pending(f.parent) {
		t.Error("the queue outlived the parent")
	}
}

// A turn that a notice started does not notify the parent's own parent when
// it ends, so notices cannot chain into a loop; and a change that is not one
// (the child repeating its state) says nothing.
func TestNoticeTurnsDoNotSetOffMoreNotices(t *testing.T) {
	c := newChatApp(t)
	c.settings.Orchestration.Enabled = true
	g, _ := c.openChat(context.Background(), ChatRequest{Agent: "fake", Cwd: "/x"})
	p, _ := c.openChat(context.Background(), ChatRequest{Agent: "fake", Cwd: "/x", Prompt: "plan", Parent: g.ID, Origin: &as.Origin{}})
	ch, _ := c.openChat(context.Background(), ChatRequest{Agent: "fake", Cwd: "/x", Prompt: "work", Parent: p.ID, Origin: &as.Origin{}})
	f := &family{chatApp: c, parent: p.ID, child: ch.ID}

	// The parent finishes its own first turn: its parent is told.
	f.idle(t, p.ID)
	f.end(p.ID, as.StopEndTurn)
	f.waitNotices(t, g.ID, 1)

	// The child finishes: the parent is told, and that notice starts a turn.
	f.end(ch.ID, as.StopEndTurn)
	f.waitNotices(t, p.ID, 1)
	f.idle(t, p.ID)
	// The turn the notice started ends: nothing goes up again.
	f.end(p.ID, as.StopEndTurn)
	time.Sleep(250 * time.Millisecond)
	if got := f.noticesFor(g.ID); len(got) != 1 {
		t.Fatalf("the grandparent was told %d times: %q", len(got), got)
	}
	// The parent's next turn of its own is news again, once the grandparent
	// has dealt with the first notice.
	f.end(g.ID, as.StopEndTurn)
	f.idle(t, g.ID)
	f.idle(t, p.ID)
	if err := f.promptChat(p.ID, "more of my own", nil, true, nil); err != nil {
		t.Fatal(err)
	}
	f.end(p.ID, as.StopEndTurn)
	f.waitNotices(t, g.ID, 2)
}
