package workflows

import (
	"errors"
	"strings"
	"testing"

	"flint/internal/agenttask"
	"flint/internal/feature"
)

// endTurn ends the current turn of step i's chat with output and waits until
// the step was asked again (nudges) or ended.
func (f *fixture) endTurn(id string, i int, output string, nudges int) Run {
	f.t.Helper()
	chat := f.chatOf(id, i)
	f.h.SetChatOutput(chat, output)
	f.h.FinishChat(chat, feature.ChatResult{State: agenttask.Finished})
	return f.waitFor(id, "step "+strconv(i)+" asked again or ended", func(r Run) bool {
		return r.Records[i].Nudges == nudges || r.Records[i].State != StepRunning
	})
}

func TestNudgeAsksTheAgentToFinishThenGoesOn(t *testing.T) {
	f := newFixture(t)
	v := f.start("feature")
	f.finishFirst(v.ID, "RESULT: DONE")
	r := f.endTurn(v.ID, 1, "The tests are still running; a monitor will report back.", 1)
	if r.State != StateRunning || r.Records[1].State != StepRunning || r.Records[1].Nudges != 1 {
		t.Fatalf("after the first turn: %s, record %+v", r.State, r.Records[1])
	}
	prompts := f.h.StepPrompts()
	if len(prompts) != 1 || prompts[0].Chat != r.Records[1].Chat || !strings.Contains(prompts[0].Text, `"RESULT: DONE"`) {
		t.Fatalf("prompts %+v", prompts)
	}
	if last := r.Log[len(r.Log)-1].Text; !strings.Contains(last, "without a result line") || !strings.Contains(last, "(1 of 2)") {
		t.Errorf("log %q", last)
	}
	f.h.SetChatOutput(r.Records[1].Chat, "Ran them in the foreground: all green.\nRESULT: DONE")
	f.h.FinishChat(r.Records[1].Chat, feature.ChatResult{State: agenttask.Finished})
	r = f.waitFor(v.ID, "the review", func(r Run) bool { return r.Current == 2 })
	if r.Records[1].State != StepDone || r.Records[1].Attempts != 1 {
		t.Errorf("implement record %+v", r.Records[1])
	}
}

func TestNudgesRunOutThenTheRunWaits(t *testing.T) {
	f := newFixture(t)
	f.settings(base(func(s *Settings) { s.Nudges = 1 }))
	v := f.start("feature")
	f.finishFirst(v.ID, "RESULT: DONE")
	f.endTurn(v.ID, 1, "Waiting for the build.", 1)
	r := f.endTurn(v.ID, 1, "Still waiting for the build.", 2)
	if r.State != StateFailed || r.Error != reasonNoResult || r.Records[1].Nudges != 1 || len(f.h.StepPrompts()) != 1 {
		t.Fatalf("run %s %q, record %+v, prompts %d", r.State, r.Error, r.Records[1], len(f.h.StepPrompts()))
	}
	// A new attempt may be asked again.
	if err := f.api.Retry(v.ID); err != nil {
		t.Fatal(err)
	}
	r = f.waitFor(v.ID, "the retry", func(r Run) bool { return r.Records[1].Attempts == 2 && r.Records[1].Chat != "" })
	if r.Records[1].Nudges != 0 {
		t.Errorf("a new attempt starts with %d nudges", r.Records[1].Nudges)
	}
}

func TestNudgeAsksTheReviewerForAVerdict(t *testing.T) {
	f := newFixture(t)
	v := f.start("feature")
	f.finishFirst(v.ID, "RESULT: DONE")
	f.finishChat(v.ID, 1, "RESULT: DONE")
	r := f.endTurn(v.ID, 2, "I read the diff.", 1)
	prompts := f.h.StepPrompts()
	if r.Records[2].Nudges != 1 || len(prompts) != 1 || !strings.Contains(prompts[0].Text, `"RESULT: PASS"`) {
		t.Fatalf("record %+v, prompts %+v", r.Records[2], prompts)
	}
}

func TestNudgeNeverAsksAfterAResult(t *testing.T) {
	f := newFixture(t)
	v := f.start("feature")
	f.finishFirst(v.ID, "RESULT: DONE")
	r := f.finishChat(v.ID, 1, "Which flag library?\nRESULT: BLOCKED")
	if r.State != StateFailed || r.Error != reasonBlocked || len(f.h.StepPrompts()) != 0 {
		t.Fatalf("blocked: %s %q, prompts %d", r.State, r.Error, len(f.h.StepPrompts()))
	}
}

func TestNudgeThatCannotBeSentWaitsForTheUser(t *testing.T) {
	f := newFixture(t)
	f.h.SetStepPromptError(errors.New("today's budget is spent"))
	v := f.start("feature")
	f.finishFirst(v.ID, "RESULT: DONE")
	r := f.finishChat(v.ID, 1, "Waiting for the tests.")
	if r.State != StateFailed || r.Error != reasonNoResult || r.Records[1].Nudges != 0 {
		t.Fatalf("run %s %q, record %+v", r.State, r.Error, r.Records[1])
	}
	found := false
	for _, l := range r.Log {
		found = found || strings.Contains(l.Text, "Could not ask") && strings.Contains(l.Text, "budget is spent")
	}
	if !found {
		t.Errorf("no log line about the failed ask: %+v", r.Log)
	}
}

func TestNudgeTheFirstStepTakenByTheTask(t *testing.T) {
	f := newFixture(t)
	v := f.start("feature")
	chat := f.taskChat(v.ID, "task-chat-"+v.ID)
	r, _ := f.e.get(v.ID)
	task := r.Task
	f.h.SetChatOutput(chat, "PLAN.md is written; waiting for the linter in the background.")
	f.h.UpdateAgentTask(task, func(t *feature.AgentTask) { t.State = agenttask.Finished })
	f.e.kickTasks()
	r = f.waitFor(v.ID, "the first step asked again", func(r Run) bool { return r.Records[0].Nudges == 1 })
	if !byTask(r) || len(f.h.StepPrompts()) != 1 || f.h.StepPrompts()[0].Chat != chat {
		t.Fatalf("run %+v, prompts %+v", r, f.h.StepPrompts())
	}
	f.e.kickTasks() // the task works again: nothing more happens
	if r, _ = f.e.get(v.ID); r.Records[0].Nudges != 1 || len(f.h.StepPrompts()) != 1 {
		t.Fatalf("asked twice: %+v", r.Records[0])
	}
	f.h.SetChatOutput(chat, "Done.\nRESULT: DONE")
	f.h.UpdateAgentTask(task, func(t *feature.AgentTask) { t.State = agenttask.Finished })
	f.e.kickTasks()
	r = f.waitFor(v.ID, "the first step's end", func(r Run) bool { return !byTask(r) })
	if r.Records[0].State != StepDone {
		t.Errorf("first record %+v", r.Records[0])
	}
}
