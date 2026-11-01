package github

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// ErrBadID: a run or job id that can't be GitHub's (ids come from the web view).
var ErrBadID = errors.New("invalid GitHub Actions run or job id")

// maxLogBytes bounds what Flint keeps of a failed log: the tail, where the
// failure is. A security limit, not a setting: gh could print gigabytes.
const maxLogBytes = 8 << 20

// maxID: ids pass through JavaScript numbers, which are exact up to 2^53.
const maxID = 1<<53 - 1

func checkID(id int64) error {
	if id <= 0 || id > maxID {
		return fmt.Errorf("%w: %d", ErrBadID, id)
	}
	return nil
}

// actionsRE matches the rest of a details URL after "<repo URL>/actions/runs/".
var actionsRE = regexp.MustCompile(`^(\d{1,16})(?:/job/(\d{1,16}))?(?:[/?#]|$)`)

// actionsIDs reads the run and job ids from a check's details URL, only when it
// is a run of this repository's GitHub Actions (0, 0 otherwise: another CI, or
// a URL a check from elsewhere chose).
func actionsIDs(repoURL, url string) (run, job int64) {
	if repoURL == "" {
		return 0, 0
	}
	rest, ok := strings.CutPrefix(url, strings.TrimSuffix(repoURL, "/")+"/actions/runs/")
	if !ok {
		return 0, 0
	}
	m := actionsRE.FindStringSubmatch(rest)
	if m == nil {
		return 0, 0
	}
	run, _ = strconv.ParseInt(m[1], 10, 64)
	if checkID(run) != nil {
		return 0, 0
	}
	if m[2] != "" {
		job, _ = strconv.ParseInt(m[2], 10, 64)
		if checkID(job) != nil {
			job = 0
		}
	}
	return run, job
}

// PullRequestRef is the pull request a branch is the head of.
type PullRequestRef struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	URL     string `json:"url"`
	State   string `json:"state"` // OPEN, CLOSED, MERGED
	HeadRef string `json:"headRef"`
	HeadSHA string `json:"headSha"`
	// CrossRepository: the head is in a fork, whose author wrote the workflow
	// names and the commands the log shows.
	CrossRepository bool `json:"crossRepository"`
}

// BranchChecks is the CI state of a branch's pull request; PR is nil when the
// branch has none.
type BranchChecks struct {
	PR     *PullRequestRef `json:"pr"`
	Checks []Check         `json:"checks"`
	Counts CheckCounts     `json:"counts"`
}

// checkBranch accepts a branch name git allows that gh can't read as a flag.
func checkBranch(b string) error {
	switch {
	case b == "" || len(b) > 250:
		return fmt.Errorf("invalid branch name %q", b)
	case strings.HasPrefix(b, "-"):
		return fmt.Errorf("invalid branch name %q: starts with '-'", b)
	case strings.ContainsFunc(b, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }):
		return fmt.Errorf("invalid branch name %q: contains spaces or control characters", b)
	case strings.Contains(b, ".."), strings.ContainsAny(b, "~^:?*[\\"):
		return fmt.Errorf("invalid branch name %q", b)
	}
	return nil
}

