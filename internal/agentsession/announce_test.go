package agentsession

import (
	"context"
	"testing"
)

func TestAnnounceStampsSequenceInOrderWithOtherEvents(t *testing.T) {
	rec := &recorder{}
	m := NewManager(rec.emit)
	m.Register("fake", newFake(""))
	info, err := m.Start(context.Background(), "fake", "/p")
	if err != nil {
		t.Fatal(err)
	}
	origin := &Origin{SessionID: "parent", Agent: "claude", Role: "reviewer"}
	for i := 0; i < 2; i++ {
		if err := m.Announce(info.ID, Event{Kind: KindUserMessage, Text: "hi", Origin: origin}); err != nil {
			t.Fatal(err)
		}
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.events) != 2 {
		t.Fatalf("got %d events, want 2", len(rec.events))
	}
	for i, e := range rec.events {
		if e.SessionID != info.ID || e.Seq != uint64(i+1) || e.Origin != origin {
			t.Errorf("event %d = %+v", i, e)
		}
	}
}

func TestAnnounceRefusesUnknownChat(t *testing.T) {
	rec := &recorder{}
	m := NewManager(rec.emit)
	if err := m.Announce("nope", Event{Kind: KindUserMessage}); err == nil {
		t.Fatal("Announce on an unknown chat must fail")
	}
	if len(rec.kinds()) != 0 {
		t.Fatal("nothing may be emitted for an unknown chat")
	}
}
