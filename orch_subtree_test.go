package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"flint/internal/mcptools"
	"flint/internal/orchestrate"
)

// M2: a grant ends with the chat that was asked, and so do the agents it
// started.

func TestClosingTheRootRevokesAndStopsItsWholeSubtree(t *testing.T) {
	o := newOrchApp(t)
	child := o.start(t, `{"task":"work"}`)
	o.caller.Store(child)
	grandchild := o.start(t, `{"task":"sub"}`)
	o.caller.Store(o.chat)

	if err := (&AgentChatAPI{app: o.App}).Stop(o.chat); err != nil { // the user closes the chat
		t.Fatal(err)
	}
	for _, id := range []string{o.chat, child, grandchild} {
		if _, ok := o.orch.Get(id); ok {
			t.Errorf("%s is still registered", id)
		}
		if o.orchGranted(id) {
			t.Errorf("%s keeps a working grant", id)
		}
	}
	if got, want := strings.Join(o.driver.stopped, ","), strings.Join([]string{grandchild, child, o.chat}, ","); got != want {
		t.Errorf("stopped %q, want deepest first: %q", got, want)
	}
}

func TestChildCallAfterItsGrantingAncestorClosedIsRefused(t *testing.T) {
	o := newOrchApp(t)
	child := o.start(t, `{"task":"work"}`)
	// The ancestor's grant is gone, but the child's copy was left behind (a
	// cleanup that did not run): the copy alone must not work.
	o.orchGrants.forget(o.chat)
	if o.orchGranted(child) {
		t.Fatal("a child's grant outlived the grant it was copied from")
	}
	o.caller.Store(child)
	o.fails(t, "list_agents", `{}`, "no permission")
	o.fails(t, "start_agent", `{"task":"x"}`, "no permission")
}

// L1: a chat closed while its dialog is open gets no grant and no second dialog.

func TestChatClosedWhileItsDialogIsOpenGetsNoGrantAndNoSecondDialog(t *testing.T) {
	g := newGrantApp(t)
	release := make(chan struct{})
	var asked atomic.Int32
	g.ask = func(_, _, _ string) bool {
		asked.Add(1)
		<-release
		return true
	}
	ctx, call := g.call()
	done := make(chan error, 1)
	go func() { done <- g.approveAction(ctx, call) }()
	for i := 0; i < 500 && asked.Load() == 0; i++ {
		time.Sleep(5 * time.Millisecond)
	}
	if asked.Load() != 1 {
		t.Fatal("the dialog was not shown")
	}
	g.forgetChat(g.chat) // the user closes the chat
	close(release)       // and answers yes afterwards
	<-done
	if _, ok := g.orchGrants.get(g.chat); ok {
		t.Fatal("a yes for a chat that was closed left a grant behind")
	}
	if err := g.approveAction(ctx, call); err == nil {
		t.Error("a closed chat was approved")
	}
	if asked.Load() != 1 {
		t.Errorf("the dialog came back for a closed chat (%d questions)", asked.Load())
	}
}

