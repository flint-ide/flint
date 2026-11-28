package workflows

import (
	"cmp"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"flint/internal/agenttask"
	"flint/internal/textsafe"
)

// Run states.
const (
	// StateRunning: the current step is being taken (or is about to be).
	StateRunning = "running"
	// StateWaiting: a gate waits for the user.
	StateWaiting = "waiting"
	// StateFailed: a step failed and nothing sends the work back by itself.
	StateFailed = "failed"
	// StatePaused: the user paused the run; the next step has not started.
	StatePaused = "paused"
	// StateDone and StateStopped: the run is over.
	StateDone    = "done"
	StateStopped = "stopped"
)

// Step record states.
const (
	StepPending = "pending"
	StepRunning = "running"
	StepDone    = "done"
	StepFailed  = "failed"
)

// Bounds of a run.
const (
	// MaxOutput bounds the output a record keeps (its end), in bytes.
	MaxOutput = 4096
	// MaxNote bounds the note of a send-back, in characters.
	MaxNote = 4000
	// MaxLog bounds a run's log; the oldest entries are dropped.
	MaxLog = 200
	// maxRunTitle bounds a run's title, in characters.
	maxRunTitle = 80
	// resultLines is how many of the last non-empty lines ParseResult reads.
	resultLines = 20
)

// Record is what happened to one step of a run.
type Record struct {
	State string `json:"state"`
	// Attempts is how many times the step was started.
	Attempts int `json:"attempts,omitempty"`
	// Chat is the chat id of an agent or review step's latest attempt.
	Chat string `json:"chat,omitempty"`
	// RunTab is the run id of a script step's latest attempt.
	RunTab string `json:"runTab,omitempty"`
	// Output is the end of what the step said or printed (MaxOutput bytes).
	Output string `json:"output,omitempty"`
	// Skipped: the step was passed with "Continue anyway".
	Skipped bool `json:"skipped,omitempty"`
	// Seen is the engine's note that the first step's task was seen working.
	Seen bool `json:"seen,omitempty"`
	// Agent is who takes the step instead of its own agent (a fallback
	// after a usage limit, Fallback); cleared when the step passes or the
	// user sends the work back.
	Agent string `json:"agent,omitempty"`
	// Holds counts the holds of the step (Held) since it last passed.
	Holds int `json:"holds,omitempty"`
	// Nudges counts how many times Flint asked this attempt's chat to
	// finish after a turn without a result line (Nudged).
	Nudges int `json:"nudges,omitempty"`
}

// Hold says why a run is paused by a limit rather than by the user: Flint's
// usage budget ("budget") or the agent's own usage limit ("limit").
type Hold struct {
	Kind   string `json:"kind"`
	Reason string `json:"reason"`
	// Until is when the run resumes by itself (unix seconds; 0: it waits
	// for the user).
	Until int64 `json:"until,omitempty"`
	// Count is how many times the step was held since it last passed.
	Count int `json:"count,omitempty"`
}

// LogEntry is one line of a run's log.
type LogEntry struct {
	At   int64  `json:"at"`
	Step string `json:"step,omitempty"`
	Text string `json:"text"`
}

// Run is one taking of a workflow. It is a value: every transition returns a
// new Run and leaves the receiver (and the slices it holds) untouched. Steps
// is never modified at all, so copies share it.
type Run struct {
	ID          string `json:"id"`
	Workflow    string `json:"workflow"`
	Title       string `json:"title"`
	Description string `json:"description"` // the task text the user wrote
	// Task is the agent task id; Dir the checkout the run was started from;
	// Agent the run's default agent.
	Task  string `json:"task"`
	Dir   string `json:"dir"`
	Agent string `json:"agent"`
	// Steps is the frozen, resolved copy of the workflow's steps.
	Steps []Step `json:"steps"`
	// Records has one entry per step.
	Records []Record `json:"records"`
	State   string   `json:"state"`
	// Error says why State is failed.
	Error string `json:"error,omitempty"`
	// Current is the index of the current step (len(Steps) when done).
	Current int `json:"current"`
	// Paused: a pause was requested; the run holds before the next step.
	Paused bool `json:"paused,omitempty"`
	// Feedback is a note for the current step's agent (from a failed check or
	// a send-back); cleared when that step passes.
	Feedback string `json:"feedback,omitempty"`
	// Hold is set while the run is paused by a limit (Held). It is never
	// changed in place: a transition sets a new one or nil.
	Hold *Hold `json:"hold,omitempty"`
	// Exec is bumped every time a step is started, so the completion of an
	// earlier start is recognised as stale and ignored.
	Exec int        `json:"exec"`
	Log  []LogEntry `json:"log,omitempty"`
	// Created and Updated are unix seconds.
	Created int64 `json:"created"`
	Updated int64 `json:"updated"`
	// Origin is an opaque note of who started the run ("" the user,
	// "schedule:<name>", "item:<...>", "agent").
	Origin string `json:"origin,omitempty"`
}

