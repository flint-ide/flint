package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Every mutation here changes something on GitHub, so each runs only on an explicit
// user action and drops the cached pull request afterwards.

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
	case n.Path == "" || n.Line < 1:
		return errors.New("pick a line to comment on")
	case n.Side != "LEFT" && n.Side != "RIGHT":
		return fmt.Errorf("unknown diff side %q", n.Side)
	case n.StartLine < 0 || n.StartLine > n.Line:
		return errors.New("the comment's first line is after its last")
	}
	return nil
}

// target resolves the repository and the pull request a mutation acts on.
func (c *Client) target(ctx context.Context, dir string, number int) (Details, error) {
	data, err := c.prData(ctx, dir, number, false)
	if err != nil {
		return Details{}, err
	}
	return data.details, nil
}

// Reply answers a review thread (posted right away).
func (c *Client) Reply(ctx context.Context, dir string, number int, threadID, body string) error {
	if strings.TrimSpace(body) == "" {
		return errors.New("the reply is empty")
	}
	d, err := c.target(ctx, dir, number)
	if err != nil {
		return err
	}
	defer c.Invalidate(d.Repo, number)
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	return c.graphql(ctx, dir, replyMutation, map[string]any{"thread": threadID, "body": body}, nil)
}

// SetResolved resolves or unresolves a review thread.
func (c *Client) SetResolved(ctx context.Context, dir string, number int, threadID string, resolved bool) error {
	d, err := c.target(ctx, dir, number)
	if err != nil {
		return err
	}
	defer c.Invalidate(d.Repo, number)
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	query := unresolveMutation
	if resolved {
		query = resolveMutation
	}
	return c.graphql(ctx, dir, query, map[string]any{"thread": threadID}, nil)
}

// AddComment comments on lines of a file: either a single comment posted at once
// (REST, like "Add single comment"), or a thread in my pending review (GraphQL,
// like "Start a review"), which only I see until the review is submitted.
func (c *Client) AddComment(ctx context.Context, dir string, number int, n NewComment) error {
	if err := n.validate(); err != nil {
		return err
	}
	d, err := c.target(ctx, dir, number)
	if err != nil {
		return err
	}
	defer c.Invalidate(d.Repo, number)
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	if !n.Review {
		body := map[string]any{"body": n.Body, "commit_id": d.HeadOID, "path": n.Path, "line": n.Line, "side": n.Side}
		if n.StartLine > 0 && n.StartLine < n.Line {
			body["start_line"], body["start_side"] = n.StartLine, n.Side
		}
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		_, err = c.run(ctx, dir, payload, "api", "--method", "POST",
			fmt.Sprintf("repos/%s/pulls/%s/comments", d.Repo.NameWithOwner, strconv.Itoa(number)), "--input", "-")
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
		if err := c.graphql(ctx, dir, startReviewMutation, map[string]any{"pr": d.ID, "commit": d.HeadOID}, &started); err != nil {
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
	return c.graphql(ctx, dir, addThreadMutation, vars, nil)
}

// SubmitReview submits my review: the pending one with its comments, or a new one.
// event is COMMENT, APPROVE or REQUEST_CHANGES.
func (c *Client) SubmitReview(ctx context.Context, dir string, number int, event, body string) error {
	switch event {
	case "COMMENT", "APPROVE", "REQUEST_CHANGES":
	default:
		return fmt.Errorf("unknown review event %q", event)
	}
	d, err := c.target(ctx, dir, number)
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
		return c.graphql(ctx, dir, submitPendingMutation, map[string]any{"review": d.PendingReviewID, "event": event, "body": text}, nil)
	}
	return c.graphql(ctx, dir, submitNewMutation, map[string]any{"pr": d.ID, "event": event, "body": text}, nil)
}

// DiscardReview deletes my pending review and its comments.
func (c *Client) DiscardReview(ctx context.Context, dir string, number int) error {
	d, err := c.target(ctx, dir, number)
	if err != nil {
		return err
	}
	if d.PendingReviewID == "" {
		return errors.New("there is no pending review")
	}
	defer c.Invalidate(d.Repo, number)
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	return c.graphql(ctx, dir, discardMutation, map[string]any{"review": d.PendingReviewID}, nil)
}

// Checkout switches the project to the pull request's branch (gh pr checkout). It
// refuses when dirty lists local changes to tracked files, which a checkout could
// carry over or clobber.
func (c *Client) Checkout(ctx context.Context, dir string, number int, dirty []string) error {
	if len(dirty) > 0 {
		return fmt.Errorf("commit, stash or roll back your %d changed file(s) before checking out #%d (e.g. %s)", len(dirty), number, dirty[0])
	}
	repo, err := c.Repo(ctx, dir)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, checkoutTimeout)
	defer cancel()
	_, err = c.run(ctx, dir, nil, "pr", "checkout", strconv.Itoa(number), "--repo", repo.NameWithOwner)
	return err
}
