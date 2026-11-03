package catalog

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

const themeManifest = `{"contract":1,"id":"acme.theme","name":"Acme Theme","version":"1.0.0","run":{},
"contributes":{"themes":[{"id":"acme.theme.dark","label":"Acme Dark","base":"dark","colors":{"--bg-editor":"#000000"}}]}}`

const toolManifest = `{"contract":1,"id":"acme.tool","name":"Acme Tool","version":"0.2.0","run":{"command":"tool"},
"activation":["onCommand:acme.tool.run"],"permissions":["workspace.read"],
"contributes":{"commands":[{"id":"acme.tool.run","title":"Run"}]}}`

func write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// newRepo is a git repository with a theme, a Go extension, a broken
// manifest and a folder that is no extension.
func newRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := t.TempDir()
	write(t, filepath.Join(repo, "theme", "flint-extension.json"), themeManifest)
	write(t, filepath.Join(repo, "tool", "flint-extension.json"), toolManifest)
	write(t, filepath.Join(repo, "tool", "go.mod"), "module example.com/acmetool\n\ngo 1.21\n")
	write(t, filepath.Join(repo, "tool", "main.go"), "package main\n\nfunc main() {}\n")
	write(t, filepath.Join(repo, "tool", ".gitignore"), "tool\n")
	write(t, filepath.Join(repo, "broken", "flint-extension.json"), `{"contract":99}`)
	write(t, filepath.Join(repo, ".github", "flint-extension.json"), themeManifest)
	write(t, filepath.Join(repo, "docs", "README.md"), "no extension")
	run(t, repo, "init", "--quiet", "--initial-branch=main")
	run(t, repo, "add", ".")
	run(t, repo, "commit", "--quiet", "-m", "init")
	return repo
}

func TestListClonesAndReadsEntries(t *testing.T) {
	repo := newRepo(t)
	c := New(Options{Dir: filepath.Join(t.TempDir(), "cache", "repo"), URL: repo})
	if c.Fetched() {
		t.Fatal("fetched before the first List")
	}
	l, err := c.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !c.Fetched() || l.Revision == "" || l.Source != repo {
		t.Fatalf("listing = %+v", l)
	}
	if len(l.Entries) != 3 {
		t.Fatalf("entries = %+v, want broken, theme, tool", l.Entries)
	}
	byID := map[string]Entry{}
	for _, e := range l.Entries {
		byID[e.ID] = e
	}
	if e := byID["acme.theme"]; e.Program || e.Themes != 1 || e.Version != "1.0.0" || e.Error != "" {
		t.Errorf("theme = %+v", e)
	}
	if e := byID["acme.tool"]; !e.Program || e.Folder != "tool" || len(e.Permissions) != 1 {
		t.Errorf("tool = %+v", e)
	}
	if e := byID["broken"]; e.Error == "" {
		t.Errorf("broken manifest has no error: %+v", e)
	}
}

func TestRefreshPullsNewCommits(t *testing.T) {
	repo := newRepo(t)
	c := New(Options{Dir: filepath.Join(t.TempDir(), "repo"), URL: repo})
	first, err := c.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(repo, "theme2", "flint-extension.json"),
		`{"contract":1,"id":"acme.theme2","name":"Another","version":"1.0.0","run":{},"contributes":{"themes":[]}}`)
	run(t, repo, "add", ".")
	run(t, repo, "commit", "--quiet", "-m", "more")
	if l, _ := c.List(context.Background()); len(l.Entries) != len(first.Entries) {
		t.Fatal("List fetched; only Refresh should")
	}
	l, err := c.Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Entries) != len(first.Entries)+1 || l.Revision == first.Revision {
		t.Fatalf("after refresh: %+v", l)
	}
}