// NewRun makes a running run at its first step. The title is the first line
// of the description (80 characters at most), or the workflow's name when the
// description is empty.
func NewRun(id, workflow string, steps []Step, description, agent, dir string, now int64) Run {
	r := Run{
		ID: id, Workflow: workflow, Description: description, Agent: agent, Dir: dir,
		Title:   runTitle(description, workflow),
		Steps:   slices.Clone(steps),
		Records: make([]Record, len(steps)),
		State:   StateRunning,
		Created: now, Updated: now,
	}
	for i := range r.Records {
		r.Records[i].State = StepPending
	}
	if len(steps) == 0 {
		r.State = StateDone
	}
	return r.logged(now, -1, "Run started")
}

func runTitle(description, fallback string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(description), "\n")
	line = strings.TrimSpace(line)
	if line == "" {
		return fallback
	}
	if utf8.RuneCountInString(line) > maxRunTitle {
		line = string([]rune(line)[:maxRunTitle-1]) + "…"
	}
	return line
}

// Over reports whether the run is done or stopped.
func (r Run) Over() bool { return r.State == StateDone || r.State == StateStopped }

// NeedsUser reports whether the run waits for the user: at a gate, or after a
// failure.
func (r Run) NeedsUser() bool { return r.State == StateWaiting || r.State == StateFailed }

// Next is the step the engine must start now: only while the run is running
// and its current step is pending.
func (r Run) Next() (Step, bool) {
	if r.State != StateRunning || r.Current >= len(r.Steps) || r.Records[r.Current].State != StepPending {
		return Step{}, false
	}
	return r.Steps[r.Current], true
}

// ---- helpers (all work on a run that is already a private copy) ----

// clone copies what a transition changes.
func (r Run) clone() Run {
	r.Records = slices.Clone(r.Records)
	r.Log = slices.Clone(r.Log)
	return r
}

// logged adds a log line about step i (-1: none) and stamps the run.
func (r Run) logged(now int64, i int, text string) Run {
	e := LogEntry{At: now, Text: text}
	if i >= 0 && i < len(r.Steps) {
		e.Step = r.Steps[i].ID
	}
	r.Log = append(r.Log, e)
	if len(r.Log) > MaxLog {
		r.Log = slices.Clone(r.Log[len(r.Log)-MaxLog:])
	}
	r.Updated = now
	return r
}

func (r Run) title(i int) string {
	if i >= 0 && i < len(r.Steps) {
		return r.Steps[i].Title
	}
	return ""
}

// valid reports whether Current points at a step.
func (r Run) valid() bool { return r.Current >= 0 && r.Current < len(r.Steps) }

// advance moves past the current step, which is already marked.
func (r Run) advance(now int64) Run {
	r.Feedback, r.Hold = "", nil
	r.Current++
	switch {
	case r.Current >= len(r.Steps):
		r.State, r.Paused = StateDone, false
		return r.logged(now, -1, "Run done")
	case r.Paused:
		r.State = StatePaused
		return r.logged(now, r.Current, "Paused before "+r.title(r.Current))
	}
	r.State = StateRunning
	return r
}

// resetFrom sets the records from index from to the end back to pending
// (their attempts are kept) and makes from the current step.
func (r Run) resetFrom(from int) Run {
	for i := from; i < len(r.Records); i++ {
		r.Records[i].State = StepPending
		r.Records[i].Skipped = false
		r.Records[i].Seen = false
	}
	r.Hold = nil
	r.Current = from
	return r
}

func tailBytes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	s = s[len(s)-max:]
	for len(s) > 0 && !utf8.RuneStart(s[0]) {
		s = s[1:]
	}
	return s
}

// ---- transitions ----

// Started marks the current step as started: its record is running, Attempts
// and Exec go up and what its earlier attempt left (chat, run tab, output) is
// cleared. A gate is not started: the run waits (StateWaiting) with the
// record still pending, Attempts and Exec unchanged, and Approve (or
// SendBack, Stop) goes on from there.
func (r Run) Started(now int64) Run {
	if !r.valid() {
		return r
	}
	r = r.clone()
	if r.Steps[r.Current].Kind == KindGate {
		r.State = StateWaiting
		return r.logged(now, r.Current, "Waiting for your approval: "+r.title(r.Current))
	}
	old := r.Records[r.Current]
	r.Records[r.Current] = Record{State: StepRunning, Attempts: old.Attempts + 1, Agent: old.Agent, Holds: old.Holds}
	r.Exec++
	return r.logged(now, r.Current, fmt.Sprintf("Started %s (attempt %d)", r.title(r.Current), r.Records[r.Current].Attempts))
}

