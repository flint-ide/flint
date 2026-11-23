package workflows

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"flint/internal/agentdefs"
	"flint/internal/agenttask"
	"flint/internal/prompts"
	"flint/internal/textsafe"
)

// Bounds of a workflow file.
const (
	// MaxSteps bounds a workflow's steps, counted after the steps of the
	// workflows it names were put in.
	MaxSteps = 30
	// MaxDepth bounds workflows inside workflows.
	MaxDepth = 3
	// MaxText bounds one step's text, in characters.
	MaxText = 16000
	// MaxTitle bounds a step's title, MaxDescription a workflow's description.
	MaxTitle       = 80
	MaxDescription = 200
	// MaxRunName bounds a run configuration's name in a script step.
	MaxRunName = 100
	// MaxWorkflows bounds the workflows read from one folder.
	MaxWorkflows = 50
)

// Kind is what a step is.
type Kind string

const (
	// KindAgent: an agent takes the step's text in a new chat.
	KindAgent Kind = "agent"
	// KindScript: a run configuration runs; exit code 0 passes.
	KindScript Kind = "script"
	// KindReview: another agent reviews the change and gives a verdict.
	KindReview Kind = "review"
	// KindGate: the run waits for the user's approval.
	KindGate Kind = "gate"
	// KindWorkflow: the named workflow's steps take the step's place
	// (Resolve); a run never holds a step of this kind.
	KindWorkflow Kind = "workflow"
)

// Step is one step of a workflow.
type Step struct {
	// ID is the title as a slug, unique in the workflow.
	ID    string `json:"id"`
	Title string `json:"title"`
	Kind  Kind   `json:"kind"`
	// Agent takes an agent or review step ("" = the run's agent).
	Agent string `json:"agent,omitempty"`
	// Run is a script step's run configuration, by name.
	Run string `json:"run,omitempty"`
	// Workflow is the workflow a workflow step stands for.
	Workflow string `json:"workflow,omitempty"`
	// OnFail is the id of the earlier agent step the work goes back to when
	// this script or review step fails ("" = the run waits for the user).
	OnFail string `json:"onFail,omitempty"`
	// Text is the step's instructions (agent, review) or what to check (gate).
	Text string `json:"text,omitempty"`
}

// Source says where a workflow comes from.
type Source string

const (
	SourceBuiltin  Source = "builtin"
	SourcePersonal Source = "personal"
	SourceProject  Source = "project"
)

// Workflow is a parsed workflow file.
type Workflow struct {
	// Name is the file name without .md.
	Name        string `json:"name"`
	Description string `json:"description"`
	Steps       []Step `json:"steps"`
	Source      Source `json:"source"`
	// Path is the file ("" for a built-in one).
	Path string `json:"path"`
}

var (
	stepKeys = []string{"type", "agent", "run", "on-fail", "workflow"}
	keyLine  = regexp.MustCompile(`^([A-Za-z][A-Za-z-]*):(.*)$`)
)

// ValidName reports whether name can name a workflow, a schedule or a
// project item's workflow key: lowercase letters, digits and dashes, 64
// characters at most (textsafe.ValidSlugName).
func ValidName(name string) bool { return textsafe.ValidSlugName(name) }

// Parse reads a workflow file. Its steps may still name other workflows
// (Resolve puts those in).
func Parse(name string, data []byte) (Workflow, error) {
	if !ValidName(name) {
		return Workflow{}, errors.New("the file name must be lowercase letters, digits and dashes")
	}
	fields, body, err := prompts.FrontMatter(data)
	if err != nil {
		return Workflow{}, err
	}
	w := Workflow{Name: name}
	for _, f := range fields {
		if f.Key != "description" {
			continue // unknown keys are ignored, as in prompt files
		}
		if utf8.RuneCountInString(f.Value) > MaxDescription {
			return Workflow{}, fmt.Errorf("description is longer than %d characters", MaxDescription)
		}
		w.Description = f.Value
	}
	if r, bad := textsafe.HiddenRune(body); bad {
		return Workflow{}, fmt.Errorf("the file contains %s", textsafe.RuneKind(r))
	}
	sections := splitSteps(body)
	if len(sections) == 0 {
		return Workflow{}, errors.New("no steps: a step starts with a line like \"## Implement\"")
	}
	if len(sections) > MaxSteps {
		return Workflow{}, fmt.Errorf("more than %d steps", MaxSteps)
	}
	for _, sec := range sections {
		step, err := parseStep(sec)
		if err != nil {
			return Workflow{}, fmt.Errorf("step %q: %w", sec.title, err)
		}
		if slices.ContainsFunc(w.Steps, func(s Step) bool { return s.ID == step.ID }) {
			return Workflow{}, fmt.Errorf("two steps are named %q: give each step its own title", step.Title)
		}
		w.Steps = append(w.Steps, step)
	}
	if err := checkOnFail(w.Steps); err != nil {
		return Workflow{}, err
	}
	return w, nil
}