func TestObtainStopsWhenTheContextIsDoneOrTheChatIsGone(t *testing.T) {
	var g orchGrants
	asked := false
	ask := func() bool { asked = true; return true }
	if ok, err := g.obtain(context.Background(), "c", orchGrant{}, func() bool { return false }, ask); ok || err == nil {
		t.Errorf("unregistered chat: ok %v, err %v", ok, err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if ok, err := g.obtain(cancelled, "c", orchGrant{}, func() bool { return true }, ask); ok || err == nil {
		t.Errorf("done context: ok %v, err %v", ok, err)
	}
	if asked {
		t.Error("the dialog was shown")
	}
	if _, ok := g.get("c"); ok {
		t.Error("a grant was written")
	}
}

// L2: a child never registers without its parent, and a start that loses the
// race to the parent's closing is undone.

func TestRegisterRefusesAChildOfAChatThatIsGone(t *testing.T) {
	o := newOrchApp(t)
	_, err := o.openChat(context.Background(), ChatRequest{Agent: "fake", Cwd: o.project, Parent: "ghost"})
	if err == nil {
		t.Fatal("a child of an unknown parent was opened")
	}
	if len(o.orch.All()) != 1 {
		t.Errorf("registry = %+v", o.orch.All())
	}
	if len(o.driver.stopped) != 1 {
		t.Errorf("the agent that was started for it was not stopped: %v", o.driver.stopped)
	}
	if err := o.orch.Register(orchestrate.Node{ID: "x", Parent: "ghost"}); err == nil {
		t.Error("Register accepted a node whose parent is not registered")
	}
}

func TestStartThatLosesTheRaceWithTheParentClosingStopsTheChild(t *testing.T) {
	o := newOrchApp(t)
	child, err := o.openChat(context.Background(), ChatRequest{Agent: "fake", Cwd: o.project, Parent: o.chat})
	if err != nil {
		t.Fatal(err)
	}
	// The parent is closed after the child registered and before Start looked again.
	o.forgetChat(o.chat)
	orch := o.mcpSources.Orchestrator.(*orchestrator)
	if err := orch.keepOrStop(o.chat, child.ID); err == nil {
		t.Fatal("start_agent reported success for a child whose parent was closed")
	}
	if _, ok := o.orch.Get(child.ID); ok {
		t.Error("the orphan is still registered")
	}
	if got := o.driver.stopped; len(got) != 1 || got[0] != child.ID {
		t.Errorf("stopped %v, want the orphan", got)
	}
}

func TestChildOfAParentWhoseGrantWentFirstGetsNoGrant(t *testing.T) {
	o := newOrchApp(t)
	o.ok(t, "list_agents", `{}`) // the grant
	o.orchGrants.forget(o.chat)  // forgetChat forgets the grant before the registry entry
	child, err := o.openChat(context.Background(), ChatRequest{Agent: "fake", Cwd: o.project, Parent: o.chat})
	if err != nil {
		t.Fatal(err)
	}
	if o.orchGranted(child.ID) {
		t.Error("a child registered in the gap inherited a grant")
	}
}

// L3: the child limit holds under parallel starts, and a slow start does not
// hold up other chats.

func TestParallelStartAgentNeverExceedsMaxChildren(t *testing.T) {
	o := newOrchApp(t)
	o.settings.Orchestration.MaxChildren = 3
	o.ok(t, "list_agents", `{}`) // the grant
	orch := o.mcpSources.Orchestrator.(*orchestrator)

	var wg sync.WaitGroup
	var started atomic.Int32
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := orch.Start(context.Background(), o.chat, o.project, mcptools.StartRequest{Task: "x"}); err == nil {
				started.Add(1)
			}
		}()
	}
	wg.Wait()
	if n := len(o.orch.Children(o.chat)); n != 3 || started.Load() != 3 {
		t.Fatalf("%d children, %d starts succeeded, limit 3", n, started.Load())
	}
}

func TestSlowStartDoesNotBlockAnotherChatsStart(t *testing.T) {
	o := newOrchApp(t)
	child := o.start(t, `{"task":"work"}`) // gets an inherited grant
	orch := o.mcpSources.Orchestrator.(*orchestrator)

	gate := make(chan struct{})
	o.driver.holdNextStart(gate)
	slow := make(chan error, 1)
	go func() {
		_, err := orch.Start(context.Background(), o.chat, o.project, mcptools.StartRequest{Task: "slow"})
		slow <- err
	}()
	for i := 0; i < 500 && !o.driver.isStartHeld(); i++ {
		time.Sleep(5 * time.Millisecond)
	}
	if !o.driver.isStartHeld() {
		t.Fatal("the slow start never reached the agent")
	}
	fast := make(chan error, 1)
	go func() {
		_, err := orch.Start(context.Background(), child, o.project, mcptools.StartRequest{Task: "fast"})
		fast <- err
	}()
	select {
	case err := <-fast:
		if err != nil {
			t.Fatalf("the other chat's start: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("one chat's slow start blocked another chat's start_agent")
	}
	close(gate)
	if err := <-slow; err != nil {
		t.Fatal(err)
	}
}

func TestFailedStartReleasesItsReservation(t *testing.T) {
	o := newOrchApp(t)
	o.settings.Orchestration.MaxChildren = 1
	o.ok(t, "list_agents", `{}`)
	orch := o.mcpSources.Orchestrator.(*orchestrator)
	if _, err := orch.Start(context.Background(), o.chat, o.project, mcptools.StartRequest{Task: "x", Agent: "no-such-agent"}); err == nil {
		t.Fatal("an unknown agent started")
	}
	if _, err := orch.Start(context.Background(), o.chat, o.project, mcptools.StartRequest{Task: "x"}); err != nil {
		t.Fatalf("the failed start kept its slot: %v", err)
	}
	if got := fmt.Sprint(len(orch.reserved)); got != "0" {
		t.Errorf("reservations left: %s", got)
	}
}