// WithChat records the chat of the current step's attempt.
func (r Run) WithChat(chat string) Run {
	if !r.valid() {
		return r
	}
	r = r.clone()
	r.Records[r.Current].Chat = chat
	return r
}

// WithRunTab records the run tab of the current step's attempt.
func (r Run) WithRunTab(id string) Run {
	if !r.valid() {
		return r
	}
	r = r.clone()
	r.Records[r.Current].RunTab = id
	return r
}

// Nudged records that Flint asked the current step's chat, whose turn ended
// without a result line, to finish: the nth of at most max times.
func (r Run) Nudged(max int, now int64) Run {
	if !r.valid() {
		return r
	}
	r = r.clone()
	r.Records[r.Current].Nudges++
	return r.logged(now, r.Current, fmt.Sprintf("%s ended its turn without a result line: Flint asked it to finish (%d of %d)",
		r.title(r.Current), r.Records[r.Current].Nudges, max))
}

// WithSeen records that the first step's task was seen working.
func (r Run) WithSeen() Run {
	if !r.valid() {
		return r
	}
	r = r.clone()
	r.Records[r.Current].Seen = true
	return r
}

// Passed marks the current step done, keeping the end of output, and goes on:
// to done after the last step, to paused when a pause was requested, else to
// the next step (pending, for Next).
func (r Run) Passed(output string, now int64) Run {
	if !r.valid() {
		return r
	}
	r = r.clone()
	r.Records[r.Current].State = StepDone
	r.Records[r.Current].Output = tailBytes(output, MaxOutput)
	r.Records[r.Current].Agent, r.Records[r.Current].Holds = "", 0
	r = r.logged(now, r.Current, "Passed: "+r.title(r.Current))
	return r.advance(now)
}

// Failed marks the current step failed. With rework, an on-fail step and
// attempts left (the failing record's Attempts below maxAttempts) the work
// goes back to the on-fail step by itself: it and everything after it is
// pending again and Feedback tells that agent which step failed, why and what
// it printed. Otherwise the run is failed with reason as its Error.
func (r Run) Failed(reason, output string, rework bool, maxAttempts int, now int64) Run {
	if !r.valid() {
		return r
	}
	r = r.clone()
	r.Records[r.Current].State = StepFailed
	r.Records[r.Current].Output = tailBytes(output, MaxOutput)
	step, attempts := r.Steps[r.Current], r.Records[r.Current].Attempts
	r = r.logged(now, r.Current, fmt.Sprintf("Failed: %s: %s", step.Title, reason))
	if rework && step.OnFail != "" && attempts < maxAttempts {
		if j := slices.IndexFunc(r.Steps[:r.Current], func(s Step) bool { return s.ID == step.OnFail }); j >= 0 {
			fb := fmt.Sprintf("The step %q failed: %s", step.Title, reason)
			if out := strings.TrimSpace(r.Records[r.Current].Output); out != "" {
				fb += "\nWhat it said:\n" + out
			}
			r = r.resetFrom(j)
			// The feedback goes into a prompt: what a script printed or a
			// reviewer wrote must not hide or break lines there.
			r.Feedback, r.Error = textsafe.Plain(fb), ""
			r.State = StateRunning
			if r.Paused {
				r.State = StatePaused
			}
			return r.logged(now, j, "Sent back to "+r.title(j)+" automatically")
		}
	}
	r.State, r.Error = StateFailed, reason
	return r
}

// Approve passes the gate the run waits at.
func (r Run) Approve(now int64) (Run, error) {
	if r.State != StateWaiting || !r.valid() || r.Steps[r.Current].Kind != KindGate {
		return r, errors.New("the run is not waiting for approval")
	}
	r = r.clone()
	r.Records[r.Current].State = StepDone
	r = r.logged(now, r.Current, "Approved: "+r.title(r.Current))
	return r.advance(now), nil
}

