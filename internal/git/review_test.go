package git

import (
	"context"
	"strings"
	"testing"
)

func TestWorkingDiff(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()
	commitFile(t, r.Root, "a.go", "package a\n\nfunc A() {}\n", "add a")
	writeFile(t, r.Root, "a.go", "package a\n\nfunc A() int { return 1 }\n")
	mustGit(t, r.Root, "add", "a.go") // staged and unstaged changes look the same: HEAD vs working tree
	writeFile(t, r.Root, "a.go", "package a\n\nfunc A() int { return 2 }\n")

	diff, err := r.WorkingDiff(ctx, "a.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "-func A() {}") || !strings.Contains(diff, "+func A() int { return 2 }") {
		t.Errorf("diff:\n%s", diff)
	}

	mustGit(t, r.Root, "mv", "README.md", "DOCS.md")
	diff, err = r.WorkingDiff(ctx, "DOCS.md", "README.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "rename from README.md") {
		t.Errorf("rename:\n%s", diff)
	}
	if _, err := r.WorkingDiff(ctx); err == nil {
		t.Error("no paths: no error")
	}
}

func TestWorkingDiffBeforeFirstCommit(t *testing.T) {
	r := newEmptyRepo(t)
	writeFile(t, r.Root, "new.go", "package x\n")
	mustGit(t, r.Root, "add", "new.go")
	diff, err := r.WorkingDiff(context.Background(), "new.go")
	if err != nil || !strings.Contains(diff, "+package x") {
		t.Errorf("diff = %q, %v", diff, err)
	}
}
