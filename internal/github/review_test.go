package github

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// gqlRequest decodes the GraphQL request a call sent on stdin.
func gqlRequest(t *testing.T, cl call) (string, map[string]any) {
	t.Helper()
	if cl.String() != "api graphql --input -" {
		t.Fatalf("call %v is not a GraphQL request", cl)
	}
	var req struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	if err := json.Unmarshal([]byte(cl.stdin), &req); err != nil {
		t.Fatal(err)
	}
	return req.Query, req.Variables
}

// loaded returns a fake with #7 loaded (details cached) and the read calls forgotten,
// so a test sees exactly the calls its mutation makes.
func loaded(t *testing.T, f *fakeGH) (*Client, string) {
	t.Helper()
	c, dir := f.client(), t.TempDir()
	if _, err := c.Details(context.Background(), dir, 7, false); err != nil {
		t.Fatal(err)
	}
	f.reset()
	return c, dir
}

func TestReply(t *testing.T) {
	f := prFake(t)
	f.on("addPullRequestReviewThreadReply", `{"data":{"addPullRequestReviewThreadReply":{"comment":{"id":"C_9"}}}}`)
	c, dir := loaded(t, f)
	if err := c.Reply(context.Background(), dir, 7, "T_1", "Thanks, --fixed @file"); err != nil {
		t.Fatal(err)
	}
	calls := f.calls()
	if len(calls) != 1 {
		t.Fatalf("calls = %v", calls)
	}
	query, vars := gqlRequest(t, calls[0])
	if query != replyMutation || !reflect.DeepEqual(vars, map[string]any{"thread": "T_1", "body": "Thanks, --fixed @file"}) {
		t.Fatalf("request = %s %v", query, vars)
	}
	// The pull request is refetched afterwards.
	if _, err := c.Details(context.Background(), dir, 7, false); err != nil {
		t.Fatal(err)
	}
	if !hasCall(f, "pr view 7 --repo acme/flint --json "+viewFields) {
		t.Error("a reply must drop the cached pull request")
	}
}

func TestReplyRejectsEmpty(t *testing.T) {
	f := prFake(t)
	c, dir := loaded(t, f)
	if err := c.Reply(context.Background(), dir, 7, "T_1", "  \n"); err == nil {
		t.Fatal("empty reply accepted")
	}
	if n := len(f.calls()); n != 0 {
		t.Fatalf("an empty reply made %d gh calls", n)
	}
}

func TestSetResolved(t *testing.T) {
	f := prFake(t)
	f.on("unresolveReviewThread", `{"data":{"unresolveReviewThread":{"thread":{"id":"T_2","isResolved":false}}}}`)
	f.on("resolveReviewThread", `{"data":{"resolveReviewThread":{"thread":{"id":"T_1","isResolved":true}}}}`)
	c, dir := loaded(t, f)
	if err := c.SetResolved(context.Background(), dir, 7, "T_1", true); err != nil {
		t.Fatal(err)
	}
	if err := c.SetResolved(context.Background(), dir, 7, "T_2", false); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, cl := range f.calls() {
		if cl.String() != "api graphql --input -" || strings.Contains(cl.stdin, "reviewThreads(first") {
			continue // refetch of the pull request between the two mutations
		}
		query, vars := gqlRequest(t, cl)
		got = append(got, query+" "+vars["thread"].(string))
	}
	want := []string{resolveMutation + " T_1", unresolveMutation + " T_2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mutations = %q", got)
	}
}

func TestAddSingleComment(t *testing.T) {
	f := prFake(t)
	f.on("pulls/7/comments", `{"id":1}`)
	c, dir := loaded(t, f)
	err := c.AddComment(context.Background(), dir, 7, NewComment{Path: "main.go", Line: 6, StartLine: 5, Side: "RIGHT", Body: "nit"})
	if err != nil {
		t.Fatal(err)
	}
	calls := f.calls()
	if len(calls) != 1 || calls[0].String() != "api --method POST repos/acme/flint/pulls/7/comments --input -" {
		t.Fatalf("calls = %v", calls)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(calls[0].stdin), &body); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"body": "nit", "commit_id": strings.Repeat("h", 40), "path": "main.go", "line": float64(6), "side": "RIGHT",
		"start_line": float64(5), "start_side": "RIGHT",
	}
	if !reflect.DeepEqual(body, want) {
		t.Fatalf("body = %v", body)
	}
}

