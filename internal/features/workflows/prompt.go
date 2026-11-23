package workflows

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"flint/internal/textsafe"
)

// MaxPrompt bounds the text of a step prompt, in bytes. When it is too long,
// the feedback is cut first, then the task, then the step's own text.
const MaxPrompt = 48 << 10

const (
	promptHead     = "===== Workflow step (from Flint) ====="
	promptFeedback = "===== Sent back to this step ====="
	promptStep     = "===== Your step ====="
	promptTask     = "===== The task ====="
	cutNote        = "[cut: too long]"

	// defaultReview is what a review step says when its file says nothing.
	defaultReview = "Review the diff of this branch against its base: look for bugs, missing tests, " +
		"and whether the change does what the task asks."
)

// StepPrompt is the text the agent of the run's current step (agent or
// review) gets. With withTask the task's description closes it; without, the
// text ends with the "===== The task =====" line (no newline after it),
// because Flint appends the task's description itself for the first step.
// Every text put in is made fence-safe, and the whole is at most MaxPrompt
// bytes.
func StepPrompt(r Run, withTask bool) string {
	if !r.valid() {
		return ""
	}
	step := r.Steps[r.Current]
	text := strings.TrimSpace(step.Text)
	if text == "" && step.Kind == KindReview {
		text = defaultReview
	}
	p := promptParts{
		head:     promptIntro(r, step),
		feedback: strings.TrimSpace(r.Feedback),
		text:     text,
		desc:     strings.TrimSpace(r.Description),
		withTask: withTask,
	}
	return p.fit().render()
}

// promptIntro is the fixed part: where the step is in the run, and the rules.
func promptIntro(r Run, step Step) string {
	var b strings.Builder
	b.WriteString(promptHead + "\n")
	fmt.Fprintf(&b, "You are taking step %d of %d, %q, of the workflow %q.\n",
		r.Current+1, len(r.Steps), textsafe.FenceSafe(oneLine(step.Title)), textsafe.FenceSafe(oneLine(r.Workflow)))
	b.WriteString("The steps: " + stepList(r) + "\n")
	b.WriteString("Earlier steps left their work in this folder: read the files and the commits.\n")
	if step.Kind == KindReview {
		b.WriteString("You are reviewing work another agent did. Do not change any files.\n")
		b.WriteString("Do not ask the user questions: nobody reads this chat until the step is over.\n")
		b.WriteString("End your answer with one line, exactly: \"RESULT: PASS\" when the work is good as it is, " +
			"or \"RESULT: FAIL\" after listing what must change.\n")
	} else {
		b.WriteString("Do only this step. Do not ask the user questions: nobody reads this chat until the step is over.\n")
		b.WriteString("End your answer with one line, exactly: \"RESULT: DONE\" when the step is done, " +
			"or \"RESULT: BLOCKED\" when you cannot finish it, after saying why.\n")
	}
	return b.String()
}

// stepList is "1. Plan (done) · 2. Implement (now) · 3. Test (script) · …".
func stepList(r Run) string {
	items := make([]string, len(r.Steps))
	for i, s := range r.Steps {
		label := ""
		switch {
		case r.Records[i].State == StepDone:
			label = " (done)"
		case i == r.Current:
			label = " (now)"
		case s.Kind == KindScript:
			label = " (script)"
		case s.Kind == KindGate:
			label = " (you)"
		}
		items[i] = fmt.Sprintf("%d. %s%s", i+1, textsafe.FenceSafe(oneLine(s.Title)), label)
	}
	return strings.Join(items, " · ")
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// promptParts are the variable texts of a prompt; render shortens them until
// the whole fits.
type promptParts struct {
	head, feedback, text, desc string
	withTask                   bool
}

func (p promptParts) render() string {
	var b strings.Builder
	b.WriteString(p.head)
	if p.feedback != "" {
		b.WriteString(promptFeedback + "\n" + textsafe.FenceSafe(p.feedback) + "\n")
	}
	b.WriteString(promptStep + "\n" + textsafe.FenceSafe(p.text) + "\n")
	b.WriteString(promptTask)
	if p.withTask {
		b.WriteString("\n" + textsafe.FenceSafe(p.desc))
	}
	return b.String()
}

// fit cuts the feedback, then the task, then the step's text, until the
// prompt is at most MaxPrompt bytes.
func (p promptParts) fit() promptParts {
	for _, field := range []*string{&p.feedback, &p.desc, &p.text} {
		over := len(p.render()) - MaxPrompt
		if over <= 0 {
			return p
		}
		if *field == "" || (field == &p.desc && !p.withTask) {
			continue
		}
		*field = cutText(*field, len(*field)-over)
	}
	return p
}

// cutText keeps the beginning of s, at most keep bytes of it, and says that
// the rest was cut.
func cutText(s string, keep int) string {
	keep -= len(cutNote) + 1
	if keep <= 0 {
		return cutNote
	}
	if keep > len(s) {
		keep = len(s)
	}
	for keep > 0 && keep < len(s) && !utf8.RuneStart(s[keep]) {
		keep--
	}
	return strings.TrimRight(s[:keep], " \t\n") + "\n" + cutNote
}
