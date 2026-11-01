package github

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
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
	// RunID and JobID name the GitHub Actions run and job behind a check run
	// (0 for other CI systems): what Fix with agent and Open log read.
	RunID int64 `json:"runId"`
	JobID int64 `json:"jobId"`
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

// ghCheck is an item of a statusCheckRollup: a CheckRun or a StatusContext,
// as GraphQL returns it (REST check runs and statuses are mapped to it).
type ghCheck struct {
	Typename     string `json:"__typename"`
	Name         string `json:"name"`
	WorkflowName string `json:"-"`
	Status       string `json:"status"`
	Conclusion   string `json:"conclusion"`
	DetailsURL   string `json:"detailsUrl"`
	Context      string `json:"context"`
	State        string `json:"state"`
	TargetURL    string `json:"targetUrl"`
	CheckSuite   *struct {
		WorkflowRun *struct {
			Workflow struct {
				Name string `json:"name"`
			} `json:"workflow"`
		} `json:"workflowRun"`
	} `json:"checkSuite"`
}

// workflow is the name of the Actions workflow behind a check run ("" for other CI).
func (g ghCheck) workflow() string {
	if g.WorkflowName != "" {
		return g.WorkflowName
	}
	if g.CheckSuite != nil && g.CheckSuite.WorkflowRun != nil {
		return g.CheckSuite.WorkflowRun.Workflow.Name
	}
	return ""
}

// rollupField is the head commit's check rollup as a GraphQL selection on a
// PullRequest (aliased: the details also select the commit list).
const rollupField = `rollup: commits(last:1){nodes{commit{statusCheckRollup{contexts(first:100){nodes{
  __typename
  ... on CheckRun{name status conclusion detailsUrl checkSuite{workflowRun{workflow{name}}}}
  ... on StatusContext{context state targetUrl}
}}}}}}`

// rollup decodes rollupField.
type rollup struct {
	Nodes []struct {
		Commit struct {
			StatusCheckRollup *struct {
				Contexts struct {
					Nodes []ghCheck `json:"nodes"`
				} `json:"contexts"`
			} `json:"statusCheckRollup"`
		} `json:"commit"`
	} `json:"nodes"`
}

func (r rollup) checks() []ghCheck {
	if len(r.Nodes) == 0 || r.Nodes[0].Commit.StatusCheckRollup == nil {
		return nil
	}
	return r.Nodes[0].Commit.StatusCheckRollup.Contexts.Nodes
}

