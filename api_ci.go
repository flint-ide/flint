package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"flint/internal/cilog"
	"flint/internal/github"
	"flint/internal/redact"
	"flint/internal/settings"
)

// Security limits of what a failed CI log can bring into Flint (not settings:
// the excerpt sizes the user tunes are settings.CI).
const (
	maxCIJobs      = 8     // failed jobs described in one report
	maxCIFileRefs  = 30    // file references per job
	maxCILogLines  = 20000 // lines shown by Open log (the tail)
	maxCILogLinks  = 500   // distinct paths Open log resolves
	ciStepHeadline = "── %s › %s ──"
)

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
}

// CIReport is what Fix with agent puts in the chat draft: redacted, bounded by
// settings.CI, with references mapped to the workspace.
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
}

// CILog is a run's failed-steps log for the Open log tab.
type CILog struct {
	Text string `json:"text"`
	// Files maps paths as the log writes them to workspace paths (relative to
	// the folder asked about), for the tab's file:line links.
	Files     map[string]string `json:"files"`
	Truncated bool              `json:"truncated"`
}

// BranchChecks reads the CI checks of branch's pull request (no pull request:
// pr is null). The frontend decides when to ask (settings.CI.PollSeconds).
func (g *GitHubAPI) BranchChecks(dir, branch string) (github.BranchChecks, error) {
	if err := checkCIDir(dir); err != nil {
		return github.BranchChecks{}, err
	}
	return g.gh.BranchChecks(g.app.ctx, dir, branch)
}

// CIFailure describes the failed jobs of a run (jobIDs narrows it; empty means
// all failed jobs) for the agent, with logs redacted and excerpts bounded.
func (g *GitHubAPI) CIFailure(dir string, runID int64, jobIDs []int64) (CIReport, error) {
	if err := checkCIDir(dir); err != nil {
		return CIReport{}, err
	}
	if err := checkJobIDs(jobIDs); err != nil {
		return CIReport{}, err
	}
	var logJob int64
	if len(jobIDs) == 1 {
		logJob = jobIDs[0]
	}
	var (
		wg     sync.WaitGroup
		run    github.Run
		runErr error
	)
	wg.Go(func() { run, runErr = g.gh.Run(g.app.ctx, dir, runID) })
	raw, logErr := g.gh.FailedLog(g.app.ctx, dir, runID, logJob)
	wg.Wait()
	if runErr != nil {
		return CIReport{}, runErr
	}
	if logErr != nil {
		// The jobs and steps still say what failed; the log may have expired.
		raw = ""
	}
	return buildCIReport(run, raw, dir, jobIDs, g.app.currentSettings().CI), nil
}

// CILog returns the failed steps' log of a run (or one job) for reading in a
// tab: cleaned, redacted, at most maxCILogLines from the end.
func (g *GitHubAPI) CILog(dir string, runID, jobID int64) (CILog, error) {
	if err := checkCIDir(dir); err != nil {
		return CILog{}, err
	}
	raw, err := g.gh.FailedLog(g.app.ctx, dir, runID, jobID)
	if err != nil {
		return CILog{}, err
	}
	return buildCILog(raw, dir), nil
}

// RerunFailed re-runs a run's failed jobs on GitHub (the UI confirms first).
func (g *GitHubAPI) RerunFailed(dir string, runID int64) error {
	if err := checkCIDir(dir); err != nil {
		return err
	}
	return g.gh.RerunFailed(g.app.ctx, dir, runID)
}

// checkCIDir accepts an existing absolute folder: the project the CI is of.
func checkCIDir(dir string) error {
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("not an absolute folder: %q", dir)
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return fmt.Errorf("not a folder: %s", dir)
	}
	return nil
}

func checkJobIDs(ids []int64) error {
	if len(ids) > maxCIJobs {
		return fmt.Errorf("too many jobs (max %d)", maxCIJobs)
	}
	for _, id := range ids {
		if id <= 0 || id > 1<<53-1 {
			return errors.Join(github.ErrBadID, fmt.Errorf("job %d", id))
		}
	}
	return nil
}

// buildCIReport turns a run and its failed log into the report. Pure apart from
// checking which referenced files exist under root.
func buildCIReport(run github.Run, raw, root string, jobIDs []int64, cfg settings.CI) CIReport {
	steps := cilog.Parse(redact.String(raw))
	var all []string
	for _, s := range steps {
		all = append(all, s.Lines...)
	}
	res := cilog.NewResolver(root, all)
	r := CIReport{
		RunID: run.ID, Workflow: redact.String(run.Workflow), Title: redact.String(run.Title), URL: run.URL,
		Branch: run.Branch, SHA: run.SHA, Event: run.Event, Attempt: run.Attempt, Jobs: []CIJob{},
	}
	for _, job := range run.Jobs {
		if len(r.Jobs) == maxCIJobs {
			break
		}
		if !job.Failed() || (len(jobIDs) > 0 && !slices.Contains(jobIDs, job.ID)) {
			continue
		}
		j := CIJob{ID: job.ID, Name: job.Name, URL: job.URL, Lines: []string{}, Files: []CIFileRef{}}
		if s := job.FailedStep(); s != nil {
			j.Step, j.StepNumber = s.Name, s.Number
		}
		log := cilog.Find(steps, job.Name, j.Step)
		if log == nil || len(log.Lines) == 0 {
			j.NoLog = true
			r.Jobs = append(r.Jobs, j)
			continue
		}
		if j.Step == "" {
			j.Step = log.Step
		}
		ex := cilog.Excerpt(log.Lines, cfg.LogTailLines, cfg.LogErrorLines)
		j.Lines, j.Omitted = ex.Lines, ex.Omitted
		for _, ref := range cilog.Refs(ex.Lines, maxCIFileRefs*2) {
			if len(j.Files) == maxCIFileRefs {
				break
			}
			if p, ok := res.Resolve(ref.File); ok {
				j.Files = append(j.Files, CIFileRef{Text: ref.Text, Path: p, Line: ref.Line, Column: ref.Column})
			}
		}
		r.Jobs = append(r.Jobs, j)
	}
	return r
}

// buildCILog renders the failed steps as text, a headline per step.
func buildCILog(raw, root string) CILog {
	steps := cilog.Parse(redact.String(raw))
	var lines, all []string
	for _, s := range steps {
		lines = append(lines, fmt.Sprintf(ciStepHeadline, s.Job, s.Step))
		lines = append(lines, s.Lines...)
		lines = append(lines, "")
		all = append(all, s.Lines...)
	}
	out := CILog{Files: map[string]string{}}
	if len(lines) > maxCILogLines {
		lines = lines[len(lines)-maxCILogLines:]
		out.Truncated = true
	}
	out.Text = strings.TrimRight(strings.Join(lines, "\n"), "\n")
	res := cilog.NewResolver(root, all)
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
