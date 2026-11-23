package workflows

import (
	"slices"
	"strings"
	"testing"

	"flint/internal/agenttask"
	"flint/internal/feature"
)

func TestDecide(t *testing.T) {
	for _, tc := range []struct {
		name string
		kind Kind
		word string
		want Verdict
	}{
		{"agent done", KindAgent, "DONE", Verdict{Pass: true}},
		{"agent blocked", KindAgent, "BLOCKED", Verdict{Reason: reasonBlocked}},
		{"agent no result", KindAgent, "", Verdict{Reason: reasonNoResult}},
		{"agent PASS is no result", KindAgent, "PASS", Verdict{Reason: reasonNoResult}},
		{"agent FAIL is no result", KindAgent, "FAIL", Verdict{Reason: reasonNoResult}},
		{"review pass", KindReview, "PASS", Verdict{Pass: true}},
		{"review fail reworks", KindReview, "FAIL", Verdict{Rework: true, Reason: reasonReview}},
		{"review DONE is no verdict", KindReview, "DONE", Verdict{Reason: reasonNoVerdict}},
		{"review blocked is no verdict", KindReview, "BLOCKED", Verdict{Reason: reasonNoVerdict}},
		{"review no result", KindReview, "", Verdict{Reason: reasonNoVerdict}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Decide(tc.kind, tc.word); got != tc.want {
				t.Errorf("Decide = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestDecideMatchesTheFirstEvaluation: a finished turn's outcome in
// chatOutcome and Decide agree for every kind and result line, so "Check
// again" judges the way the step's first evaluation does.
func TestDecideMatchesTheFirstEvaluation(t *testing.T) {
	fin := feature.ChatResult{State: agenttask.Finished}
	for _, kind := range []Kind{KindAgent, KindReview} {
		for _, line := range []string{"", "RESULT: DONE", "RESULT: BLOCKED", "RESULT: PASS", "RESULT: FAIL"} {
			out := "work\n" + line
			word, _ := ParseResult(out)
			v := Decide(kind, word)
			want := failed(v.Reason, out, v.Rework)
			if v.Pass {
				want = passed(out)
			}
			if got := chatOutcome(kind, fin, out); got != want {
				t.Errorf("%s %q: chatOutcome = %+v, Decide says %+v", kind, line, got, want)
			}
		}
	}
}

// failedAt is the test run failed at step i (an agent or review step) with a
// chat, the reason and what its chat said.
func failedAt(t *testing.T, i int, out string) Run {
	t.Helper()
	r := newTestRun()
	for range i {
		r = start(t, r, 101).Passed("ok", 102)
	}
	r = start(t, r, 103).WithChat("c1")
	word, _ := ParseResult(out)
	return r.Failed(Decide(r.Steps[i].Kind, word).Reason, out, false, 3, 104)
}

func TestRechecked(t *testing.T) {
	blocked := failedAt(t, 1, "Which database?\nRESULT: BLOCKED")
	if !blocked.Recheckable() {
		t.Fatal("a failed agent step with a chat is not recheckable")
	}
	before := blocked

	t.Run("done passes and is not continued", func(t *testing.T) {
		got, err := blocked.Rechecked("Use Postgres.\nRESULT: DONE", 3, 200)
		if err != nil {
			t.Fatal(err)
		}
		rec := got.Records[1]
		if rec.State != StepDone || rec.Skipped || got.Current != 2 || got.State != StateRunning || got.Error != "" || got.Feedback != "" {
			t.Errorf("run %+v record %+v", got, rec)
		}
		if rec.Attempts != 1 || !strings.Contains(rec.Output, "RESULT: DONE") {
			t.Errorf("attempts %d, output %q", rec.Attempts, rec.Output)
		}
		if _, ok := got.Next(); !ok {
			t.Error("the next step is not offered")
		}
		if !reflectEqualRuns(blocked, before) {
			t.Error("Rechecked changed its receiver")
		}
	})

	t.Run("a pause asked for earlier does not hold it", func(t *testing.T) {
		p := blocked
		p.Paused = true
		got, err := p.Rechecked("RESULT: DONE", 3, 200)
		if err != nil || got.State != StateRunning || got.Paused || got.Current != 2 {
			t.Errorf("run %+v, %v", got, err)
		}
	})

	t.Run("still blocked stays failed with the same reason", func(t *testing.T) {
		got, err := blocked.Rechecked("Which database?\nRESULT: BLOCKED", 3, 200)
		if err != nil || got.State != StateFailed || got.Error != reasonBlocked || got.Current != 1 || got.Records[1].State != StepFailed {
			t.Errorf("run %+v, %v", got, err)
		}
		if last := got.Log[len(got.Log)-1].Text; !strings.HasPrefix(last, "Checked again: ") {
			t.Errorf("log %q", last)
		}
	})

	t.Run("still no result line", func(t *testing.T) {
		got, err := blocked.Rechecked("I was thinking about it.", 3, 200)
		if err != nil || got.State != StateFailed || got.Error != reasonNoResult || got.Records[1].Attempts != 1 {
			t.Errorf("run %+v, %v", got, err)
		}
	})

	t.Run("a review that now fails reworks", func(t *testing.T) {
		r := failedAt(t, 3, "I could not decide.")
		got, err := r.Rechecked("Missing tests.\nRESULT: FAIL", 3, 200)
		if err != nil {
			t.Fatal(err)
		}
		if got.State != StateRunning || got.Current != 1 || got.Error != "" || !strings.Contains(got.Feedback, "the review found problems") || got.Records[3].State != StepPending {
			t.Errorf("run %+v", got)
		}
	})

	t.Run("a review that now fails, no attempts left", func(t *testing.T) {
		r := failedAt(t, 3, "I could not decide.")
		got, err := r.Rechecked("RESULT: FAIL", 1, 200)
		if err != nil || got.State != StateFailed || got.Error != reasonReview {
			t.Errorf("run %+v, %v", got, err)
		}
	})

	t.Run("a review that now passes", func(t *testing.T) {
		got, err := failedAt(t, 3, "I could not decide.").Rechecked("RESULT: PASS", 3, 200)
		if err != nil || got.Records[3].State != StepDone || got.Records[3].Skipped || got.Current != 4 {
			t.Errorf("run %+v, %v", got, err)
		}
	})
}

func reflectEqualRuns(a, b Run) bool {
	return a.State == b.State && a.Current == b.Current && a.Error == b.Error &&
		slices.Equal(a.Records, b.Records) && len(a.Log) == len(b.Log)
}

func TestNotRecheckable(t *testing.T) {
	script := newTestRun()
	for range 2 {
		script = start(t, script, 101).Passed("ok", 102)
	}
	script = start(t, script, 103).WithRunTab("tab1").Failed("it exited with code 1", "boom", false, 3, 104)
	noChat := start(t, newTestRun(), 101).Failed(reasonNoResult, "x", false, 3, 102)
	running := start(t, newTestRun(), 101).WithChat("c1")
	gate := newTestRun()
	for range 4 {
		gate = start(t, gate, 101).Passed("ok", 102)
	}
	gate = gate.Started(103)
	for name, r := range map[string]Run{"script step": script, "no chat": noChat, "running": running, "gate": gate} {
		if r.Recheckable() {
			t.Errorf("%s: Recheckable", name)
		}
		if slices.Contains(Actions(r), ActionRecheck) {
			t.Errorf("%s: %v offers recheck", name, Actions(r))
		}
		if _, err := r.Rechecked("RESULT: DONE", 3, 200); err == nil {
			t.Errorf("%s: Rechecked accepted", name)
		}
	}
	if got := Actions(failedAt(t, 1, "RESULT: BLOCKED")); !slices.Equal(got, []string{ActionSendBack, ActionRecheck, ActionRetry, ActionSkip, ActionStop}) {
		t.Errorf("Actions = %v", got)
	}
}

func TestRecheckInTheEngine(t *testing.T) {
	f := newFixture(t)
	v := f.start("feature")
	r := f.finishFirst(v.ID, "Which file should hold the plan?\nRESULT: BLOCKED")
	if r.State != StateFailed || r.Error != reasonBlocked {
		t.Fatalf("run %+v", r)
	}
	chat := r.Records[0].Chat
	if chat == "" {
		t.Fatal("the first step's record has no chat")
	}
	if view := f.api.Runs()[0]; !slices.Contains(view.Actions, ActionRecheck) {
		t.Fatalf("actions %v", view.Actions)
	}

	// The user has not answered yet: stays failed, nothing started.
	if err := f.api.Recheck(v.ID); err != nil {
		t.Fatal(err)
	}
	if r, _ := f.e.get(v.ID); r.State != StateFailed || r.Error != reasonBlocked || r.Current != 0 || r.Records[0].State != StepFailed {
		t.Fatalf("still blocked: %+v", r)
	}

	// The user answers in the chat; the agent ends with a result line.
	f.h.SetChatOutput(chat, "Which file should hold the plan?\nPLAN.md\nWrote PLAN.md.\nRESULT: DONE")
	if err := f.api.Recheck(v.ID); err != nil {
		t.Fatal(err)
	}
	r = f.waitFor(v.ID, "the second step", func(r Run) bool { return r.Current == 1 && r.Records[1].State == StepRunning })
	if rec := r.Records[0]; rec.State != StepDone || rec.Skipped || r.Error != "" {
		t.Fatalf("record %+v, run %+v", rec, r)
	}

	// Not offered any more (the run goes on), and refused.
	if err := f.api.Recheck(v.ID); err == nil || !strings.Contains(err.Error(), "failed at an agent or review step") {
		t.Fatalf("Recheck of a running run = %v", err)
	}
	if err := f.api.Recheck("0000000000000000"); err == nil {
		t.Error("Recheck of an unknown run")
	}
}

func TestRecheckAReviewInTheEngine(t *testing.T) {
	f := newFixture(t)
	v := f.start("feature")
	f.finishFirst(v.ID, "RESULT: DONE")
	f.finishChat(v.ID, 1, "RESULT: DONE")
	r := f.finishChat(v.ID, 2, "I looked at the diff but ran out of things to say.")
	if r.State != StateFailed || r.Error != reasonNoVerdict {
		t.Fatalf("run %+v", r)
	}
	f.h.SetChatOutput(r.Records[2].Chat, "Tests are missing.\nRESULT: FAIL")
	if err := f.api.Recheck(v.ID); err != nil {
		t.Fatal(err)
	}
	r = f.waitFor(v.ID, "sent back to Implement", func(r Run) bool { return r.Current == 1 && r.Records[1].State == StepRunning })
	if !strings.Contains(r.Feedback, "the review found problems") || !strings.Contains(r.Feedback, "Tests are missing") {
		t.Errorf("feedback %q", r.Feedback)
	}
}

// "Check again" that finds only the provider's limit announcement (no
// result line) pauses the run by the limit, as the first evaluation does.
func TestRecheckFindsALimit(t *testing.T) {
	f := newFixture(t)
	v := f.start("feature")
	r := f.finishFirst(v.ID, "Which file should hold the plan?\nRESULT: BLOCKED")
	chat := r.Records[0].Chat
	f.h.SetChatOutput(chat, "Working on it.\nClaude AI usage limit reached|1700000000")
	if err := f.api.Recheck(v.ID); err != nil {
		t.Fatal(err)
	}
	r, _ = f.e.get(v.ID)
	if r.State != StatePaused || r.Hold == nil || r.Hold.Kind != LimitAgent || r.Records[0].State != StepPending {
		t.Fatalf("after a recheck that found a limit: %s %+v %+v", r.State, r.Hold, r.Records[0])
	}
}

// After a restart the chat is not open any more: Check again says so (and
// points at Retry) instead of re-reporting the old failure.
func TestRecheckOfAChatThatIsNotOpen(t *testing.T) {
	f := newFixture(t)
	v := f.start("feature")
	r := f.finishFirst(v.ID, "Which file?\nRESULT: BLOCKED")
	f.h.SetChatOutput(r.Records[0].Chat, "") // the host knows no such chat
	err := f.api.Recheck(v.ID)
	want := "The step's chat is not open any more (Flint was restarted): choose Retry to take the step again."
	if err == nil || err.Error() != want {
		t.Fatalf("Recheck = %v, want %q", err, want)
	}
	if r, _ := f.e.get(v.ID); r.State != StateFailed || r.Error != reasonBlocked {
		t.Errorf("the run changed: %+v", r)
	}
}
