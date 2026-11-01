package github

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const thread1 = "PRRT_kwDOAAABc84AAAAB"

func TestReply(t *testing.T) {
	fx := newFixture(t)
	if err := fx.c.Reply(ctx(), fx.dir, 7, thread1, "Done, thanks"); err != nil {
		t.Fatal(err)
	}
	if got := fx.fake.Mutations(); !slices.Equal(got, []string{"addPullRequestReviewThreadReply"}) {
		t.Fatalf("mutations = %v", got)
	}
	// The write dropped the cache: the reply shows up.
	d, err := fx.c.Details(ctx(), fx.dir, 7, false)
	if err != nil {
		t.Fatal(err)
	}
	last := d.Threads[0].Comments[len(d.Threads[0].Comments)-1]
	if last.Body != "Done, thanks" || last.Author != "octocat" {
		t.Errorf("last comment = %+v", last)
	}
	// User text travels in the JSON body.
	var sawBody bool
	for _, r := range fx.fake.Requests() {
		if strings.Contains(r.Body, "Done, thanks") {
			var req struct{ Variables map[string]any }
			sawBody = json.Unmarshal([]byte(r.Body), &req) == nil && req.Variables["body"] == "Done, thanks"
		}
		if strings.Contains(r.Query, "Done") {
			t.Error("comment text in a URL")
		}
	}
	if !sawBody {
		t.Error("reply not sent as a GraphQL variable")
	}
}

func TestReplyValidates(t *testing.T) {
	fx := newFixture(t)
	if err := fx.c.Reply(ctx(), fx.dir, 7, thread1, "  "); err == nil {
		t.Error("empty reply accepted")
	}
	if err := fx.c.Reply(ctx(), fx.dir, 7, "PRRT_of_another_pr", "hi"); err == nil {
		t.Error("a thread of another pull request accepted")
	}
	if err := fx.c.SetResolved(ctx(), fx.dir, 7, "", true); err == nil {
		t.Error("empty thread id accepted")
	}
	if len(fx.fake.Mutations()) != 0 {
		t.Error("mutation sent for invalid input")
	}
}

func TestSetResolved(t *testing.T) {
	fx := newFixture(t)
	if err := fx.c.SetResolved(ctx(), fx.dir, 7, thread1, true); err != nil {
		t.Fatal(err)
	}
	if err := fx.c.SetResolved(ctx(), fx.dir, 7, thread1, false); err != nil {
		t.Fatal(err)
	}
	if got := fx.fake.Mutations(); !slices.Equal(got, []string{"resolveReviewThread", "unresolveReviewThread"}) {
		t.Fatalf("mutations = %v", got)
	}
}

func TestAddSingleComment(t *testing.T) {
	fx := newFixture(t)
	err := fx.c.AddComment(ctx(), fx.dir, 7, NewComment{Path: "main.go", Line: 6, StartLine: 5, Side: "RIGHT", Body: "nit"})
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	for _, r := range fx.fake.Requests() {
		if r.Method == "POST" && strings.HasSuffix(r.Path, "/pulls/7/comments") {
			_ = json.Unmarshal([]byte(r.Body), &body)
		}
	}
	if body["commit_id"] == "" || body["line"] != float64(6) || body["start_line"] != float64(5) || body["side"] != "RIGHT" {
		t.Fatalf("comment body = %v", body)
	}
}

func TestAddReviewCommentStartsReview(t *testing.T) {
	fx := newFixture(t)
	if err := fx.c.AddComment(ctx(), fx.dir, 7, NewComment{Path: "main.go", Line: 3, Side: "RIGHT", Body: "x", Review: true}); err != nil {
		t.Fatal(err)
	}
	d, _ := fx.c.Details(ctx(), fx.dir, 7, false)
	if d.PendingReviewID == "" || d.PendingReviewComments != 1 {
		t.Fatalf("pending review = %q (%d)", d.PendingReviewID, d.PendingReviewComments)
	}
	// The second comment goes into the same review.
	if err := fx.c.AddComment(ctx(), fx.dir, 7, NewComment{Path: "main.go", Line: 4, Side: "RIGHT", Body: "y", Review: true}); err != nil {
		t.Fatal(err)
	}
	want := []string{"addPullRequestReview", "addPullRequestReviewThread", "addPullRequestReviewThread"}
	if got := fx.fake.Mutations(); !slices.Equal(got, want) {
		t.Fatalf("mutations = %v", got)
	}
	if err := fx.c.SubmitReview(ctx(), fx.dir, 7, "REQUEST_CHANGES", ""); err != nil {
		t.Fatal(err)
	}
	if got := fx.fake.Mutations(); got[len(got)-1] != "submitPullRequestReview:REQUEST_CHANGES" {
		t.Fatalf("mutations = %v", got)
	}
}

