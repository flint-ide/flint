package projects

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// tempDir is a resolved temporary folder (macOS reaches /var through a link).
func tempDir(t *testing.T) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
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

const testConfigJSON = `{"name":"Acme","repositories":[{"name":"api","url":"git@github.com:acme/api.git"},{"name":"web","url":"https://github.com/acme/web.git"}]}`

// newProject writes a project folder with these items (id → file).
func newProject(t *testing.T, dir string, items map[string]string) string {
	t.Helper()
	write(t, filepath.Join(dir, ConfigFile), testConfigJSON)
	if err := os.MkdirAll(filepath.Join(dir, ItemsDir), 0o755); err != nil {
		t.Fatal(err)
	}
	for id, content := range items {
		write(t, filepath.Join(dir, ItemsDir, id+".md"), content)
	}
	return dir
}

func item(title, status string, extra ...string) string {
	return "---\ntitle: " + title + "\nstatus: " + status + "\n" + strings.Join(extra, "\n") + "\n---\nDo " + title + ".\n"
}

func TestLoad(t *testing.T) {
	base := tempDir(t)
	outside := filepath.Join(base, "outside")
	write(t, filepath.Join(outside, "secret.md"), item("Secret", "todo"))
	dir := newProject(t, filepath.Join(base, "plan"), map[string]string{
		"b":        item("Bravo", "todo"),
		"a":        item("Alpha", "doing", "priority: high"),
		"bad":      "---\ntitle: X\nstatus: started\n---\n",
		"unknown":  item("U", "todo", "repos: db"),
		"Bad Name": item("Space", "todo"),
		".hidden":  "not read",
		"big":      "---\ntitle: Big\n---\n" + strings.Repeat("x", MaxFileSize),
	})
	if err := os.Symlink(filepath.Join(outside, "secret.md"), filepath.Join(dir, ItemsDir, "leak.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, ItemsDir, "b.md"), filepath.Join(dir, ItemsDir, "link.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, ItemsDir, "dir.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, ItemsDir, "sub", "deep.md"), item("Deep", "todo"))
	write(t, filepath.Join(dir, ItemsDir, "notes.txt"), "not an item")
	l, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, it := range l.Items {
		ids = append(ids, it.ID)
	}
	if want := []string{"a", "b", "link"}; !slices.Equal(ids, want) {
		t.Errorf("items %v, want %v", ids, want)
	}
	problems := map[string]string{}
	for _, p := range l.Problems {
		problems[p.File] = p.Reason
	}
	for file, reason := range map[string]string{
		"items/bad.md":      "status",
		"items/unknown.md":  `repository "db"`,
		"items/Bad Name.md": "file name",
		"items/big.md":      "larger than",
		"items/leak.md":     "outside",
		"items/dir.md":      "not a regular file",
	} {
		if !strings.Contains(problems[file], reason) {
			t.Errorf("problem of %s = %q, want %q", file, problems[file], reason)
		}
	}
	if len(l.Problems) != 6 {
		t.Errorf("problems: %+v", l.Problems)
	}
	if l.Config.Name != "Acme" || len(l.Config.Repositories) != 2 {
		t.Errorf("config %+v", l.Config)
	}
}

func TestLoadRefusesLinksOutOfTheFolder(t *testing.T) {
	base := tempDir(t)
	other := newProject(t, filepath.Join(base, "other"), map[string]string{"x": item("X", "todo")})

	// items/ links to another folder: nothing is read from it.
	dir := filepath.Join(base, "plan")
	write(t, filepath.Join(dir, ConfigFile), `{}`)
	if err := os.Symlink(filepath.Join(other, ItemsDir), filepath.Join(dir, ItemsDir)); err != nil {
		t.Fatal(err)
	}
	l, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Items) != 0 || len(l.Problems) != 1 || !strings.Contains(l.Problems[0].Reason, "outside") {
		t.Errorf("linked items folder: %+v", l)
	}
	if _, _, err := ReadItemFile(dir, "x"); err == nil {
		t.Error("ReadItemFile read through a link out of the project")
	}

	// flint-project.json links out.
	dir2 := filepath.Join(base, "plan2")
	if err := os.MkdirAll(dir2, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(other, ConfigFile), filepath.Join(dir2, ConfigFile)); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir2); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Errorf("linked config: %v", err)
	}
	if _, err := Load(filepath.Join(base, "missing")); err == nil {
		t.Error("a folder without flint-project.json loaded")
	}
}

func TestLoadLimit(t *testing.T) {
	dir := newProject(t, tempDir(t), nil)
	for i := range MaxItems + 3 {
		write(t, filepath.Join(dir, ItemsDir, fmt.Sprintf("i%03d.md", i)), item(fmt.Sprintf("Item %d", i), "todo"))
	}
	l, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Items) != MaxItems || len(l.Problems) != 1 || !strings.Contains(l.Problems[0].Reason, "more than 500") {
		t.Errorf("%d items, problems %+v", len(l.Items), l.Problems)
	}
}
