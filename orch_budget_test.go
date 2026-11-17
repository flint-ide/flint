package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	as "flint/internal/agentsession"
)

// turnsStarted counts the prompts the fake agents were handed: every one in
// these tests was started by an agent or by Flint's notice, none by the user.
func turnsStarted(o *orchApp, ids ...string) int {
	n := 0
	for _, id := range ids {
		n += len(o.driver.got(id))
	}
	return n
}

// settle waits until no new prompt has reached the chats for a while.
func settle(o *orchApp, ids ...string) {
	last, stable := -1, 0
	for i := 0; i < 400 && stable < 8; i++ {
		time.Sleep(15 * time.Millisecond)
		if n := turnsStarted(o, ids...); n == last {
			stable++
		} else {
			last, stable = n, 0
		}
	}
}

// endTurn ends the chat's running turn as the driver's turn_end event would.
func endTurn(o *orchApp, id string) {
	o.observeChat(as.Event{SessionID: id, Kind: as.KindTurnEnd, StopReason: as.StopEndTurn})
}

// sendUntilRefused is the loop a prompt-injected parent would run: send_prompt
// to its child every time the child is ready. It returns how many were sent and
// the refusal that ended it.
func sendUntilRefused(t *testing.T, o *orchApp, child string) (int, string) {
	t.Helper()
	sent, last := 0, ""
	for i := 0; i < 300; i++ {
		// The fake agent emits no events: end the turns the way the real driver
		// would, through the app's observer, so notices are queued and delivered.
		endTurn(o, child)
		endTurn(o, o.chat)
		text, failed := o.tool(t, "send_prompt", fmt.Sprintf(`{"id":%q,"text":"again"}`, child))
		switch {
		case !failed:
			sent++
		case strings.Contains(text, "turn limit"):
			return sent, text
		default:
			last = text
		}
		time.Sleep(15 * time.Millisecond) // busy, or a notice is being delivered
	}
	t.Fatalf("send_prompt was never refused: the cycle has no limit (sent %d, last error %q)", sent, last)
	return sent, ""
}

func TestNoticeAndSendPromptCycleStopsAtTheCap(t *testing.T) {
	o := newOrchApp(t)
	o.settings.Orchestration.MaxAutoTurns = 6
	child := o.start(t, `{"task":"work"}`) // turn 1
	_, refusal := sendUntilRefused(t, o, child)
	if !strings.Contains(refusal, "report to the user") {
		t.Errorf("the refusal must tell the agent to stop and report: %q", refusal)
	}
	settle(o, o.chat, child)
	if got := turnsStarted(o, o.chat, child); got != 6 {
		t.Errorf("agents started %d turns, want exactly the cap (6)", got)
	}
	// And it stays stopped: nothing the notices do starts another turn.
	time.Sleep(300 * time.Millisecond)
	if got := turnsStarted(o, o.chat, child); got != 6 {
		t.Errorf("%d turns after the cap", got)
	}
}

func TestUserPromptResetsTheTurnBudget(t *testing.T) {
	o := newOrchApp(t)
	o.settings.Orchestration.MaxAutoTurns = 4
	child := o.start(t, `{"task":"work"}`)
	sendUntilRefused(t, o, child)
	settle(o, o.chat, child)

	api := &AgentChatAPI{app: o.App}
	// The user types into the child (any chat of the tree counts).
	if err := api.Prompt(child, "carry on", nil); err != nil {
		t.Fatal(err)
	}
	settle(o, o.chat, child)
	if sent, _ := sendUntilRefused(t, o, child); sent == 0 {
		t.Error("a message from the user did not give the agents their turns back")
	}
}

func TestStartAgentCountsItsFirstPromptAndIsRefusedAtTheCap(t *testing.T) {
	o := newOrchApp(t)
	o.settings.Orchestration.MaxAutoTurns = 1
	o.start(t, `{"task":"one"}`)
	o.fails(t, "start_agent", `{"task":"two"}`, "turn limit")
	if n := len(o.orch.Children(o.chat)); n != 1 {
		t.Errorf("%d children: a refused start_agent must not start an agent", n)
	}
}

func TestAgentTurnsOutsideAGrantAreNotCounted(t *testing.T) {
	o := newOrchApp(t)
	o.settings.Orchestration.MaxAutoTurns = 1
	// A chat the user opened and no grant covers: Flint-queued prompts to it
	// (origin nil, no parent) are not agent-to-agent turns.
	for i := 0; i < 3; i++ {
		if err := o.promptChat(o.chat, "queued", nil, false, nil); err != nil {
			t.Fatalf("prompt %d: %v", i, err)
		}
		settle(o, o.chat)
	}
}
