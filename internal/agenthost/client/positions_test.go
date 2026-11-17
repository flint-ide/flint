package client_test

import (
	"context"
	"maps"
	"testing"
	"time"

	"flint/internal/agenthost/client"
	"flint/internal/terminal"
)

// After a disconnect, Positions says where each terminal of this window
// stopped, so the next connection re-attaches without gaps or repeats.
func TestPositionsAfterDisconnect(t *testing.T) {
	rec := newTermRecorder()
	c, host := connect(t, client.Handlers{Terminal: rec})
	host.data(1, "a")
	host.data(2, "b")
	if err := c.AttachTerminal("t1", 0); err != nil {
		t.Fatal(err)
	}
	rec.waitOutput(t, "t1", "ab")
	if err := c.Terminals().Start("t2", terminal.Options{}); err != nil {
		t.Fatal(err)
	}
	if err := c.Terminals().Start("t3", terminal.Options{}); err != nil {
		t.Fatal(err)
	}
	c.Terminals().Close("t3") // closed tabs are not re-attached

	got := make(chan map[string]uint64, 1)
	go func() { got <- c.Positions() }()
	select {
	case <-got:
		t.Fatal("Positions returned while connected")
	case <-time.After(50 * time.Millisecond):
	}
	_ = host.end.Close() // the host goes away
	select {
	case p := <-got:
		if want := map[string]uint64{"t1": 2, "t2": 0}; !maps.Equal(p, want) {
			t.Errorf("Positions = %v, want %v", p, want)
		}
	case <-time.After(wait):
		t.Fatal("Positions did not return after the disconnect")
	}
}

func TestListTerminalsAndSettingsChanged(t *testing.T) {
	h := newRealHost(t)
	c := h.dial(t, client.Handlers{})
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	list, err := c.ListTerminals(ctx)
	if err != nil || len(list) != 0 {
		t.Fatalf("ListTerminals = %v, %v", list, err)
	}
	if err := c.SettingsChanged(ctx); err != nil {
		t.Fatalf("SettingsChanged = %v", err)
	}
}
