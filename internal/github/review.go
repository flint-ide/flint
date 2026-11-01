package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// Every mutation here changes something on GitHub, so each runs only on an explicit
// user action and drops the cached pull request afterwards. User text travels in
// JSON bodies, never in a URL.

const replyMutation = `mutation($thread:ID!,$body:String!){
  addPullRequestReviewThreadReply(input:{pullRequestReviewThreadId:$thread,body:$body}){comment{id}}
}`

const resolveMutation = `mutation($thread:ID!){resolveReviewThread(input:{threadId:$thread}){thread{id isResolved}}}`

const unresolveMutation = `mutation($thread:ID!){unresolveReviewThread(input:{threadId:$thread}){thread{id isResolved}}}`

const startReviewMutation = `mutation($pr:ID!,$commit:GitObjectID){
  addPullRequestReview(input:{pullRequestId:$pr,commitOID:$commit}){pullRequestReview{id}}
}`

const addThreadMutation = `mutation($review:ID!,$path:String!,$line:Int!,$side:DiffSide!,$startLine:Int,$startSide:DiffSide,$body:String!){
  addPullRequestReviewThread(input:{pullRequestReviewId:$review,path:$path,line:$line,side:$side,startLine:$startLine,startSide:$startSide,body:$body}){thread{id}}
}`

const submitPendingMutation = `mutation($review:ID!,$event:PullRequestReviewEvent!,$body:String){
  submitPullRequestReview(input:{pullRequestReviewId:$review,event:$event,body:$body}){pullRequestReview{id state}}
}`

const submitNewMutation = `mutation($pr:ID!,$event:PullRequestReviewEvent!,$body:String){
  addPullRequestReview(input:{pullRequestId:$pr,event:$event,body:$body}){pullRequestReview{id state}}
}`

const discardMutation = `mutation($review:ID!){deletePullRequestReview(input:{pullRequestReviewId:$review}){pullRequestReview{id}}}`

// maxBodyLen bounds a comment or review body (GitHub's own limit is 65536).
const maxBodyLen = 65536

// NewComment is a comment on lines of a file.
type NewComment struct {
	Path string `json:"path"`
	// Line is the (last) line, 1-based, on Side: LEFT (base) or RIGHT (head).
	Line int    `json:"line"`
	Side string `json:"side"`
	// StartLine (optional) makes it a comment on StartLine..Line.
	StartLine int    `json:"startLine"`
	Body      string `json:"body"`
	// Review adds the comment to my pending review (started if needed) instead of
	// posting it right away.
	Review bool `json:"review"`
}

func (n NewComment) validate() error {
	switch {
	case strings.TrimSpace(n.Body) == "":
		return errors.New("the comment is empty")
	case len(n.Body) > maxBodyLen:
		return errors.New("the comment is too long")
	case n.Path == "" || n.Line < 1:
		return errors.New("pick a line to comment on")
	case n.Side != "LEFT" && n.Side != "RIGHT":
		return fmt.Errorf("unknown diff side %q", n.Side)
	case n.StartLine < 0 || n.StartLine > n.Line:
		return errors.New("the comment's first line is after its last")
	}
	return nil
}

// target resolves the auth and the pull request a mutation acts on.
func (c *Client) target(ctx context.Context, dir string, number int) (Auth, Details, error) {
	data, err := c.prData(ctx, dir, number, false)
	if err != nil {
		return Auth{}, Details{}, err
	}
	a, err := c.auth()
	return a, data.details, err
}

// checkThread accepts only a thread of this pull request (ids come from the web view).
func checkThread(d Details, id string) error {
	for _, t := range d.Threads {
		if t.ID == id && id != "" {
			return nil
		}
	}
	return fmt.Errorf("no such review thread on #%d", d.Number)
}

// Reply answers a review thread (posted right away).
func (c *Client) Reply(ctx context.Context, dir string, number int, threadID, body string) error {
	if strings.TrimSpace(body) == "" {
		return errors.New("the reply is empty")
	}
	if len(body) > maxBodyLen {
		return errors.New("the reply is too long")
	}
	a, d, err := c.target(ctx, dir, number)
	if err != nil {
		return err
	}
	if err := checkThread(d, threadID); err != nil {
		return err
	}
	defer c.Invalidate(d.Repo, number)
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	return c.graphql(ctx, a, replyMutation, map[string]any{"thread": threadID, "body": body}, nil)
}

// SetResolved resolves or unresolves a review thread.
func (c *Client) SetResolved(ctx context.Context, dir string, number int, threadID string, resolved bool) error {
	a, d, err := c.target(ctx, dir, number)
	if err != nil {
		return err
	}
	if err := checkThread(d, threadID); err != nil {
		return err
	}
	defer c.Invalidate(d.Repo, number)
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	query := unresolveMutation
	if resolved {
		query = resolveMutation
	}
	return c.graphql(ctx, a, query, map[string]any{"thread": threadID}, nil)
}