// section is the raw text of one step: its heading and the lines below it.
type section struct {
	title string
	lines []string
}

// splitSteps cuts the body at its "## " lines. Text before the first one is
// a note for people and is left out; a "## " line inside a ``` code block is
// text, not a step.
func splitSteps(body string) []section {
	var out []section
	fenced := false
	for line := range strings.SplitSeq(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenced = !fenced
		}
		if title, ok := strings.CutPrefix(line, "## "); ok && !fenced {
			out = append(out, section{title: strings.TrimSpace(title)})
			continue
		}
		if len(out) > 0 {
			out[len(out)-1].lines = append(out[len(out)-1].lines, line)
		}
	}
	return out
}

// parseStep reads a step: the "key: value" lines right under the heading,
// up to the first empty line, then its text.
func parseStep(sec section) (Step, error) {
	s := Step{Title: sec.title, Kind: KindAgent, ID: agenttask.Slug(sec.title)}
	if s.Title == "" {
		return Step{}, errors.New("the step has no title")
	}
	if utf8.RuneCountInString(s.Title) > MaxTitle {
		return Step{}, fmt.Errorf("the title is longer than %d characters", MaxTitle)
	}
	if !strings.ContainsFunc(s.Title, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) {
		return Step{}, errors.New("the title needs a letter or a digit")
	}
	rest := sec.lines
	for len(rest) > 0 && strings.TrimSpace(rest[0]) != "" {
		m := keyLine.FindStringSubmatch(strings.TrimSpace(rest[0]))
		if m == nil {
			break // the text starts right under the heading
		}
		key, value := strings.ToLower(m[1]), strings.TrimSpace(m[2])
		if !slices.Contains(stepKeys, key) {
			return Step{}, fmt.Errorf("unknown key %q (the keys are %s); put an empty line before the step's text", key, strings.Join(stepKeys, ", "))
		}
		if err := s.set(key, value); err != nil {
			return Step{}, err
		}
		rest = rest[1:]
	}
	s.Text = strings.TrimSpace(strings.Join(rest, "\n"))
	if utf8.RuneCountInString(s.Text) > MaxText {
		return Step{}, fmt.Errorf("the text is longer than %d characters", MaxText)
	}
	return s, s.check()
}

func (s *Step) set(key, value string) error {
	switch key {
	case "type":
		kind := Kind(strings.ToLower(value))
		if !slices.Contains([]Kind{KindAgent, KindScript, KindReview, KindGate, KindWorkflow}, kind) {
			return fmt.Errorf("type %q is not agent, script, review, gate or workflow", value)
		}
		s.Kind = kind
	case "agent":
		if !agentdefs.ValidName(value) {
			return fmt.Errorf("agent %q is not an agent's name (lowercase letters, digits and dashes)", value)
		}
		s.Agent = value
	case "run":
		if value == "" || utf8.RuneCountInString(value) > MaxRunName {
			return fmt.Errorf("run needs the name of a run configuration (at most %d characters)", MaxRunName)
		}
		s.Run = value
	case "on-fail":
		if s.OnFail = agenttask.Slug(value); value == "" {
			return errors.New("on-fail needs the title of an earlier step")
		}
	case "workflow":
		if !ValidName(value) {
			return fmt.Errorf("workflow %q is not a workflow's name", value)
		}
		s.Workflow = value
	}
	return nil
}

