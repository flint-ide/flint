package git

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseWorktrees(t *testing.T) {
	out := []byte(strings.Join([]string{
		"worktree /repo", "HEAD " + oid1, "branch refs/heads/main", "",
		"worktree /repo.worktrees/feat", "HEAD " + oid2, "branch refs/heads/feat/x", "locked", "",
		"worktree /tmp/detached", "HEAD " + oid1, "detached", "prunable gitdir file points to non-existent location", "",
		"worktree /tmp/locked-reason", "HEAD " + oid2, "branch refs/heads/l", "locked because reasons", "",
		"bare", // stray attribute without a preceding worktree is ignored
	}, "\x00") + "\x00")

	got := parseWorktrees(out, "/repo.worktrees/feat")
	want := []Worktree{
		{Path: "/repo", Head: oid1, Branch: "main", Main: true},
		{Path: "/repo.worktrees/feat", Head: oid2, Branch: "feat/x", Current: true, Locked: true},
		{Path: "/tmp/detached", Head: oid1, Detached: true, Prunable: true},
		{Path: "/tmp/locked-reason", Head: oid2, Branch: "l", Locked: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseWorktrees =\n%+v\nwant\n%+v", got, want)
	}
}

func TestParseWorktreesEmpty(t *testing.T) {
	if got := parseWorktrees(nil, "/x"); got == nil || len(got) != 0 {
		t.Errorf("parseWorktrees(nil) = %#v, want empty non-nil", got)
	}
}

func TestSameDir(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		a, b string
		want bool
	}{
		{dir, dir, true},
		{dir, realPath(t, dir), true}, // /var vs /private/var on macOS
		{dir + "/", dir, true},
		{filepath.Join(dir, "missing"), filepath.Join(dir, "missing", "."), true},
		{filepath.Join(dir, "a"), filepath.Join(dir, "b"), false},
		{dir, filepath.Dir(dir), false},
	}
	for _, tt := range tests {
		if got := sameDir(tt.a, tt.b); got != tt.want {
			t.Errorf("sameDir(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestWorktrees(t *testing.T) {
	ctx := context.Background()
	r := newRepo(t)
	head := mustGit(t, r.Root, "rev-parse", "HEAD")
	featPath := filepath.Join(t.TempDir(), "feat")
	detPath := filepath.Join(t.TempDir(), "det")
	mustGit(t, r.Root, "worktree", "add", "-q", "-b", "feature/one", featPath)
	mustGit(t, r.Root, "worktree", "add", "-q", "--detach", detPath)

	check := func(t *testing.T, from *Repo, currentIdx int) {
		t.Helper()
		wts, err := from.Worktrees(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(wts) != 3 {
			t.Fatalf("got %d worktrees, want 3: %+v", len(wts), wts)
		}
		paths := []string{r.Root, featPath, detPath}
		branches := []string{"main", "feature/one", ""}
		for i, wt := range wts {
			samePath(t, "Path", wt.Path, paths[i])
			if wt.Main != (i == 0) || wt.Current != (i == currentIdx) {
				t.Errorf("wt[%d] Main/Current = %v/%v", i, wt.Main, wt.Current)
			}
			if wt.Branch != branches[i] || wt.Detached != (i == 2) || wt.Head != head {
				t.Errorf("wt[%d] = %+v", i, wt)
			}
			if wt.Locked || wt.Prunable {
				t.Errorf("wt[%d] unexpectedly locked/prunable: %+v", i, wt)
			}
		}
	}
	t.Run("from main", func(t *testing.T) { check(t, r, 0) })
	t.Run("from linked", func(t *testing.T) { check(t, openRepo(t, featPath), 1) })

	t.Run("locked", func(t *testing.T) {
		mustGit(t, r.Root, "worktree", "lock", detPath)
		wts, err := r.Worktrees(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !wts[2].Locked {
			t.Errorf("detached worktree not reported locked: %+v", wts[2])
		}
		mustGit(t, r.Root, "worktree", "unlock", detPath)
	})
}

func TestAddWorktree(t *testing.T) {
	ctx := context.Background()
	r := newRepo(t)
	base := mustGit(t, r.Root, "rev-parse", "HEAD")
	head := commitFile(t, r.Root, "later.txt", "later", "later")

	t.Run("new branch from base", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "agent")
		if err := r.AddWorktree(ctx, p, "agent/task-1", true, base); err != nil {
			t.Fatal(err)
		}
		if got := mustGit(t, p, "rev-parse", "HEAD"); got != base {
			t.Errorf("worktree HEAD = %s, want base %s", got, base)
		}
		if got := currentBranch(t, p); got != "agent/task-1" {
			t.Errorf("branch = %q", got)
		}
		if fileExists(p, "later.txt") {
			t.Error("later.txt should not exist at base")
		}
	})

	t.Run("new branch from HEAD", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "fromhead")
		if err := r.AddWorktree(ctx, p, "fromhead", true, ""); err != nil {
			t.Fatal(err)
		}
		if got := mustGit(t, p, "rev-parse", "HEAD"); got != head {
			t.Errorf("worktree HEAD = %s, want %s", got, head)
		}
	})

	t.Run("existing branch", func(t *testing.T) {
		mustGit(t, r.Root, "branch", "existing", base)
		p := filepath.Join(t.TempDir(), "existing")
		if err := r.AddWorktree(ctx, p, "existing", false, ""); err != nil {
			t.Fatal(err)
		}
		if got := currentBranch(t, p); got != "existing" {
			t.Errorf("branch = %q", got)
		}
		if got := mustGit(t, p, "rev-parse", "HEAD"); got != base {
			t.Errorf("HEAD = %s, want %s", got, base)
		}
	})

	errTests := []struct {
		name      string
		path      string
		branch    string
		newBranch bool
	}{
		{"relative path", "relative/dir", "rel", true},
		{"invalid new branch name", filepath.Join(t.TempDir(), "bad"), "bad..name", true},
		{"branch already checked out", filepath.Join(t.TempDir(), "dup"), "main", false},
		{"missing existing branch", filepath.Join(t.TempDir(), "missing"), "no-such", false},
	}
	for _, tt := range errTests {
		t.Run(tt.name, func(t *testing.T) {
			if err := r.AddWorktree(ctx, tt.path, tt.branch, tt.newBranch, ""); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func TestRemoveWorktree(t *testing.T) {
	ctx := context.Background()
	r := newRepo(t)
	clean := filepath.Join(t.TempDir(), "clean")
	dirty := filepath.Join(t.TempDir(), "dirty")
	mustGit(t, r.Root, "worktree", "add", "-q", "-b", "clean", clean)
	mustGit(t, r.Root, "worktree", "add", "-q", "-b", "dirty", dirty)
	writeFile(t, dirty, "README.md", "uncommitted\n")

	if err := r.RemoveWorktree(ctx, clean, false); err != nil {
		t.Fatalf("remove clean: %v", err)
	}
	if fileExists(clean, "") {
		t.Error("clean worktree directory still exists")
	}
	if err := r.RemoveWorktree(ctx, dirty, false); err == nil {
		t.Fatal("removing a dirty worktree without force should fail")
	}
	if !fileExists(dirty, "README.md") {
		t.Fatal("dirty worktree was removed despite the error")
	}
	if err := r.RemoveWorktree(ctx, dirty, true); err != nil {
		t.Fatalf("force remove: %v", err)
	}
	if fileExists(dirty, "") {
		t.Error("dirty worktree directory still exists after force")
	}
	wts, err := r.Worktrees(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(wts) != 1 {
		t.Errorf("worktrees after removal = %+v", wts)
	}
	// Branches survive worktree removal.
	mustGit(t, r.Root, "rev-parse", "--verify", "-q", "refs/heads/dirty")
}

func TestDefaultWorktreePath(t *testing.T) {
	ctx := context.Background()
	r := newRepo(t)
	linked := filepath.Join(t.TempDir(), "linked")
	mustGit(t, r.Root, "worktree", "add", "-q", "-b", "linked", linked)

	tests := []struct {
		branch string
		want   string
	}{
		{"feature", "feature"},
		{"feature/login", "feature-login"},
		{"user/feat/deep name", "user-feat-deep-name"},
	}
	for _, from := range []*Repo{r, openRepo(t, linked)} {
		for _, tt := range tests {
			got, err := from.DefaultWorktreePath(ctx, tt.branch)
			if err != nil {
				t.Fatal(err)
			}
			want := filepath.Join(filepath.Dir(realPath(t, r.Root)), "repo.worktrees", tt.want)
			if got != want {
				t.Errorf("DefaultWorktreePath(%q) from %s = %q, want %q", tt.branch, from.Root, got, want)
			}
		}
	}
}
