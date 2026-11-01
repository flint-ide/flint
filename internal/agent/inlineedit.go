package agent

import (
	"errors"
	"fmt"
	"strings"

	"flint/internal/redact"
)

// Limits of an inline edit request. They protect the agent run (prompt size,
// cost) and are not settings: the editor sends bounded context on its own
// (settings: inlineEdit.contextLines) and these only refuse what no editor sends.
const (
	MaxEditInstruction = 2000
	MaxEditSelection   = 64 << 10
	MaxEditContext     = 32 << 10 // each side of the selection
	maxEditLanguage    = 64
	maxEditPath        = 1024
)

// Markers around the selection in the context sent to the agent.
const (
	selectionStart = "<<<SELECTION>>>"
	selectionEnd   = "<<<END SELECTION>>>"
)

// EditRequest is one inline edit: rewrite Selection of the file at Path
// (relative to the project) as Instruction says. Before and After are the
// lines around it, already bounded by the editor.
type EditRequest struct {
	Path        string `json:"path"`
	Language    string `json:"language"`
	Instruction string `json:"instruction"`
	Before      string `json:"before"`
	Selection   string `json:"selection"`
	After       string `json:"after"`
}

// InlineEditPrompt checks r's sizes and returns the prompt and the input (the
// file context with the selection marked) of a headless run. The context
// around the selection is redacted; a selection that contains a credential is
// refused, since its replacement would be written back into the file.
func InlineEditPrompt(r EditRequest) (prompt, input string, err error) {
	instruction := strings.TrimSpace(r.Instruction)
	switch {
	case instruction == "":
		return "", "", errors.New("the instruction is empty")
	case len(instruction) > MaxEditInstruction:
		return "", "", fmt.Errorf("the instruction is longer than %d characters", MaxEditInstruction)
	case len(r.Selection) > MaxEditSelection:
		return "", "", fmt.Errorf("the selection is larger than %d KB", MaxEditSelection>>10)
	case len(r.Before) > MaxEditContext || len(r.After) > MaxEditContext:
		return "", "", fmt.Errorf("the surrounding context is larger than %d KB", MaxEditContext>>10)
	case len(r.Language) > maxEditLanguage || strings.ContainsAny(r.Language, "\r\n"):
		return "", "", errors.New("invalid language")
	case len(r.Path) > maxEditPath || strings.ContainsAny(r.Path, "\r\n"):
		return "", "", errors.New("invalid path")
	}
	if redact.Secrets(r.Selection) != r.Selection {
		return "", "", errors.New("the selection contains what looks like a credential; Flint does not send it to the agent")
	}

	var p strings.Builder
	p.WriteString("You are editing code inside an editor. The file context is on stdin (or below); ")
	fmt.Fprintf(&p, "the part to change is between the lines %s and %s. ", selectionStart, selectionEnd)
	p.WriteString("Rewrite that part as the instruction says and reply with its replacement text only: ")
	p.WriteString("no explanations, no code fences, no markers, nothing from outside the marked part. ")
	p.WriteString("Keep the surrounding indentation and the file's style. If the marked part is empty, reply with the code to insert there. ")
	p.WriteString("Do not change any files and do not run commands.\n\nInstruction: ")
	p.WriteString(instruction)

	language := r.Language
	if language == "" {
		language = "plain text"
	}
	var in strings.Builder
	fmt.Fprintf(&in, "File: %s\nLanguage: %s\n\n", r.Path, language)
	in.WriteString(redact.String(r.Before))
	if r.Before != "" && !strings.HasSuffix(r.Before, "\n") {
		in.WriteString("\n")
	}
	in.WriteString(selectionStart + "\n")
	in.WriteString(r.Selection)
	if r.Selection != "" && !strings.HasSuffix(r.Selection, "\n") {
		in.WriteString("\n")
	}
	in.WriteString(selectionEnd + "\n")
	in.WriteString(redact.String(r.After))
	return p.String(), in.String(), nil
}

// CleanReplacement takes the replacement text out of an agent's answer: it
// unwraps a code fence around the answer (or the first fenced block after a
// preamble, unless the selection itself holds fences, as in Markdown), drops
// echoed selection markers and blank lines at both ends, and keeps the
// indentation of the first line. Line breaks come back as \n.
func CleanReplacement(answer, selection string) string {
	lines := strings.Split(strings.ReplaceAll(answer, "\r\n", "\n"), "\n")
	lines = trimBlank(lines)
	if len(lines) > 0 && isFence(lines[0]) {
		lines = unfence(lines)
	} else if !strings.Contains(selection, "```") && !strings.Contains(selection, "~~~") {
		if i := firstFence(lines); i >= 0 {
			lines = unfence(lines[i:])
		}
	}
	lines = trimBlank(lines)
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == selectionStart {
		lines = lines[1:]
	}
	if n := len(lines); n > 0 && strings.TrimSpace(lines[n-1]) == selectionEnd {
		lines = lines[:n-1]
	}
	return strings.Join(trimBlank(lines), "\n")
}

// unfence returns the lines inside the fence that opens lines: up to the
// closing fence of the same kind and at least the same length, or the end.
func unfence(lines []string) []string {
	open := fenceRun(lines[0])
	for i := 1; i < len(lines); i++ {
		s := strings.TrimSpace(lines[i])
		if s == strings.Repeat(open[:1], len(s)) && len(s) >= len(open) {
			return lines[1:i]
		}
	}
	return lines[1:]
}

func firstFence(lines []string) int {
	for i, l := range lines {
		if isFence(l) {
			return i
		}
	}
	return -1
}

func isFence(line string) bool { return fenceRun(line) != "" }

// fenceRun is the run of ``` or ~~~ (3 or more) that starts line, or "".
func fenceRun(line string) string {
	s := strings.TrimLeft(line, " ")
	if len(line)-len(s) > 3 || len(s) < 3 {
		return ""
	}
	c := s[0]
	if c != '`' && c != '~' {
		return ""
	}
	n := 0
	for n < len(s) && s[n] == c {
		n++
	}
	if n < 3 {
		return ""
	}
	return s[:n]
}

func trimBlank(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
