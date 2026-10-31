package github

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"strconv"
	"strings"
	"sync"

	"flint/internal/preview"
)

// listLimit caps the list; older pull requests are a search away on GitHub.
const listLimit = 50

// ListFilter selects pull requests like the tabs of GitHub's list.
type ListFilter struct {
	// Who: "all", "mine" (authored by me) or "review" (my review is requested).
	Who string `json:"who"`
	// State: "open", "closed", "merged" or "all".
	State string `json:"state"`
}

// Check is one CI check (a check run or a commit status).
type Check struct {
	Name     string `json:"name"`
	Workflow string `json:"workflow"`
	// State is the normalized outcome: "success", "failure", "pending", "skipped" or "neutral".
	State string `json:"state"`
	// Status and Conclusion are GitHub's own values, shown in tooltips.
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	URL        string `json:"url"`
}

// CheckCounts sums checks by normalized state (the list's CI badge).
type CheckCounts struct {
	Success int `json:"success"`
	Failure int `json:"failure"`
	Pending int `json:"pending"`
	Skipped int `json:"skipped"`
	Neutral int `json:"neutral"`
}

// PullRequest is one row of the list.
type PullRequest struct {
	Number         int         `json:"number"`
	Title          string      `json:"title"`
	Author         string      `json:"author"`
	HeadRef        string      `json:"headRef"`
	BaseRef        string      `json:"baseRef"`
	Draft          bool        `json:"draft"`
	State          string      `json:"state"`          // OPEN, CLOSED, MERGED
	ReviewDecision string      `json:"reviewDecision"` // APPROVED, CHANGES_REQUESTED, REVIEW_REQUIRED or ""
	Checks         CheckCounts `json:"checks"`
	UpdatedAt      string      `json:"updatedAt"`
	URL            string      `json:"url"`
}

type ghAuthor struct {
	Login string `json:"login"`
}

// ghCheck is an item of gh's statusCheckRollup: a CheckRun or a StatusContext.
type ghCheck struct {
	Typename     string `json:"__typename"`
	Name         string `json:"name"`
	WorkflowName string `json:"workflowName"`
	Status       string `json:"status"`
	Conclusion   string `json:"conclusion"`
	DetailsURL   string `json:"detailsUrl"`
	Context      string `json:"context"`
	State        string `json:"state"`
	TargetURL    string `json:"targetUrl"`
}

func (g ghCheck) check() Check {
	if g.Typename == "StatusContext" {
		return Check{Name: g.Context, State: statusState(g.State), Status: g.State, URL: g.TargetURL}
	}
	return Check{
		Name:       g.Name,
		Workflow:   g.WorkflowName,
		State:      checkRunState(g.Status, g.Conclusion),
		Status:     g.Status,
		Conclusion: g.Conclusion,
		URL:        g.DetailsURL,
	}
}

// checkRunState follows GitHub's own rollup: anything not completed is pending, and
// cancelled or timed-out runs fail the commit.
func checkRunState(status, conclusion string) string {
	if status != "" && status != "COMPLETED" {
		return "pending"
	}
	switch conclusion {
	case "SUCCESS":
		return "success"
	case "SKIPPED":
		return "skipped"
	case "NEUTRAL":
		return "neutral"
	case "":
		return "pending"
	default: // FAILURE, CANCELLED, TIMED_OUT, ACTION_REQUIRED, STARTUP_FAILURE, STALE
		return "failure"
	}
}

func statusState(state string) string {
	switch state {
	case "SUCCESS":
		return "success"
	case "PENDING", "EXPECTED":
		return "pending"
	default: // ERROR, FAILURE
		return "failure"
	}
}

func countChecks(raw []ghCheck) CheckCounts {
	var n CheckCounts
	for _, r := range raw {
		switch r.check().State {
		case "success":
			n.Success++
		case "failure":
			n.Failure++
		case "pending":
			n.Pending++
		case "skipped":
			n.Skipped++
		default:
			n.Neutral++
		}
	}
	return n
}

// listArgs builds the gh pr list invocation for a filter.
func listArgs(repo Repo, f ListFilter) ([]string, error) {
	state := f.State
	switch state {
	case "":
		state = "open"
	case "open", "closed", "merged", "all":
	default:
		return nil, fmt.Errorf("unknown pull request state %q", f.State)
	}
	args := []string{
		"pr", "list", "--repo", repo.NameWithOwner, "--state", state, "--limit", strconv.Itoa(listLimit),
		"--json", "number,title,author,headRefName,baseRefName,isDraft,state,reviewDecision,statusCheckRollup,updatedAt,url",
	}
	switch f.Who {
	case "", "all":
	case "mine":
		args = append(args, "--author", "@me")
	case "review":
		args = append(args, "--search", "review-requested:@me")
	default:
		return nil, fmt.Errorf("unknown pull request filter %q", f.Who)
	}
	return args, nil
}

