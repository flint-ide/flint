package workflows

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"flint/internal/agenttask"
	"flint/internal/feature"
)

func TestLimitKind(t *testing.T) {
	cases := []struct {
		text string
		want string
	}{
		// Flint's own budget (usageGate's wording, also wrapped).
		{"paused: today's budget of 100 tokens is spent (120). Choose Continue anyway in the budget warning above the chat, or raise the budget in Settings ▸ Usage", LimitBudget},
		{"start Claude for task 0123456789abcdef: paused: this task's budget of $5.00 is spent ($5.10, partly estimated). Choose …", LimitBudget},
		{"Paused: Today's budget of $20.00 is spent ($21.00)", LimitBudget},
		// The providers'.
		{"Quota exceeded", LimitAgent},
		{"You've hit your limit · resets 3pm (Europe/Warsaw)", LimitAgent},
		{"Claude AI usage limit reached|1760000000", LimitAgent},
		{"You've reached your usage limit. Upgrade to Pro", LimitAgent},
		{`API Error: 429 {"type":"error","error":{"type":"rate_limit_error"}}`, LimitAgent},
		{"stream error: status 429 Too Many Requests", LimitAgent},
		{"HTTP 529: Overloaded", LimitAgent},
		{`{"type":"overloaded_error","message":"Overloaded"}`, LimitAgent},
		{"The API is currently overloaded, try again later", LimitAgent},
		{"Rate-limited by the provider", LimitAgent},
		{"Your credit balance is too low to access the Anthropic API", LimitAgent},
		{"insufficient_quota: You exceeded your current quota", LimitAgent},
		{"Insufficient credits", LimitAgent},
		{"error: too many requests", LimitAgent},
		{"weekly limit reached", LimitAgent},
		// Ordinary failures.
		{"", ""},
		{"exit status 1", ""},
		{"the file exceeds the line limit of the file", ""},
		{"access is limited to admins", ""},
		{"the limit of 10 items was kept", ""},
		{"see the quotation in README", ""},
		{"line 429 of main.go: undefined: x", ""},
		{"the method Parse is overloaded in Java", ""},
		{"the budget is fine", ""},
		{"paused: the user paused it", ""},
		{"rate of change is too high", ""},
	}
	for _, c := range cases {
		if got := LimitKind(c.text); got != c.want {
			t.Errorf("LimitKind(%q) = %q, want %q", c.text, got, c.want)
		}
	}
}

func TestLimitInOutput(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string
	}{
		// What the providers print (a finished turn that is the limit's notice).
		{"claude usage limit", "Claude AI usage limit reached|1760000000", LimitAgent},
		{"claude usage limit, label", "Claude usage limit reached. Your limit will reset at 3pm.", LimitAgent},
		{"hit your limit", "You've hit your limit · resets 3pm (Europe/Warsaw)", LimitAgent},
		{"hit your limit, curly apostrophe", "You\u2019ve hit your limit · resets 3pm", LimitAgent},
		{"you have reached your usage limit", "You have reached your usage limit. Upgrade to Pro.", LimitAgent},
		{"codex", "You've hit your usage limit. Try again at 3:14 PM.", LimitAgent},
		{"five hour", "5-hour limit reached ∙ resets 3pm", LimitAgent},
		{"weekly", "Weekly limit reached ∙ resets Mon 9am", LimitAgent},
		{"quota code", "insufficient_quota", LimitAgent},
		{"error label", "Error: insufficient_quota: You exceeded your current quota", LimitAgent},
		{"429", "429 Too Many Requests", LimitAgent},
		{"api error 429", "API Error: 429 Too Many Requests", LimitAgent},
		{"credit balance", "Your credit balance is too low to access the Anthropic API.", LimitAgent},
		{"in the last lines", "Working on it\nstill working\nYou've hit your limit · resets 3pm", LimitAgent},
		{"quoted by the CLI", "> You've hit your limit", LimitAgent},
		// An agent that works on rate limiting, quotas or retries ends with
		// ordinary words: never a limit.
		{"rate limit middleware", "Added the rate limit middleware", ""},
		{"rate limiter", "I added a rate limiter to the API and tests for it.", ""},
		{"rate limit summary", "Summary:\n- rate limit middleware\n- quota checks per user\nAll done.", ""},
		{"quota", "Implemented the quota check", ""},
		{"usage limit feature", "Added a usage limit setting to the admin page", ""},
		{"usage limit reached mid line", "Show a banner when the usage limit reached 100%", ""},
		{"hit your limit mid line", "Tell users when they've hit your limit of 10 items", ""},
		{"429 handling", "Handled the 429 Too Many Requests response with a retry", ""},
		{"429 in a sentence", "The server answers 429 Too Many Requests when ...", ""},
		{"too many requests test", "Added a test for too many requests", ""},
		{"insufficient_quota in a sentence", "Mapped insufficient_quota to a friendly error", ""},
		{"credit balance feature", "The credit balance is too low state is now shown in the UI", ""},
		{"overloaded", "The method is overloaded", ""},
		{"limited", "access is limited to admins", ""},
		{"empty", "", ""},
		{"budget text is not a provider limit", "paused: today's budget of 100 tokens is spent (120)", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := LimitInOutput(c.text); got != c.want {
				t.Errorf("LimitInOutput(%q) = %q, want %q", c.text, got, c.want)
			}
		})
	}
}

