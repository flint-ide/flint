package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"flint/internal/github"
	"flint/internal/settings"
)

func ciFixtures(t *testing.T) (github.Run, string, string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("internal", "cilog", "testdata", "log_failed.txt"))
	if err != nil {
		t.Fatal(err)
	}
	// The run as github.Client.Run returns it for the run the log is from.
	steps := []github.Step{{Number: 11, Name: "go vet", Conclusion: "success"}, {Number: 12, Name: "go test", Conclusion: "failure"}}
	run := github.Run{
		ID: 41307693761, Workflow: "CI", Title: "feat: rate limiting", Branch: "feat/rate-limit",
		SHA: "785d8f7a66d3403ed32421b45c87ede4fbda8bda", URL: "https://github.com/acme/flint/actions/runs/41307693761", Attempt: 1,
		Jobs: []github.Job{
			{ID: 124851367741, Name: "Build and test (macOS)", Conclusion: "failure", URL: "https://github.com/acme/flint/actions/runs/41307693761/job/124851367741", Steps: steps},
			{ID: 124851367742, Name: "Docs", Conclusion: "success"},
		},
	}
	root := t.TempDir()
	for _, f := range []string{"go.mod", "internal/remote/review_test.go", "internal/ratelimit/bucket.go"} {
		p := filepath.Join(root, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("module flint\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return run, string(raw), root
}

func TestBuildCIReport(t *testing.T) {
	run, raw, root := ciFixtures(t)
	cfg := settings.CI{LogTailLines: 6, LogErrorLines: 10}
	r := buildCIReport(context.Background(), run, []jobLog{{text: raw}}, root, nil, cfg)
	if r.Workflow != "CI" || r.Branch != "feat/rate-limit" || r.RunID != 41307693761 || r.SHA == "" || len(r.Jobs) != 1 {
		t.Fatalf("report = %+v", r)
	}
	j := r.Jobs[0]
	if j.Name != "Build and test (macOS)" || j.Step != "go test" || j.StepNumber != 12 || j.URL == "" {
		t.Errorf("job = %+v", j)
	}
	text := strings.Join(j.Lines, "\n")
	for _, want := range []string{
		"internal/ratelimit/bucket.go:42:9: undefined: refillRate",
		"--- FAIL: TestStopDoesNotWaitForAClientThatStoppedReading",
		"review_test.go:316: Stop took",
		"Error: Process completed with exit code 1.",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("excerpt lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "ok  \tflint/cmd/docsite") {
		t.Errorf("excerpt kept passing packages outside the tail:\n%s", text)
	}
	if strings.Contains(raw, "ghp_") && strings.Contains(strings.Join(j.Lines, ""), "ghp_") {
		t.Error("token not redacted")
	}
	got := map[string]string{}
	for _, f := range j.Files {
		got[f.Text] = f.Path
	}
	want := map[string]string{
		"internal/ratelimit/bucket.go:42:9": "internal/ratelimit/bucket.go",
		"review_test.go:316":                "internal/remote/review_test.go",
	}
	if len(got) != len(want) || got["review_test.go:316"] != want["review_test.go:316"] || got["internal/ratelimit/bucket.go:42:9"] != want["internal/ratelimit/bucket.go:42:9"] {
		t.Errorf("files = %v, want %v", got, want)
	}
	if j.Omitted == 0 {
		t.Error("nothing omitted with a 6-line tail")
	}
}

func TestBuildCIReportSecretsNeverLeave(t *testing.T) {
	run, raw, root := ciFixtures(t)
	r := buildCIReport(context.Background(), run, []jobLog{{text: raw}}, root, nil, settings.CI{LogTailLines: settings.MaxCILogTailLines, LogErrorLines: 10})
	b, _ := json.Marshal(r)
	if strings.Contains(string(b), "ghp_0123456789") {
		t.Errorf("the token reached the report: %s", b)
	}
}

func TestBuildCIReportJobFilter(t *testing.T) {
	run, raw, root := ciFixtures(t)
	r := buildCIReport(context.Background(), run, []jobLog{{text: raw}}, root, []int64{999}, settings.CI{LogTailLines: 10})
	if len(r.Jobs) != 0 {
		t.Errorf("jobs = %+v, want none for an unknown job", r.Jobs)
	}
	r = buildCIReport(context.Background(), run, []jobLog{{text: raw}}, root, []int64{124851367741}, settings.CI{LogTailLines: 10})
	if len(r.Jobs) != 1 {
		t.Errorf("jobs = %d", len(r.Jobs))
	}
	// A failed job whose log is missing still says what failed.
	r = buildCIReport(context.Background(), run, []jobLog{{}}, root, nil, settings.CI{LogTailLines: 10})
	if len(r.Jobs) != 1 || !r.Jobs[0].NoLog || len(r.Jobs[0].Lines) != 0 {
		t.Errorf("no log = %+v", r.Jobs)
	}
}

func TestBuildCILog(t *testing.T) {
	_, raw, root := ciFixtures(t)
	l := buildCILog(context.Background(), raw, false, root)
	if !strings.HasPrefix(l.Text, "── Build and test (macOS) › go test ──\n") {
		t.Errorf("text starts %q", l.Text[:min(80, len(l.Text))])
	}
	if strings.Contains(l.Text, "ghp_0123") || strings.Contains(l.Text, "\x1b[") || strings.Contains(l.Text, "2026-11-01T") {
		t.Error("log not cleaned and redacted")
	}
	if l.Files["review_test.go"] != "internal/remote/review_test.go" || l.Files["internal/ratelimit/bucket.go"] != "internal/ratelimit/bucket.go" {
		t.Errorf("files = %v", l.Files)
	}
	if l.Truncated {
		t.Error("a short log reported truncated")
	}
}

func TestCIJobIDsValidated(t *testing.T) {
	if err := checkJobIDs(slices.Repeat([]int64{1}, maxCIJobs*2+1)); err == nil {
		t.Error("too many job ids accepted")
	}
	if err := checkJobIDs([]int64{1, 0}); err == nil {
		t.Error("job id 0 accepted")
	}
	if err := checkJobIDs([]int64{1, 2}); err != nil {
		t.Error(err)
	}
}
