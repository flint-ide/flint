package prompts

import (
	"slices"
	"testing"
)

func TestExpand(t *testing.T) {
	body := "Review {{ file }} on {{branch}}.\n\n{{selection}}\n\n\n\n{{diff}}\nTicket {{ticket}}, {{unknown}}."
	got := Expand(body, map[string]string{
		"file":      "a/b.go",
		"branch":    "main",
		"selection": "",
		"diff":      "{{ticket}} stays literal",
		"ticket":    "FL-1",
	})
	want := "Review a/b.go on main.\n\n{{ticket}} stays literal\nTicket FL-1, {{unknown}}."
	if got != want {
		t.Fatalf("got\n%q\nwant\n%q", got, want)
	}
}

func TestPlaceholders(t *testing.T) {
	got := Placeholders("{{diff}} {{ file }} {{diff}} {{bad name}} {{x-y}}")
	if !slices.Equal(got, []string{"diff", "file", "x-y"}) {
		t.Fatalf("got %v", got)
	}
}

func TestFenced(t *testing.T) {
	if got := Fenced("a ``` b", "diff"); got != "````diff\na ``` b\n````" {
		t.Fatalf("got %q", got)
	}
	if got := Fenced("", ""); got != "" {
		t.Fatalf("empty text fenced: %q", got)
	}
}