// The two classifiers differ on purpose: the words of ordinary work are a
// limit in an error text, never in the output of a turn that ended well.
func TestLimitKindAndLimitInOutputDiffer(t *testing.T) {
	for _, text := range []string{"Added the rate limit middleware", "Implemented the quota check"} {
		if LimitKind(text) != LimitAgent || LimitInOutput(text) != "" {
			t.Errorf("%q: LimitKind %q, LimitInOutput %q", text, LimitKind(text), LimitInOutput(text))
		}
	}
}

func TestLastLines(t *testing.T) {
	if got := lastLines("a\n\nb\r\nc\n  \nd\n", 3); got != "b\nc\nd" {
		t.Errorf("lastLines = %q", got)
	}
}

func TestHeldAndResume(t *testing.T) {
	r := start(t, newTestRun(), 101).Passed("ok", 102)
	r = start(t, r, 103).WithChat("c1")
	before := r
	h := r.Held(LimitBudget, "paused: today's budget is spent", 0, 104)
	if h.State != StatePaused || !h.Paused || h.Records[1].State != StepPending || h.Records[1].Attempts != 0 || h.Records[1].Holds != 1 ||
		h.Hold == nil || *h.Hold != (Hold{Kind: LimitBudget, Reason: "paused: today's budget is spent", Count: 1}) {
		t.Fatalf("held %+v hold %+v", h.Records[1], h.Hold)
	}
	if before.Hold != nil || before.Records[1].State != StepRunning || before.Records[1].Attempts != 1 {
		t.Error("Held changed its receiver")
	}
	if last := h.Log[len(h.Log)-1].Text; last != "Paused: Flint's usage budget is spent" {
		t.Errorf("log %q", last)
	}
	if _, ok := h.Next(); ok {
		t.Error("a held run offers its step")
	}
	res, err := h.Resume(105)
	if err != nil || res.Hold != nil || res.State != StateRunning {
		t.Fatalf("resume %s %+v %v", res.State, res.Hold, err)
	}
	res = res.Started(106)
	if res.Records[1].Attempts != 1 || res.Records[1].Holds != 1 {
		t.Errorf("after resume %+v", res.Records[1])
	}
	again := res.Held(LimitAgent, "quota", 999, 107)
	if again.Hold.Count != 2 || again.Hold.Until != 999 || !strings.Contains(again.Log[len(again.Log)-1].Text, `the agent "claude" reached its usage limit`) {
		t.Errorf("second hold %+v %q", again.Hold, again.Log[len(again.Log)-1].Text)
	}
	if p := again.Started(108).Passed("ok", 109); p.Hold != nil || p.Records[1].Holds != 0 {
		t.Errorf("passing keeps the hold: %+v", p.Records[1])
	}
	if s, _ := again.Stop("", 108); s.Hold != nil {
		t.Error("Stop keeps the hold")
	}
	if s, _ := again.SendBack("plan", "", 108); s.Hold != nil || s.State != StateRunning {
		t.Error("SendBack keeps the hold")
	}
}

