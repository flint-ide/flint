package prompts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func names(list []Prompt) []string {
	out := []string{}
	for _, p := range list {
		out = append(out, p.Name)
	}
	return out
}

func TestLoadDir(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "review.md"), "---\ndescription: Team review\n---\nReview it.")
	write(t, filepath.Join(dir, "notes.txt"), "not a prompt")
	write(t, filepath.Join(dir, ".hidden.md"), "skipped")
	write(t, filepath.Join(dir, "bad name.md"), "body")
	write(t, filepath.Join(dir, "broken.md"), "---\nno colon\n---\nbody")
	write(t, filepath.Join(dir, "huge.md"), strings.Repeat("x", MaxFileSize+1))
	if err := os.Mkdir(filepath.Join(dir, "folder.md"), 0o755); err != nil {
		t.Fatal(err)
	}

	list, problems := LoadDir(dir, dir, SourceProject)
	if got := names(list); len(got) != 1 || got[0] != "review" {
		t.Fatalf("loaded %v", got)
	}
	p := list[0]
	if p.Source != SourceProject || p.Path != filepath.Join(dir, "review.md") || p.Description != "Team review" {
		t.Fatalf("prompt %+v", p)
	}
	joined := strings.Join(problems, "\n")
	for _, want := range []string{"bad name.md", "broken.md", "huge.md", "folder.md"} {
		if !strings.Contains(joined, want) {
			t.Errorf("no problem reported for %s in:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "notes.txt") || strings.Contains(joined, ".hidden.md") {
		t.Errorf("ignored files reported:\n%s", joined)
	}
}

func TestLoadDirMissing(t *testing.T) {
	list, problems := LoadDir(filepath.Join(t.TempDir(), "nope"), "", SourcePersonal)
	if len(list) != 0 || len(problems) != 0 {
		t.Fatalf("%v %v", list, problems)
	}
}

func TestLoadDirSymlinks(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	dir := filepath.Join(root, ".flint", "prompts")
	write(t, filepath.Join(outside, "secret.md"), "outside the repo")
	write(t, filepath.Join(dir, "real.md"), "inside")
	if err := os.Symlink(filepath.Join(outside, "secret.md"), filepath.Join(dir, "escape.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "real.md"), filepath.Join(dir, "alias.md")); err != nil {
		t.Fatal(err)
	}
	list, problems := LoadDir(dir, root, SourceProject)
	got := names(list)
	if len(got) != 2 || got[0] != "alias" || got[1] != "real" {
		t.Fatalf("loaded %v (problems %v)", got, problems)
	}
	if !strings.Contains(strings.Join(problems, "\n"), "escape.md") {
		t.Errorf("escaping link not reported: %v", problems)
	}
}

func TestLoadDirFolderLinkedOutside(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	write(t, filepath.Join(outside, "prompts", "x.md"), "outside")
	if err := os.Symlink(outside, filepath.Join(root, ".flint")); err != nil {
		t.Fatal(err)
	}
	list, problems := LoadDir(filepath.Join(root, ".flint", "prompts"), root, SourceProject)
	if len(list) != 0 || len(problems) != 1 {
		t.Fatalf("loaded %v, problems %v", names(list), problems)
	}
}

func TestLoadDirLimit(t *testing.T) {
	dir := t.TempDir()
	for i := range MaxPrompts + 5 {
		write(t, filepath.Join(dir, "p"+strings.Repeat("a", i%3)+string(rune('a'+i%26))+itoa(i)+".md"), "body")
	}
	list, problems := LoadDir(dir, "", SourcePersonal)
	if len(list) != MaxPrompts || len(problems) == 0 {
		t.Fatalf("loaded %d, problems %v", len(list), problems)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	s := ""
	for ; i > 0; i /= 10 {
		s = string(rune('0'+i%10)) + s
	}
	return s
}

func TestCreate(t *testing.T) {
	root := t.TempDir()
	dir := ProjectDir(root)
	path, err := Create(dir, root, "my-prompt")
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(dir, "my-prompt.md") {
		t.Fatalf("path %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse("my-prompt", data); err != nil {
		t.Fatalf("template does not parse: %v", err)
	}
	if _, err := Create(dir, root, "my-prompt"); err == nil {
		t.Error("overwrote an existing prompt")
	}
	if _, err := Create(dir, root, "../x"); err == nil {
		t.Error("accepted an invalid name")
	}
}

func TestCreateRefusesLinkOutside(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, ".flint")); err != nil {
		t.Fatal(err)
	}
	if _, err := Create(ProjectDir(root), root, "x"); err == nil {
		t.Fatal("created a prompt outside the project")
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatalf("wrote outside: %v", entries)
	}
}
