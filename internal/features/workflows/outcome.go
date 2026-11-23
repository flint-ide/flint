package workflows

// Verdict is what the end of an agent or review step's turn decides, from
// its result line alone (ParseResult): the one rule the first evaluation of a
// step and "Check again" (Run.Rechecked) share.
type Verdict struct {
	// Pass: the step passes.
	Pass bool
	// Rework: a failure that may send the work back to the step's on-fail
	// step (a reviewer's FAIL).
	Rework bool
	// Reason says why the step does not pass ("" when it does).
	Reason string
}

// Decide is the verdict for a step of kind (agent or review) whose result
// line is word ("" when there is none; ParseResult's upper case words):
// an agent step passes on DONE; a review step passes on PASS and fails with
// rework on FAIL; BLOCKED, no result line and, for a review, no verdict wait
// for the user.
func Decide(kind Kind, word string) Verdict {
	switch {
	case kind == KindReview && word == "PASS":
		return Verdict{Pass: true}
	case kind == KindReview && word == "FAIL":
		return Verdict{Rework: true, Reason: reasonReview}
	case kind == KindReview:
		return Verdict{Reason: reasonNoVerdict}
	case word == "DONE":
		return Verdict{Pass: true}
	case word == "BLOCKED":
		return Verdict{Reason: reasonBlocked}
	}
	return Verdict{Reason: reasonNoResult}
}
