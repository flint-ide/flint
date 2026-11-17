package main

import (
	"context"
	"errors"
	"testing"

	as "flint/internal/agentsession"
	"flint/internal/orchestrate"
)

var sender = as.Origin{SessionID: "parent", Agent: "claude", Role: "lead"}

func TestSendToChatRefusesAndLeavesTheTargetUnprompted(t *testing.T) {
	permission := as.Event{Kind: as.KindPermission, Permission: &as.PermissionRequest{ID: "p"}}
	for _, tc := range []struct {
		name  string
		setup func(c *chatApp, id string)
		want  error
	}{
		{"draft open", func(c *chatApp, id string) {
			if err := (&AgentChatAPI{app: c.App}).DraftChanged(id, true); err != nil {
				t.Fatal(err)
			}
		}, orchestrate.ErrDraftOpen},
		{"waiting for approval", func(c *chatApp, id string) {
			c.orch.Prompted(id)
			e := permission
			e.SessionID = id
			c.orch.Observe(e)
		}, orchestrate.ErrWaitingApproval},
		{"busy", func(c *chatApp, id string) {
			c.driver.hold = make(chan struct{})
			t.Cleanup(func() { close(c.driver.hold) })
			if err := c.promptChat(id, "working", nil, true, nil); err != nil {
				t.Fatal(err)
			}
			c.driver.waitPrompts(t, id, 1)
		}, orchestrate.ErrBusy},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newChatApp(t)
			info, err := c.openChat(context.Background(), ChatRequest{Agent: "fake", Cwd: "/x"})
			if err != nil {
				t.Fatal(err)
			}
			tc.setup(c, info.ID)
			before := len(c.driver.got(info.ID))

			if err := c.sendToChat(info.ID, "do this", sender); !errors.Is(err, tc.want) {
				t.Fatalf("sendToChat = %v, want %v", err, tc.want)
			}
			if got := len(c.driver.got(info.ID)); got != before {
				t.Errorf("the target was prompted %d time(s), want %d", got, before)
			}
			if len(c.userMessages()) != 0 {
				t.Error("a refused message must not appear in the target's transcript")
			}
		})
	}
}

func TestSendToChatDeliversRedactedTextWithOrigin(t *testing.T) {
	c := newChatApp(t)
	info, _ := c.openChat(context.Background(), ChatRequest{Agent: "fake", Cwd: "/x"})
	api := &AgentChatAPI{app: c.App}
	// A draft that was closed again does not block.
	_ = api.DraftChanged(info.ID, true)
	_ = api.DraftChanged(info.ID, false)

	if err := c.sendToChat(info.ID, "use ghp_0123456789abcdefghijklmnopqrstuvwxyzAB", sender); err != nil {
		t.Fatal(err)
	}
	got := c.driver.waitPrompts(t, info.ID, 1)
	msgs := c.userMessages()
	if len(msgs) != 1 || msgs[0].Origin == nil || *msgs[0].Origin != sender || msgs[0].Text != got[0] {
		t.Fatalf("messages = %+v, agent got %q", msgs, got)
	}
	if got[0] != "use [github token]" {
		t.Errorf("text = %q, want it redacted", got[0])
	}
}

func TestSendToUnknownChat(t *testing.T) {
	c := newChatApp(t)
	if err := c.sendToChat("nope", "hi", sender); !errors.Is(err, orchestrate.ErrUnknown) {
		t.Fatalf("err = %v, want ErrUnknown", err)
	}
}

func TestDraftChangedValidatesTheID(t *testing.T) {
	c := newChatApp(t)
	info, _ := c.openChat(context.Background(), ChatRequest{Agent: "fake", Cwd: "/x"})
	api := &AgentChatAPI{app: c.App}
	for _, bad := range []string{"", "nope", "../c1"} {
		if err := api.DraftChanged(bad, true); err == nil {
			t.Errorf("DraftChanged(%q) must fail", bad)
		}
	}
	if err := api.DraftChanged(info.ID, true); err != nil {
		t.Fatal(err)
	}
	if n, _ := c.orch.Get(info.ID); !n.Draft {
		t.Error("the draft flag was not set")
	}
	_ = api.DraftChanged(info.ID, false)
	if n, _ := c.orch.Get(info.ID); n.Draft {
		t.Error("the draft flag was not cleared")
	}
}
