package review

import (
	"strings"
	"testing"
)

var files = map[string][]string{
	"main.go":     {"package main", "", "func main() {", "\tx := 1", "}"},
	"pkg/util.go": {"package pkg", "func A() {}"},
	"unknown.go":  nil, // content not available: any line is accepted
}

func TestParsePlainJSON(t *testing.T) {
	answer := `{"findings":[{"file":"main.go","line":4,"severity":"error","title":"Unused variable","explanation":"x is never used.","suggestion":"\t_ = 1"}]}`
	p, err := Parse(answer, files)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Findings) != 1 {
		t.Fatalf("findings = %+v", p.Findings)
	}
	f := p.Findings[0]
	if f.Path != "main.go" || f.Line != 4 || f.EndLine != 4 || f.Severity != "error" || f.Title != "Unused variable" ||
		f.Explanation != "x is never used." || f.Suggestion != "\t_ = 1" {
		t.Errorf("finding = %+v", f)
	}
	if f.LineText != "\tx := 1" || f.Before != "func main() {" || f.After != "}" {
		t.Errorf("anchor = %q %q %q", f.Before, f.LineText, f.After)
	}
	if f.ID == "" || p.Partial || p.Dropped != 0 {
		t.Errorf("parsed = %+v", p)
	}
}

func TestParseFencedArrayWithPreamble(t *testing.T) {
	answer := "Here is my review {of sorts}:\n\n```json\n[\n  {\"path\": \"./pkg/util.go\", \"line\": \"2\", \"severity\": \"nit\", \"message\": \"Missing doc comment.\"}\n]\n```\nThanks!"
	p, err := Parse(answer, files)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Findings) != 1 {
		t.Fatalf("findings = %+v", p.Findings)
	}
	f := p.Findings[0]
	if f.Path != "pkg/util.go" || f.Line != 2 || f.Severity != "info" || f.Title != "Missing doc comment." || f.Explanation != "Missing doc comment." {
		t.Errorf("finding = %+v", f)
	}
}

func TestParseTruncatedAnswerKeepsCompleteFindings(t *testing.T) {
	answer := `{"findings": [
	  {"file": "main.go", "line": 3, "severity": "warning", "title": "A", "explanation": "a"},
	  {"file": "main.go", "line": 5, "severity": "high", "title": "B", "explanation": "b"},
	  {"file": "main.go", "line": 1, "severity": "low", "title": "C", "expl`
	p, err := Parse(answer, files)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Partial || len(p.Findings) != 2 || p.Findings[1].Severity != "error" {
		t.Errorf("parsed = %+v", p)
	}
}

func TestParseNoFindings(t *testing.T) {
	for _, answer := range []string{`{"findings": []}`, "```json\n[]\n```", `{"findings": null}`} {
		p, err := Parse(answer, files)
		if err != nil || len(p.Findings) != 0 {
			t.Errorf("%q: %+v %v", answer, p, err)
		}
	}
}

func TestParseRejectsAnswersWithoutJSON(t *testing.T) {
	for _, answer := range []string{"", "Looks good to me!", "{not json at all", `{"verdict": "fine"}`} {
		if _, err := Parse(answer, files); err == nil {
			t.Errorf("%q: no error", answer)
		}
	}
}