func TestFallback(t *testing.T) {
	r := start(t, newTestRun(), 101)
	fb := r.Fallback("codex", 102)
	if fb.State != StateRunning || fb.Records[0].State != StepPending || fb.Records[0].Attempts != 0 || fb.Records[0].Agent != "codex" || fb.StepAgent() != "codex" {
		t.Fatalf("fallback %s %+v", fb.State, fb.Records[0])
	}
	if last := fb.Log[len(fb.Log)-1].Text; last != "claude reached its usage limit: codex takes the step" {
		t.Errorf("log %q", last)
	}
	if r.Records[0].Agent != "" {
		t.Error("Fallback changed its receiver")
	}
	st := fb.Started(103)
	if st.Records[0].Agent != "codex" {
		t.Error("Started dropped the override")
	}
	if p := st.Passed("ok", 104); p.Records[0].Agent != "" {
		t.Error("passing keeps the override")
	}
	failedRun := st.Failed("x", "", false, 3, 104)
	if rt, _ := failedRun.Retry(105); rt.Records[0].Agent != "codex" {
		t.Error("Retry dropped the override")
	}
	if sb, _ := failedRun.SendBack("plan", "", 105); sb.Records[0].Agent != "" {
		t.Error("the user's send-back keeps the override")
	}
	paused, _ := r.Pause(102)
	if p := paused.Fallback("codex", 103); p.State != StatePaused {
		t.Errorf("a pause asked for is kept: %s", p.State)
	}
}

func TestLimited(t *testing.T) {
	r := start(t, newTestRun(), 101)
	avail := []string{"claude", "codex"}
	cases := []struct {
		name      string
		r         Run
		kind      string
		p         policy
		fallback  bool
		wantUntil int64
	}{
		{"no fallback", r, LimitAgent, policy{}, false, 0},
		{"fallback", r, LimitAgent, policy{fallback: "codex", available: avail}, true, 0},
		{"budget never falls back", r, LimitBudget, policy{fallback: "codex", available: avail}, false, 0},
		{"fallback not available", r, LimitAgent, policy{fallback: "codex", available: []string{"claude"}}, false, 0},
		{"fallback is the limited agent", r.Fallback("codex", 102).Started(103), LimitAgent, policy{fallback: "codex", available: avail}, false, 0},
		{"resume after", r, LimitAgent, policy{resumeAfter: 600}, false, 1600},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := limited(c.r, c.kind, "why", c.p, 1000)
			if c.fallback {
				if got.Hold != nil || got.Records[0].Agent != "codex" || got.State != StateRunning {
					t.Errorf("no fallback: %s %+v", got.State, got.Records[0])
				}
				return
			}
			if got.Hold == nil || got.Hold.Kind != c.kind || got.Hold.Until != c.wantUntil {
				t.Errorf("hold %+v", got.Hold)
			}
		})
	}
	bound := r
	for range MaxAutoResumes {
		bound = limited(bound, LimitAgent, "q", policy{resumeAfter: 60}, 1000).Started(1001)
	}
	if h := limited(bound, LimitAgent, "q", policy{resumeAfter: 60}, 1000).Hold; h.Count != MaxAutoResumes+1 || h.Until != 0 {
		t.Errorf("after %d automatic resumes: %+v", MaxAutoResumes, h)
	}
}

// ---- the engine

const budgetErr = "paused: today's budget of 100 tokens is spent (120). Choose Continue anyway in the budget warning above the chat, or raise the budget in Settings ▸ Usage"

// atImplement takes a feature run past its plan.
func atImplement(f *fixture) RunView {
	f.t.Helper()
	v := f.start("feature")
	f.finishFirst(v.ID, "RESULT: DONE")
	return v
}

