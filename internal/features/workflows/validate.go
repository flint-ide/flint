package workflows

import (
	"errors"
	"flint/internal/agentdefs"
	"flint/internal/textsafe"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"flint/internal/agenttask"
)

// A stored run comes back from state.json, a file any program of the user
// can write. The engine starts agents and scripts from what a run holds
// (its steps' kinds, agents, run configuration names, text), so a run is
// checked before it is kept: what this engine could have written, within
// the bounds it keeps, and nothing else (validRun).

// Bounds of a stored run, past the ones its transitions keep.
const (
	// maxStoredTitle bounds a step's title: a resolved step's title is
	// prefixed by the titles of the steps that named its workflow.
	maxStoredTitle = (MaxDepth + 1) * (MaxTitle + 3)
	// maxStoredID bounds a step's id (prefixed the same way).
	maxStoredID = (MaxDepth + 1) * (MaxTitle + 1)
	// maxStoredChat bounds a chat or run tab id.
	maxStoredChat = 128
	// maxStoredLogText bounds one log line.
	maxStoredLogText = 2 * (MaxNote + MaxOutput)
)

var (
	// stepID is a resolved step's id: slugs joined by dashes.
	stepID = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	// runStates and stepStates are the states a run and a record may have.
	runStates  = []string{StateRunning, StateWaiting, StateFailed, StatePaused, StateDone, StateStopped}
	stepStates = []string{StepPending, StepRunning, StepDone, StepFailed}
	// runKinds are the kinds of a resolved step (a workflow step is
	// replaced by its workflow's steps).
	runKinds = []Kind{KindAgent, KindScript, KindReview, KindGate}
)

// validRun reports why a stored run is not one this engine wrote, nil when
// it is.
func validRun(r Run) error {
	var errs []error
	bad := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }
	switch {
	case !runID.MatchString(r.ID):
		bad("invalid id")
	case !agenttask.ValidID(r.Task):
		bad("invalid agent task")
	case !ValidName(r.Workflow):
		bad("invalid workflow name")
	case r.Dir == "" || !filepath.IsAbs(r.Dir) || filepath.Clean(r.Dir) != r.Dir || len(r.Dir) > maxPath:
		bad("invalid checkout")
	case r.Agent != "" && !agentdefs.ValidName(r.Agent):
		bad("invalid agent")
	case len(r.Steps) == 0 || len(r.Steps) > MaxSteps || len(r.Records) != len(r.Steps):
		bad("%d steps, %d records", len(r.Steps), len(r.Records))
	case r.Current < 0 || r.Current > len(r.Steps):
		bad("current step %d", r.Current)
	case !slices.Contains(runStates, r.State):
		bad("state %q", r.State)
	case r.Exec < 0 || len(r.Log) > MaxLog:
		bad("exec %d, %d log entries", r.Exec, len(r.Log))
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	text := []struct {
		what string
		s    string
		max  int
	}{
		{"title", r.Title, 4 * maxRunTitle},
		{"description", r.Description, agenttask.MaxPrompt},
		{"error", r.Error, maxStoredLogText},
		{"feedback", r.Feedback, maxStoredLogText},
		{"origin", r.Origin, agenttask.MaxOrigin},
	}
	for _, t := range text {
		if len(t.s) > t.max {
			bad("%s over %d bytes", t.what, t.max)
		}
	}
	if r.Hold != nil && (len(r.Hold.Reason) > maxStoredLogText || r.Hold.Kind != LimitBudget && r.Hold.Kind != LimitAgent) {
		bad("invalid hold")
	}
	for i, st := range r.Steps {
		if err := validStep(st); err != nil {
			bad("step %d: %v", i+1, err)
		}
		if err := validRecord(r.Records[i]); err != nil {
			bad("record %d: %v", i+1, err)
		}
	}
	for _, l := range r.Log {
		if len(l.Text) > maxStoredLogText || len(l.Step) > maxStoredID {
			bad("log entry over its bound")
			break
		}
	}
	return errors.Join(errs...)
}

// validStep checks a stored step: a known kind, an id and a title of their
// form, an agent's name, a run configuration's name, an on-fail step's id
// and the text within their bounds and without hidden characters.
func validStep(st Step) error {
	switch {
	case !slices.Contains(runKinds, st.Kind):
		return fmt.Errorf("kind %q", st.Kind)
	case len(st.ID) > maxStoredID || !stepID.MatchString(st.ID):
		return errors.New("invalid id")
	case st.Title == "" || len(st.Title) > maxStoredTitle || hasHidden(st.Title) || strings.ContainsAny(st.Title, "\r\n"):
		return errors.New("invalid title")
	case st.Agent != "" && !agentdefs.ValidName(st.Agent):
		return errors.New("invalid agent")
	case len(st.Run) > MaxRunName || hasHidden(st.Run) || strings.ContainsAny(st.Run, "\r\n"):
		return errors.New("invalid run configuration")
	case st.Kind == KindScript && st.Run == "":
		return errors.New("a script step without a run configuration")
	case st.OnFail != "" && (len(st.OnFail) > maxStoredID || !stepID.MatchString(st.OnFail)):
		return errors.New("invalid on-fail")
	case st.Workflow != "" && !ValidName(st.Workflow):
		return errors.New("invalid workflow")
	case len(st.Text) > MaxText || hasHidden(st.Text):
		return errors.New("invalid text")
	}
	return nil
}

// validRecord checks a stored record.
func validRecord(rec Record) error {
	switch {
	case !slices.Contains(stepStates, rec.State):
		return fmt.Errorf("state %q", rec.State)
	case rec.Attempts < 0 || rec.Holds < 0:
		return errors.New("negative count")
	case len(rec.Chat) > maxStoredChat || hasHidden(rec.Chat) || len(rec.RunTab) > maxStoredChat || hasHidden(rec.RunTab):
		return errors.New("invalid chat or run tab")
	case rec.Agent != "" && !agentdefs.ValidName(rec.Agent):
		return errors.New("invalid agent")
	case len(rec.Output) > MaxOutput:
		return errors.New("output over its bound")
	}
	return nil
}

// hasHidden reports a character the text rules refuse (textsafe.HiddenRune).
func hasHidden(s string) bool {
	_, bad := textsafe.HiddenRune(s)
	return bad
}
