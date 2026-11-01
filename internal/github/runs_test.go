package github

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestActionsIDs(t *testing.T) {
	tests := []struct {
		url      string
		run, job int64
	}{
		{"https://github.com/acme/flint/actions/runs/41307693761/job/124851367741", 41307693761, 124851367741},
		{"https://github.com/acme/flint/actions/runs/41307693761", 41307693761, 0},
		{"https://github.com/acme/flint/actions/runs/12/job/34?pr=1", 12, 34},
		{"https://ci.example.com/build/1", 0, 0},
		// Another repository's runs (a check can link anywhere), or look-alikes.
		{"https://github.com/evil/flint/actions/runs/12/job/34", 0, 0},
		{"https://github.com/acme/flint-fork/actions/runs/12/job/34", 0, 0},
		{"https://evil.example/https://github.com/acme/flint/actions/runs/12", 0, 0},
		{"https://github.com/acme/flint/actions/runs/12x", 0, 0},
		{"https://github.com/acme/flint/actions/runs/0/job/1", 0, 0},
		{"https://github.com/acme/flint/actions/runs/99999999999999999999/job/1", 0, 0},
		{"", 0, 0},
	}
	for _, tt := range tests {
		run, job := actionsIDs("https://github.com/acme/flint", tt.url)
		if run != tt.run || job != tt.job {
			t.Errorf("actionsIDs(%q) = %d, %d; want %d, %d", tt.url, run, job, tt.run, tt.job)
		}
	}
	if run, _ := actionsIDs("", "https://github.com/acme/flint/actions/runs/12"); run != 0 {
		t.Error("ids read without knowing the repository")
	}
}

func TestBranchChecks(t *testing.T) {
	f := newFake(t)
	f.on("repo view", repoJSON)
	f.onFixture("pr view", "pr_view_ci.json")
	c := f.client()
	got, err := c.BranchChecks(context.Background(), t.TempDir(), "feat/rate-limit")
	if err != nil {
		t.Fatal(err)
	}
	if got.PR == nil || got.PR.Number != 29 || got.PR.HeadSHA != "785d8f7a66d3403ed32421b45c87ede4fbda8bda" || got.PR.HeadRef != "feat/rate-limit" {
		t.Fatalf("PR = %+v", got.PR)
	}
	if got.Counts != (CheckCounts{Success: 2, Failure: 1, Pending: 1}) {
		t.Errorf("counts = %+v", got.Counts)
	}
	failed := got.Checks[0]
	if failed.State != "failure" || failed.RunID != 41307693761 || failed.JobID != 124851367741 || failed.Workflow != "CI" {
		t.Errorf("failed check = %+v", failed)
	}
	if got.Checks[3].RunID != 0 {
		t.Errorf("a commit status has no run: %+v", got.Checks[3])
	}
	calls := f.calls()
	last := calls[len(calls)-1].args
	want := []string{"pr", "view", "--repo=acme/flint", "--json=number,title,url,state,headRefName,headRefOid,isCrossRepository,statusCheckRollup", "--", "feat/rate-limit"}
	if !slices.Equal(last, want) {
		t.Errorf("args = %q, want %q", last, want)
	}
}

// gh pr view reads "123" as pull request #123: another branch's pull request
// must not be shown as this branch's.
func TestBranchChecksNumericBranch(t *testing.T) {
	f := newFake(t)
	f.on("repo view", repoJSON)
	f.onFixture("pr view", "pr_view_ci.json") // headRefName feat/rate-limit
	got, err := f.client().BranchChecks(context.Background(), t.TempDir(), "29")
	if err != nil || got.PR != nil || len(got.Checks) != 0 {
		t.Errorf("BranchChecks(\"29\") = %+v, %v; want no pull request", got, err)
	}
}

func TestBranchChecksCrossRepository(t *testing.T) {
	f := newFake(t)
	f.on("repo view", repoJSON)
	f.on("pr view", `{"number":3,"title":"t","url":"u","state":"OPEN","headRefName":"patch-1","headRefOid":"abc","isCrossRepository":true,"statusCheckRollup":[]}`)
	got, err := f.client().BranchChecks(context.Background(), t.TempDir(), "patch-1")
	if err != nil || got.PR == nil || !got.PR.CrossRepository {
		t.Errorf("BranchChecks = %+v, %v; want a cross-repository pull request", got.PR, err)
	}
}

func TestBranchChecksNoPullRequest(t *testing.T) {
	f := newFake(t)
	f.on("repo view", repoJSON)
	f.onExit("pr view", "", `no pull requests found for branch "wip"`, 1)
	got, err := f.client().BranchChecks(context.Background(), t.TempDir(), "wip")
	if err != nil || got.PR != nil || len(got.Checks) != 0 {
		t.Errorf("BranchChecks = %+v, %v; want no pull request", got, err)
	}
}

func TestBranchChecksRejectsBadBranches(t *testing.T) {
	f := newFake(t)
	f.on("repo view", repoJSON)
	c := f.client()
	for _, b := range []string{"", "-x", "--repo=evil/x", "a b", "a\nb", "a..b", strings.Repeat("a", 300)} {
		if _, err := c.BranchChecks(context.Background(), t.TempDir(), b); err == nil {
			t.Errorf("branch %q accepted", b)
		}
	}
	for _, call := range f.calls() {
		if call.args[0] == "pr" {
			t.Errorf("gh ran for a bad branch: %v", call)
		}
	}
}

