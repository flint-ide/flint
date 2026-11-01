package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode"
)

// ErrBadID: a run or job id that can't be GitHub's (ids come from the web view).
var ErrBadID = errors.New("invalid GitHub Actions run or job id")

// maxLogBytes bounds what Flint keeps of a failed log: the tail, where the
// failure is. A security limit, not a setting: a log can be gigabytes.
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

// checkBranch accepts a branch name git allows that can't be read as a flag.
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

// restPull is an item of GET /repos/{owner}/{repo}/pulls.
type restPull struct {
	Number    int     `json:"number"`
	Title     string  `json:"title"`
	HTMLURL   string  `json:"html_url"`
	State     string  `json:"state"` // open, closed
	MergedAt  *string `json:"merged_at"`
	UpdatedAt string  `json:"updated_at"`
	Head      struct {
		Ref  string `json:"ref"`
		SHA  string `json:"sha"`
		Repo *struct {
			FullName string `json:"full_name"`
		} `json:"repo"`
	} `json:"head"`
}

// BranchChecks reads the checks of the pull request whose head is branch (an
// open one first, else the latest). It is polled while checks run, so every
// request is conditional: when nothing changed GitHub answers 304, which
// costs no rate limit. Not cached otherwise: callers decide how often to ask
// (see settings.CI).
func (c *Client) BranchChecks(ctx context.Context, dir, branch string) (BranchChecks, error) {
	if err := checkBranch(branch); err != nil {
		return BranchChecks{}, err
	}
	a, info, err := c.session(ctx, dir)
	if err != nil {
		return BranchChecks{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	pr, err := c.branchPull(ctx, a, info, branch)
	if err != nil || pr == nil {
		return BranchChecks{Checks: []Check{}}, err
	}
	if !shaRE.MatchString(pr.Head.SHA) {
		return BranchChecks{}, fmt.Errorf("unexpected head commit of #%d: %q", pr.Number, pr.Head.SHA)
	}
	raw, err := c.commitChecks(ctx, a, info.Repo, pr.Head.SHA)
	if err != nil {
		return BranchChecks{}, err
	}
	state := strings.ToUpper(pr.State)
	if pr.MergedAt != nil && *pr.MergedAt != "" {
		state = "MERGED"
	}
	out := BranchChecks{
		PR: &PullRequestRef{
			Number: pr.Number, Title: pr.Title, URL: pr.HTMLURL, State: state, HeadRef: pr.Head.Ref, HeadSHA: pr.Head.SHA,
			CrossRepository: pr.Head.Repo == nil || !strings.EqualFold(pr.Head.Repo.FullName, info.NameWithOwner),
		},
		Checks: make([]Check, 0, len(raw)),
		Counts: countChecks(raw),
	}
	for _, ch := range raw {
		out.Checks = append(out.Checks, ch.check(info.URL))
	}
	return out, nil
}

// branchPull finds the pull request of branch: pushed to this repository or
// to one of the forks the project's remotes point to.
func (c *Client) branchPull(ctx context.Context, a Auth, info repoInfo, branch string) (*restPull, error) {
	owners := append([]string{info.Owner}, info.owners...)
	var best *restPull
	seen := map[string]bool{}
	for _, owner := range owners {
		if seen[strings.ToLower(owner)] {
			continue
		}
		seen[strings.ToLower(owner)] = true
		q := url.Values{"head": {owner + ":" + branch}, "state": {"all"}, "per_page": {"10"}, "sort": {"updated"}, "direction": {"desc"}}
		var pulls []restPull
		u := rest(a, "/repos/%s/%s/pulls", info.Owner, info.Name) + "?" + q.Encode()
		if err := c.getJSON(ctx, a, call{url: u, conditional: true}, &pulls); err != nil {
			return nil, err
		}
		for i := range pulls {
			p := &pulls[i]
			if p.Head.Ref != branch {
				continue
			}
			switch {
			case best == nil,
				p.State == "open" && best.State != "open",
				p.State == best.State && p.UpdatedAt > best.UpdatedAt:
				best = p
			}
		}
	}
	return best, nil
}

// commitChecks reads a commit's check runs and statuses (REST, conditional)
// in the shape of GraphQL's statusCheckRollup.
func (c *Client) commitChecks(ctx context.Context, a Auth, repo Repo, sha string) ([]ghCheck, error) {
	var (
		wg                 sync.WaitGroup
		runs               restCheckRuns
		status             restStatus
		workflows          restWorkflowRuns
		errR, errS, errW   error
		base               = rest(a, "/repos/%s/%s", repo.Owner, repo.Name)
		runsURL, statusURL = base + "/commits/" + sha + "/check-runs?per_page=100", base + "/commits/" + sha + "/status?per_page=100"
		workflowsURL       = base + "/actions/runs?per_page=100&head_sha=" + sha
	)
	wg.Go(func() { errR = c.getJSON(ctx, a, call{url: runsURL, conditional: true}, &runs) })
	wg.Go(func() { errS = c.getJSON(ctx, a, call{url: statusURL, conditional: true}, &status) })
	wg.Go(func() { errW = c.getJSON(ctx, a, call{url: workflowsURL, conditional: true}, &workflows) })
	wg.Wait()
	if err := errors.Join(errR, errS); err != nil {
		return nil, err
	}
	names := map[int64]string{} // check suite id → workflow name (best effort: errW only loses names)
	if errW == nil {
		for _, w := range workflows.WorkflowRuns {
			names[w.CheckSuiteID] = w.Name
		}
	}
	out := make([]ghCheck, 0, len(runs.CheckRuns)+len(status.Statuses))
	for _, r := range runs.CheckRuns {
		url := r.DetailsURL
		if url == "" {
			url = r.HTMLURL
		}
		out = append(out, ghCheck{
			Typename: "CheckRun", Name: r.Name, Status: strings.ToUpper(r.Status), Conclusion: strings.ToUpper(r.Conclusion),
			DetailsURL: url, WorkflowName: names[r.CheckSuite.ID],
		})
	}
	for _, s := range status.Statuses {
		out = append(out, ghCheck{Typename: "StatusContext", Context: s.Context, State: strings.ToUpper(s.State), TargetURL: s.TargetURL})
	}
	return out, nil
}

type restCheckRuns struct {
	CheckRuns []struct {
		Name       string `json:"name"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		DetailsURL string `json:"details_url"`
		HTMLURL    string `json:"html_url"`
		CheckSuite struct {
			ID int64 `json:"id"`
		} `json:"check_suite"`
	} `json:"check_runs"`
}

type restStatus struct {
	Statuses []struct {
		Context   string `json:"context"`
		State     string `json:"state"`
		TargetURL string `json:"target_url"`
	} `json:"statuses"`
}

type restWorkflowRuns struct {
	WorkflowRuns []struct {
		ID           int64  `json:"id"`
		Name         string `json:"name"`
		CheckSuiteID int64  `json:"check_suite_id"`
	} `json:"workflow_runs"`
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
	// StartedAt and CompletedAt (RFC 3339) place the step's lines in the job's log.
	StartedAt   string `json:"startedAt"`
	CompletedAt string `json:"completedAt"`
}

// failed reports whether a job or step did not succeed.
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

// restJob is a job as the REST API returns it.
type restJob struct {
	ID         int64  `json:"id"`
	RunID      int64  `json:"run_id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	HTMLURL    string `json:"html_url"`
	Steps      []struct {
		Number      int    `json:"number"`
		Name        string `json:"name"`
		Status      string `json:"status"`
		Conclusion  string `json:"conclusion"`
		StartedAt   string `json:"started_at"`
		CompletedAt string `json:"completed_at"`
	} `json:"steps"`
}

func (j restJob) job() Job {
	out := Job{ID: j.ID, Name: j.Name, Status: j.Status, Conclusion: j.Conclusion, URL: j.HTMLURL, Steps: []Step{}}
	for _, s := range j.Steps {
		out.Steps = append(out.Steps, Step{
			Number: s.Number, Name: s.Name, Status: s.Status, Conclusion: s.Conclusion,
			StartedAt: s.StartedAt, CompletedAt: s.CompletedAt,
		})
	}
	return out
}

// Run reads a workflow run and its (latest attempt's) jobs.
func (c *Client) Run(ctx context.Context, dir string, runID int64) (Run, error) {
	if err := checkID(runID); err != nil {
		return Run{}, err
	}
	a, info, err := c.session(ctx, dir)
	if err != nil {
		return Run{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	var v struct {
		ID           int64  `json:"id"`
		Name         string `json:"name"`
		DisplayTitle string `json:"display_title"`
		HeadBranch   string `json:"head_branch"`
		HeadSHA      string `json:"head_sha"`
		Event        string `json:"event"`
		Status       string `json:"status"`
		Conclusion   string `json:"conclusion"`
		HTMLURL      string `json:"html_url"`
		RunAttempt   int    `json:"run_attempt"`
	}
	if err := c.getJSON(ctx, a, call{url: rest(a, "/repos/%s/%s/actions/runs/%d", info.Owner, info.Name, runID)}, &v); err != nil {
		return Run{}, err
	}
	jobs, err := c.runJobs(ctx, a, info.Repo, runID)
	if err != nil {
		return Run{}, err
	}
	run := Run{
		ID: v.ID, Workflow: v.Name, Title: v.DisplayTitle, Branch: v.HeadBranch, SHA: v.HeadSHA,
		Event: v.Event, Status: v.Status, Conclusion: v.Conclusion, URL: v.HTMLURL, Attempt: v.RunAttempt, Jobs: []Job{},
	}
	for _, j := range jobs {
		run.Jobs = append(run.Jobs, j.job())
	}
	return run, nil
}

func (c *Client) runJobs(ctx context.Context, a Auth, repo Repo, runID int64) ([]restJob, error) {
	var jobs []restJob
	first := rest(a, "/repos/%s/%s/actions/runs/%d/jobs", repo.Owner, repo.Name, runID) + "?filter=latest&per_page=100"
	err := c.pages(ctx, a, first, 10, func(body []byte) error {
		var page struct {
			Jobs []restJob `json:"jobs"`
		}
		if err := json.Unmarshal(body, &page); err != nil {
			return fmt.Errorf("unexpected answer from GitHub (run jobs): %w", err)
		}
		jobs = append(jobs, page.Jobs...)
		return nil
	})
	return jobs, err
}

// FailedLog returns the log of a run's failed steps, of one job when jobID is
// set, in the GitHub CLI's --log-failed shape ("job<TAB>step<TAB>line") that
// internal/cilog reads. Only the last maxLogBytes are kept; cut tells that the
// start is missing (the partial first line is dropped then).
func (c *Client) FailedLog(ctx context.Context, dir string, runID, jobID int64) (log string, cut bool, err error) {
	if err := checkID(runID); err != nil {
		return "", false, err
	}
	if jobID != 0 {
		if err := checkID(jobID); err != nil {
			return "", false, err
		}
	}
	a, info, err := c.session(ctx, dir)
	if err != nil {
		return "", false, err
	}
	ctx, cancel := context.WithTimeout(ctx, logTimeout)
	defer cancel()
	var jobs []restJob
	if jobID != 0 {
		var j restJob
		if err := c.getJSON(ctx, a, call{url: rest(a, "/repos/%s/%s/actions/jobs/%d", info.Owner, info.Name, jobID)}, &j); err != nil {
			return "", false, err
		}
		if j.RunID != runID {
			return "", false, fmt.Errorf("%w: job %d is not part of run %d", ErrBadID, jobID, runID)
		}
		jobs = []restJob{j}
	} else {
		all, err := c.runJobs(ctx, a, info.Repo, runID)
		if err != nil {
			return "", false, err
		}
		for _, j := range all {
			if failed(j.Conclusion) {
				jobs = append(jobs, j)
			}
		}
	}
	out := &tailBuffer{max: maxLogBytes}
	var firstErr error
	got := 0
	for _, j := range jobs {
		raw, err := c.jobLog(ctx, a, info.Repo, j.ID)
		if err != nil {
			firstErr = cmp(firstErr, err)
			continue
		}
		got++
		out.cut = out.cut || raw.cut
		_, _ = out.Write([]byte(failedSteps(j.job(), raw.String())))
	}
	if got == 0 && firstErr != nil {
		return "", false, firstErr
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

func cmp(a, b error) error {
	if a != nil {
		return a
	}
	return b
}

// RerunFailed re-runs the failed jobs of a run (and the jobs they need). It acts
// on GitHub: the UI asks first.
func (c *Client) RerunFailed(ctx context.Context, dir string, runID int64) error {
	if err := checkID(runID); err != nil {
		return err
	}
	a, info, err := c.session(ctx, dir)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	_, err = c.do(ctx, a, call{method: http.MethodPost, url: rest(a, "/repos/%s/%s/actions/runs/%d/rerun-failed-jobs", info.Owner, info.Name, runID)})
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