// AddComment comments on lines of a file: either a single comment posted at once
// (REST, like "Add single comment"), or a thread in my pending review (GraphQL,
// like "Start a review"), which only I see until the review is submitted.
func (c *Client) AddComment(ctx context.Context, dir string, number int, n NewComment) error {
	if err := n.validate(); err != nil {
		return err
	}
	a, d, err := c.target(ctx, dir, number)
	if err != nil {
		return err
	}
	if !changesFile(d, n.Path) {
		return fmt.Errorf("%s is not changed by #%d", n.Path, number)
	}
	defer c.Invalidate(d.Repo, number)
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	if !n.Review {
		body := map[string]any{"body": n.Body, "commit_id": d.HeadOID, "path": n.Path, "line": n.Line, "side": n.Side}
		if n.StartLine > 0 && n.StartLine < n.Line {
			body["start_line"], body["start_side"] = n.StartLine, n.Side
		}
		_, err := c.do(ctx, a, call{method: http.MethodPost, url: rest(a, "/repos/%s/%s/pulls/%d/comments", d.Repo.Owner, d.Repo.Name, number), body: body})
		return err
	}
	review := d.PendingReviewID
	if review == "" {
		var started struct {
			AddPullRequestReview struct {
				PullRequestReview struct {
					ID string `json:"id"`
				} `json:"pullRequestReview"`
			} `json:"addPullRequestReview"`
		}
		if err := c.graphql(ctx, a, startReviewMutation, map[string]any{"pr": d.ID, "commit": d.HeadOID}, &started); err != nil {
			return err
		}
		review = started.AddPullRequestReview.PullRequestReview.ID
		if review == "" {
			return errors.New("GitHub did not start a review")
		}
	}
	vars := map[string]any{"review": review, "path": n.Path, "line": n.Line, "side": n.Side, "body": n.Body, "startLine": nil, "startSide": nil}
	if n.StartLine > 0 && n.StartLine < n.Line {
		vars["startLine"], vars["startSide"] = n.StartLine, n.Side
	}
	return c.graphql(ctx, a, addThreadMutation, vars, nil)
}

func changesFile(d Details, path string) bool {
	for _, f := range d.Files {
		if f.Path == path {
			return true
		}
	}
	return false
}

// SubmitReview submits my review: the pending one with its comments, or a new one.
// event is COMMENT, APPROVE or REQUEST_CHANGES.
func (c *Client) SubmitReview(ctx context.Context, dir string, number int, event, body string) error {
	switch event {
	case "COMMENT", "APPROVE", "REQUEST_CHANGES":
	default:
		return fmt.Errorf("unknown review event %q", event)
	}
	if len(body) > maxBodyLen {
		return errors.New("the review summary is too long")
	}
	a, d, err := c.target(ctx, dir, number)
	if err != nil {
		return err
	}
	if event != "APPROVE" && strings.TrimSpace(body) == "" && d.PendingReviewComments == 0 {
		return errors.New("write a summary (or add comments) first")
	}
	defer c.Invalidate(d.Repo, number)
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	var text any // null rather than "" when there is no summary
	if strings.TrimSpace(body) != "" {
		text = body
	}
	if d.PendingReviewID != "" {
		return c.graphql(ctx, a, submitPendingMutation, map[string]any{"review": d.PendingReviewID, "event": event, "body": text}, nil)
	}
	return c.graphql(ctx, a, submitNewMutation, map[string]any{"pr": d.ID, "event": event, "body": text}, nil)
}

// DiscardReview deletes my pending review and its comments.
func (c *Client) DiscardReview(ctx context.Context, dir string, number int) error {
	a, d, err := c.target(ctx, dir, number)
	if err != nil {
		return err
	}
	if d.PendingReviewID == "" {
		return errors.New("there is no pending review")
	}
	defer c.Invalidate(d.Repo, number)
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	return c.graphql(ctx, a, discardMutation, map[string]any{"review": d.PendingReviewID}, nil)
}

// Checkout switches the project to the pull request's branch: git fetch
// <remote> pull/N/head, then a local branch at that commit (created, or
// fast-forwarded). A branch of this repository keeps its name and tracks it;
// a fork's branch is named <owner>/<branch>. It refuses when dirty lists local
// changes to tracked files, which a checkout could carry over or clobber.
func (c *Client) Checkout(ctx context.Context, dir string, number int, dirty []string) error {
	if len(dirty) > 0 {
		return fmt.Errorf("commit, stash or roll back your %d changed file(s) before checking out #%d (e.g. %s)", len(dirty), number, dirty[0])
	}
	data, err := c.prData(ctx, dir, number, true)
	if err != nil {
		return err
	}
	d := data.details
	if err := checkBranch(d.HeadRef); err != nil {
		return err
	}
	branch, upstream := d.HeadRef, d.HeadRef
	if d.CrossRepository {
		if !validName(d.HeadOwner) {
			return fmt.Errorf("unexpected owner of #%d's branch: %q", number, d.HeadOwner)
		}
		branch, upstream = d.HeadOwner+"/"+d.HeadRef, ""
	}
	ctx, cancel := context.WithTimeout(ctx, checkoutTimeout)
	defer cancel()
	g, err := openGit(ctx, dir)
	if err != nil {
		return err
	}
	return g.CheckoutPullRequest(ctx, d.Repo.Remote, number, branch, upstream)
}