// SendBack takes the steps from stepID (an id, or a title) on again, with
// note for its agent. It is allowed while the run waits, failed or paused,
// and only to the current step or an earlier one.
func (r Run) SendBack(stepID, note string, now int64) (Run, error) {
	if r.State != StateWaiting && r.State != StateFailed && r.State != StatePaused {
		return r, errors.New("a run can be sent back only while it waits, has failed or is paused")
	}
	target := slices.IndexFunc(r.Steps, func(s Step) bool { return s.ID == stepID })
	if target < 0 {
		id := agenttask.Slug(stepID)
		target = slices.IndexFunc(r.Steps, func(s Step) bool { return s.ID == id })
	}
	switch {
	case target < 0:
		return r, fmt.Errorf("the run has no step %q", stepID)
	case target > r.Current:
		return r, fmt.Errorf("the step %q has not been reached: send back to the current step or an earlier one", r.Steps[target].Title)
	}
	note = strings.TrimSpace(note)
	if utf8.RuneCountInString(note) > MaxNote {
		return r, fmt.Errorf("the note is longer than %d characters", MaxNote)
	}
	if c, bad := textsafe.HiddenRune(note); bad {
		return r, fmt.Errorf("the note contains %s", textsafe.RuneKind(c))
	}
	r = r.sentBack(target, note, "Sent back to "+r.title(target), now)
	for i := target; i < len(r.Records); i++ {
		r.Records[i].Agent = "" // the user chose: each step's own agent again
	}
	return r, nil
}

// sentBack is SendBack without its checks.
func (r Run) sentBack(target int, feedback, log string, now int64) Run {
	r = r.clone().resetFrom(target)
	r.State, r.Paused, r.Error, r.Feedback = StateRunning, false, "", feedback
	return r.logged(now, target, log)
}

// Retry takes the failed step again, keeping the feedback it had.
func (r Run) Retry(now int64) (Run, error) {
	if r.State != StateFailed || !r.valid() {
		return r, errors.New("the run has not failed")
	}
	return r.sentBack(r.Current, r.Feedback, "Taking "+r.title(r.Current)+" again", now), nil
}

// Recheckable reports whether Rechecked applies: the run has failed at an
// agent or review step that has a chat.
func (r Run) Recheckable() bool {
	if r.State != StateFailed || !r.valid() {
		return false
	}
	k := r.Steps[r.Current].Kind
	return (k == KindAgent || k == KindReview) && r.Records[r.Current].Chat != ""
}

// Rechecked is "Check again": output is the end of the failed step's chat as
// it reads now (the user answered the agent there), and its result line is
// judged as when the turn first ended (Decide). DONE or PASS passes the step
// (it is not marked continued) and the run goes on; a reviewer's FAIL fails
// as usual, with rework; BLOCKED or no result line leaves the run failed with
// the same reason. Attempts are not counted.
func (r Run) Rechecked(output string, maxAttempts int, now int64) (Run, error) {
	if !r.Recheckable() {
		return r, errors.New("only a run that failed at an agent or review step with a chat can be checked again")
	}
	step := r.Steps[r.Current]
	word, _ := ParseResult(output)
	v := Decide(step.Kind, word)
	r = r.clone()
	switch {
	case v.Pass:
		r.Error, r.Paused = "", false
		r = r.logged(now, r.Current, "Checked again: "+step.Title+" ends with RESULT: "+word)
		return r.Passed(output, now), nil
	case v.Rework:
		r.Error = ""
		return r.logged(now, r.Current, "Checked again: "+step.Title+" ends with RESULT: "+word).
			Failed(v.Reason, output, true, maxAttempts, now), nil
	}
	r.State, r.Error = StateFailed, v.Reason
	r.Records[r.Current].Output = tailBytes(output, MaxOutput)
	return r.logged(now, r.Current, "Checked again: "+v.Reason), nil
}

// Skip passes the failed step ("Continue anyway") and goes on.
func (r Run) Skip(now int64) (Run, error) {
	if r.State != StateFailed || !r.valid() {
		return r, errors.New("the run has not failed")
	}
	r = r.clone()
	// A pause asked for before the failure is over: the run goes on.
	r.Error, r.Paused = "", false
	r.Records[r.Current].State = StepDone
	r.Records[r.Current].Skipped = true
	r = r.logged(now, r.Current, "Continued anyway past "+r.title(r.Current))
	return r.advance(now), nil
}

// Pause holds the run before its next step. While a step runs, the run stays
// running with Paused set; when no step runs (the next one is pending), it is
// paused at once.
func (r Run) Pause(now int64) (Run, error) {
	switch {
	case r.State == StatePaused || (r.State == StateRunning && r.Paused):
		return r, errors.New("the run is already paused")
	case r.State != StateRunning:
		return r, errors.New("only a running run can be paused")
	}
	r = r.clone()
	r.Paused = true
	if r.valid() && r.Records[r.Current].State == StepPending {
		r.State = StatePaused
		return r.logged(now, r.Current, "Paused"), nil
	}
	return r.logged(now, r.Current, "Pause requested: the run pauses before the next step"), nil
}

