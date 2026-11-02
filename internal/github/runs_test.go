package github

import (
	"errors"
	"strings"
	"testing"

	"flint/internal/cilog"
	"flint/internal/github/githubtest"
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
	fx := newFixture(t)
	bc, err := fx.c.BranchChecks(ctx(), fx.dir, "feat/rate-limit")
	if err != nil {
		t.Fatal(err)
	}
	if bc.PR == nil || bc.PR.Number != 7 || bc.PR.State != "OPEN" || bc.PR.HeadSHA != githubtest.HeadSHA || bc.PR.CrossRepository {
		t.Fatalf("pr = %+v", bc.PR)
	}
	if bc.Counts != (CheckCounts{Success: 1, Failure: 1, Pending: 1}) || len(bc.Checks) != 3 {
		t.Fatalf("checks = %+v %+v", bc.Counts, bc.Checks)
	}
	var failed Check
	for _, c := range bc.Checks {
		if c.State == "failure" {
			failed = c
		}
	}
	if failed.RunID != githubtest.RunID || failed.JobID != githubtest.FailedJob || failed.Workflow != "CI" || failed.Conclusion != "FAILURE" {
		t.Errorf("failed check = %+v", failed)
	}
}

func TestBranchChecksPollsConditionally(t *testing.T) {
	fx := newFixture(t)
	if _, err := fx.c.BranchChecks(ctx(), fx.dir, "feat/rate-limit"); err != nil {
		t.Fatal(err)
	}
	before := fx.fake.NotModified()
	again, err := fx.c.BranchChecks(ctx(), fx.dir, "feat/rate-limit")
	if err != nil || again.PR == nil || len(again.Checks) != 3 {
		t.Fatalf("second poll = %+v, %v", again, err)
	}
	if got := fx.fake.NotModified() - before; got != 4 {
		t.Errorf("304 answers on the second poll = %d, want 4 (pulls, check runs, status, workflow runs)", got)
	}
}

func TestBranchChecksCrossRepository(t *testing.T) {
	fx := newFixture(t)
	// bob pushes main to his fork; the project has a remote for it.
	runGit(t, fx.dir, "remote", "add", "bob", fx.fake.Web+"/bob/widgets.git")
	fx.c.Reset()
	bc, err := fx.c.BranchChecks(ctx(), fx.dir, "main")
	if err != nil || bc.PR == nil || bc.PR.Number != 8 || !bc.PR.CrossRepository {
		t.Fatalf("fork branch = %+v, %v", bc.PR, err)
	}
}

func TestBranchChecksNoPullRequest(t *testing.T) {
	fx := newFixture(t)
	bc, err := fx.c.BranchChecks(ctx(), fx.dir, "no-pr")
	if err != nil || bc.PR != nil || bc.Checks == nil {
		t.Fatalf("= %+v, %v", bc, err)
	}
}

func TestBranchChecksRejectsBadBranches(t *testing.T) {
	fx := newFixture(t)
	for _, b := range []string{"", "-x", "a b", "a..b", "a:b", "a\nb", strings.Repeat("x", 300)} {
		if _, err := fx.c.BranchChecks(ctx(), fx.dir, b); err == nil {
			t.Errorf("%q accepted", b)
		}
	}
	if len(fx.fake.Requests()) != 0 {
		t.Error("asked GitHub about an invalid branch")
	}
}