// check refuses keys that mean nothing for the step's kind, so a typo in
// "type" does not silently turn a test run into a prompt.
func (s Step) check() error {
	deny := func(set bool, key string) error {
		if set {
			return fmt.Errorf("a %s step has no %s", s.Kind, key)
		}
		return nil
	}
	var errs []error
	switch s.Kind {
	case KindAgent:
		if s.Text == "" {
			errs = append(errs, errors.New("an agent step needs text: what the agent should do"))
		}
		errs = append(errs, deny(s.Run != "", "run"), deny(s.Workflow != "", "workflow"), deny(s.OnFail != "", "on-fail"))
	case KindScript:
		if s.Run == "" {
			errs = append(errs, errors.New("a script step needs run: the name of a run configuration"))
		}
		errs = append(errs, deny(s.Agent != "", "agent"), deny(s.Workflow != "", "workflow"))
	case KindReview:
		errs = append(errs, deny(s.Run != "", "run"), deny(s.Workflow != "", "workflow"))
	case KindGate:
		errs = append(errs, deny(s.Agent != "", "agent"), deny(s.Run != "", "run"), deny(s.Workflow != "", "workflow"), deny(s.OnFail != "", "on-fail"))
	case KindWorkflow:
		if s.Workflow == "" {
			errs = append(errs, errors.New("a workflow step needs workflow: the name of a workflow"))
		}
		errs = append(errs, deny(s.Agent != "", "agent"), deny(s.Run != "", "run"), deny(s.OnFail != "", "on-fail"), deny(s.Text != "", "text"))
	}
	return errors.Join(errs...)
}

// checkOnFail: on-fail names an earlier agent step.
func checkOnFail(steps []Step) error {
	for i, s := range steps {
		if s.OnFail == "" {
			continue
		}
		j := slices.IndexFunc(steps, func(t Step) bool { return t.ID == s.OnFail })
		switch {
		case j < 0:
			return fmt.Errorf("step %q: on-fail names no step of this workflow", s.Title)
		case j >= i:
			return fmt.Errorf("step %q: on-fail must name an earlier step", s.Title)
		case steps[j].Kind != KindAgent:
			return fmt.Errorf("step %q: on-fail must name an agent step, %q is a %s step", s.Title, steps[j].Title, steps[j].Kind)
		}
	}
	return nil
}

// Resolve returns w's steps with the steps of the workflows it names put in
// their place (ids prefixed with the naming step's id, titles with its
// title), as a run takes them. find looks a workflow up by name.
func Resolve(w Workflow, find func(name string) (Workflow, bool)) ([]Step, error) {
	steps, err := resolve(w, find, []string{w.Name})
	if err != nil {
		return nil, err
	}
	if len(steps) > MaxSteps {
		return nil, fmt.Errorf("more than %d steps once the workflows it names are put in", MaxSteps)
	}
	if steps[0].Kind != KindAgent {
		return nil, fmt.Errorf("the first step must be an agent step, %q is a %s step", steps[0].Title, steps[0].Kind)
	}
	seen := map[string]bool{}
	for _, s := range steps {
		if seen[s.ID] {
			return nil, fmt.Errorf("two steps are named %q once the workflows it names are put in", s.ID)
		}
		seen[s.ID] = true
	}
	return steps, nil
}

func resolve(w Workflow, find func(string) (Workflow, bool), path []string) ([]Step, error) {
	var out []Step
	for _, s := range w.Steps {
		if s.Kind != KindWorkflow {
			out = append(out, s)
			continue
		}
		if slices.Contains(path, s.Workflow) {
			return nil, fmt.Errorf("step %q: a cycle: %s → %s", s.Title, strings.Join(path, " → "), s.Workflow)
		}
		if len(path) >= MaxDepth {
			return nil, fmt.Errorf("step %q: workflows are nested more than %d deep", s.Title, MaxDepth)
		}
		sub, ok := find(s.Workflow)
		if !ok {
			return nil, fmt.Errorf("step %q: no workflow named %q", s.Title, s.Workflow)
		}
		inner, err := resolve(sub, find, append(slices.Clone(path), s.Workflow))
		if err != nil {
			return nil, err
		}
		for _, in := range inner {
			in.ID = s.ID + "-" + in.ID
			in.Title = s.Title + " ▸ " + in.Title
			if in.OnFail != "" {
				in.OnFail = s.ID + "-" + in.OnFail
			}
			out = append(out, in)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("workflow %q has no steps", w.Name)
	}
	return out, nil
}