func TestAddReviewCommentToPendingReview(t *testing.T) {
	f := prFake(t) // the fixture has a pending review PRR_pending
	f.on("addPullRequestReviewThread(", `{"data":{"addPullRequestReviewThread":{"thread":{"id":"T_9"}}}}`)
	c, dir := loaded(t, f)
	if err := c.AddComment(context.Background(), dir, 7, NewComment{Path: "main.go", Line: 2, Side: "LEFT", Body: "why?", Review: true}); err != nil {
		t.Fatal(err)
	}
	calls := f.calls()
	if len(calls) != 1 {
		t.Fatalf("calls = %v", calls)
	}
	query, vars := gqlRequest(t, calls[0])
	want := map[string]any{"review": "PRR_pending", "path": "main.go", "line": float64(2), "side": "LEFT", "body": "why?", "startLine": nil, "startSide": nil}
	if query != addThreadMutation || !reflect.DeepEqual(vars, want) {
		t.Fatalf("request = %s %v", query, vars)
	}
}

func TestAddReviewCommentStartsReview(t *testing.T) {
	f := newFake(t)
	f.on("repo view", repoJSON)
	f.onFixture("pr view 7", "pr_view.json")
	f.onFixture("pulls/7/files", "pr_files.json")
	f.on("reviewThreads(first", `{"data":{"repository":{"pullRequest":{"id":"PR_7","reviews":{"nodes":[]},"reviewThreads":{"pageInfo":{"hasNextPage":false},"nodes":[]}}}}}`)
	f.on("addPullRequestReviewThread(", `{"data":{"addPullRequestReviewThread":{"thread":{"id":"T_9"}}}}`)
	f.on("addPullRequestReview(", `{"data":{"addPullRequestReview":{"pullRequestReview":{"id":"PRR_new"}}}}`)
	c, dir := loaded(t, f)
	if err := c.AddComment(context.Background(), dir, 7, NewComment{Path: "main.go", Line: 6, Side: "RIGHT", Body: "hm", Review: true}); err != nil {
		t.Fatal(err)
	}
	calls := f.calls()
	if len(calls) != 2 {
		t.Fatalf("calls = %v", calls)
	}
	q1, v1 := gqlRequest(t, calls[0])
	q2, v2 := gqlRequest(t, calls[1])
	if q1 != startReviewMutation || !reflect.DeepEqual(v1, map[string]any{"pr": "PR_7", "commit": strings.Repeat("h", 40)}) {
		t.Fatalf("first request = %s %v", q1, v1)
	}
	if q2 != addThreadMutation || v2["review"] != "PRR_new" {
		t.Fatalf("second request = %s %v", q2, v2)
	}
}

func TestAddCommentValidation(t *testing.T) {
	f := prFake(t)
	c, dir := loaded(t, f)
	for _, n := range []NewComment{
		{Path: "main.go", Line: 1, Side: "RIGHT", Body: " "},
		{Path: "main.go", Line: 0, Side: "RIGHT", Body: "x"},
		{Path: "main.go", Line: 1, Side: "MIDDLE", Body: "x"},
		{Path: "main.go", Line: 1, StartLine: 3, Side: "LEFT", Body: "x"},
	} {
		if err := c.AddComment(context.Background(), dir, 7, n); err == nil {
			t.Errorf("AddComment(%+v) accepted", n)
		}
	}
	if n := len(f.calls()); n != 0 {
		t.Fatalf("invalid comments made %d gh calls", n)
	}
}

