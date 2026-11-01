package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"flint/internal/cilog"
	"flint/internal/github"
	"flint/internal/redact"
	"flint/internal/settings"
	"flint/internal/workspace"
)

// Security limits of what a failed CI log can bring into Flint (not settings:
// the excerpt sizes the user tunes are settings.CI).
const (
	maxCIJobs      = 4     // failed jobs described in one report (each log is read on its own)
	maxCIFileRefs  = 30    // file references per job
	maxCILogLines  = 20000 // lines Open log keeps (the tail)
	maxCILogLinks  = 500   // distinct paths Open log resolves
	maxCILineRunes = 2000  // a longer line in Open log is cut
	maxCINameRunes = 120   // workflow, job and step names, pull request titles
	ciStepHeadline = "── %s › %s ──"
	// ciWorkTimeout bounds the work on a log after gh returned it (redaction,
	// parsing, file lookups); gh calls have their own timeouts.
	ciWorkTimeout = 30 * time.Second
)

// ciGitHub is what the CI bindings need from gh (a fake in tests).
type ciGitHub interface {
	Repo(ctx context.Context, dir string) (github.Repo, error)
	BranchChecks(ctx context.Context, dir, branch string) (github.BranchChecks, error)
	Run(ctx context.Context, dir string, runID int64) (github.Run, error)
	FailedLog(ctx context.Context, dir string, runID, jobID int64) (string, bool, error)
	RerunFailed(ctx context.Context, dir string, runID int64) error
}

func (g *GitHubAPI) ci() ciGitHub {
	if g.ciGH != nil {
		return g.ciGH
	}
	return g.gh
}