// Resume lets a paused run go on.
func (r Run) Resume(now int64) (Run, error) {
	switch {
	case r.State == StateFailed:
		return r, errors.New("the run has failed: take the step again or continue past it instead")
	case !r.Paused:
		return r, errors.New("the run is not paused")
	}
	r = r.clone()
	r.Paused, r.Hold = false, nil
	if r.State == StatePaused {
		r.State = StateRunning
	}
	return r.logged(now, r.Current, "Resumed"), nil
}

// Stop ends the run; a step that was running is failed. reason is logged.
func (r Run) Stop(reason string, now int64) (Run, error) {
	if r.Over() {
		return r, errors.New("the run is already over")
	}
	r = r.clone()
	if r.valid() && r.Records[r.Current].State == StepRunning {
		r.Records[r.Current].State = StepFailed
	}
	r.State, r.Paused, r.Hold = StateStopped, false, nil
	text := "Stopped"
	if reason = strings.TrimSpace(reason); reason != "" {
		text += ": " + reason
	}
	return r.logged(now, r.Current, text), nil
}

// Interrupted is for the start of the app: a step that was running when
// Flint quit has failed, and Retry takes it again. Other runs are unchanged.
func (r Run) Interrupted(now int64) Run {
	if !r.valid() || r.Records[r.Current].State != StepRunning || r.Over() {
		return r
	}
	r = r.clone()
	r.Records[r.Current].State = StepFailed
	r.State = StateFailed
	r.Error = "Flint quit while this step was running: Retry takes it again"
	return r.logged(now, r.Current, r.Error)
}

// StepAgent is who takes the current step: its override (Record.Agent),
// else the step's agent, else the run's.
func (r Run) StepAgent() string {
	if !r.valid() {
		return r.Agent
	}
	return cmp.Or(r.Records[r.Current].Agent, r.Steps[r.Current].Agent, r.Agent)
}

// unstarted puts the running current step back to pending, undoing the
// count of its start: a start a limit refused is not an attempt.
func (r Run) unstarted() Run {
	rec := &r.Records[r.Current]
	if rec.State == StepRunning && rec.Attempts > 0 {
		rec.Attempts--
	}
	rec.State = StepPending
	return r
}

// Held pauses the run because a limit stopped its current step: Flint's
// usage budget (LimitBudget) or the agent's usage limit (LimitAgent). The
// step goes back to pending without counting the start, nothing is sent
// back, and Resume takes the step again. until is when the engine resumes
// it by itself (0: the user does).
func (r Run) Held(kind, reason string, until, now int64) Run {
	if !r.valid() || r.Over() {
		return r
	}
	r = r.clone().unstarted()
	r.Records[r.Current].Holds++
	r.Hold = &Hold{Kind: kind, Reason: tailBytes(strings.TrimSpace(reason), MaxNote), Until: until, Count: r.Records[r.Current].Holds}
	r.State, r.Paused, r.Error = StatePaused, true, ""
	text := "Paused: Flint's usage budget is spent"
	if kind != LimitBudget {
		text = fmt.Sprintf("Paused: the agent %q reached its usage limit", r.StepAgent())
	}
	return r.logged(now, r.Current, text)
}

// Fallback takes the current step again at once with agent, because the
// agent that took it reached its usage limit: the start is not counted and
// the record keeps agent as its override.
func (r Run) Fallback(agent string, now int64) Run {
	if !r.valid() || r.Over() {
		return r
	}
	before := r.StepAgent()
	r = r.clone().unstarted()
	r.Records[r.Current].Agent = agent
	r.State, r.Error = StateRunning, ""
	if r.Paused {
		r.State = StatePaused
	}
	return r.logged(now, r.Current, fmt.Sprintf("%s reached its usage limit: %s takes the step", before, agent))
}

var resultLine = regexp.MustCompile(`(?i)^RESULT:\s*(DONE|BLOCKED|PASS|FAIL)$`)

// ParseResult finds the verdict an agent ended with: the last of the final
// 20 non-empty lines that, without surrounding spaces, '*' and '`', reads
// "RESULT: DONE", "BLOCKED", "PASS" or "FAIL" (any case). It returns the word
// in upper case.
func ParseResult(output string) (string, bool) {
	lines := strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n")
	seen := 0
	for i := len(lines) - 1; i >= 0 && seen < resultLines; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		seen++
		if m := resultLine.FindStringSubmatch(strings.Trim(line, "*` \t")); m != nil {
			return strings.ToUpper(m[1]), true
		}
	}
	return "", false
}
