package git

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestStashPushListApplyDrop(t *testing.T) {
	ctx := context.Background()
	r := newRepo(t)

	if err := r.StashPush(ctx, "nothing", false); !errors.Is(err, ErrNoLocalChanges) {
		t.Fatalf("clean tree: err = %v, want ErrNoLocalChanges", err)
	}
	if list, err := r.Stashes(ctx); err != nil || len(list) != 0 {
		t.Fatalf("Stashes = %v, %v", list, err)
	}

	writeFile(t, r.Root, "README.md", "changed\n")
	writeFile(t, r.Root, "new.txt", "untracked\n")
	if err := r.StashPush(ctx, "  first  ", false); err != nil {
		t.Fatal(err)
	}
	if readFile(t, r.Root, "README.md") != "hello\n" || !fileExists(r.Root, "new.txt") {
		t.Fatal("tracked change should be stashed, untracked file left alone")
	}
	if err := r.StashPush(ctx, "second", true); err != nil {
		t.Fatal(err)
	}
	if fileExists(r.Root, "new.txt") {
		t.Fatal("untracked file should be stashed with includeUntracked")
	}

	list, err := r.Stashes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("Stashes = %+v", list)
	}
	second, first := list[0], list[1]
	if !strings.HasSuffix(second.Message, ": second") || second.Index != 0 || !second.Untracked || second.Time == 0 {
		t.Errorf("newest = %+v", second)
	}
	if !strings.HasSuffix(first.Message, ": first") || first.Index != 1 || first.Untracked {
		t.Errorf("oldest = %+v", first)
	}

	files, err := r.StashFiles(ctx, second.Hash)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "new.txt" || files[0].Kind != Untracked {
		t.Errorf("StashFiles(second) = %+v", files)
	}
	files, err = r.StashFiles(ctx, first.Hash)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "README.md" || files[0].Kind != Modified {
		t.Errorf("StashFiles(first) = %+v", files)
	}
	if rev, err := r.FileAt(ctx, second.Hash+"^3", "new.txt"); err != nil || rev.Content != "untracked\n" {
		t.Errorf("untracked content = %+v, %v", rev, err)
	}

	// Addressed by hash: stash@{1} is resolved right before running.
	if err := r.StashApply(ctx, first.Hash); err != nil {
		t.Fatal(err)
	}
	if readFile(t, r.Root, "README.md") != "changed\n" {
		t.Error("apply should restore the change")
	}
	if list, _ := r.Stashes(ctx); len(list) != 2 {
		t.Errorf("apply must keep the stash, have %d", len(list))
	}
	mustGit(t, r.Root, "checkout", "--", "README.md")

	if err := r.StashDrop(ctx, first.Hash); err != nil {
		t.Fatal(err)
	}
	list, _ = r.Stashes(ctx)
	if len(list) != 1 || list[0].Hash != second.Hash {
		t.Fatalf("after drop = %+v", list)
	}
	if err := r.StashDrop(ctx, first.Hash); err == nil || !strings.Contains(err.Error(), "no longer exists") {
		t.Errorf("dropping a gone stash: err = %v", err)
	}
}

func TestStashPop(t *testing.T) {
	ctx := context.Background()
	r := newRepo(t)
	writeFile(t, r.Root, "README.md", "changed\n")
	if err := r.StashPush(ctx, "", false); err != nil {
		t.Fatal(err)
	}
	list, err := r.Stashes(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("Stashes = %v, %v", list, err)
	}
	if err := r.StashPop(ctx, list[0].Hash); err != nil {
		t.Fatal(err)
	}
	if readFile(t, r.Root, "README.md") != "changed\n" {
		t.Error("pop should restore the change")
	}
	if list, _ := r.Stashes(ctx); len(list) != 0 {
		t.Errorf("pop should drop the stash: %+v", list)
	}
}

func TestStashPopConflictKeepsStash(t *testing.T) {
	ctx := context.Background()
	r := newRepo(t)
	writeFile(t, r.Root, "README.md", "stashed\n")
	if err := r.StashPush(ctx, "wip", false); err != nil {
		t.Fatal(err)
	}
	commitFile(t, r.Root, "README.md", "committed meanwhile\n", "conflicting")
	list, _ := r.Stashes(ctx)
	if err := r.StashPop(ctx, list[0].Hash); err == nil {
		t.Fatal("pop onto a conflicting change should fail")
	}
	if after, _ := r.Stashes(ctx); len(after) != 1 {
		t.Errorf("a conflicting pop keeps the stash, have %d", len(after))
	}
}

func TestStashRefRejectsEmpty(t *testing.T) {
	r := newRepo(t)
	if _, err := r.stashRef(context.Background(), ""); err == nil {
		t.Error("empty hash should be rejected")
	}
}
