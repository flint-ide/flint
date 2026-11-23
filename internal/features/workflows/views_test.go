package workflows

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"flint/internal/agenttask"
	"flint/internal/feature"
)

func TestActions(t *testing.T) {
	running := start(t, newTestRun(), 101)
	pauseAsked, _ := running.Pause(102)
	pausedPending, _ := start(t, newTestRun(), 101).Passed("ok", 102).Pause(103)
	failed := running.Failed("boom", "", false, 3, 102)
	gate := newTestRun()
	for range 4 {
		gate = start(t, gate, 101).Passed("ok", 102)
	}
	gate = gate.Started(103)
	held := running.Held(LimitAgent, "quota", 0, 102)
	stopped, _ := running.Stop("", 102)
	done, _ := gate.Approve(104)
	cases := []struct {
		name string
		r    Run
		want []string
	}{
		{"running", running, []string{ActionPause, ActionStop}},
		{"pause asked", pauseAsked, []string{ActionResume, ActionStop}},
		{"paused", pausedPending, []string{ActionSendBack, ActionResume, ActionStop}},
		{"failed", failed, []string{ActionSendBack, ActionRetry, ActionSkip, ActionStop}},
		{"waiting", gate, []string{ActionApprove, ActionSendBack, ActionStop}},
		{"held", held, []string{ActionSendBack, ActionResume, ActionStop}},
		{"stopped", stopped, []string{ActionRemove}},
		{"done", done, []string{ActionRemove}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Actions(c.r); !slices.Equal(got, c.want) {
				t.Errorf("Actions = %v, want %v", got, c.want)
			}
		})
	}
}

func TestRunViewJSON(t *testing.T) {
	r := start(t, newTestRun(), 101).WithChat("c1").Held(LimitAgent, "Quota exceeded", 4000, 102)
	r.Task = "0123456789abcdef"
	v := runView(r, agenttask.Working)
	if v.Hold == nil || v.Hold == r.Hold || v.NeedsUser || v.Steps[0].Chat != "c1" || v.Steps[0].Attempts != 0 {
		t.Fatalf("view %+v", v)
	}
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"hold":{"kind":"limit","reason":"Quota exceeded","until":4000,"count":1}`, `"taskState":"working"`,
		`"actions":["sendBack","resume","stop"]`, `"takenBy":""`, `"runTab":""`, `"needsUser":false`} {
		if !strings.Contains(string(data), key) {
			t.Errorf("JSON lacks %s: %s", key, data)
		}
	}
	data, _ = json.Marshal(runView(newTestRun(), ""))
	if !strings.Contains(string(data), `"hold":null`) {
		t.Errorf("no hold: %s", data)
	}
	if w := r.Held(LimitBudget, "spent", 0, 103); !runView(w, "").NeedsUser {
		t.Error("a hold without a time to resume needs the user")
	}
}

func TestChatOutcome(t *testing.T) {
	fin := feature.ChatResult{State: agenttask.Finished}
	cases := []struct {
		name string
		kind Kind
		res  feature.ChatResult
		out  string
		want outcome
	}{
		{"done", KindAgent, fin, "x\nRESULT: DONE", passed("x\nRESULT: DONE")},
		{"blocked", KindAgent, fin, "RESULT: BLOCKED", failed(reasonBlocked, "RESULT: BLOCKED", false)},
		{"no result", KindAgent, fin, "done I think", failed(reasonNoResult, "done I think", false)},
		{"PASS from an agent step", KindAgent, fin, "RESULT: PASS", failed(reasonNoResult, "RESULT: PASS", false)},
		{"pass", KindReview, fin, "RESULT: PASS", passed("RESULT: PASS")},
		{"fail", KindReview, fin, "bad\nRESULT: FAIL", failed(reasonReview, "bad\nRESULT: FAIL", true)},
		{"no verdict", KindReview, fin, "RESULT: DONE", failed(reasonNoVerdict, "RESULT: DONE", false)},
		{"failed turn", KindAgent, feature.ChatResult{State: agenttask.Failed, Error: "exit 1"}, "", failed("exit 1", "", false)},
		{"failed turn without error", KindAgent, feature.ChatResult{State: agenttask.Failed}, "", failed("the agent failed", "", false)},
		{"cancelled", KindReview, feature.ChatResult{State: agenttask.Idle}, "o", failed(reasonCanceled, "o", false)},
		{"odd state", KindAgent, feature.ChatResult{State: "weird"}, "", failed(`the agent's turn ended as "weird"`, "", false)},
		{"quota in the error", KindAgent, feature.ChatResult{State: agenttask.Failed, Error: "Quota exceeded"}, "",
			outcome{limit: LimitAgent, reason: "Quota exceeded"}},
		{"limit in a failed turn's last lines", KindAgent, feature.ChatResult{State: agenttask.Failed, Error: "exit 1"}, "You've hit your limit · resets 3pm",
			outcome{limit: LimitAgent, reason: "exit 1", output: "You've hit your limit · resets 3pm"}},
		{"limit printed as text", KindAgent, fin, "Claude AI usage limit reached", outcome{limit: LimitAgent, reason: "Claude AI usage limit reached", output: "Claude AI usage limit reached"}},
		{"a rate limiter work summary is no limit", KindAgent, fin, "Added the rate limit middleware", failed(reasonNoResult, "Added the rate limit middleware", false)},
		{"a quota feature summary is no limit", KindReview, fin, "The quota check looks fine to me.", failed(reasonNoVerdict, "The quota check looks fine to me.", false)},
		{"limit text with a result line", KindAgent, fin, "Added a rate limit.\nRESULT: DONE", passed("Added a rate limit.\nRESULT: DONE")},
		{"limit text early in the output", KindAgent, fin, "rate limit middleware\na\nb\nc\nd\ne\nall good", failed(reasonNoResult, "rate limit middleware\na\nb\nc\nd\ne\nall good", false)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := chatOutcome(c.kind, c.res, c.out); got != c.want {
				t.Errorf("chatOutcome = %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestScriptOutcome(t *testing.T) {
	cases := []struct {
		res  feature.RunResult
		want outcome
	}{
		{feature.RunResult{Code: 0, Output: "ok"}, passed("ok")},
		{feature.RunResult{Code: 2, Output: "x"}, failed("it exited with code 2", "x", true)},
		{feature.RunResult{Code: -1, Error: "no such file"}, failed("no such file", "", true)},
		{feature.RunResult{Code: -1, Stopped: true, Output: "y"}, failed("it was stopped", "y", false)},
	}
	for _, c := range cases {
		if got := scriptOutcome(c.res); got != c.want {
			t.Errorf("scriptOutcome(%+v) = %+v, want %+v", c.res, got, c.want)
		}
	}
}
