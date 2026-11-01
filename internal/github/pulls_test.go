package github

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// prFake answers the read-only calls for pull request #7 of acme/flint.
func prFake(t *testing.T) *fakeGH {
	t.Helper()
	f := newFake(t)
	f.on("repo view", repoJSON)
	f.onFixture("pr list", "pr_list.json")
	f.onFixture("pr view 7", "pr_view.json")
	f.onFixture("pulls/7/files", "pr_files.json")
	f.onFixture("reviewThreads(first", "threads.json")
	return f
}

func TestListArgs(t *testing.T) {
	repo := Repo{NameWithOwner: "acme/flint"}
	tests := []struct {
		filter ListFilter
		want   []string // after --json …
		state  string
	}{
		{ListFilter{}, nil, "open"},
		{ListFilter{Who: "mine", State: "closed"}, []string{"--author", "@me"}, "closed"},
		{ListFilter{Who: "review", State: "all"}, []string{"--search", "review-requested:@me"}, "all"},
		{ListFilter{Who: "all", State: "merged"}, nil, "merged"},
	}
	for _, tt := range tests {
		args, err := listArgs(repo, tt.filter)
		if err != nil {
			t.Fatal(err)
		}
		i := slices.Index(args, "--json")
		if args[slices.Index(args, "--state")+1] != tt.state || !slices.Equal(args[i+2:], tt.want) {
			t.Errorf("listArgs(%+v) = %v", tt.filter, args)
		}
	}
	for _, bad := range []ListFilter{{Who: "nobody"}, {State: "draft"}} {
		if _, err := listArgs(repo, bad); err == nil {
			t.Errorf("listArgs(%+v) accepted an unknown filter", bad)
		}
	}
}

