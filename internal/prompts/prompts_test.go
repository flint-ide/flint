package prompts

import (
	"slices"
	"strings"
	"testing"
)

func TestParseFrontMatter(t *testing.T) {
	src := "---\r\ndescription: \"Fix the issue\"\nagent: claude\n# a comment\nargs: [ticket, scope]\nunknown: kept out\n---\nFix {{ticket}} in {{scope}}.\n"
	p, err := Parse("fix-issue", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "fix-issue" || p.Description != "Fix the issue" || p.Agent != "claude" {
		t.Fatalf("parsed %+v", p)
	}
	if !slices.Equal(p.Args, []string{"ticket", "scope"}) {
		t.Fatalf("args %v", p.Args)
	}
	if p.Body != "Fix {{ticket}} in {{scope}}." {
		t.Fatalf("body %q", p.Body)
	}
}

func TestParseWithoutFrontMatter(t *testing.T) {
	p, err := Parse("explain", []byte("Explain {{file}}.\n\n"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Description != "" || len(p.Args) != 0 || p.Body != "Explain {{file}}." {
		t.Fatalf("parsed %+v", p)
	}
}

func TestParseRejects(t *testing.T) {
	cases := map[string]struct{ name, src string }{
		"unclosed front matter": {"a", "---\ndescription: x\nbody"},
		"line without colon":    {"a", "---\njust words\n---\nbody"},
		"bad name":              {"../evil", "body"},
		"name with space":       {"two words", "body"},
		"empty body":            {"a", "---\ndescription: x\n---\n  \n"},
		"reserved arg":          {"a", "---\nargs: diff\n---\nbody"},
		"invalid arg":           {"a", "---\nargs: no spaces allowed\n---\nbody"},
		"duplicate arg":         {"a", "---\nargs: x, x\n---\nbody"},
		"too many args":         {"a", "---\nargs: a,b,c,d,e,f,g,h,i\n---\nbody"},
		"invalid agent":         {"a", "---\nagent: rm -rf\n---\nbody"},
		"long description":      {"a", "---\ndescription: " + strings.Repeat("x", maxDescription+1) + "\n---\nbody"},
		"control in descr":      {"a", "---\ndescription: a\x1bb\n---\nbody"},
		"invalid utf8":          {"a", "\xff\xfe"},
	}
	for label, c := range cases {
		if _, err := Parse(c.name, []byte(c.src)); err == nil {
			t.Errorf("%s: parsed without an error", label)
		}
	}
}

func TestValidName(t *testing.T) {
	for _, ok := range []string{"review", "fix-issue", "Test_2", "a"} {
		if !ValidName(ok) {
			t.Errorf("%q rejected", ok)
		}
	}
	for _, bad := range []string{"", "-x", "_x", "a.b", "a/b", "a b", strings.Repeat("a", maxName+1)} {
		if ValidName(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestMergeOverridesByName(t *testing.T) {
	builtin := []Prompt{{Name: "review", Source: SourceBuiltin}, {Name: "explain", Source: SourceBuiltin}}
	personal := []Prompt{{Name: "Review", Source: SourcePersonal}, {Name: "mine", Source: SourcePersonal}}
	project := []Prompt{{Name: "review", Source: SourceProject}, {Name: "team", Source: SourceProject}}
	got, shadowed := Merge(builtin, personal, project, true)
	if len(shadowed) != 0 {
		t.Fatalf("shadowed %v", shadowed)
	}
	want := map[string]Source{"explain": SourceBuiltin, "mine": SourcePersonal, "review": SourceProject, "team": SourceProject}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for _, p := range got {
		if want[strings.ToLower(p.Name)] != p.Source {
			t.Errorf("%s from %s", p.Name, p.Source)
		}
	}
	if !slices.IsSortedFunc(got, func(a, b Prompt) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) }) {
		t.Errorf("not sorted: %+v", got)
	}
}

func TestMergeUntrustedProjectNeverOverrides(t *testing.T) {
	builtin := []Prompt{{Name: "review", Source: SourceBuiltin}}
	personal := []Prompt{{Name: "mine", Source: SourcePersonal}}
	project := []Prompt{{Name: "review", Source: SourceProject}, {Name: "MINE", Source: SourceProject}, {Name: "team", Source: SourceProject}}
	got, shadowed := Merge(builtin, personal, project, false)
	if !slices.Equal(shadowed, []string{"review", "MINE"}) {
		t.Fatalf("shadowed %v", shadowed)
	}
	for _, p := range got {
		switch strings.ToLower(p.Name) {
		case "review":
			if p.Source != SourceBuiltin {
				t.Errorf("review from %s", p.Source)
			}
		case "mine":
			if p.Source != SourcePersonal {
				t.Errorf("mine from %s", p.Source)
			}
		case "team":
			if !p.Untrusted {
				t.Error("an untrusted project prompt is not marked")
			}
		}
	}
}

func TestBuiltinsParse(t *testing.T) {
	list, problems := Builtins()
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	names := []string{}
	for _, p := range list {
		names = append(names, p.Name)
		if p.Source != SourceBuiltin || p.Description == "" {
			t.Errorf("%s: %+v", p.Name, p)
		}
	}
	for _, want := range []string{"review", "test", "explain", "fix"} {
		if !slices.Contains(names, want) {
			t.Errorf("missing built-in %s (have %v)", want, names)
		}
	}
}