// CIFileRef is a file:line reference from a CI log that exists in the workspace.
type CIFileRef struct {
	// Text is the reference as the log writes it.
	Text string `json:"text"`
	// Path is relative to the folder the report was made for.
	Path   string `json:"path"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

// CIJob is one failed job of a run: its failed step and the relevant lines.
type CIJob struct {
	ID         int64       `json:"id"`
	Name       string      `json:"name"`
	URL        string      `json:"url"`
	Step       string      `json:"step"`
	StepNumber int         `json:"stepNumber"`
	Lines      []string    `json:"lines"`
	Omitted    int         `json:"omitted"`
	Files      []CIFileRef `json:"files"`
	// NoLog: GitHub returned no log for the job (expired, or still uploading).
	NoLog bool `json:"noLog"`
	// LogCut: the log was longer than Flint keeps; only its end was read.
	LogCut bool `json:"logCut"`
}

// CIReport is what Fix with agent puts in the chat draft: redacted, bounded by
// settings.CI, with references mapped to the workspace. Names come from the
// workflow file (a fork's author, for cross-repository pull requests): they
// are one line each, without control characters, and cut to maxCINameRunes.
type CIReport struct {
	RunID    int64   `json:"runId"`
	Workflow string  `json:"workflow"`
	Title    string  `json:"title"`
	URL      string  `json:"url"`
	Branch   string  `json:"branch"`
	SHA      string  `json:"sha"`
	Event    string  `json:"event"`
	Attempt  int     `json:"attempt"`
	Jobs     []CIJob `json:"jobs"`
	// More: failed jobs beyond maxCIJobs that the report leaves out.
	More int `json:"more"`
}

// CILog is a run's failed-steps log for the Open log tab.
type CILog struct {
	Text string `json:"text"`
	// Files maps paths as the log writes them to workspace paths (relative to
	// the folder asked about), for the tab's file:line links.
	Files map[string]string `json:"files"`
	// Truncated: the start is missing (the log was longer than Flint keeps).
	Truncated bool `json:"truncated"`
}

// jobLog is the failed-steps log of one job, as gh returned it.
type jobLog struct {
	text string
	cut  bool
}

// BranchChecks reads the CI checks of branch's pull request (no pull request:
// pr is null). The frontend decides when to ask (settings.CI.PollSeconds).
func (g *GitHubAPI) BranchChecks(dir, branch string) (github.BranchChecks, error) {
	if err := g.checkCIDir(dir); err != nil {
		return github.BranchChecks{}, err
	}
	return g.ci().BranchChecks(g.app.ctx, dir, branch)
}

// CIFailure describes the failed jobs of a run (jobIDs narrows it; empty means
// all failed jobs) for the agent, with logs redacted and excerpts bounded.
func (g *GitHubAPI) CIFailure(dir string, runID int64, jobIDs []int64) (CIReport, error) {
	if err := g.checkCIDir(dir); err != nil {
		return CIReport{}, err
	}
	if err := checkJobIDs(jobIDs); err != nil {
		return CIReport{}, err
	}
	run, err := g.ci().Run(g.app.ctx, dir, runID)
	if err != nil {
		return CIReport{}, err
	}
	jobs, _ := failedJobs(run, jobIDs)
	// One log per job: a whole run's log is cut at 8 MB from the end, which would
	// leave the first failed jobs without a log.
	logs := make([]jobLog, len(jobs))
	var wg sync.WaitGroup
	for i, j := range jobs {
		wg.Go(func() {
			text, cut, err := g.ci().FailedLog(g.app.ctx, dir, runID, j.ID)
			if err == nil { // no log (expired, still uploading): the job still says what failed
				logs[i] = jobLog{text: text, cut: cut}
			}
		})
	}
	wg.Wait()
	ctx, cancel := context.WithTimeout(g.app.ctx, ciWorkTimeout)
	defer cancel()
	return buildCIReport(ctx, run, logs, dir, jobIDs, g.app.currentSettings().CI), nil
}

// CILog returns the failed steps' log of a run (or one job) for reading in a
// tab: cleaned, redacted, at most maxCILogLines from the end.
func (g *GitHubAPI) CILog(dir string, runID, jobID int64) (CILog, error) {
	if err := g.checkCIDir(dir); err != nil {
		return CILog{}, err
	}
	raw, cut, err := g.ci().FailedLog(g.app.ctx, dir, runID, jobID)
	if err != nil {
		return CILog{}, err
	}
	ctx, cancel := context.WithTimeout(g.app.ctx, ciWorkTimeout)
	defer cancel()
	return buildCILog(ctx, raw, cut, dir), nil
}

// RerunFailed re-runs a run's failed jobs on GitHub, after a native
// confirmation naming the repository and the workflow (the web view can't
// answer it).
func (g *GitHubAPI) RerunFailed(dir string, runID int64) error {
	if err := g.checkCIDir(dir); err != nil {
		return err
	}
	repo, err := g.ci().Repo(g.app.ctx, dir)
	if err != nil {
		return err
	}
	run, err := g.ci().Run(g.app.ctx, dir, runID)
	if err != nil {
		return err
	}
	_, failed := failedJobs(run, nil)
	msg := fmt.Sprintf("Flint will ask GitHub to re-run the %d failed job(s) of the workflow %q (run %d, attempt %d) in %s, on %s.",
		failed, oneLine(run.Workflow), run.ID, run.Attempt, repo.NameWithOwner, oneLine(run.Branch))
	if !g.app.askUser("Re-run failed jobs?", msg, "Re-run") {
		return errRerunDeclined
	}
	return g.ci().RerunFailed(g.app.ctx, dir, runID)
}

var errRerunDeclined = errors.New("re-run cancelled")

// checkCIDir accepts a folder of the workspace, the git work tree around one
// (the folder opened may be inside its repository), or an agent task's worktree.
func (g *GitHubAPI) checkCIDir(dir string) error {
	return ciDirAllowed(g.app.projectRoots(), dir, func(d string) bool { return g.app.taskByFolder(d) != "" })
}

func ciDirAllowed(roots []string, dir string, isTask func(string) bool) error {
	if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir {
		return fmt.Errorf("not a clean absolute folder: %q", dir)
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return fmt.Errorf("not a folder: %s", dir)
	}
	if _, ok := workspace.RootOf(roots, dir); ok || isTask(dir) {
		return nil
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		for _, r := range roots {
			if workspace.Within(dir, r) {
				return nil
			}
		}
	}
	return fmt.Errorf("%w: %s", workspace.ErrOutsideProject, dir)
}

func checkJobIDs(ids []int64) error {
	if len(ids) > maxCIJobs*2 {
		return fmt.Errorf("too many jobs (max %d)", maxCIJobs*2)
	}
	for _, id := range ids {
		if id <= 0 || id > 1<<53-1 {
			return errors.Join(github.ErrBadID, fmt.Errorf("job %d", id))
		}
	}
	return nil
}

// failedJobs are the run's failed jobs the report covers (at most maxCIJobs,
// narrowed to jobIDs when given), and how many failed in all.
func failedJobs(run github.Run, jobIDs []int64) ([]github.Job, int) {
	var out []github.Job
	n := 0
	for _, job := range run.Jobs {
		if !job.Failed() || (len(jobIDs) > 0 && !slices.Contains(jobIDs, job.ID)) {
			continue
		}
		n++
		if len(out) < maxCIJobs {
			out = append(out, job)
		}
	}
	return out, n
}

// oneLine makes a name from a workflow file safe to show in a draft: no
// newlines or control characters, collapsed spaces, at most maxCINameRunes.
func oneLine(s string) string {
	s = strings.Join(strings.FieldsFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r) || r == ' ' || r == ' '
	}), " ")
	if r := []rune(s); len(r) > maxCINameRunes {
		return string(r[:maxCINameRunes]) + "…"
	}
	return s
}

// cleanLine drops control characters (but tabs) a log line could carry.
func cleanLine(s string) string {
	return strings.Map(func(r rune) rune {
		if r != '\t' && (unicode.IsControl(r) || r == ' ' || r == ' ') {
			return -1
		}
		return r
	}, s)
}

// buildCIReport turns a run and its failed jobs' logs (in failedJobs order) into
// the report. Pure apart from checking which referenced files exist under root.
func buildCIReport(ctx context.Context, run github.Run, logs []jobLog, root string, jobIDs []int64, cfg settings.CI) CIReport {
	jobs, failed := failedJobs(run, jobIDs)
	r := CIReport{
		RunID: run.ID, Workflow: oneLine(redact.String(run.Workflow)), Title: oneLine(redact.String(run.Title)), URL: run.URL,
		Branch: oneLine(run.Branch), SHA: oneLine(run.SHA), Event: oneLine(run.Event), Attempt: run.Attempt, Jobs: []CIJob{},
		More: failed - len(jobs),
	}
	for i, job := range jobs {
		j := CIJob{ID: job.ID, Name: oneLine(redact.String(job.Name)), URL: job.URL, Lines: []string{}, Files: []CIFileRef{}}
		var stepName string
		if s := job.FailedStep(); s != nil {
			stepName, j.StepNumber = s.Name, s.Number
		}
		j.Step = oneLine(redact.String(stepName))
		var raw jobLog
		if i < len(logs) {
			raw = logs[i]
		}
		j.LogCut = raw.cut
		steps := cilog.Parse(redact.String(raw.text))
		log := cilog.Find(steps, job.Name, stepName)
		if log == nil || len(log.Lines) == 0 {
			j.NoLog = true
			r.Jobs = append(r.Jobs, j)
			continue
		}
		if j.Step == "" {
			j.Step = oneLine(log.Step)
		}
		ex := cilog.Excerpt(log.Lines, cfg.LogTailLines, cfg.LogErrorLines)
		j.Omitted = ex.Omitted
		for _, l := range ex.Lines {
			j.Lines = append(j.Lines, cleanLine(l))
		}
		res := cilog.NewResolver(ctx, root, allLines(steps))
		for _, ref := range cilog.Refs(ex.Lines, maxCIFileRefs*2) {
			if len(j.Files) == maxCIFileRefs {
				break
			}
			if p, ok := res.Resolve(ref.File); ok {
				j.Files = append(j.Files, CIFileRef{Text: cleanLine(ref.Text), Path: p, Line: ref.Line, Column: ref.Column})
			}
		}
		res.Close()
		r.Jobs = append(r.Jobs, j)
	}
	return r
}

func allLines(steps []cilog.StepLog) []string {
	var all []string
	for _, s := range steps {
		all = append(all, s.Lines...)
	}
	return all
}

// buildCILog renders the failed steps as text, a headline per step, with
// control characters removed and very long lines cut.
func buildCILog(ctx context.Context, raw string, cut bool, root string) CILog {
	steps := cilog.Parse(redact.String(raw))
	var lines []string
	for _, s := range steps {
		lines = append(lines, fmt.Sprintf(ciStepHeadline, oneLine(s.Job), oneLine(s.Step)))
		for _, l := range s.Lines {
			lines = append(lines, cutLine(cleanLine(l)))
		}
		lines = append(lines, "")
	}
	out := CILog{Files: map[string]string{}, Truncated: cut}
	if len(lines) > maxCILogLines {
		lines = lines[len(lines)-maxCILogLines:]
		out.Truncated = true
	}
	out.Text = strings.TrimRight(strings.Join(lines, "\n"), "\n")
	res := cilog.NewResolver(ctx, root, allLines(steps))
	defer res.Close()
	for _, ref := range cilog.Refs(lines, maxCILogLinks) {
		if _, done := out.Files[ref.File]; done {
			continue
		}
		if p, ok := res.Resolve(ref.File); ok {
			out.Files[ref.File] = p
		}
	}
	return out
}

func cutLine(s string) string {
	if len(s) <= maxCILineRunes {
		return s
	}
	if r := []rune(s); len(r) > maxCILineRunes {
		return string(r[:maxCILineRunes]) + "…"
	}
	return s
}