// List lists pull requests of the repository of dir.
func (c *Client) List(ctx context.Context, dir string, f ListFilter, force bool) ([]PullRequest, error) {
	repo, err := c.Repo(ctx, dir)
	if err != nil {
		return nil, err
	}
	args, err := listArgs(repo, f)
	if err != nil {
		return nil, err
	}
	return c.lists.get(repo.NameWithOwner+"|"+f.Who+"|"+f.State, force, func() ([]PullRequest, error) {
		ctx, cancel := context.WithTimeout(ctx, readTimeout)
		defer cancel()
		var raw []struct {
			Number            int       `json:"number"`
			Title             string    `json:"title"`
			Author            ghAuthor  `json:"author"`
			HeadRefName       string    `json:"headRefName"`
			BaseRefName       string    `json:"baseRefName"`
			IsDraft           bool      `json:"isDraft"`
			State             string    `json:"state"`
			ReviewDecision    string    `json:"reviewDecision"`
			StatusCheckRollup []ghCheck `json:"statusCheckRollup"`
			UpdatedAt         string    `json:"updatedAt"`
			URL               string    `json:"url"`
		}
		if err := c.runJSON(ctx, dir, nil, &raw, args...); err != nil {
			return nil, err
		}
		prs := make([]PullRequest, 0, len(raw))
		for _, r := range raw {
			prs = append(prs, PullRequest{
				Number: r.Number, Title: r.Title, Author: r.Author.Login,
				HeadRef: r.HeadRefName, BaseRef: r.BaseRefName, Draft: r.IsDraft, State: r.State,
				ReviewDecision: r.ReviewDecision, Checks: countChecks(r.StatusCheckRollup),
				UpdatedAt: r.UpdatedAt, URL: r.URL,
			})
		}
		return prs, nil
	})
}

// File is a file changed by a pull request.
type File struct {
	Path         string `json:"path"`
	PreviousPath string `json:"previousPath"` // renames
	Status       string `json:"status"`       // added, removed, modified, renamed, copied, changed
	Additions    int    `json:"additions"`
	Deletions    int    `json:"deletions"`
	// NoPatch: GitHub sent no patch (binary or very large file).
	NoPatch bool `json:"noPatch"`
}

// Commit is a commit of a pull request.
type Commit struct {
	OID      string `json:"oid"`
	Headline string `json:"headline"`
	Author   string `json:"author"`
	Date     string `json:"date"`
}

// Review is a submitted review (approval, changes requested or comment).
type Review struct {
	Author      string `json:"author"`
	State       string `json:"state"` // APPROVED, CHANGES_REQUESTED, COMMENTED, DISMISSED
	BodyHTML    string `json:"bodyHtml"`
	SubmittedAt string `json:"submittedAt"`
}

// Comment is a review comment in a thread.
type Comment struct {
	ID         string `json:"id"`
	DatabaseID int64  `json:"databaseId"`
	Author     string `json:"author"`
	Body       string `json:"body"`
	BodyHTML   string `json:"bodyHtml"`
	CreatedAt  string `json:"createdAt"`
	URL        string `json:"url"`
	// Pending: part of my review that isn't submitted yet (only I see it).
	Pending bool `json:"pending"`
}

// Thread is a review thread on a line (or a range of lines) of a file.
type Thread struct {
	ID   string `json:"id"`
	Path string `json:"path"`
	// Line is the (last) commented line on Side, 1-based; 0 when the thread is
	// outdated (the line is gone from the current diff) or on the whole file.
	Line         int    `json:"line"`
	StartLine    int    `json:"startLine"`
	OriginalLine int    `json:"originalLine"`
	Side         string `json:"side"` // LEFT (base) or RIGHT (head)
	Resolved     bool   `json:"resolved"`
	Outdated     bool   `json:"outdated"`
	// FileLevel: a comment on the file rather than on lines.
	FileLevel    bool      `json:"fileLevel"`
	ResolvedBy   string    `json:"resolvedBy"`
	CanResolve   bool      `json:"canResolve"`
	CanUnresolve bool      `json:"canUnresolve"`
	CanReply     bool      `json:"canReply"`
	Comments     []Comment `json:"comments"`
}

