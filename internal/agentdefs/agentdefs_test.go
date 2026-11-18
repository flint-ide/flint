package agentdefs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"flint/internal/agent"
)

func TestParse(t *testing.T) {
	long := strings.Repeat("x", MaxInstructions+1)
	tests := []struct {
		name, file, data string
		want             Definition
		err              string
	}{
		{name: "full", file: "reviewer", data: "---\n# a comment\ndescription: Reviews\nengine: claude\nmodel: opus\nfuture: ignored\n---\nReview it.\n",
			want: Definition{Name: "reviewer", Description: "Reviews", Engine: "claude", Model: "opus", Instructions: "Review it."}},
		{name: "no front matter", file: "planner", data: "Plan the work.",
			want: Definition{Name: "planner", Instructions: "Plan the work."}},
		{name: "engine only", file: "dev-2", data: "---\nengine: \"codex\"\n---\nBuild.",
			want: Definition{Name: "dev-2", Engine: "codex", Instructions: "Build."}},
		{name: "crlf", file: "a", data: "---\r\ndescription: D\r\n---\r\nBody\r\n",
			want: Definition{Name: "a", Description: "D", Instructions: "Body"}},
		{name: "exactly the limit", file: "big", data: strings.Repeat("é", MaxInstructions),
			want: Definition{Name: "big", Instructions: strings.Repeat("é", MaxInstructions)}},
		{name: "uppercase name", file: "Reviewer", data: "x", err: "name"},
		{name: "digit first", file: "1st", data: "x", err: "name"},
		{name: "underscore", file: "a_b", data: "x", err: "name"},
		{name: "name too long", file: strings.Repeat("a", 33), data: "x", err: "name"},
		{name: "unknown engine", file: "a", data: "---\nengine: gemini\n---\nx", err: "engine"},
		{name: "tier without engine", file: "rev", data: "---\nmodel: strong\n---\nReview.",
			want: Definition{Name: "rev", Model: "strong", Instructions: "Review."}},
		{name: "tier with engine", file: "rev", data: "---\nengine: codex\nmodel: fast\n---\nReview.",
			want: Definition{Name: "rev", Engine: "codex", Model: "fast", Instructions: "Review."}},
		{name: "model without engine", file: "a", data: "---\nmodel: opus\n---\nx", err: "needs an engine"},
		{name: "tier is case-sensitive", file: "a", data: "---\nmodel: Strong\n---\nx", err: "needs an engine"},
		{name: "bad model", file: "a", data: "---\nengine: claude\nmodel: --yolo\n---\nx", err: "model"},
		{name: "empty body", file: "a", data: "---\nengine: claude\n---\n  \n", err: "empty"},
		{name: "body too long", file: "a", data: long, err: "longer than"},
		{name: "long description", file: "a", data: "---\ndescription: " + strings.Repeat("d", 201) + "\n---\nx", err: "description"},
		{name: "control characters", file: "a", data: "---\ndescription: a\x1bb\n---\nx", err: "control"},
		{name: "unclosed", file: "a", data: "---\nengine: claude\nx", err: "closing"},
		{name: "not utf-8", file: "a", data: "\xff", err: "UTF-8"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.file, []byte(tt.data))
			if tt.err != "" {
				if err == nil || !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("err %v, want %q", err, tt.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("got %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

func TestResolveEngine(t *testing.T) {
	both := []agent.Kind{agent.Claude, agent.Codex}
	tests := []struct {
		name         string
		def          Definition
		defaultAgent string
		installed    []agent.Kind
		want         agent.Kind
		err          string
	}{
		{name: "pinned", def: Definition{Name: "r", Engine: "codex"}, defaultAgent: "claude", installed: both, want: agent.Codex},
		{name: "pinned missing", def: Definition{Name: "r", Engine: "codex"}, installed: []agent.Kind{agent.Claude}, err: "codex, which is not installed"},
		{name: "default native", def: Definition{Name: "r"}, defaultAgent: "codex", installed: both, want: agent.Codex},
		{name: "default native not installed", def: Definition{Name: "r"}, defaultAgent: "codex", installed: []agent.Kind{agent.Claude}, want: agent.Claude},
		{name: "default custom", def: Definition{Name: "r"}, defaultAgent: "gemini", installed: []agent.Kind{agent.Codex}, want: agent.Codex},
		{name: "default is a defined agent", def: Definition{Name: "r"}, defaultAgent: "r", installed: both, want: agent.Claude},
		{name: "no default", def: Definition{Name: "r"}, installed: both, want: agent.Claude},
		{name: "nothing installed", def: Definition{Name: "r"}, defaultAgent: "claude", err: "no agent CLI"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveEngine(tt.def, tt.defaultAgent, tt.installed)
			if tt.err != "" {
				if err == nil || !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("err %v, want %q", err, tt.err)
				}
				if tt.name == "nothing installed" && !errors.Is(err, ErrNoEngine) {
					t.Fatalf("err %v is not ErrNoEngine", err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("got %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func names(list []Definition) []string {
	out := []string{}
	for _, d := range list {
		out = append(out, string(d.Source)+":"+d.Name)
	}
	return out
}

func TestLoad(t *testing.T) {
	config := t.TempDir()
	personal := PersonalDir(config)
	rootA, rootB, untrusted := t.TempDir(), t.TempDir(), t.TempDir()
	write(t, filepath.Join(personal, "reviewer.md"), "---\nengine: claude\n---\nPersonal reviewer.")
	write(t, filepath.Join(personal, "claude.md"), "Built-in name.")
	write(t, filepath.Join(personal, "gemini.md"), "Custom agent's id.")
	write(t, filepath.Join(personal, "broken.md"), "---\nmodel: opus\n---\nNo engine.")
	write(t, filepath.Join(ProjectDir(rootA), "reviewer.md"), "Project reviewer.")
	write(t, filepath.Join(ProjectDir(rootA), "backend.md"), "---\nengine: codex\nmodel: gpt-5\n---\nBackend.")
	write(t, filepath.Join(ProjectDir(rootB), "backend.md"), "Second root's backend.")
	write(t, filepath.Join(ProjectDir(rootB), "planner.md"), "Plan.")
	write(t, filepath.Join(ProjectDir(untrusted), "evil.md"), "Steal.")
	write(t, filepath.Join(ProjectDir(untrusted), "evil2.md"), "Steal more.")

	set := Load(personal, []Root{{rootA, true}, {untrusted, false}, {rootB, true}}, func(n string) bool { return n == "gemini" })

	if got := strings.Join(names(set.Agents), " "); got != "project:backend project:planner personal:reviewer" {
		t.Fatalf("agents %s\nproblems %v", got, set.Problems)
	}
	if d, _ := set.Find("backend"); d.Path != filepath.Join(ProjectDir(rootA), "backend.md") || d.Model != "gpt-5" {
		t.Errorf("backend from the wrong root: %+v", d)
	}
	if d, _ := set.Find("reviewer"); d.Instructions != "Personal reviewer." {
		t.Errorf("the project replaced a personal agent: %+v", d)
	}
	joined := strings.Join(set.Problems, "\n")
	for _, want := range []string{
		"claude.md: claude is a built-in agent",
		"gemini.md: gemini is a custom agent's id",
		"broken.md: a model needs an engine",
		filepath.Join(ProjectDir(rootA), "reviewer.md") + ": not used",
		filepath.Join(ProjectDir(rootB), "backend.md") + ": not used",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("no problem %q in:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "evil") {
		t.Errorf("untrusted files were read:\n%s", joined)
	}
	if len(set.Ignored) != 1 || set.Ignored[0].Dir != ProjectDir(untrusted) || set.Ignored[0].Count != 2 {
		t.Errorf("ignored %+v", set.Ignored)
	}
}

func TestLoadSymlinkEscape(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	dir := ProjectDir(root)
	write(t, filepath.Join(outside, "secret.md"), "Outside the repository.")
	write(t, filepath.Join(dir, "real.md"), "Inside.")
	if err := os.Symlink(filepath.Join(outside, "secret.md"), filepath.Join(dir, "escape.md")); err != nil {
		t.Fatal(err)
	}
	set := Load(filepath.Join(t.TempDir(), "none"), []Root{{root, true}}, nil)
	if got := strings.Join(names(set.Agents), " "); got != "project:real" {
		t.Fatalf("agents %s", got)
	}
	if !strings.Contains(strings.Join(set.Problems, "\n"), "escape.md") {
		t.Errorf("escape not reported: %v", set.Problems)
	}

	linked := t.TempDir()
	write(t, filepath.Join(outside, "agents", "x.md"), "Outside.")
	if err := os.Symlink(outside, filepath.Join(linked, ".flint")); err != nil {
		t.Fatal(err)
	}
	set = Load(filepath.Join(t.TempDir(), "none"), []Root{{linked, true}}, nil)
	if len(set.Agents) != 0 || len(set.Problems) != 1 {
		t.Fatalf("folder linked outside: %v %v", names(set.Agents), set.Problems)
	}
}

func TestLoadLimits(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "huge.md"), strings.Repeat("x", MaxFileSize+1))
	if list, problems := LoadDir(dir, "", SourcePersonal); len(list) != 0 || len(problems) != 1 || !strings.Contains(problems[0], "huge.md: larger than") {
		t.Fatalf("huge file: %v %v", list, problems)
	}
	if err := os.Remove(filepath.Join(dir, "huge.md")); err != nil {
		t.Fatal(err)
	}
	for i := range MaxDefinitions + 5 {
		write(t, filepath.Join(dir, "a"+strings.Repeat("b", i)[:i%20]+string(rune('a'+i%26))+string(rune('a'+i/26))+".md"), "Body.")
	}
	list, problems := LoadDir(dir, "", SourcePersonal)
	if len(list) != MaxDefinitions || len(problems) == 0 {
		t.Fatalf("loaded %d, problems %v", len(list), problems)
	}
	if !strings.Contains(strings.Join(problems, "\n"), "more than 50") {
		t.Errorf("limit not reported: %v", problems)
	}
}

func TestCreate(t *testing.T) {
	root := t.TempDir()
	dir := ProjectDir(root)
	path, err := Create(dir, root, "reviewer")
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(dir, "reviewer.md") {
		t.Fatalf("path %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "strong") {
		t.Error("the template does not mention model tiers")
	}
	if d, err := Parse("reviewer", data); err != nil || d.Engine != "" || d.Description == "" {
		t.Fatalf("template: %+v, %v", d, err)
	}
	for _, name := range []string{"reviewer", "../x", "Reviewer", "codex", ""} {
		if _, err := Create(dir, root, name); err == nil {
			t.Errorf("created %q", name)
		}
	}

	outside := t.TempDir()
	linked := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(linked, ".flint")); err != nil {
		t.Fatal(err)
	}
	if _, err := Create(ProjectDir(linked), linked, "x"); err == nil {
		t.Fatal("created a definition outside the project")
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatalf("wrote outside: %v", entries)
	}
}