func TestRefreshReportsGitErrors(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	c := New(Options{Dir: filepath.Join(t.TempDir(), "repo"), URL: filepath.Join(t.TempDir(), "missing")})
	l, err := c.Refresh(context.Background())
	if err == nil {
		t.Fatal("cloning a missing repository succeeded")
	}
	if l.Entries == nil || c.Fetched() {
		t.Fatalf("listing = %+v, fetched = %v", l, c.Fetched())
	}
}

func TestPrepare(t *testing.T) {
	repo := newRepo(t)
	c := New(Options{Dir: filepath.Join(t.TempDir(), "repo"), URL: repo})
	ctx := context.Background()
	if _, err := c.List(ctx); err != nil {
		t.Fatal(err)
	}
	dir, release, err := c.Prepare(ctx, "theme")
	if err != nil || filepath.Base(dir) != "theme" {
		t.Fatalf("theme: %q, %v", dir, err)
	}
	release()
	release() // twice is harmless
	if _, _, err := c.Prepare(ctx, "nope"); !errors.Is(err, ErrUnknown) {
		t.Fatalf("unknown folder: %v", err)
	}
	if _, _, err := c.Prepare(ctx, "broken"); err == nil {
		t.Fatal("prepared a broken manifest")
	}
	if _, err := findGo(); err != nil {
		t.Skip("go not installed")
	}
	dir, release, err = c.Prepare(ctx, "tool")
	if err != nil {
		t.Fatal(err)
	}
	release()
	if info, err := os.Stat(filepath.Join(dir, "tool")); err != nil || info.Mode()&0o111 == 0 {
		t.Fatalf("program not built: %v", err)
	}
}

func TestRefreshWaitsForAnInstall(t *testing.T) {
	repo := newRepo(t)
	c := New(Options{Dir: filepath.Join(t.TempDir(), "repo"), URL: repo})
	ctx := context.Background()
	if _, err := c.List(ctx); err != nil {
		t.Fatal(err)
	}
	_, release, err := c.Prepare(ctx, "theme")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		_, _ = c.Refresh(ctx)
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("Refresh changed the clone while an install was open")
	case <-time.After(200 * time.Millisecond):
	}
	release()
	<-done
}

func TestListReclonesAnotherURL(t *testing.T) {
	first, second := newRepo(t), newRepo(t)
	write(t, filepath.Join(second, "extra", "flint-extension.json"),
		`{"contract":1,"id":"acme.extra","name":"Extra","version":"1.0.0","run":{},"contributes":{"themes":[]}}`)
	run(t, second, "add", ".")
	run(t, second, "commit", "--quiet", "-m", "extra")
	dir := filepath.Join(t.TempDir(), "repo")
	a, err := New(Options{Dir: dir, URL: first}).List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	b, err := New(Options{Dir: dir, URL: second}).List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Entries) != len(a.Entries)+1 || b.Source != second {
		t.Fatalf("still the first repository: %+v", b)
	}
}

func TestBuildTargetStaysInside(t *testing.T) {
	dir, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, "bin")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "x"), filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"bin/git", "../x", "a/../../x", "/abs", "", "link"} {
		if out, err := buildTarget(dir, command); err == nil {
			t.Errorf("%q: built to %s", command, out)
		}
	}
	out, err := buildTarget(dir, "sub/prog")
	if err != nil {
		t.Fatal(err)
	}
	real, _ := filepath.EvalSymlinks(dir)
	if out != filepath.Join(real, "sub", "prog") {
		t.Errorf("out = %s", out)
	}
}

func TestNewPicksURL(t *testing.T) {
	t.Setenv(URLEnv, "")
	if c := New(Options{}); c.o.URL != DefaultURL {
		t.Errorf("default URL = %q", c.o.URL)
	}
	t.Setenv(URLEnv, "/srv/mirror")
	if c := New(Options{}); c.o.URL != "/srv/mirror" {
		t.Errorf("env URL = %q", c.o.URL)
	}
	if c := New(Options{URL: "x"}); c.o.URL != "x" {
		t.Errorf("explicit URL = %q", c.o.URL)
	}
}