func TestSubmitReview(t *testing.T) {
	f := prFake(t)
	f.on("submitPullRequestReview", `{"data":{"submitPullRequestReview":{"pullRequestReview":{"id":"PRR_pending","state":"APPROVED"}}}}`)
	c, dir := loaded(t, f)
	if err := c.SubmitReview(context.Background(), dir, 7, "APPROVE", ""); err != nil {
		t.Fatal(err)
	}
	query, vars := gqlRequest(t, f.calls()[0])
	if query != submitPendingMutation || !reflect.DeepEqual(vars, map[string]any{"review": "PRR_pending", "event": "APPROVE", "body": nil}) {
		t.Fatalf("request = %s %v", query, vars)
	}
}

func TestSubmitNewReview(t *testing.T) {
	f := newFake(t)
	f.on("repo view", repoJSON)
	f.onFixture("pr view 7", "pr_view.json")
	f.onFixture("pulls/7/files", "pr_files.json")
	f.on("reviewThreads(first", `{"data":{"repository":{"pullRequest":{"id":"PR_7","reviews":{"nodes":[]},"reviewThreads":{"pageInfo":{"hasNextPage":false},"nodes":[]}}}}}`)
	f.on("addPullRequestReview(", `{"data":{"addPullRequestReview":{"pullRequestReview":{"id":"PRR_1","state":"CHANGES_REQUESTED"}}}}`)
	c, dir := loaded(t, f)

	// Without pending comments, "request changes" needs a summary: no call at all.
	if err := c.SubmitReview(context.Background(), dir, 7, "REQUEST_CHANGES", ""); err == nil {
		t.Fatal("empty request-changes review accepted")
	}
	if err := c.SubmitReview(context.Background(), dir, 7, "MERGE", "x"); err == nil {
		t.Fatal("unknown event accepted")
	}
	if n := len(f.calls()); n != 0 {
		t.Fatalf("rejected reviews made %d gh calls", n)
	}
	if err := c.SubmitReview(context.Background(), dir, 7, "REQUEST_CHANGES", "Needs tests"); err != nil {
		t.Fatal(err)
	}
	query, vars := gqlRequest(t, f.calls()[0])
	if query != submitNewMutation || !reflect.DeepEqual(vars, map[string]any{"pr": "PR_7", "event": "REQUEST_CHANGES", "body": "Needs tests"}) {
		t.Fatalf("request = %s %v", query, vars)
	}
}

func TestDiscardReview(t *testing.T) {
	f := prFake(t)
	f.on("deletePullRequestReview", `{"data":{"deletePullRequestReview":{"pullRequestReview":{"id":"PRR_pending"}}}}`)
	c, dir := loaded(t, f)
	if err := c.DiscardReview(context.Background(), dir, 7); err != nil {
		t.Fatal(err)
	}
	query, vars := gqlRequest(t, f.calls()[0])
	if query != discardMutation || vars["review"] != "PRR_pending" {
		t.Fatalf("request = %s %v", query, vars)
	}
}

func TestMutationErrorsPassThrough(t *testing.T) {
	f := prFake(t)
	f.onExit("resolveReviewThread", `{"data":null,"errors":[{"message":"Resource not accessible by integration"}]}`,
		"gh: Resource not accessible by integration\n", 1)
	c, dir := loaded(t, f)
	err := c.SetResolved(context.Background(), dir, 7, "T_1", true)
	if err == nil || err.Error() != "Resource not accessible by integration" {
		t.Fatalf("err = %v", err)
	}
}

func TestCheckoutRefusesDirtyWorktree(t *testing.T) {
	f := prFake(t)
	err := f.client().Checkout(context.Background(), t.TempDir(), 7, []string{"main.go", "go.mod"})
	if err == nil || !strings.Contains(err.Error(), "2 changed file(s)") || !strings.Contains(err.Error(), "main.go") {
		t.Fatalf("err = %v", err)
	}
	if n := len(f.calls()); n != 0 {
		t.Fatalf("a dirty checkout made %d gh calls", n)
	}
}

func TestCheckout(t *testing.T) {
	f := prFake(t)
	f.on("pr checkout 7", "")
	if err := f.client().Checkout(context.Background(), t.TempDir(), 7, nil); err != nil {
		t.Fatal(err)
	}
	if !hasCall(f, "pr checkout 7 --repo acme/flint") {
		t.Fatalf("calls = %v", f.calls())
	}
}