func TestRun(t *testing.T) {
	fx := newFixture(t)
	run, err := fx.c.Run(ctx(), fx.dir, githubtest.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Workflow != "CI" || run.Attempt != 1 || run.Branch != "feat/rate-limit" || len(run.Jobs) != 2 {
		t.Fatalf("run = %+v", run)
	}
	j := run.Jobs[0]
	if !j.Failed() || j.FailedStep() == nil || j.FailedStep().Name != "go test" || run.Jobs[1].Failed() {
		t.Errorf("jobs = %+v", run.Jobs)
	}
}

func TestFailedLog(t *testing.T) {
	fx := newFixture(t)
	for _, job := range []int64{githubtest.FailedJob, 0} {
		log, cut, err := fx.c.FailedLog(ctx(), fx.dir, githubtest.RunID, job)
		if err != nil || cut {
			t.Fatalf("job %d: cut=%v err=%v", job, cut, err)
		}
		steps := cilog.Parse(log)
		if len(steps) != 1 || steps[0].Job != "Build and test (macOS)" || steps[0].Step != "go test" {
			t.Fatalf("job %d: steps = %+v\n%s", job, steps, log)
		}
		text := strings.Join(steps[0].Lines, "\n")
		if !strings.Contains(text, "--- FAIL: TestStopDoesNotWaitForAClientThatStoppedReading") || strings.Contains(text, "Current runner version") || strings.Contains(text, "orphan processes") {
			t.Errorf("job %d: lines = %s", job, text)
		}
	}
	// The redirected log host never saw the token.
	logs := fx.fake.LogRequests()
	if len(logs) != 2 {
		t.Fatalf("log requests = %+v", logs)
	}
	for _, r := range logs {
		if r.Auth != "" {
			t.Errorf("token sent to the log host: %q", r.Auth)
		}
	}
}

func TestFailedLogValidatesJob(t *testing.T) {
	fx := newFixture(t)
	if _, _, err := fx.c.FailedLog(ctx(), fx.dir, 999, githubtest.FailedJob); err == nil {
		t.Error("a job of another run accepted")
	}
	_, _, err := fx.c.FailedLog(ctx(), fx.dir, githubtest.RunID, githubtest.PassedJob)
	if err == nil || !strings.Contains(err.Error(), "no log") {
		t.Errorf("expired log: %v", err)
	}
}

func TestRerunFailed(t *testing.T) {
	fx := newFixture(t)
	if err := fx.c.RerunFailed(ctx(), fx.dir, githubtest.RunID); err != nil {
		t.Fatal(err)
	}
	if fx.fake.Reruns() != 1 {
		t.Error("rerun not requested")
	}
}

func TestRunIDsValidated(t *testing.T) {
	fx := newFixture(t)
	for _, id := range []int64{0, -1, 1 << 53} {
		if _, err := fx.c.Run(ctx(), fx.dir, id); !errors.Is(err, ErrBadID) {
			t.Errorf("Run(%d): %v", id, err)
		}
		if err := fx.c.RerunFailed(ctx(), fx.dir, id); !errors.Is(err, ErrBadID) {
			t.Errorf("RerunFailed(%d): %v", id, err)
		}
		if _, _, err := fx.c.FailedLog(ctx(), fx.dir, id, 1); !errors.Is(err, ErrBadID) {
			t.Errorf("FailedLog(run %d): %v", id, err)
		}
		if id == 0 {
			continue // job 0 means all failed jobs
		}
		if _, _, err := fx.c.FailedLog(ctx(), fx.dir, 1, id); !errors.Is(err, ErrBadID) {
			t.Errorf("FailedLog(job %d): %v", id, err)
		}
	}
	if len(fx.fake.Requests()) != 0 {
		t.Error("asked GitHub with an invalid id")
	}
}

func TestFailedStepsSplitsByTime(t *testing.T) {
	job := Job{Name: "build", Steps: []Step{
		{Name: "setup", Conclusion: "success", StartedAt: "2026-11-01T10:00:00Z", CompletedAt: "2026-11-01T10:00:05Z"},
		{Name: "test", Conclusion: "failure", StartedAt: "2026-11-01T10:00:06Z", CompletedAt: "2026-11-01T10:00:09Z"},
		{Name: "post", Conclusion: "success", StartedAt: "2026-11-01T10:00:20Z", CompletedAt: "2026-11-01T10:00:21Z"},
	}}
	raw := "\uFEFF2026-11-01T10:00:01.5Z setting up\n" +
		"2026-11-01T10:00:06.1Z ##[group]Run go test\n" +
		"2026-11-01T10:00:09.9Z FAIL x\n" +
		"  continuation\n" +
		"2026-11-01T10:00:20.0Z cleanup\n"
	got := failedSteps(job, raw)
	want := "build\ttest\t2026-11-01T10:00:06.1Z ##[group]Run go test\n" +
		"build\ttest\t2026-11-01T10:00:09.9Z FAIL x\n" +
		"  continuation\n"
	if got != want {
		t.Fatalf("failedSteps =\n%q\nwant\n%q", got, want)
	}
	// No step times: the whole log stands for the job.
	bare := failedSteps(Job{Name: "b\tx"}, "2026-11-01T10:00:01Z a\n")
	if bare != "b x\t\t2026-11-01T10:00:01Z a\n" {
		t.Errorf("no steps: %q", bare)
	}
}