// check normalizes the item; repoURL (the repository's https URL) decides
// whether its details URL names a run of this repository's Actions.
func (g ghCheck) check(repoURL string) Check {
	if g.Typename == "StatusContext" {
		return Check{Name: g.Context, State: statusState(g.State), Status: g.State, URL: g.TargetURL}
	}
	run, job := actionsIDs(repoURL, g.DetailsURL)
	return Check{
		Name:       g.Name,
		Workflow:   g.workflow(),
		State:      checkRunState(g.Status, g.Conclusion),
		Status:     g.Status,
		Conclusion: g.Conclusion,
		URL:        g.DetailsURL,
		RunID:      run,
		JobID:      job,
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
		switch r.check("").State {
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

// listFields are what a row of the list needs.
const listFields = `number title author{login} headRefName baseRefName isDraft state reviewDecision updatedAt url ` + rollupField

const listQuery = `query($owner:String!,$name:String!,$states:[PullRequestState!],$first:Int!){
  repository(owner:$owner,name:$name){
    pullRequests(states:$states,first:$first,orderBy:{field:CREATED_AT,direction:DESC}){nodes{` + listFields + `}}
  }
}`

const searchQuery = `query($q:String!,$first:Int!){
  search(query:$q,type:ISSUE,first:$first){nodes{... on PullRequest{` + listFields + `}}}
}`

type listNode struct {
	Number         int      `json:"number"`
	Title          string   `json:"title"`
	Author         ghAuthor `json:"author"`
	HeadRefName    string   `json:"headRefName"`
	BaseRefName    string   `json:"baseRefName"`
	IsDraft        bool     `json:"isDraft"`
	State          string   `json:"state"`
	ReviewDecision string   `json:"reviewDecision"`
	UpdatedAt      string   `json:"updatedAt"`
	URL            string   `json:"url"`
	Rollup         rollup   `json:"rollup"`
}

// listRequest picks the query for a filter, like the GitHub CLI: the
// repository's pull requests for everyone's, a search for "mine" and "review".
// "closed" includes merged pull requests, as on GitHub.
func listRequest(repo Repo, f ListFilter) (string, map[string]any, error) {
	var states []string
	search := "repo:" + repo.NameWithOwner + " is:pr"
	switch f.State {
	case "", "open":
		states, search = []string{"OPEN"}, search+" is:open"
	case "closed":
		states, search = []string{"CLOSED", "MERGED"}, search+" is:closed"
	case "merged":
		states, search = []string{"MERGED"}, search+" is:merged"
	case "all":
		states = []string{"OPEN", "CLOSED", "MERGED"}
	default:
		return "", nil, fmt.Errorf("unknown pull request state %q", f.State)
	}
	switch f.Who {
	case "", "all":
		return listQuery, map[string]any{"owner": repo.Owner, "name": repo.Name, "states": states, "first": listLimit}, nil
	case "mine":
		search += " author:@me"
	case "review":
		search += " review-requested:@me"
	default:
		return "", nil, fmt.Errorf("unknown pull request filter %q", f.Who)
	}
	return searchQuery, map[string]any{"q": search + " sort:created-desc", "first": listLimit}, nil
}

// List lists pull requests of the repository of dir.
func (c *Client) List(ctx context.Context, dir string, f ListFilter, force bool) ([]PullRequest, error) {
	a, info, err := c.session(ctx, dir)
	if err != nil {
		return nil, err
	}
	query, vars, err := listRequest(info.Repo, f)
	if err != nil {
		return nil, err
	}
	return c.lists.get(a.Host+"|"+info.NameWithOwner+"|"+f.Who+"|"+f.State, force, func() ([]PullRequest, error) {
		ctx, cancel := context.WithTimeout(ctx, readTimeout)
		defer cancel()
		var data struct {
			Repository *struct {
				PullRequests struct {
					Nodes []listNode `json:"nodes"`
				} `json:"pullRequests"`
			} `json:"repository"`
			Search *struct {
				Nodes []listNode `json:"nodes"`
			} `json:"search"`
		}
		if err := c.graphql(ctx, a, query, vars, &data); err != nil {
			return nil, err
		}
		var nodes []listNode
		switch {
		case data.Repository != nil:
			nodes = data.Repository.PullRequests.Nodes
		case data.Search != nil:
			nodes = data.Search.Nodes
		}
		prs := make([]PullRequest, 0, len(nodes))
		for _, r := range nodes {
			if r.Number == 0 { // a search hit that isn't a pull request
				continue
			}
			prs = append(prs, PullRequest{
				Number: r.Number, Title: r.Title, Author: r.Author.Login,
				HeadRef: r.HeadRefName, BaseRef: r.BaseRefName, Draft: r.IsDraft, State: r.State,
				ReviewDecision: r.ReviewDecision, Checks: countChecks(r.Rollup.checks()),
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

// viewQuery reads what the tab shows of a pull request (threads come apart:
// they are paginated).
const viewQuery = `query($owner:String!,$name:String!,$number:Int!){
  repository(owner:$owner,name:$name){
    pullRequest(number:$number){
      number title body author{login} baseRefName headRefName baseRefOid headRefOid isCrossRepository
      headRepositoryOwner{login} isDraft state reviewDecision url createdAt updatedAt additions deletions
      commits(first:100){nodes{commit{oid messageHeadline committedDate authors(first:1){nodes{name user{login}}}}}}
      reviews(first:100){nodes{author{login} state body submittedAt}}
      labels(first:100){nodes{name}}
      ` + rollupField + `
    }
  }
}`

// Details loads a pull request: the view (GraphQL), the changed files (REST)
// and the review threads (GraphQL), fetched in parallel.
func (c *Client) Details(ctx context.Context, dir string, number int, force bool) (Details, error) {
	data, err := c.prData(ctx, dir, number, force)
	if err != nil {
		return Details{}, err
	}
	return data.details, nil
}

func (c *Client) prData(ctx context.Context, dir string, number int, force bool) (*prData, error) {
	if number <= 0 {
		return nil, fmt.Errorf("invalid pull request number %d", number)
	}
	a, info, err := c.session(ctx, dir)
	if err != nil {
		return nil, err
	}
	repo := info.Repo
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
		wg.Go(func() { view, errView = c.view(ctx, a, repo, number) })
		wg.Go(func() { files, errFiles = c.files(ctx, a, repo, number) })
		wg.Go(func() { threads, errThreads = c.threads(ctx, a, repo, number) })
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
	Number              int      `json:"number"`
	Title               string   `json:"title"`
	Body                string   `json:"body"`
	Author              ghAuthor `json:"author"`
	BaseRefName         string   `json:"baseRefName"`
	HeadRefName         string   `json:"headRefName"`
	BaseRefOid          string   `json:"baseRefOid"`
	HeadRefOid          string   `json:"headRefOid"`
	IsCrossRepository   bool     `json:"isCrossRepository"`
	HeadRepositoryOwner ghAuthor `json:"headRepositoryOwner"`
	IsDraft             bool     `json:"isDraft"`
	State               string   `json:"state"`
	ReviewDecision      string   `json:"reviewDecision"`
	URL                 string   `json:"url"`
	CreatedAt           string   `json:"createdAt"`
	UpdatedAt           string   `json:"updatedAt"`
	Additions           int      `json:"additions"`
	Deletions           int      `json:"deletions"`
	Rollup              rollup   `json:"rollup"`
	Commits             struct {
		Nodes []struct {
			Commit struct {
				Oid             string `json:"oid"`
				MessageHeadline string `json:"messageHeadline"`
				CommittedDate   string `json:"committedDate"`
				Authors         struct {
					Nodes []struct {
						Name string    `json:"name"`
						User *ghAuthor `json:"user"`
					} `json:"nodes"`
				} `json:"authors"`
			} `json:"commit"`
		} `json:"nodes"`
	} `json:"commits"`
	Reviews struct {
		Nodes []struct {
			Author      ghAuthor `json:"author"`
			State       string   `json:"state"`
			Body        string   `json:"body"`
			SubmittedAt string   `json:"submittedAt"`
		} `json:"nodes"`
	} `json:"reviews"`
	Labels struct {
		Nodes []struct {
			Name string `json:"name"`
		} `json:"nodes"`
	} `json:"labels"`
}

func (c *Client) view(ctx context.Context, a Auth, repo Repo, number int) (prView, error) {
	var data struct {
		Repository struct {
			PullRequest *prView `json:"pullRequest"`
		} `json:"repository"`
	}
	err := c.graphql(ctx, a, viewQuery, map[string]any{"owner": repo.Owner, "name": repo.Name, "number": number}, &data)
	if err != nil {
		return prView{}, err
	}
	if data.Repository.PullRequest == nil {
		return prView{}, fmt.Errorf("pull request #%d not found in %s", number, repo.NameWithOwner)
	}
	return *data.Repository.PullRequest, nil
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

// files lists the changed files with their patches (GitHub caps a PR at 3000
// files: 30 pages of 100).
func (c *Client) files(ctx context.Context, a Auth, repo Repo, number int) ([]restFile, error) {
	var all []restFile
	first := rest(a, "/repos/%s/%s/pulls/%d/files", repo.Owner, repo.Name, number) + "?per_page=100"
	err := c.pages(ctx, a, first, maxPages, func(body []byte) error {
		var page []restFile
		if err := json.Unmarshal(body, &page); err != nil {
			return fmt.Errorf("unexpected answer from GitHub (pull request files): %w", err)
		}
		all = append(all, page...)
		return nil
	})
	return all, err
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
	for _, l := range v.Labels.Nodes {
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
	for _, n := range v.Commits.Nodes {
		cm := n.Commit
		author := ""
		if len(cm.Authors.Nodes) > 0 {
			first := cm.Authors.Nodes[0]
			if first.User != nil {
				author = first.User.Login
			}
			if author == "" {
				author = first.Name
			}
		}
		d.Commits = append(d.Commits, Commit{OID: cm.Oid, Headline: cm.MessageHeadline, Author: author, Date: cm.CommittedDate})
	}
	for _, ch := range v.Rollup.checks() {
		d.Checks = append(d.Checks, ch.check(repo.URL))
	}
	for _, r := range v.Reviews.Nodes {
		// Bare comments of a review show up in its threads; keep reviews that say
		// something. My pending review is shown through its threads.
		if r.State == "PENDING" || (r.State == "COMMENTED" && strings.TrimSpace(r.Body) == "") {
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
