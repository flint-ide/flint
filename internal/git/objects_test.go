package git

import (
	"context"
	"strings"
	"testing"
)

func TestObjectAndMergeBase(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()
	base := mustGit(t, r.Root, "rev-parse", "HEAD")
	mustGit(t, r.Root, "switch", "-q", "-c", "feature")
	feature := commitFile(t, r.Root, "a.txt", "feature\n", "feature")
	mustGit(t, r.Root, "switch", "-q", "main")
	main := commitFile(t, r.Root, "b.txt", "main\n", "main")

	if got, ok := r.MergeBase(ctx, main, feature); !ok || got != base {
		t.Fatalf("MergeBase = %q, %v; want %q", got, ok, base)
	}
	if _, ok := r.MergeBase(ctx, main, strings.Repeat("e", 40)); ok {
		t.Fatal("MergeBase with an unknown commit must report !ok")
	}

	if b, ok := r.Object(ctx, feature+":a.txt"); !ok || string(b) != "feature\n" {
		t.Fatalf("Object(rev:path) = %q, %v", b, ok)
	}
	blob := mustGit(t, r.Root, "rev-parse", feature+":a.txt")
	if b, ok := r.Object(ctx, blob); !ok || string(b) != "feature\n" {
		t.Fatalf("Object(sha) = %q, %v", b, ok)
	}
	for _, missing := range []string{base + ":a.txt", strings.Repeat("e", 40), "nope:a.txt"} {
		if _, ok := r.Object(ctx, missing); ok {
			t.Errorf("Object(%q) reported a missing object as present", missing)
		}
	}
}