func TestRun(t *testing.T) {
	f := newFake(t)
	f.on("repo view", repoJSON)
	f.onFixture("run view 41307693761 --repo=acme/flint --json", "run_view.json")
	run, err := f.client().Run(context.Background(), t.TempDir(), 41307693761)
	if err != nil {
		t.Fatal(err)
	}
	if run.ID != 41307693761 || run.Workflow != "CI" || run.Branch != "feat/rate-limit" || run.SHA == "" || run.Attempt != 1 || len(run.Jobs) != 2 {
		t.Fatalf("run = %+v", run)
	}
	j := run.Jobs[0]
	if j.ID != 124851367741 || j.Conclusion != "failure" || len(j.Steps) != 5 {
		t.Fatalf("job = %+v", j)
	}
	if s := j.FailedStep(); s == nil || s.Name != "go test" || s.Number != 12 {
		t.Errorf("failed step = %+v", s)
	}
	if s := run.Jobs[1].FailedStep(); s != nil {
		t.Errorf("a passing job has failed step %+v", s)
	}
}

func TestFailedLog(t *testing.T) {
	f := newFake(t)
	f.on("repo view", repoJSON)
	f.onFixture("--log-failed", "log_failed.txt")
	c := f.client()
	dir := t.TempDir()
	log, cut, err := c.FailedLog(context.Background(), dir, 41307693761, 0)
	if err != nil || cut {
		t.Fatal(cut, err)
	}
	want, _ := os.ReadFile(filepath.Join("testdata", "log_failed.txt"))
	if log != string(want) {
		t.Errorf("log differs:\n%s", log)
	}
	if _, _, err := c.FailedLog(context.Background(), dir, 41307693761, 124851367741); err != nil {
		t.Fatal(err)
	}
	calls := f.calls()
	if got := calls[len(calls)-2].args; !slices.Equal(got, []string{"run", "view", "41307693761", "--repo=acme/flint", "--log-failed"}) {
		t.Errorf("run log args = %q", got)
	}
	if got := calls[len(calls)-1].args; !slices.Equal(got, []string{"run", "view", "41307693761", "--repo=acme/flint", "--job=124851367741", "--log-failed"}) {
		t.Errorf("job log args = %q", got)
	}
}

// A log longer than maxLogBytes keeps its end, says so, and starts at a whole line.
func TestFailedLogReportsTheCut(t *testing.T) {
	f := newFake(t)
	f.on("repo view", repoJSON)
	line := "job\tstep\t2026-01-01T00:00:00.0000000Z " + strings.Repeat("x", 200) + "\n"
	big := strings.Repeat(line, maxLogBytes/len(line)+50) + "job\tstep\t2026-01-01T00:00:00.0000000Z the end\n"
	f.on("--log-failed", big)
	log, cut, err := f.client().FailedLog(context.Background(), t.TempDir(), 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !cut || len(log) > maxLogBytes || !strings.HasSuffix(log, "the end\n") {
		t.Errorf("cut = %v, %d bytes, ends %q", cut, len(log), log[max(0, len(log)-20):])
	}
	if !strings.HasPrefix(log, "job\tstep\t") {
		t.Errorf("starts with a partial line: %q", log[:40])
	}
}

func TestFailedLogKeepsTheTail(t *testing.T) {
	var b tailBuffer
	b.max = 10
	for _, s := range []string{"0123456789", "abcdef", "XYZ"} {
		if _, err := b.Write([]byte(s)); err != nil {
			t.Fatal(err)
		}
	}
	if got := b.String(); got != "9abcdefXYZ" {
		t.Errorf("tail = %q, want %q", got, "9abcdefXYZ")
	}
	if !b.cut {
		t.Error("cut not reported")
	}
}

func TestRerunFailed(t *testing.T) {
	f := newFake(t)
	f.on("repo view", repoJSON)
	f.on("run rerun", "")
	if err := f.client().RerunFailed(context.Background(), t.TempDir(), 41307693761); err != nil {
		t.Fatal(err)
	}
	calls := f.calls()
	if got := calls[len(calls)-1].args; !slices.Equal(got, []string{"run", "rerun", "41307693761", "--repo=acme/flint", "--failed"}) {
		t.Errorf("args = %q", got)
	}
}

func TestRunIDsValidated(t *testing.T) {
	f := newFake(t)
	f.on("repo view", repoJSON)
	c := f.client()
	ctx := context.Background()
	dir := t.TempDir()
	for _, id := range []int64{0, -1, 1 << 53} {
		if _, err := c.Run(ctx, dir, id); !errors.Is(err, ErrBadID) {
			t.Errorf("Run(%d) = %v", id, err)
		}
		if _, _, err := c.FailedLog(ctx, dir, id, 0); !errors.Is(err, ErrBadID) {
			t.Errorf("FailedLog(%d) = %v", id, err)
		}
		if id != 0 {
			if _, _, err := c.FailedLog(ctx, dir, 1, id); !errors.Is(err, ErrBadID) {
				t.Errorf("FailedLog(job %d) = %v", id, err)
			}
		}
		if err := c.RerunFailed(ctx, dir, id); !errors.Is(err, ErrBadID) {
			t.Errorf("RerunFailed(%d) = %v", id, err)
		}
	}
	for _, call := range f.calls() {
		if call.args[0] == "run" {
			t.Errorf("gh ran with a bad id: %v", call)
		}
	}
}