func TestParseValidatesFindings(t *testing.T) {
	answer := `[
	  {"file": "other.go", "line": 1, "title": "outside the review"},
	  {"file": "main.go", "line": 99, "title": "past the end"},
	  {"file": "main.go", "line": 2, "endLine": 1, "title": "end before start", "severity": "bogus"},
	  {"file": "main.go", "line": 3},
	  {"file": "/abs/repo/pkg/util.go", "line": 1, "title": "absolute path"},
	  {"file": "unknown.go", "line": 400, "title": "unknown content"},
	  "not an object",
	  {"file": "main.go", "line": 2, "title": "control\u0007 chars\u001b[31m"}
	]`
	p, err := Parse(answer, files)
	if err != nil {
		t.Fatal(err)
	}
	byTitle := map[string]Finding{}
	for _, f := range p.Findings {
		byTitle[f.Title] = f
	}
	if _, ok := byTitle["outside the review"]; ok {
		t.Error("finding outside the reviewed files kept")
	}
	if f := byTitle["past the end"]; f.Line != 0 {
		t.Errorf("past the end: %+v", f)
	}
	if f := byTitle["end before start"]; f.Line != 2 || f.EndLine != 2 || f.Severity != "warning" {
		t.Errorf("end before start: %+v", f)
	}
	if f := byTitle["absolute path"]; f.Path != "pkg/util.go" {
		t.Errorf("absolute path: %+v", f)
	}
	if f := byTitle["unknown content"]; f.Line != 400 || f.LineText != "" {
		t.Errorf("unknown content: %+v", f)
	}
	if _, ok := byTitle["control chars[31m"]; !ok {
		t.Errorf("control characters not stripped: %v", p.Findings)
	}
	if p.Dropped != 3 { // outside, empty, not an object
		t.Errorf("dropped = %d", p.Dropped)
	}
}

func TestParseCapsSizes(t *testing.T) {
	long := strings.Repeat("x", 50000)
	var b strings.Builder
	b.WriteString(`[`)
	for i := range MaxFindings + 10 {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"file":"main.go","line":1,"title":"` + long + `","explanation":"` + long + `","suggestion":"` + long + `"}`)
	}
	b.WriteString(`]`)
	p, err := Parse(b.String(), files)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Findings) != MaxFindings || p.Dropped != 10 {
		t.Errorf("%d findings, %d dropped", len(p.Findings), p.Dropped)
	}
	f := p.Findings[0]
	if len(f.Title) > maxTitle+4 || len(f.Explanation) > maxExplanation+4 || len(f.Suggestion) > maxSuggestion+4 {
		t.Errorf("sizes %d %d %d", len(f.Title), len(f.Explanation), len(f.Suggestion))
	}
	seen := map[string]bool{}
	for _, f := range p.Findings {
		if seen[f.ID] {
			t.Fatalf("duplicate id %s", f.ID)
		}
		seen[f.ID] = true
	}
}

// An agent may quote a credential it read (a planted instruction can ask it
// to): findings carry no secret values, and no bidi or zero-width tricks.
func TestParseRedactsAndStripsFormatCharacters(t *testing.T) {
	answer := `[{"file": "main.go", "line": 1, "title": "Leaked ‮key‬ AKIAIOSFODNN7EXAMPLE",
	  "explanation": "The .env has DB_PASSWORD=hunter2 and ghp_0123456789abcdefghijklmnopqrstuvwxyzAB​.",
	  "suggestion": "\tpassword := \"hunter2-secret\""}]`
	p, err := Parse(answer, files)
	if err != nil || len(p.Findings) != 1 {
		t.Fatalf("%+v %v", p, err)
	}
	f := p.Findings[0]
	all := f.Title + f.Explanation + f.Suggestion
	for _, leak := range []string{"AKIAIOSFODNN7EXAMPLE", "hunter2", "ghp_0123", "‮", "‬", "​"} {
		if strings.Contains(all, leak) {
			t.Errorf("finding keeps %q: %+v", leak, f)
		}
	}
	if !strings.HasPrefix(f.Suggestion, "\tpassword := ") {
		t.Errorf("suggestion lost its indentation: %q", f.Suggestion)
	}
}

func TestPlace(t *testing.T) {
	f := Place(Finding{Line: 4, EndLine: 9}, files["main.go"])
	if f.Line != 4 || f.EndLine != 5 || f.LineText != "\tx := 1" || f.Before != "func main() {" || f.After != "}" {
		t.Errorf("Place = %+v", f)
	}
	if f := Place(Finding{Line: 6, LineText: "stale"}, files["main.go"]); f.Line != 0 || f.LineText != "" {
		t.Errorf("past the end = %+v", f)
	}
	if f := Place(Finding{Line: 40}, nil); f.Line != 40 || f.EndLine != 40 {
		t.Errorf("unknown lines = %+v", f)
	}
}

func TestPromptAsksForJSON(t *testing.T) {
	p := Prompt()
	for _, want := range []string{`"findings"`, "severity", "line", "Do not change any files"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}
}
