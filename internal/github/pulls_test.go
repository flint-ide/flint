package github

import (
	"strings"
	"testing"
)

func TestListRequest(t *testing.T) {
	repo := Repo{Owner: "acme", Name: "widgets", NameWithOwner: "acme/widgets"}
	tests := []struct {
		f      ListFilter
		query  string
		states []string
		search string
	}{
		{ListFilter{}, "pullRequests(", []string{"OPEN"}, ""},
		{ListFilter{Who: "all", State: "closed"}, "pullRequests(", []string{"CLOSED", "MERGED"}, ""},
		{ListFilter{Who: "all", State: "all"}, "pullRequests(", []string{"OPEN", "CLOSED", "MERGED"}, ""},
		{ListFilter{Who: "mine", State: "open"}, "search(", nil, "repo:acme/widgets is:pr is:open author:@me sort:created-desc"},
		{ListFilter{Who: "review", State: "merged"}, "search(", nil, "repo:acme/widgets is:pr is:merged review-requested:@me sort:created-desc"},
	}
	for _, tt := range tests {
		q, vars, err := listRequest(repo, tt.f)
		if err != nil || !strings.Contains(q, tt.query) {
			t.Fatalf("%+v: %v", tt.f, err)
		}
		if tt.search != "" && vars["q"] != tt.search {
			t.Errorf("%+v: q = %v", tt.f, vars["q"])
		}
		if tt.states != nil && strings.Join(vars["states"].([]string), ",") != strings.Join(tt.states, ",") {
			t.Errorf("%+v: states = %v", tt.f, vars["states"])
		}
	}
	for _, bad := range []ListFilter{{Who: "x"}, {State: "draft"}} {
		if _, _, err := listRequest(repo, bad); err == nil {
			t.Errorf("%+v accepted", bad)
		}
	}
}

func TestList(t *testing.T) {
	fx := newFixture(t)
	prs, err := fx.c.List(ctx(), fx.dir, ListFilter{Who: "all", State: "open"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 2 {
		t.Fatalf("prs = %+v", prs)
	}
	p := prs[0]
	if p.Number != 7 || p.Author != "alice" || p.HeadRef != "feat/rate-limit" || p.ReviewDecision != "CHANGES_REQUESTED" {
		t.Errorf("row = %+v", p)
	}
	if p.Checks != (CheckCounts{Success: 1, Failure: 1, Pending: 1}) {
		t.Errorf("checks = %+v", p.Checks)
	}
	if !prs[1].Draft || prs[1].Checks != (CheckCounts{}) {
		t.Errorf("second row = %+v", prs[1])
	}
	review, err := fx.c.List(ctx(), fx.dir, ListFilter{Who: "review", State: "open"}, false)
	if err != nil || len(review) != 1 || review[0].Number != 7 {
		t.Errorf("review requested = %+v, %v", review, err)
	}
	// Cached for the same filter; force asks again.
	n := len(fx.fake.Requests())
	_, _ = fx.c.List(ctx(), fx.dir, ListFilter{Who: "all", State: "open"}, false)
	if len(fx.fake.Requests()) != n {
		t.Error("list not cached")
	}
	_, _ = fx.c.List(ctx(), fx.dir, ListFilter{Who: "all", State: "open"}, true)
	if len(fx.fake.Requests()) == n {
		t.Error("force did not refetch")
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
	fx := newFixture(t)
	d, err := fx.c.Details(ctx(), fx.dir, 7, false)
	if err != nil {
		t.Fatal(err)
	}
	if d.ID == "" || d.Title != "Add rate limiting" || d.HeadOID == "" || d.CrossRepository || d.Repo.NameWithOwner != "acme/widgets" {
		t.Errorf("details = %+v", d)
	}
	if strings.Contains(d.BodyHTML, "<script") || !strings.Contains(d.BodyHTML, "token bucket") {
		t.Errorf("body not sanitized: %q", d.BodyHTML)
	}
	if len(d.Files) != 2 || d.Files[0].Path != "main.go" || d.Files[1].Status != "added" {
		t.Errorf("files = %+v", d.Files)
	}
	if len(d.Commits) != 1 || d.Commits[0].Author != "alice" {
		t.Errorf("commits = %+v", d.Commits)
	}
	if len(d.Reviews) != 1 || d.Reviews[0].Author != "dave" {
		t.Errorf("reviews (empty COMMENTED dropped) = %+v", d.Reviews)
	}
	if len(d.Labels) != 1 || len(d.Checks) != 3 {
		t.Errorf("labels %v checks %+v", d.Labels, d.Checks)
	}
	var lint Check
	for _, c := range d.Checks {
		if c.State == "failure" {
			lint = c
		}
	}
	if lint.RunID != 4242 || lint.JobID != 5151 || lint.Workflow != "CI" {
		t.Errorf("failed check = %+v", lint)
	}
	if len(d.Threads) != 2 || d.Threads[0].Line != 6 || d.Threads[1].Line != 0 || !d.Threads[1].Outdated || d.Threads[1].ResolvedBy != "alice" {
		t.Errorf("threads = %+v", d.Threads)
	}
	fork, err := fx.c.Details(ctx(), fx.dir, 8, false)
	if err != nil || !fork.CrossRepository || fork.HeadOwner != "bob" {
		t.Errorf("fork PR = %+v, %v", fork, err)
	}
}

func TestDetailsMissingPullRequest(t *testing.T) {
	fx := newFixture(t)
	if _, err := fx.c.Details(ctx(), fx.dir, 99, false); err == nil {
		t.Fatal("missing pull request loaded")
	}
	if _, err := fx.c.Details(ctx(), fx.dir, 0, false); err == nil {
		t.Fatal("pull request 0 loaded")
	}
}
