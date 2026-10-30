package agent

import (
	"fmt"
	"strings"
)

// CommitMessagePrompt asks for a message that follows the repository's conventions,
// shown by its recent subjects.
func CommitMessagePrompt(recentSubjects []string) string {
	var b strings.Builder
	b.WriteString("Write a git commit message for the diff on stdin. ")
	b.WriteString("Reply with the commit message only: no preamble, no code fences, no quotes. ")
	b.WriteString("Use a concise subject line (max ~72 chars), then a blank line and a short body ")
	b.WriteString("explaining why, only if the change is not obvious. Do not invent changes that are not in the diff.")
	if len(recentSubjects) > 0 {
		b.WriteString("\n\nMatch the style and language of this repository's recent commit subjects:\n")
		for _, s := range recentSubjects {
			fmt.Fprintf(&b, "- %s\n", s)
		}
	}
	return b.String()
}

// ConflictPrompt asks an interactive agent to resolve conflicts but leave committing
// (and the final say) to the user.
func ConflictPrompt(operation string, files []string) string {
	if operation == "" {
		operation = "merging"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Git is in the middle of %s and these files have conflicts:\n", operation)
	for _, f := range files {
		fmt.Fprintf(&b, "- %s\n", f)
	}
	b.WriteString("\nResolve them: understand what both sides intended (use git log / git diff on each side), ")
	b.WriteString("keep both intents where possible, remove all conflict markers, and run the project's build or tests if they exist. ")
	b.WriteString("Then stage the resolved files with git add. Do NOT commit and do NOT continue the ")
	b.WriteString(operation)
	b.WriteString(" — I will review and do that. If a conflict is ambiguous, ask me instead of guessing.")
	return b.String()
}