func TestAddCommentValidation(t *testing.T) {
	fx := newFixture(t)
	for _, n := range []NewComment{
		{Path: "main.go", Line: 1, Side: "RIGHT"},
		{Path: "main.go", Line: 0, Side: "RIGHT", Body: "x"},
		{Path: "main.go", Line: 1, Side: "UP", Body: "x"},
		{Path: "main.go", Line: 1, StartLine: 2, Side: "LEFT", Body: "x"},
		{Path: "other.go", Line: 1, Side: "LEFT", Body: "x"},
		{Path: "main.go", Line: 1, Side: "LEFT", Body: strings.Repeat("x", maxBodyLen+1)},
	} {
		if err := fx.c.AddComment(ctx(), fx.dir, 7, n); err == nil {
			t.Errorf("%+v accepted", n)
		}
	}
}

func TestSubmitNewReview(t *testing.T) {
	fx := newFixture(t)
	if err := fx.c.SubmitReview(ctx(), fx.dir, 7, "COMMENT", " "); err == nil {
		t.Error("empty comment review accepted")
	}
	if err := fx.c.SubmitReview(ctx(), fx.dir, 7, "MERGE", "x"); err == nil {
		t.Error("unknown event accepted")
	}
	if err := fx.c.SubmitReview(ctx(), fx.dir, 7, "APPROVE", ""); err != nil {
		t.Fatal(err)
	}
	if got := fx.fake.Mutations(); !slices.Equal(got, []string{"addPullRequestReview:APPROVE"}) {
		t.Fatalf("mutations = %v", got)
	}
}

func TestDiscardReview(t *testing.T) {
	fx := newFixture(t)
	if err := fx.c.DiscardReview(ctx(), fx.dir, 7); err == nil {
		t.Error("discarded a review that doesn't exist")
	}
	_ = fx.c.AddComment(ctx(), fx.dir, 7, NewComment{Path: "main.go", Line: 3, Side: "RIGHT", Body: "x", Review: true})
	if err := fx.c.DiscardReview(ctx(), fx.dir, 7); err != nil {
		t.Fatal(err)
	}
	if got := fx.fake.Mutations(); got[len(got)-1] != "deletePullRequestReview" {
		t.Fatalf("mutations = %v", got)
	}
}

func TestCheckoutRefusesDirtyWorktree(t *testing.T) {
	fx := newFixture(t)
	if err := fx.c.Checkout(ctx(), fx.dir, 7, []string{"a.go"}); err == nil || !strings.Contains(err.Error(), "a.go") {
		t.Fatalf("err = %v", err)
	}
	if len(fx.fake.Requests()) != 0 {
		t.Error("GitHub asked before refusing")
	}
}

// checkoutFixture serves refs/pull/<n>/head from a local bare repository that
// stands in for the fake's acme/widgets (git's url.<base>.insteadOf).
func checkoutFixture(t *testing.T) (fixture, string) {
	fx := newFixture(t)
	bare := filepath.Join(t.TempDir(), "widgets.git")
	runGit(t, t.TempDir(), "init", "-q", "--bare", "-b", "main", bare)
	runGit(t, fx.dir, "config", "url."+bare+".insteadOf", fx.fake.RepoURL()+".git")
	runGit(t, fx.dir, "commit", "-q", "--allow-empty", "-m", "base")
	runGit(t, fx.dir, "push", "-q", "origin", "main")
	runGit(t, fx.dir, "switch", "-q", "-c", "tmp")
	runGit(t, fx.dir, "commit", "-q", "--allow-empty", "-m", "pr")
	head := runGit(t, fx.dir, "rev-parse", "HEAD")
	runGit(t, fx.dir, "push", "-q", "origin", "HEAD:refs/pull/7/head", "HEAD:refs/pull/8/head")
	runGit(t, fx.dir, "switch", "-q", "main")
	runGit(t, fx.dir, "branch", "-q", "-D", "tmp")
	return fx, head
}

func TestCheckout(t *testing.T) {
	fx, head := checkoutFixture(t)
	if err := fx.c.Checkout(ctx(), fx.dir, 7, nil); err != nil {
		t.Fatal(err)
	}
	if got := runGit(t, fx.dir, "branch", "--show-current"); got != "feat/rate-limit" {
		t.Errorf("branch = %s", got)
	}
	if got := runGit(t, fx.dir, "rev-parse", "HEAD"); got != head {
		t.Errorf("HEAD = %s, want %s", got, head)
	}
	if got := runGit(t, fx.dir, "config", "branch.feat/rate-limit.merge"); got != "refs/heads/feat/rate-limit" {
		t.Errorf("upstream = %s", got)
	}
}

func TestCheckoutForkUsesOwnerPrefix(t *testing.T) {
	fx, _ := checkoutFixture(t)
	if err := fx.c.Checkout(ctx(), fx.dir, 8, nil); err != nil {
		t.Fatal(err)
	}
	// #8 is bob's main: it must not move the local main.
	if got := runGit(t, fx.dir, "branch", "--show-current"); got != "bob/main" {
		t.Errorf("branch = %s", got)
	}
}
