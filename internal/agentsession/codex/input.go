package codex

import (
	"fmt"
	"strings"

	"flint/internal/agentsession"
)

// buildInput turns the prompt and its attachments into UserInput text items.
func buildInput(text string, atts []agentsession.Attachment) []userInput {
	items := []userInput{textInput(text)}
	for _, a := range atts {
		if a.Path == "" && a.Text == "" {
			continue
		}
		items = append(items, textInput(attachmentText(a)))
	}
	return items
}

func textInput(text string) userInput {
	return userInput{Type: "text", Text: text, TextElements: []any{}}
}

// attachmentText renders an attachment as "path:start-end" plus a fenced
// block, or a pointer the agent can follow itself.
func attachmentText(a agentsession.Attachment) string {
	if a.Text == "" {
		return "Relevant file: " + a.Path
	}
	header := a.Path
	if a.StartLine > 0 {
		header += fmt.Sprintf(":%d-%d", a.StartLine, max(a.EndLine, a.StartLine))
	}
	fence := fenceFor(a.Text)
	body := a.Text
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	return header + "\n" + fence + "\n" + body + fence
}

// fenceFor returns a backtick fence longer than any backtick run in text.
func fenceFor(text string) string {
	longest, run := 0, 0
	for _, r := range text {
		if r == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	return strings.Repeat("`", max(3, longest+1))
}
