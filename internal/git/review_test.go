package git

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestWorkingDiffs(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()
	commitFile(t, r.Root, "a.go", "package a\n\nfunc A() {}\n", "add a")
	commitFile(t, r.Root, "old.txt", "one\ntwo\nthree\n", "add old")
	commitFile(t, r.Root, "gone.txt", "bye\n", "add gone")
	writeFile(t, r.Root, "a.go", "package a\n\nfunc A() int { return 1 }\n")
	mustGit(t, r.Root, "add", "a.go") // staged and unstaged look the same: HEAD vs working tree
	writeFile(t, r.Root, "a.go", "package a\n\nfunc A() int { return 2 }\n")
	writeFile(t, r.Root, "with space.go", "package a\n")
	mustGit(t, r.Root, "add", "with space.go")
	mustGit(t, r.Root, "mv", "README.md", "DOCS.md")
	// An unpaired rename: moved, then rewritten beyond recognition.
	mustGit(t, r.Root, "mv", "old.txt", "new.txt")
	writeFile(t, r.Root, "new.txt", "completely\ndifferent\ncontent\nnow\n")
	if err := removeFile(r.Root, "gone.txt"); err != nil {
		t.Fatal(err)
	}

	diffs, err := r.WorkingDiffs(ctx, [][]string{{"a.go"}, {"DOCS.md", "README.md"}, {"new.txt", "old.txt"}, {"gone.txt"}, {"with space.go"}})
	if err != nil {
		t.Fatal(err)
	}
	if d := diffs["a.go"]; !strings.Contains(d, "-func A() {}") || !strings.Contains(d, "+func A() int { return 2 }") {
		t.Errorf("a.go:\n%s", d)
	}
	if d := diffs["DOCS.md"]; !strings.Contains(d, "rename from README.md") {
		t.Errorf("rename:\n%s", d)
	}
	if d := diffs["new.txt"]; !strings.Contains(d, "+completely") || strings.Contains(d, "-one") || strings.Count(d, "diff --git") != 1 {
		t.Errorf("unpaired rename, new side:\n%s", d)
	}
	if d := diffs["old.txt"]; !strings.Contains(d, "-one") {
		t.Errorf("unpaired rename, old side:\n%s", d)
	}
	if d := diffs["gone.txt"]; !strings.Contains(d, "-bye") {
		t.Errorf("deletion:\n%s", d)
	}
	if d := diffs["with space.go"]; !strings.Contains(d, "+package a") {
		t.Errorf("path with a space: %q (keys %v)", d, keys(diffs))
	}
	if d, err := r.WorkingDiffs(ctx, nil); err != nil || len(d) != 0 {
		t.Errorf("no paths: %v %v", d, err)
	}
}

func TestWorkingDiffsBeforeFirstCommit(t *testing.T) {
	r := newEmptyRepo(t)
	writeFile(t, r.Root, "new.go", "package x\n")
	mustGit(t, r.Root, "add", "new.go")
	diffs, err := r.WorkingDiffs(context.Background(), [][]string{{"new.go"}})
	if err != nil || !strings.Contains(diffs["new.go"], "+package x") {
		t.Errorf("diffs = %q, %v", diffs, err)
	}
}

func TestBatchGroupsKeepsGroupsWhole(t *testing.T) {
	var groups [][]string
	for i := range 1000 {
		groups = append(groups, []string{fmt.Sprintf("dir/file%04d.go", i), fmt.Sprintf("dir/orig%04d.go", i)})
	}
	batches := batchGroups(groups)
	total := 0
	for _, b := range batches {
		size := 0
		for _, p := range b {
			size += len(p) + 1
		}
		if len(b) > diffBatchPaths || size > diffBatchBytes || len(b)%2 != 0 {
			t.Errorf("batch of %d paths, %d bytes", len(b), size)
		}
		total += len(b)
	}
	if total != 2000 || len(batches) < 5 {
		t.Errorf("%d paths in %d batches", total, len(batches))
	}
}

func TestSectionPath(t *testing.T) {
	for section, want := range map[string]string{
		"diff --git a/x.bin b/x.bin\nBinary files a/x.bin and b/x.bin differ\n":               "x.bin",
		"diff --git a/a b/b\nsimilarity index 100%\nrename from a\nrename to b\n":             "b",
		"diff --git \"a/t\\tb\" \"b/t\\tb\"\n--- \"a/t\\tb\"\n+++ \"b/t\\tb\"\n@@ -1 +1 @@\n": "t\tb",
		"diff --git a/d b/d\n--- a/d\n+++ /dev/null\n@@ -1 +0,0 @@\n-x\n":                     "d",
		"diff --git a/sp ace b/sp ace\n--- a/sp ace\t\n+++ b/sp ace\t\n@@ -1 +1 @@\n":         "sp ace",
	} {
		if got := sectionPath(section); got != want {
			t.Errorf("sectionPath(%q) = %q, want %q", section, got, want)
		}
	}
}

func keys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