// Details is everything the pull request tab shows.
type Details struct {
	ID              string   `json:"id"` // GraphQL node id
	Number          int      `json:"number"`
	Title           string   `json:"title"`
	URL             string   `json:"url"`
	State           string   `json:"state"`
	Draft           bool     `json:"draft"`
	Author          string   `json:"author"`
	BaseRef         string   `json:"baseRef"`
	HeadRef         string   `json:"headRef"`
	BaseOID         string   `json:"baseOid"`
	HeadOID         string   `json:"headOid"`
	CrossRepository bool     `json:"crossRepository"`
	HeadOwner       string   `json:"headOwner"`
	ReviewDecision  string   `json:"reviewDecision"`
	CreatedAt       string   `json:"createdAt"`
	UpdatedAt       string   `json:"updatedAt"`
	Additions       int      `json:"additions"`
	Deletions       int      `json:"deletions"`
	Labels          []string `json:"labels"`
	BodyHTML        string   `json:"bodyHtml"`
	Files           []File   `json:"files"`
	Commits         []Commit `json:"commits"`
	Checks          []Check  `json:"checks"`
	Reviews         []Review `json:"reviews"`
	Threads         []Thread `json:"threads"`
	// PendingReviewID is my unsubmitted review ("" when none); comments added "to the
	// review" collect there until it is submitted.
	PendingReviewID       string `json:"pendingReviewId"`
	PendingReviewComments int    `json:"pendingReviewComments"`
	Repo                  Repo   `json:"repo"`
}

// prData is a cached pull request: its details plus the changed files with their
// patches, which stay in Go (the diff tab rebuilds both sides from them).
type prData struct {
	details Details
	files   map[string]restFile // by path
}

// viewFields are the gh pr view --json fields the tab needs.
const viewFields = "number,title,body,author,baseRefName,headRefName,baseRefOid,headRefOid,isCrossRepository," +
	"headRepositoryOwner,isDraft,state,reviewDecision,url,createdAt,updatedAt,additions,deletions,commits," +
	"statusCheckRollup,reviews,labels"

// Details loads a pull request: gh pr view, the changed files (REST) and the review
// threads (GraphQL), fetched in parallel.
func (c *Client) Details(ctx context.Context, dir string, number int, force bool) (Details, error) {
	data, err := c.prData(ctx, dir, number, force)
	if err != nil {
		return Details{}, err
	}
	return data.details, nil
}

func (c *Client) prData(ctx context.Context, dir string, number int, force bool) (*prData, error) {
	repo, err := c.Repo(ctx, dir)
	if err != nil {
		return nil, err
	}
	return c.details.get(prKey(repo, number), force, func() (*prData, error) {
		ctx, cancel := context.WithTimeout(ctx, readTimeout)
		defer cancel()
		var (
			wg                            sync.WaitGroup
			view                          prView
			files                         []restFile
			threads                       threadPage
			errView, errFiles, errThreads error
		)
		wg.Go(func() {
			errView = c.runJSON(ctx, dir, nil, &view, "pr", "view", strconv.Itoa(number), "--repo", repo.NameWithOwner, "--json", viewFields)
		})
		wg.Go(func() { files, errFiles = c.files(ctx, dir, repo, number) })
		wg.Go(func() { threads, errThreads = c.threads(ctx, dir, repo, number) })
		wg.Wait()
		for _, err := range []error{errView, errFiles, errThreads} {
			if err != nil {
				return nil, err
			}
		}
		return buildDetails(repo, view, files, threads), nil
	})
}

