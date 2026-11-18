package client_test

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"flint/internal/agenthost"
	"flint/internal/agenthost/client"
	as "flint/internal/agentsession"
	"flint/internal/sessions"
	"flint/internal/terminal"
)

// realHost is a real server with the fake driver, for what only the real
// protocol answers.
type realHost struct {
	srv    *agenthost.Server
	driver *fakeDriver
}

func newRealHost(t *testing.T) *realHost {
	t.Helper()
	h := &realHost{driver: newFakeDriver()}
	srv, err := agenthost.New(agenthost.Config{
		Token: token, Version: "test",
		Terminals: func(ev terminal.Events) sessions.Terminals { return terminal.NewManager(ev) },
		Agents: func(emit func(as.Event)) sessions.Agents {
			m := as.NewManager(emit)
			m.Register("fake", h.driver)
			return m
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	h.srv = srv
	t.Cleanup(srv.Close)
	return h
}

func (h *realHost) dial(t *testing.T, handlers client.Handlers) *client.Client {
	t.Helper()
	a, b := net.Pipe()
	go func() { _ = h.srv.Serve(b) }()
	c, err := client.New(a, client.Config{Token: token, Handlers: handlers})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// A stopped chat stays readable: a window that attaches afterwards gets its
// history, and cannot prompt it.
func TestStoppedChatCanStillBeAttached(t *testing.T) {
	h := newRealHost(t)
	a := h.dial(t, client.Handlers{})
	info, err := a.Agents().Start(context.Background(), "fake", "/work")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Agents().Prompt(info.ID, "hi", nil); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the turn to end", func() bool { return !a.Agents().Busy(info.ID) })
	if err := a.Agents().Stop(info.ID); err != nil {
		t.Fatal(err)
	}

	events := make(chan as.Event, 16)
	b := h.dial(t, client.Handlers{Agent: func(e as.Event) { events <- e }})
	if got := b.Hello().Chats; len(got) != 1 || got[0] != info.ID {
		t.Fatalf("the stopped chat is not listed: %v", got)
	}
	if err := b.AttachChat(info.ID, 0); err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for range 3 {
		select {
		case e := <-events:
			kinds = append(kinds, e.Kind)
		case <-time.After(wait):
			t.Fatalf("history of the stopped chat: only %v", kinds)
		}
	}
	if kinds[0] != as.KindTurnStart || kinds[1] != as.KindMessageChunk || kinds[2] != as.KindTurnEnd {
		t.Errorf("history = %v", kinds)
	}
	if err := b.Agents().Prompt(info.ID, "again", nil); err == nil {
		t.Error("a stopped chat took a prompt")
	}
}

// A prompt cannot be sent in pieces, so one that does not fit a message is
// refused before anything is sent, with an error that says why.
func TestPromptThatCannotFitAMessageIsRefusedClearly(t *testing.T) {
	h := newRealHost(t)
	c := h.dial(t, client.Handlers{})
	info, err := c.Agents().Start(context.Background(), "fake", "/work")
	if err != nil {
		t.Fatal(err)
	}
	// Control characters are six bytes each once encoded.
	text := strings.Repeat("\x01", 1<<20)
	err = c.Agents().Prompt(info.ID, text, nil)
	if !errors.Is(err, client.ErrPromptTooLarge) {
		t.Fatalf("Prompt = %v, want ErrPromptTooLarge", err)
	}
	if c.Agents().Busy(info.ID) {
		t.Error("the refused prompt reached the agent")
	}
	// And the connection is still good.
	if err := c.Agents().Prompt(info.ID, "fine", nil); err != nil {
		t.Errorf("the next prompt = %v", err)
	}
}

func TestShellOverTheWireIsRefusedByAHostRunByFlint(t *testing.T) {
	h := newRealHost(t)
	c := h.dial(t, client.Handlers{})
	err := c.Terminals().Start("t1", terminal.Options{Shell: "/bin/sh", Args: []string{"-c", "true"}})
	if err == nil || !strings.Contains(err.Error(), "cannot be chosen") {
		t.Errorf("Start with a shell = %v", err)
	}
	if err := c.Terminals().Start("t2", terminal.Options{Env: []string{"lower=x"}}); err == nil {
		t.Error("Start with an invalid variable succeeded")
	}
}

func TestUsageBacklogReportsWhatTheHostDropped(t *testing.T) {
	c, host := connect(t, client.Handlers{})
	ev, _ := json.Marshal(agenthost.EventMsg{Event: as.Event{SessionID: "c", Seq: 1, Kind: as.KindUsage}, UsageSeq: 5})
	host.mu.Lock()
	host.backlog = agenthost.UsageBacklogResult{Events: []json.RawMessage{ev}, Dropped: 3}
	host.mu.Unlock()
	events, dropped, err := c.UsageBacklogDropped(context.Background())
	if err != nil || len(events) != 1 || events[0].Seq != 5 || dropped != 3 {
		t.Errorf("backlog = %+v, dropped %d, %v", events, dropped, err)
	}
	if plain, err := c.UsageBacklog(context.Background()); err != nil || len(plain) != 1 {
		t.Errorf("UsageBacklog = %+v, %v", plain, err)
	}
}