func TestBudgetRefusalHoldsTheRun(t *testing.T) {
	f := newFixture(t)
	f.h.SetStepChatError(errors.New(budgetErr))
	v := atImplement(f)
	r := f.waitFor(v.ID, "held", func(r Run) bool { return r.Hold != nil })
	if r.State != StatePaused || r.Hold.Kind != LimitBudget || r.Records[1].Attempts != 0 || r.Records[1].State != StepPending || r.Error != "" {
		t.Fatalf("held run %s %+v %+v", r.State, r.Hold, r.Records[1])
	}
	if len(f.timers.live()) != 0 {
		t.Error("a timer without resumeAfterMinutes")
	}
	f.h.SetStepChatError(nil)
	if err := f.api.Resume(v.ID); err != nil {
		t.Fatal(err)
	}
	f.chatOf(v.ID, 1)
	if r, _ := f.e.get(v.ID); r.Records[1].Attempts != 1 || r.Hold != nil {
		t.Errorf("resumed %+v", r.Records[1])
	}
}

func TestQuotaInAFailedTurnHoldsTheRun(t *testing.T) {
	f := newFixture(t)
	v := atImplement(f)
	r := f.endChat(v.ID, 1, feature.ChatResult{State: agenttask.Failed, Error: "Quota exceeded"}, "working…")
	if r.State != StatePaused || r.Hold == nil || r.Hold.Kind != LimitAgent || r.Hold.Reason != "Quota exceeded" || r.Records[1].Chat == "" {
		t.Fatalf("run %s hold %+v", r.State, r.Hold)
	}
}

func TestOrdinaryFailureIsStillAFailure(t *testing.T) {
	f := newFixture(t)
	v := atImplement(f)
	r := f.endChat(v.ID, 1, feature.ChatResult{State: agenttask.Failed, Error: "exit status 2"}, "compiling")
	if r.State != StateFailed || r.Hold != nil || r.Error != "exit status 2" {
		t.Errorf("run %s %q %+v", r.State, r.Error, r.Hold)
	}
}

func TestFallbackAgentTakesTheStep(t *testing.T) {
	f := newFixture(t)
	f.settings(Settings{MaxAttempts: 3, FallbackAgent: "codex"})
	v := atImplement(f)
	chat := f.chatOf(v.ID, 1)
	f.endChat(v.ID, 1, feature.ChatResult{State: agenttask.Failed, Error: "You've hit your limit · resets 3pm"}, "")
	next := f.chatOf(v.ID, 1)
	r, _ := f.e.get(v.ID)
	calls := f.h.StepChats()
	if next == chat || calls[len(calls)-1].Agent != "codex" || r.State != StateRunning || r.Paused || r.Records[1].Agent != "codex" || r.Records[1].Attempts != 1 {
		t.Fatalf("fallback: calls %+v run %s %+v", calls, r.State, r.Records[1])
	}
	// The fallback reaches its limit too: the run is held.
	r = f.endChat(v.ID, 1, feature.ChatResult{State: agenttask.Failed, Error: "rate_limit_error"}, "")
	if r.State != StatePaused || r.Hold == nil || r.Records[1].Agent != "codex" {
		t.Errorf("fallback limited: %s %+v %+v", r.State, r.Hold, r.Records[1])
	}
	if n := len(f.h.StepChats()); n != 2 {
		t.Errorf("%d chats", n)
	}
}

func TestFallbackOnTheFirstStep(t *testing.T) {
	f := newFixture(t)
	f.settings(Settings{MaxAttempts: 3, FallbackAgent: "codex"})
	v := f.start("feature")
	f.h.UpdateAgentTask(v.Task, func(t *feature.AgentTask) { t.State, t.Error = agenttask.Failed, "Claude AI usage limit reached" })
	f.e.kickTasks()
	f.chatOf(v.ID, 0)
	calls := f.h.StepChats()
	if len(calls) != 1 || calls[0].Agent != "codex" || !strings.HasSuffix(calls[0].Prompt, "Add a --verbose flag") {
		t.Errorf("calls %+v", calls)
	}
}

