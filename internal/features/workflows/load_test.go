package workflows

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestBuiltinsParseAndResolve(t *testing.T) {
	list, problems := Builtins()
	if len(problems) > 0 {
		t.Fatalf("problems: %v", problems)
	}
	var names []string
	for _, w := range list {
		names = append(names, w.Name)
		if w.Source != SourceBuiltin || w.Path != "" || w.Description == "" {
			t.Errorf("%s: %+v", w.Name, w)
		}
		steps, err := Resolve(w, Find(list))
		if err != nil {
			t.Errorf("%s does not resolve: %v", w.Name, err)
			continue
		}
		for _, s := range steps {
			if s.Kind == KindScript {
				t.Errorf("%s: a built-in workflow has a script step (%q)", w.Name, s.Title)
			}
		}
		if last := steps[len(steps)-1]; last.Kind != KindGate {
			t.Errorf("%s ends with a %s step, not a gate", w.Name, last.Kind)
		}
	}
	if !slices.Equal(names, []string{"bug-fix", "feature"}) {
		t.Errorf("built-ins = %v", names)
	}
}

func TestBuiltinShapes(t *testing.T) {
	list, _ := Builtins()
	find := Find(list)
	for name, want := range map[string]struct {
		titles []string
		onFail string
	}{
		"bug-fix": {[]string{"Reproduce", "Fix", "Review", "Approve"}, "fix"},
		"feature": {[]string{"Plan", "Implement", "Review", "Approve"}, "implement"},
	} {
		w, ok := find(name)
		if !ok {
			t.Fatalf("no %s", name)
		}
		var titles []string
		for _, s := range w.Steps {
			titles = append(titles, s.Title)
		}
		if !slices.Equal(titles, want.titles) {
			t.Errorf("%s steps = %v", name, titles)
		}
		if r := w.Steps[2]; r.Kind != KindReview || r.OnFail != want.onFail {
			t.Errorf("%s review = %+v", name, r)
		}
	}
}

func TestDirs(t *testing.T) {
	if got := ProjectDir("/r"); got != filepath.Join("/r", ".flint", "workflows") {
		t.Errorf("ProjectDir = %s", got)
	}
	if got := PersonalDir("/c"); got != filepath.Join("/c", "workflows") {
		t.Errorf("PersonalDir = %s", got)
	}
}

func TestLoadDir(t *testing.T) {
	root := t.TempDir()
	dir := ProjectDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("good.md", "---\ndescription: ok\n---\n## A\n\ndo it\n")
	write("bad.md", "## A\nmodel: x\n\ndo it\n")
	write("Bad Name.md", "## A\n\ndo it\n")
	write("notes.txt", "ignored")
	write(".hidden.md", "## A\n\nx\n")

	list, problems := LoadDir(dir, root, SourceProject)
	if len(list) != 1 || list[0].Name != "good.md"[:4] || list[0].Source != SourceProject || list[0].Path != filepath.Join(dir, "good.md") || list[0].Description != "ok" {
		t.Errorf("list = %+v", list)
	}
	slices.Sort(problems)
	if len(problems) != 2 || !strings.HasPrefix(problems[0], "Bad Name.md: ") || !strings.HasPrefix(problems[1], "bad.md: ") || !strings.Contains(problems[1], "unknown key") {
		t.Errorf("problems = %q", problems)
	}
}

func TestLoadDirMissingAndContained(t *testing.T) {
	root := t.TempDir()
	if list, problems := LoadDir(ProjectDir(root), root, SourceProject); len(list) != 0 || len(problems) != 0 {
		t.Errorf("a missing folder: %v, %v", list, problems)
	}
	// A symlinked folder that leaves the project is refused.
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "evil.md"), []byte("## A\n\nx\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".flint"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, ProjectDir(root)); err != nil {
		t.Skip("no symlinks:", err)
	}
	list, problems := LoadDir(ProjectDir(root), root, SourceProject)
	if len(list) != 0 || len(problems) == 0 {
		t.Errorf("list = %v, problems = %v", list, problems)
	}
}

func TestLoadDirLimit(t *testing.T) {
	dir := t.TempDir()
	for i := range MaxWorkflows + 2 {
		name := filepath.Join(dir, "w"+strings.Repeat("a", i)+".md")
		if err := os.WriteFile(name, []byte("## A\n\nx\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	list, problems := LoadDir(dir, "", SourcePersonal)
	if len(list) != MaxWorkflows || len(problems) != 1 || !strings.Contains(problems[0], "more than") {
		t.Errorf("%d workflows, problems = %v", len(list), problems)
	}
}

func TestMerge(t *testing.T) {
	mk := func(name string, src Source) Workflow { return Workflow{Name: name, Source: src} }
	got := Merge(
		[]Workflow{mk("b", SourceBuiltin), mk("a", SourceBuiltin), mk("c", SourceBuiltin)},
		[]Workflow{mk("c", SourcePersonal), mk("d", SourcePersonal)},
		[]Workflow{mk("a", SourceProject), mk("c", SourceProject)},
	)
	var out []string
	for _, w := range got {
		out = append(out, w.Name+":"+string(w.Source))
	}
	want := []string{"a:project", "b:builtin", "c:project", "d:personal"}
	if !slices.Equal(out, want) {
		t.Errorf("Merge = %v, want %v", out, want)
	}
	if got := Merge(nil, nil, nil); len(got) != 0 {
		t.Errorf("Merge of nothing = %v", got)
	}
}

func TestFind(t *testing.T) {
	find := Find([]Workflow{{Name: "a", Description: "first"}, {Name: "b"}})
	if w, ok := find("a"); !ok || w.Description != "first" {
		t.Errorf("Find(a) = %+v, %v", w, ok)
	}
	if _, ok := find("zzz"); ok {
		t.Error("Find found a workflow that is not there")
	}
}

func TestLoadDirNames(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"good.md", "Upper.md", "under_score.md", strings.Repeat("a", 65) + ".md"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("## A\n\ndo it\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	list, problems := LoadDir(dir, "", SourcePersonal)
	if len(list) != 1 || list[0].Name != "good" {
		t.Errorf("list = %+v", list)
	}
	slices.Sort(problems)
	want := []string{
		strings.Repeat("a", 65) + ".md: the file name must be lowercase letters, digits and dashes",
		"Upper.md: the file name must be lowercase letters, digits and dashes",
		"under_score.md: the file name must be lowercase letters, digits and dashes",
	}
	slices.Sort(want)
	if !slices.Equal(problems, want) {
		t.Errorf("problems = %q, want %q", problems, want)
	}
}