func TestList(t *testing.T) {
	f := prFake(t)
	c := f.client()
	dir := t.TempDir()
	prs, err := c.List(context.Background(), dir, ListFilter{Who: "mine"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 2 {
		t.Fatalf("got %d pull requests, want 2", len(prs))
	}
	want := PullRequest{
		Number: 7, Title: "Add x", Author: "alice", HeadRef: "feat/x", BaseRef: "main", State: "OPEN",
		ReviewDecision: "CHANGES_REQUESTED", Checks: CheckCounts{Success: 1, Failure: 1, Pending: 2},
		UpdatedAt: "2026-10-30T10:00:00Z", URL: "https://github.com/acme/flint/pull/7",
	}
	if prs[0] != want {
		t.Errorf("prs[0] = %+v\nwant %+v", prs[0], want)
	}
	if !prs[1].Draft || prs[1].Checks != (CheckCounts{}) {
		t.Errorf("prs[1] = %+v", prs[1])
	}

	calls := f.calls()
	wantArgs := []string{
		"pr", "list", "--repo", "acme/flint", "--state", "open", "--limit", "50",
		"--json", "number,title,author,headRefName,baseRefName,isDraft,state,reviewDecision,statusCheckRollup,updatedAt,url",
		"--author", "@me",
	}
	if len(calls) != 2 || calls[0].String() != "repo view --json name,nameWithOwner,owner,url,defaultBranchRef" || !slices.Equal(calls[1].args, wantArgs) {
		t.Fatalf("calls = %v", calls)
	}

	// Cached until forced.
	f.reset()
	if _, err := c.List(context.Background(), dir, ListFilter{Who: "mine"}, false); err != nil {
		t.Fatal(err)
	}
	if n := len(f.calls()); n != 0 {
		t.Fatalf("cached list made %d gh calls", n)
	}
	if _, err := c.List(context.Background(), dir, ListFilter{Who: "mine"}, true); err != nil {
		t.Fatal(err)
	}
	if n := len(f.calls()); n != 1 {
		t.Fatalf("forced list made %d gh calls, want 1", n)
	}
}

func TestCheckStates(t *testing.T) {
	tests := []struct {
		check ghCheck
		want  string
	}{
		{ghCheck{Status: "COMPLETED", Conclusion: "SUCCESS"}, "success"},
		{ghCheck{Status: "COMPLETED", Conclusion: "FAILURE"}, "failure"},
		{ghCheck{Status: "COMPLETED", Conclusion: "CANCELLED"}, "failure"},
		{ghCheck{Status: "COMPLETED", Conclusion: "TIMED_OUT"}, "failure"},
		{ghCheck{Status: "COMPLETED", Conclusion: "SKIPPED"}, "skipped"},
		{ghCheck{Status: "COMPLETED", Conclusion: "NEUTRAL"}, "neutral"},
		{ghCheck{Status: "QUEUED"}, "pending"},
		{ghCheck{Status: "IN_PROGRESS"}, "pending"},
		{ghCheck{Typename: "StatusContext", State: "SUCCESS"}, "success"},
		{ghCheck{Typename: "StatusContext", State: "EXPECTED"}, "pending"},
		{ghCheck{Typename: "StatusContext", State: "ERROR"}, "failure"},
	}
	for _, tt := range tests {
		if got := tt.check.check("").State; got != tt.want {
			t.Errorf("%+v → %q, want %q", tt.check, got, tt.want)
		}
	}
}

func TestDetails(t *testing.T) {
	f := prFake(t)
	d, err := f.client().Details(context.Background(), t.TempDir(), 7, false)
	if err != nil {
		t.Fatal(err)
	}
	if d.ID != "PR_7" || d.HeadOID != strings.Repeat("h", 40) || d.BaseOID != strings.Repeat("b", 40) || d.Repo.NameWithOwner != "acme/flint" {
		t.Errorf("identity: %+v", d)
	}
	if !strings.Contains(d.BodyHTML, "<strong>bug</strong>") || strings.Contains(d.BodyHTML, "<script") {
		t.Errorf("body must be rendered Markdown without raw HTML: %q", d.BodyHTML)
	}
	if !slices.Equal(d.Labels, []string{"bug"}) || len(d.Commits) != 1 || d.Commits[0].Author != "alice" {
		t.Errorf("labels/commits: %+v %+v", d.Labels, d.Commits)
	}
	wantFiles := []File{
		{Path: "main.go", Status: "modified", Additions: 3, Deletions: 1},
		{Path: "docs/new.md", Status: "added", Additions: 2},
		{Path: "logo.png", Status: "removed", NoPatch: true},
		{Path: "lib/b go.go", PreviousPath: "lib/a.go", Status: "renamed", NoPatch: true},
	}
	if !reflect.DeepEqual(d.Files, wantFiles) {
		t.Errorf("files (both pages) = %+v", d.Files)
	}
	states := []string{}
	for _, ch := range d.Checks {
		states = append(states, ch.Name+":"+ch.State)
	}
	if strings.Join(states, " ") != "build:success lint:failure e2e:pending deploy/preview:pending" || d.Checks[3].URL != "https://deploy.example/7" {
		t.Errorf("checks = %+v", d.Checks)
	}
	if len(d.Reviews) != 1 || d.Reviews[0].Author != "dave" || !strings.Contains(d.Reviews[0].BodyHTML, "<em>lint</em>") {
		t.Errorf("reviews (empty COMMENTED dropped) = %+v", d.Reviews)
	}
	if d.PendingReviewID != "PRR_pending" || d.PendingReviewComments != 1 {
		t.Errorf("pending review = %q (%d)", d.PendingReviewID, d.PendingReviewComments)
	}
	if len(d.Threads) != 2 {
		t.Fatalf("threads = %+v", d.Threads)
	}
	t1, t2 := d.Threads[0], d.Threads[1]
	if t1.Line != 6 || t1.Side != "RIGHT" || t1.Resolved || !t1.CanResolve || len(t1.Comments) != 2 ||
		!t1.Comments[1].Pending || t1.Comments[0].Pending || !strings.Contains(t1.Comments[0].BodyHTML, "<code>fmt</code>") {
		t.Errorf("thread 1 = %+v", t1)
	}
	if t2.Line != 0 || !t2.Outdated || t2.OriginalLine != 2 || !t2.Resolved || t2.ResolvedBy != "alice" || t2.Side != "LEFT" {
		t.Errorf("thread 2 (outdated) = %+v", t2)
	}

	// The three reads, and the GraphQL variables of the threads query.
	var view, files, threads bool
	for _, cl := range f.calls() {
		switch {
		case cl.String() == "pr view 7 --repo acme/flint --json "+viewFields:
			view = true
		case cl.String() == "api --paginate repos/acme/flint/pulls/7/files?per_page=100":
			files = true
		case cl.String() == "api graphql --input -":
			var req struct {
				Query     string         `json:"query"`
				Variables map[string]any `json:"variables"`
			}
			if err := json.Unmarshal([]byte(cl.stdin), &req); err != nil {
				t.Fatal(err)
			}
			want := map[string]any{"owner": "acme", "name": "flint", "number": float64(7), "cursor": nil}
			threads = req.Query == threadsQuery && reflect.DeepEqual(req.Variables, want)
		}
	}
	if !view || !files || !threads {
		t.Errorf("calls = %v (view %v, files %v, threads %v)", f.calls(), view, files, threads)
	}
}

func TestThreadsArePaged(t *testing.T) {
	f := newFake(t)
	f.on("repo view", repoJSON)
	page := func(id, cursor string, next bool) string {
		b, _ := json.Marshal(map[string]any{"data": map[string]any{"repository": map[string]any{"pullRequest": map[string]any{
			"id": "PR_7", "reviews": map[string]any{"nodes": []any{}},
			"reviewThreads": map[string]any{
				"pageInfo": map[string]any{"hasNextPage": next, "endCursor": cursor},
				"nodes":    []any{map[string]any{"id": id, "path": "a.go", "line": 1, "diffSide": "RIGHT", "comments": map[string]any{"nodes": []any{}}}},
			},
		}}}})
		return string(b)
	}
	// Rules match in order: the second page is asked for with the first page's cursor.
	f.on(`"cursor":"CUR1"`, page("T_2", "CUR2", false))
	f.on("reviewThreads(first", page("T_1", "CUR1", true))
	tp, err := f.client().threads(context.Background(), t.TempDir(), Repo{Owner: "acme", Name: "flint", NameWithOwner: "acme/flint"}, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(tp.Threads) != 2 || tp.Threads[0].ID != "T_1" || tp.Threads[1].ID != "T_2" {
		t.Fatalf("threads = %+v", tp.Threads)
	}
}

func TestDetailsMissingPullRequest(t *testing.T) {
	f := newFake(t)
	f.on("repo view", repoJSON)
	f.onExit("pr view 99", "", "GraphQL: Could not resolve to a PullRequest with the number of 99. (repository.pullRequest)\n", 1)
	f.on("pulls/99/files", "[]")
	f.on("reviewThreads(first", `{"data":{"repository":{"pullRequest":null}}}`)
	_, err := f.client().Details(context.Background(), t.TempDir(), 99, false)
	if err == nil || !strings.Contains(err.Error(), "Could not resolve to a PullRequest") {
		t.Fatalf("error = %v", err)
	}
}
