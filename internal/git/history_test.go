package git

import (
	"context"
	"reflect"
	"testing"
)

func TestFileHistory(t *testing.T) {
	ctx := context.Background()
	r := newEmptyRepo(t)
	c1 := commitFile(t, r.Root, "f.txt", "a\nb\nc\n", "first")
	commitFile(t, r.Root, "other.txt", "o", "unrelated")
	mustGit(t, r.Root, "mv", "f.txt", "g.txt")
	c3 := commitAll(t, r.Root, "rename")
	c4 := commitFile(t, r.Root, "g.txt", "a\nB\nc\n", "edit")

	hist, err := r.FileHistory(ctx, "g.txt", 0)
	if err != nil {
		t.Fatal(err)
	}
	hashes := make([]string, len(hist))
	for i, h := range hist {
		hashes[i] = h.Commit.Hash
	}
	if want := []string{c4, c3, c1}; !reflect.DeepEqual(hashes, want) {
		t.Fatalf("hashes = %v, want %v", hashes, want)
	}
	want := []FileCommit{
		{Path: "g.txt", Kind: Modified},
		{Path: "g.txt", OrigPath: "f.txt", Kind: Renamed},
		{Path: "f.txt", Kind: Added},
	}
	for i, w := range want {
		got := hist[i]
		if got.Path != w.Path || got.OrigPath != w.OrigPath || got.Kind != w.Kind {
			t.Errorf("entry %d = %s %q (from %q), want %s %q (from %q)", i, got.Kind, got.Path, got.OrigPath, w.Kind, w.Path, w.OrigPath)
		}
	}
	if hist[0].Commit.Subject != "edit" || hist[0].Commit.Author != "Test User" || hist[0].Commit.Time == 0 {
		t.Errorf("commit = %+v", hist[0].Commit)
	}

	if limited, err := r.FileHistory(ctx, "g.txt", 1); err != nil || len(limited) != 1 {
		t.Errorf("limit 1: %d entries, %v", len(limited), err)
	}
	if none, err := r.FileHistory(ctx, "missing.txt", 0); err != nil || len(none) != 0 {
		t.Errorf("missing file: %v, %v", none, err)
	}
	if none, err := newEmptyRepo(t).FileHistory(ctx, "a.txt", 0); err != nil || len(none) != 0 {
		t.Errorf("no commits: %v, %v", none, err)
	}
}

func TestParseFileHistoryMergeKeepsPath(t *testing.T) {
	out := "\x1eaaa\x1f\x1fA\x1fa@x\x1f1\x1f\x1fmerge\x00" +
		"\x1ebbb\x1f\x1fA\x1fa@x\x1f1\x1f\x1frename\x00\nR100\x00old.txt\x00new.txt\x00" +
		"\x1eccc\x1f\x1fA\x1fa@x\x1f1\x1f\x1fmerge2\x00"
	hist := parseFileHistory(out, "new.txt")
	if len(hist) != 3 {
		t.Fatalf("len = %d", len(hist))
	}
	if hist[0].Path != "new.txt" || hist[1].OrigPath != "old.txt" || hist[2].Path != "old.txt" {
		t.Errorf("paths = %+v", hist)
	}
}
