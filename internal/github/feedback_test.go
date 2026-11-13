package github

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDetailsWithPatches(t *testing.T) {
	fx := newFixture(t)
	d, patches, err := fx.c.DetailsWithPatches(ctx(), fx.dir, 7, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(patches) != 2 || !strings.HasPrefix(patches["main.go"], "@@ -1,5 +1,7 @@") || !strings.Contains(patches["docs/rate-limits.md"], "+# Rate limits") {
		t.Errorf("patches = %q", patches)
	}
	// The review's Markdown source stays in Go for the agent draft.
	if len(d.Reviews) != 1 || d.Reviews[0].Body != "Please fix the *lint*." || d.Reviews[0].State != "CHANGES_REQUESTED" {
		t.Fatalf("reviews = %+v", d.Reviews)
	}
	if raw, _ := json.Marshal(d.Reviews[0]); strings.Contains(string(raw), "*lint*") {
		t.Errorf("the review's source is sent to the web view: %s", raw)
	}
	before := len(fx.fake.Requests())
	if _, _, err := fx.c.DetailsWithPatches(ctx(), fx.dir, 7, false); err != nil || len(fx.fake.Requests()) != before {
		t.Errorf("a second read within the cache's life asked GitHub again (%v)", err)
	}
	if d.LocalBranch() != "feat/rate-limit" {
		t.Errorf("local branch = %q", d.LocalBranch())
	}
	fork, err := fx.c.Details(ctx(), fx.dir, 8, false)
	if err != nil || fork.LocalBranch() != "bob/main" {
		t.Errorf("a fork's local branch = %q (%v)", fork.LocalBranch(), err)
	}
	if _, _, err := fx.c.DetailsWithPatches(ctx(), fx.dir, 99, false); err == nil {
		t.Error("missing pull request loaded")
	}
}
