package agent

import (
	"strings"
	"testing"
)

func TestInlineEditPrompt(t *testing.T) {
	r := EditRequest{
		Path:        "internal/x/x.go",
		Language:    "go",
		Instruction: "add error handling",
		Before:      "package x\n\nfunc f() {\n",
		Selection:   "\tdo()\n",
		After:       "}\n",
	}
	prompt, input, err := InlineEditPrompt(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"add error handling", "replacement", "no code fences"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt misses %q:\n%s", want, prompt)
		}
	}
	for _, want := range []string{"internal/x/x.go", "go", "package x", selectionStart + "\n\tdo()\n" + selectionEnd, "}\n"} {
		if !strings.Contains(input, want) {
			t.Errorf("input misses %q:\n%s", want, input)
		}
	}
	if strings.Index(input, "package x") > strings.Index(input, selectionStart) {
		t.Error("context before the selection comes after it")
	}
}

func TestInlineEditPromptBounds(t *testing.T) {
	ok := EditRequest{Path: "a.go", Instruction: "x", Selection: "y"}
	tests := []struct {
		name   string
		modify func(*EditRequest)
		want   string
	}{
		{"empty instruction", func(r *EditRequest) { r.Instruction = "  " }, "instruction"},
		{"long instruction", func(r *EditRequest) { r.Instruction = strings.Repeat("a", MaxEditInstruction+1) }, "instruction"},
		{"large selection", func(r *EditRequest) { r.Selection = strings.Repeat("a", MaxEditSelection+1) }, "selection"},
		{"large context", func(r *EditRequest) { r.Before = strings.Repeat("a", MaxEditContext+1) }, "context"},
		{"long language", func(r *EditRequest) { r.Language = strings.Repeat("a", 65) }, "language"},
		{"secret in selection", func(r *EditRequest) { r.Selection = `token = "ghp_abcdefghijklmnopqrstuvwxyz0123"` }, "credential"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := ok
			tt.modify(&r)
			if _, _, err := InlineEditPrompt(r); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want containing %q", err, tt.want)
			}
		})
	}
	// An empty selection asks for code to insert.
	if _, _, err := InlineEditPrompt(EditRequest{Path: "a.go", Instruction: "write a loop"}); err != nil {
		t.Errorf("empty selection refused: %v", err)
	}
}

func TestInlineEditPromptRedactsContext(t *testing.T) {
	_, input, err := InlineEditPrompt(EditRequest{
		Path: "a.go", Instruction: "x", Selection: "y",
		Before: `key := "sk-abcdefghijklmnopqrstuvwxyz"` + "\n",
		After:  "password = hunter22\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(input, "sk-abcdefghijklmnopqrstuvwxyz") || strings.Contains(input, "hunter22") {
		t.Errorf("secret left in the context:\n%s", input)
	}
}

func TestCleanReplacement(t *testing.T) {
	tests := []struct {
		name, answer, selection, want string
	}{
		{"plain", "\treturn nil\n", "x", "\treturn nil"},
		{"keeps leading indentation", "\n\n    if err != nil {\n        return err\n    }\n\n", "x", "    if err != nil {\n        return err\n    }"},
		{"fenced", "```go\nreturn nil\n```", "x", "return nil"},
		{"fenced no language", "```\na\nb\n```\n", "x", "a\nb"},
		{"tilde fence", "~~~python\nprint(1)\n~~~", "x", "print(1)"},
		{"preamble and fence", "Here is the updated code:\n\n```ts\nconst a = 1\n```\n\nThis adds a constant.", "x", "const a = 1"},
		{"unclosed fence", "```go\nreturn nil", "x", "return nil"},
		{"longer fence around inner fence", "````md\n```go\nx\n```\n````", "x", "```go\nx\n```"},
		{"markdown selection keeps inner fences", "Intro\n\n```go\nx\n```", "Intro\n\n```sh\ny\n```", "Intro\n\n```go\nx\n```"},
		{"markers echoed", selectionStart + "\nfoo()\n" + selectionEnd, "x", "foo()"},
		{"crlf", "```go\r\na\r\nb\r\n```\r\n", "x", "a\nb"},
		{"empty", "  \n", "x", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CleanReplacement(tt.answer, tt.selection); got != tt.want {
				t.Errorf("CleanReplacement(%q) = %q, want %q", tt.answer, got, tt.want)
			}
		})
	}
}
