package agentsession

import (
	"context"
	"testing"
)

// slotDriver records the CallerSlot its Start received and what the slot said
// while Start was still running (the token of a starting session is issued
// at that point).
type slotDriver struct {
	*fakeDriver
	slot      *CallerSlot
	idInStart string
}

func (d *slotDriver) Start(ctx context.Context, cwd string, sink Sink) (SessionInfo, error) {
	d.slot = CallerSlotFrom(ctx)
	d.idInStart = d.slot.ID()
	return d.fakeDriver.Start(ctx, cwd, sink)
}

func TestManagerFillsCallerSlotAfterStart(t *testing.T) {
	d := &slotDriver{fakeDriver: newFake("")}
	m := NewManager(func(Event) {})
	m.Register("fake", d)
	info, err := m.Start(context.Background(), "fake", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if d.slot == nil {
		t.Fatal("the driver's context carries no CallerSlot")
	}
	if d.idInStart != "" {
		t.Errorf("slot said %q before the session id was known", d.idInStart)
	}
	if got := d.slot.ID(); got != info.ID {
		t.Errorf("slot id = %q, want %q", got, info.ID)
	}
}

func TestCallerSlotNilSafe(t *testing.T) {
	var s *CallerSlot
	s.Set("x")
	if s.ID() != "" || CallerSlotFrom(context.Background()) != nil {
		t.Fatal("a missing slot must read as empty")
	}
	slot := &CallerSlot{}
	if CallerSlotFrom(WithCaller(context.Background(), slot)) != slot {
		t.Fatal("WithCaller lost the slot")
	}
}

func TestOfferPassesContext(t *testing.T) {
	slot := &CallerSlot{}
	slot.Set("chat-1")
	var got string
	p := MCPProvider(func(ctx context.Context, _ string) (MCPServer, bool) {
		got = CallerSlotFrom(ctx).ID()
		return MCPServer{URL: "u", Token: "t"}, true
	})
	if _, ok := p.Offer(WithCaller(context.Background(), slot), "/p"); !ok || got != "chat-1" {
		t.Fatalf("ok=%v caller=%q", ok, got)
	}
}
