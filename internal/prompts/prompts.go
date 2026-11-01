// Package prompts is the prompt library: Markdown files whose text fills the
// agent chat draft when the user picks them with "/". Prompts come from three
// places, later ones overriding earlier ones with the same name: the built-in
// starters, the user's own folder in Flint's config directory, and the
// project's .flint/prompts folder (shared with the team through the repo).
//
// A prompt is only ever text for the draft: nothing here runs a command, and
// nothing is sent to an agent without the user pressing Send.
package prompts

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Source says where a prompt comes from.
type Source string

const (
	SourceBuiltin  Source = "builtin"
	SourcePersonal Source = "personal"
	SourceProject  Source = "project"
)

// Prompt is one prompt of the library.
type Prompt struct {
	// Name is what the user types after "/" (the file name without .md).
	Name string `json:"name"`
	// Description is the one line the picker shows next to the name.
	Description string `json:"description"`
	// Agent, when set, limits the prompt to chats with that agent.
	Agent string `json:"agent"`
	// Args are named placeholders the user fills in when choosing the prompt.
	Args []string `json:"args"`
	// Body is the prompt text with its {{placeholders}}.
	Body   string `json:"body"`
	Source Source `json:"source"`
	// Path is the Markdown file ("" for a built-in prompt).
	Path string `json:"path"`
	// Untrusted marks a project prompt from a folder in Restricted Mode.
	Untrusted bool `json:"untrusted"`
}

// Limits on what a prompt file may hold. They protect the picker and the
// draft from a hostile repository, so they are not settings.
const (
	MaxFileSize    = 64 << 10
	MaxPrompts     = 200
	maxName        = 64
	maxDescription = 200
	maxArgs        = 8
	maxArgName     = 32
	maxAgentName   = 64
)

// Reserved are the placeholders Flint fills itself; an arg may not use them.
var Reserved = []string{"selection", "file", "diff", "branch", "problems"}

var (
	namePattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)
	argPattern   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)
	agentPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
)

// ValidName reports whether name can name a prompt: letters, digits, '-' and
// '_', starting with a letter or digit.
func ValidName(name string) bool {
	return len(name) <= maxName && namePattern.MatchString(name)
}

// Parse reads a prompt file: optional front matter (simple "key: value"
// lines between two "---" lines; '#' starts a comment line) and the body.
// Keys: description, agent, args (comma-separated, optionally in [ ]).
// Unknown keys are ignored, so newer files still load.
func Parse(name string, data []byte) (Prompt, error) {
	if !ValidName(name) {
		return Prompt{}, fmt.Errorf("name %q: use letters, digits, - and _ (at most %d)", name, maxName)
	}
	if !utf8.Valid(data) {
		return Prompt{}, errors.New("not UTF-8 text")
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	p := Prompt{Name: name, Args: []string{}}
	if rest, ok := strings.CutPrefix(text, "---\n"); ok {
		header, body, found := cutFrontMatter(rest)
		if !found {
			return Prompt{}, errors.New("front matter has no closing ---")
		}
		if err := p.applyHeader(header); err != nil {
			return Prompt{}, err
		}
		text = body
	}
	p.Body = strings.TrimSpace(text)
	if p.Body == "" {
		return Prompt{}, errors.New("the prompt text is empty")
	}
	return p, nil
}

// cutFrontMatter splits what follows the opening "---" at the closing one.
func cutFrontMatter(s string) (header, body string, ok bool) {
	if rest, found := strings.CutPrefix(s, "---\n"); found || s == "---" {
		return "", rest, true
	}
	header, body, ok = strings.Cut(s, "\n---\n")
	if !ok && strings.HasSuffix(s, "\n---") {
		return strings.TrimSuffix(s, "\n---"), "", true
	}
	return header, body, ok
}

func (p *Prompt) applyHeader(header string) error {
	for line := range strings.SplitSeq(header, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			return fmt.Errorf("front matter line %q is not key: value", line)
		}
		value = unquote(strings.TrimSpace(value))
		if strings.ContainsFunc(value, unicode.IsControl) {
			return fmt.Errorf("%s contains control characters", strings.TrimSpace(key))
		}
		var err error
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "description":
			err = p.setDescription(value)
		case "agent":
			err = p.setAgent(value)
		case "args":
			err = p.setArgs(value)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (p *Prompt) setDescription(v string) error {
	if utf8.RuneCountInString(v) > maxDescription {
		return fmt.Errorf("description longer than %d characters", maxDescription)
	}
	p.Description = v
	return nil
}

func (p *Prompt) setAgent(v string) error {
	if v != "" && (len(v) > maxAgentName || !agentPattern.MatchString(v)) {
		return fmt.Errorf("agent %q is not an agent name", v)
	}
	p.Agent = v
	return nil
}

func (p *Prompt) setArgs(v string) error {
	v = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(v, "["), "]"))
	args := []string{}
	for a := range strings.SplitSeq(v, ",") {
		a = unquote(strings.TrimSpace(a))
		switch {
		case a == "":
			continue
		case len(a) > maxArgName || !argPattern.MatchString(a):
			return fmt.Errorf("arg %q: use letters, digits, - and _", a)
		case slices.Contains(Reserved, strings.ToLower(a)):
			return fmt.Errorf("arg %q is filled by Flint; pick another name", a)
		case slices.Contains(args, a):
			return fmt.Errorf("arg %q is listed twice", a)
		}
		args = append(args, a)
	}
	if len(args) > maxArgs {
		return fmt.Errorf("more than %d args", maxArgs)
	}
	p.Args = args
	return nil
}

func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}

// key is how names compare: "Review" overrides "review".
func key(name string) string { return strings.ToLower(name) }

// Merge combines the three sources: a personal prompt overrides a built-in
// one with the same name, and a project prompt overrides both, unless the
// project is untrusted. An untrusted repository must not replace a prompt the
// user relies on, so its prompts only add new names (marked Untrusted), and
// the names it tried to take are returned as shadowed. The result is sorted
// by name.
func Merge(builtin, personal, project []Prompt, trusted bool) (list []Prompt, shadowed []string) {
	byName := map[string]Prompt{}
	for _, p := range builtin {
		byName[key(p.Name)] = p
	}
	for _, p := range personal {
		byName[key(p.Name)] = p
	}
	for _, p := range project {
		if !trusted {
			if _, taken := byName[key(p.Name)]; taken {
				shadowed = append(shadowed, p.Name)
				continue
			}
			p.Untrusted = true
		}
		byName[key(p.Name)] = p
	}
	list = make([]Prompt, 0, len(byName))
	for _, p := range byName {
		list = append(list, p)
	}
	slices.SortFunc(list, func(a, b Prompt) int { return strings.Compare(key(a.Name), key(b.Name)) })
	return list, shadowed
}