type prView struct {
	Number              int       `json:"number"`
	Title               string    `json:"title"`
	Body                string    `json:"body"`
	Author              ghAuthor  `json:"author"`
	BaseRefName         string    `json:"baseRefName"`
	HeadRefName         string    `json:"headRefName"`
	BaseRefOid          string    `json:"baseRefOid"`
	HeadRefOid          string    `json:"headRefOid"`
	IsCrossRepository   bool      `json:"isCrossRepository"`
	HeadRepositoryOwner ghAuthor  `json:"headRepositoryOwner"`
	IsDraft             bool      `json:"isDraft"`
	State               string    `json:"state"`
	ReviewDecision      string    `json:"reviewDecision"`
	URL                 string    `json:"url"`
	CreatedAt           string    `json:"createdAt"`
	UpdatedAt           string    `json:"updatedAt"`
	Additions           int       `json:"additions"`
	Deletions           int       `json:"deletions"`
	StatusCheckRollup   []ghCheck `json:"statusCheckRollup"`
	Commits             []struct {
		Oid             string `json:"oid"`
		MessageHeadline string `json:"messageHeadline"`
		CommittedDate   string `json:"committedDate"`
		Authors         []struct {
			Login string `json:"login"`
			Name  string `json:"name"`
		} `json:"authors"`
	} `json:"commits"`
	Reviews []struct {
		Author      ghAuthor `json:"author"`
		State       string   `json:"state"`
		Body        string   `json:"body"`
		SubmittedAt string   `json:"submittedAt"`
	} `json:"reviews"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
}

// restFile is an item of GET /repos/{owner}/{repo}/pulls/{number}/files.
type restFile struct {
	Filename         string `json:"filename"`
	PreviousFilename string `json:"previous_filename"`
	Status           string `json:"status"`
	Additions        int    `json:"additions"`
	Deletions        int    `json:"deletions"`
	Patch            string `json:"patch"`
	SHA              string `json:"sha"`
}

// files lists the changed files with their patches (GitHub caps a PR at 3000 files).
func (c *Client) files(ctx context.Context, dir string, repo Repo, number int) ([]restFile, error) {
	out, err := c.run(ctx, dir, nil, "api", "--paginate", fmt.Sprintf("repos/%s/pulls/%d/files?per_page=100", repo.NameWithOwner, number))
	if err != nil {
		return nil, err
	}
	// --paginate prints one JSON array per page, back to back.
	dec := json.NewDecoder(strings.NewReader(string(out)))
	var all []restFile
	for dec.More() {
		var page []restFile
		if err := dec.Decode(&page); err != nil {
			return nil, fmt.Errorf("gh api pulls/files: unexpected output: %w", err)
		}
		all = append(all, page...)
	}
	return all, nil
}

func buildDetails(repo Repo, v prView, files []restFile, tp threadPage) *prData {
	d := Details{
		ID: tp.ID, Number: v.Number, Title: v.Title, URL: v.URL, State: v.State, Draft: v.IsDraft,
		Author: v.Author.Login, BaseRef: v.BaseRefName, HeadRef: v.HeadRefName,
		BaseOID: v.BaseRefOid, HeadOID: v.HeadRefOid, CrossRepository: v.IsCrossRepository,
		HeadOwner: v.HeadRepositoryOwner.Login, ReviewDecision: v.ReviewDecision,
		CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt, Additions: v.Additions, Deletions: v.Deletions,
		BodyHTML: markdown(v.Body), PendingReviewID: tp.PendingReviewID, PendingReviewComments: tp.PendingComments,
		Labels: []string{}, Files: []File{}, Commits: []Commit{}, Checks: []Check{}, Reviews: []Review{},
		Threads: tp.Threads, Repo: repo,
	}
	if d.Threads == nil {
		d.Threads = []Thread{}
	}
	for _, l := range v.Labels {
		d.Labels = append(d.Labels, l.Name)
	}
	byPath := make(map[string]restFile, len(files))
	for _, f := range files {
		d.Files = append(d.Files, File{
			Path: f.Filename, PreviousPath: f.PreviousFilename, Status: f.Status,
			Additions: f.Additions, Deletions: f.Deletions, NoPatch: f.Patch == "",
		})
		byPath[f.Filename] = f
	}
	for _, cm := range v.Commits {
		author := ""
		if len(cm.Authors) > 0 {
			author = cm.Authors[0].Login
			if author == "" {
				author = cm.Authors[0].Name
			}
		}
		d.Commits = append(d.Commits, Commit{OID: cm.Oid, Headline: cm.MessageHeadline, Author: author, Date: cm.CommittedDate})
	}
	for _, ch := range v.StatusCheckRollup {
		d.Checks = append(d.Checks, ch.check())
	}
	for _, r := range v.Reviews {
		// Bare comments of a review show up in its threads; keep reviews that say something.
		if r.State == "COMMENTED" && strings.TrimSpace(r.Body) == "" {
			continue
		}
		d.Reviews = append(d.Reviews, Review{Author: r.Author.Login, State: r.State, BodyHTML: markdown(r.Body), SubmittedAt: r.SubmittedAt})
	}
	return &prData{details: d, files: byPath}
}

// markdown renders GitHub-flavoured Markdown without raw HTML (see internal/preview).
func markdown(src string) string {
	if strings.TrimSpace(src) == "" {
		return ""
	}
	out, err := preview.Markdown(src)
	if err != nil {
		return "<pre>" + html.EscapeString(src) + "</pre>"
	}
	return out
}