func TestResumeAfterMinutes(t *testing.T) {
	f := newFixture(t)
	f.settings(Settings{MaxAttempts: 3, ResumeAfterMinutes: 30})
	f.h.SetStepChatError(errors.New("start claude: " + budgetErr))
	v := atImplement(f)
	r := f.waitFor(v.ID, "held", func(r Run) bool { return r.Hold != nil })
	if r.Hold.Until != 1000+30*60 {
		t.Fatalf("until %d", r.Hold.Until)
	}
	if live := f.timers.live(); len(live) != 1 || live[0].d != 30*time.Minute {
		t.Fatalf("timers %+v", live)
	}
	// Every automatic resume is refused again: the bound ends it.
	for i := 1; i <= MaxAutoResumes; i++ {
		f.clock.Add(30 * 60)
		f.timers.fire(t)
		r = f.waitFor(v.ID, "the next hold", func(r Run) bool { return r.Hold != nil && r.Hold.Count == i+1 })
	}
	if r.Hold.Until != 0 || len(f.timers.live()) != 0 || len(f.h.StepChats()) != MaxAutoResumes+1 {
		t.Errorf("after %d resumes: %+v, %d timers, %d calls", MaxAutoResumes, r.Hold, len(f.timers.live()), len(f.h.StepChats()))
	}
	// The user resumes; the step goes.
	f.h.SetStepChatError(nil)
	if err := f.api.Resume(v.ID); err != nil {
		t.Fatal(err)
	}
	f.chatOf(v.ID, 1)
}

func TestResumeTimerIsCancelledAndRearmed(t *testing.T) {
	f := newFixture(t)
	f.settings(Settings{MaxAttempts: 3, ResumeAfterMinutes: 10})
	v := atImplement(f)
	f.endChat(v.ID, 1, feature.ChatResult{State: agenttask.Failed, Error: "Quota exceeded"}, "")
	if len(f.timers.live()) != 1 {
		t.Fatal("no timer")
	}
	// Restart: the timer is armed again from the stored time.
	f.clock.Add(4 * 60)
	f.restart()
	if live := f.timers.live(); len(live) != 1 || live[0].d != 6*time.Minute {
		t.Fatalf("after restart: %d timers", len(live))
	}
	r, _ := f.e.get(v.ID)
	if r.Hold == nil || r.State != StatePaused {
		t.Fatalf("a held run was changed by the restart: %s", r.State)
	}
	f.timers.fire(t)
	f.chatOf(v.ID, 1)

	// Stop cancels it.
	f.endChat(v.ID, 1, feature.ChatResult{State: agenttask.Failed, Error: "Quota exceeded"}, "")
	if len(f.timers.live()) != 1 {
		t.Fatal("no timer for the second hold")
	}
	if err := f.api.Stop(v.ID); err != nil {
		t.Fatal(err)
	}
	if len(f.timers.live()) != 0 {
		t.Error("Stop left the timer")
	}
}

func TestCloseStopsTheResumeTimers(t *testing.T) {
	f := newFixture(t)
	f.settings(Settings{MaxAttempts: 3, ResumeAfterMinutes: 10})
	v := atImplement(f)
	f.endChat(v.ID, 1, feature.ChatResult{State: agenttask.Failed, Error: "Quota exceeded"}, "")
	f.e.close()
	if len(f.timers.live()) != 0 {
		t.Error("Close left a timer")
	}
	if f.h.Stopped() != nil {
		t.Error("Close stopped a chat")
	}
}

func TestAutoResumeIntoAnUntrustedWorktreeFails(t *testing.T) {
	f := newFixture(t)
	f.settings(Settings{MaxAttempts: 3, ResumeAfterMinutes: 10})
	v := atImplement(f)
	f.endChat(v.ID, 1, feature.ChatResult{State: agenttask.Failed, Error: "Quota exceeded"}, "")
	f.h.SetStepChatError(errors.New("the worktree " + os.TempDir() + " is in Restricted Mode"))
	f.timers.fire(t)
	r := f.waitFor(v.ID, "a failure", func(r Run) bool { return r.State == StateFailed })
	if r.Hold != nil || len(f.timers.live()) != 0 {
		t.Errorf("run %+v timers %d", r.Hold, len(f.timers.live()))
	}
}