// BranchChecks reads the checks of the pull request whose head is branch. It is
// not cached: callers decide how often to ask (see settings.CI).
func (c *Client) BranchChecks(ctx context.Context, dir, branch string) (BranchChecks, error) {
	if err := checkBranch(branch); err != nil {
		return BranchChecks{}, err
	}
	repo, err := c.Repo(ctx, dir)
	if err != nil {
		return BranchChecks{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	var v struct {
		Number            int       `json:"number"`
		Title             string    `json:"title"`
		URL               string    `json:"url"`
		State             string    `json:"state"`
		HeadRefName       string    `json:"headRefName"`
		HeadRefOid        string    `json:"headRefOid"`
		IsCrossRepository bool      `json:"isCrossRepository"`
		StatusCheckRollup []ghCheck `json:"statusCheckRollup"`
	}
	err = c.runJSON(ctx, dir, nil, &v, "pr", "view", "--repo="+repo.NameWithOwner,
		"--json=number,title,url,state,headRefName,headRefOid,isCrossRepository,statusCheckRollup", "--", branch)
	if err != nil {
		var ghErr *Error
		if errors.As(err, &ghErr) && strings.Contains(strings.ToLower(ghErr.Msg), "no pull requests found") {
			return BranchChecks{Checks: []Check{}}, nil
		}
		return BranchChecks{}, err
	}
	if v.HeadRefName != branch {
		// gh read a numeric branch name ("123") as pull request #123, or a URL.
		return BranchChecks{Checks: []Check{}}, nil
	}
	out := BranchChecks{
		PR: &PullRequestRef{
			Number: v.Number, Title: v.Title, URL: v.URL, State: v.State, HeadRef: v.HeadRefName, HeadSHA: v.HeadRefOid,
			CrossRepository: v.IsCrossRepository,
		},
		Checks: make([]Check, 0, len(v.StatusCheckRollup)),
		Counts: countChecks(v.StatusCheckRollup),
	}
	for _, ch := range v.StatusCheckRollup {
		out.Checks = append(out.Checks, ch.check(repo.URL))
	}
	return out, nil
}

// Run is a GitHub Actions workflow run with its jobs.
type Run struct {
	ID         int64  `json:"id"`
	Workflow   string `json:"workflow"`
	Title      string `json:"title"`
	Branch     string `json:"branch"`
	SHA        string `json:"sha"`
	Event      string `json:"event"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	URL        string `json:"url"`
	Attempt    int    `json:"attempt"`
	Jobs       []Job  `json:"jobs"`
}

// Job is one job of a run.
type Job struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	URL        string `json:"url"`
	Steps      []Step `json:"steps"`
}

// Step is one step of a job.
type Step struct {
	Number     int    `json:"number"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}

// Failed reports whether the job (or step) did not succeed.
func failed(conclusion string) bool {
	switch strings.ToLower(conclusion) {
	case "failure", "timed_out", "cancelled", "startup_failure":
		return true
	}
	return false
}

// Failed reports whether the job failed (or timed out, or was cancelled).
func (j Job) Failed() bool { return failed(j.Conclusion) }

// FailedStep is the first step that failed, or nil.
func (j Job) FailedStep() *Step {
	for i := range j.Steps {
		if failed(j.Steps[i].Conclusion) {
			return &j.Steps[i]
		}
	}
	return nil
}

// Run reads a workflow run and its jobs.
func (c *Client) Run(ctx context.Context, dir string, runID int64) (Run, error) {
	if err := checkID(runID); err != nil {
		return Run{}, err
	}
	repo, err := c.Repo(ctx, dir)
	if err != nil {
		return Run{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	var v struct {
		DatabaseID   int64  `json:"databaseId"`
		WorkflowName string `json:"workflowName"`
		DisplayTitle string `json:"displayTitle"`
		HeadBranch   string `json:"headBranch"`
		HeadSha      string `json:"headSha"`
		Event        string `json:"event"`
		Status       string `json:"status"`
		Conclusion   string `json:"conclusion"`
		URL          string `json:"url"`
		Attempt      int    `json:"attempt"`
		Jobs         []struct {
			DatabaseID int64  `json:"databaseId"`
			Name       string `json:"name"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
			URL        string `json:"url"`
			Steps      []Step `json:"steps"`
		} `json:"jobs"`
	}
	err = c.runJSON(ctx, dir, nil, &v, "run", "view", strconv.FormatInt(runID, 10), "--repo="+repo.NameWithOwner,
		"--json=databaseId,workflowName,displayTitle,headBranch,headSha,event,status,conclusion,url,attempt,jobs")
	if err != nil {
		return Run{}, err
	}
	run := Run{
		ID: v.DatabaseID, Workflow: v.WorkflowName, Title: v.DisplayTitle, Branch: v.HeadBranch, SHA: v.HeadSha,
		Event: v.Event, Status: v.Status, Conclusion: v.Conclusion, URL: v.URL, Attempt: v.Attempt, Jobs: []Job{},
	}
	for _, j := range v.Jobs {
		steps := j.Steps
		if steps == nil {
			steps = []Step{}
		}
		run.Jobs = append(run.Jobs, Job{ID: j.DatabaseID, Name: j.Name, Status: j.Status, Conclusion: j.Conclusion, URL: j.URL, Steps: steps})
	}
	return run, nil
}

// FailedLog returns the log of a run's failed steps (gh run view --log-failed),
// of one job when jobID is set. Only the last maxLogBytes are kept; cut tells
// that the start is missing (the partial first line is dropped then).
func (c *Client) FailedLog(ctx context.Context, dir string, runID, jobID int64) (log string, cut bool, err error) {
	if err := checkID(runID); err != nil {
		return "", false, err
	}
	if jobID != 0 {
		if err := checkID(jobID); err != nil {
			return "", false, err
		}
	}
	repo, err := c.Repo(ctx, dir)
	if err != nil {
		return "", false, err
	}
	ctx, cancel := context.WithTimeout(ctx, writeTimeout) // logs are downloaded as a zip
	defer cancel()
	args := []string{"run", "view", strconv.FormatInt(runID, 10), "--repo=" + repo.NameWithOwner}
	if jobID != 0 {
		args = append(args, "--job="+strconv.FormatInt(jobID, 10))
	}
	args = append(args, "--log-failed")
	out := &tailBuffer{max: maxLogBytes}
	stderr, err := c.execTo(ctx, dir, nil, out, args...)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", false, fmt.Errorf("gh %s timed out", firstArgs(args))
		}
		if errors.Is(err, ErrNotInstalled) {
			return "", false, err
		}
		msg := strings.TrimSpace(stderr)
		if msg == "" {
			msg = err.Error()
		}
		return "", false, classify(args, msg, err)
	}
	text := out.String()
	if out.cut {
		if i := strings.IndexByte(text, '\n'); i >= 0 {
			text = text[i+1:]
		} else {
			text = ""
		}
	}
	return text, out.cut, nil
}

// RerunFailed re-runs the failed jobs of a run (and the jobs they need). It acts
// on GitHub: the UI asks first.
func (c *Client) RerunFailed(ctx context.Context, dir string, runID int64) error {
	if err := checkID(runID); err != nil {
		return err
	}
	repo, err := c.Repo(ctx, dir)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	_, err = c.run(ctx, dir, nil, "run", "rerun", strconv.FormatInt(runID, 10), "--repo="+repo.NameWithOwner, "--failed")
	return err
}

// tailBuffer keeps the last max bytes written to it.
type tailBuffer struct {
	max int
	buf []byte
	cut bool
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	n := len(p)
	b.buf = append(b.buf, p...)
	// Trim when twice the limit, so dropping the head stays amortized O(1).
	if len(b.buf) > 2*b.max || (len(b.buf) > b.max && n >= b.max) {
		b.trim()
	}
	return n, nil
}

func (b *tailBuffer) trim() {
	if over := len(b.buf) - b.max; over > 0 {
		b.buf = append(b.buf[:0], b.buf[over:]...)
		b.cut = true
	}
}

func (b *tailBuffer) String() string {
	b.trim()
	return string(b.buf)
}
