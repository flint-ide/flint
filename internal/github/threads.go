package github

import (
	"context"
	"fmt"
)

// maxThreadPages bounds the review-thread pages fetched (100 threads each).
const maxThreadPages = 10

const threadsQuery = `query($owner:String!,$name:String!,$number:Int!,$cursor:String){
  repository(owner:$owner,name:$name){
    pullRequest(number:$number){
      id
      reviews(states:PENDING,first:1){nodes{id comments{totalCount}}}
      reviewThreads(first:100,after:$cursor){
        pageInfo{hasNextPage endCursor}
        nodes{
          id isResolved isOutdated path line startLine originalLine diffSide subjectType
          viewerCanResolve viewerCanUnresolve viewerCanReply resolvedBy{login}
          comments(first:100){nodes{id databaseId author{login} body createdAt url state}}
        }
      }
    }
  }
}`

// threadPage is the part of a pull request the threads query returns.
type threadPage struct {
	ID              string
	PendingReviewID string
	PendingComments int
	Threads         []Thread
}

type gqlThread struct {
	ID                 string   `json:"id"`
	IsResolved         bool     `json:"isResolved"`
	IsOutdated         bool     `json:"isOutdated"`
	Path               string   `json:"path"`
	Line               *int     `json:"line"`
	StartLine          *int     `json:"startLine"`
	OriginalLine       *int     `json:"originalLine"`
	DiffSide           string   `json:"diffSide"`
	SubjectType        string   `json:"subjectType"`
	ViewerCanResolve   bool     `json:"viewerCanResolve"`
	ViewerCanUnresolve bool     `json:"viewerCanUnresolve"`
	ViewerCanReply     bool     `json:"viewerCanReply"`
	ResolvedBy         ghAuthor `json:"resolvedBy"`
	Comments           struct {
		Nodes []gqlComment `json:"nodes"`
	} `json:"comments"`
}

type gqlComment struct {
	ID         string   `json:"id"`
	DatabaseID int64    `json:"databaseId"`
	Author     ghAuthor `json:"author"`
	Body       string   `json:"body"`
	CreatedAt  string   `json:"createdAt"`
	URL        string   `json:"url"`
	State      string   `json:"state"`
}

// threads loads every review thread of a pull request, page by page.
func (c *Client) threads(ctx context.Context, a Auth, repo Repo, number int) (threadPage, error) {
	var page threadPage
	var cursor any // JSON null on the first page
	for range maxThreadPages {
		var data struct {
			Repository struct {
				PullRequest *struct {
					ID      string `json:"id"`
					Reviews struct {
						Nodes []struct {
							ID       string `json:"id"`
							Comments struct {
								TotalCount int `json:"totalCount"`
							} `json:"comments"`
						} `json:"nodes"`
					} `json:"reviews"`
					ReviewThreads struct {
						PageInfo struct {
							HasNextPage bool   `json:"hasNextPage"`
							EndCursor   string `json:"endCursor"`
						} `json:"pageInfo"`
						Nodes []gqlThread `json:"nodes"`
					} `json:"reviewThreads"`
				} `json:"pullRequest"`
			} `json:"repository"`
		}
		vars := map[string]any{"owner": repo.Owner, "name": repo.Name, "number": number, "cursor": cursor}
		if err := c.graphql(ctx, a, threadsQuery, vars, &data); err != nil {
			return threadPage{}, err
		}
		pr := data.Repository.PullRequest
		if pr == nil {
			return threadPage{}, fmt.Errorf("pull request #%d not found in %s", number, repo.NameWithOwner)
		}
		page.ID = pr.ID
		if len(pr.Reviews.Nodes) > 0 {
			page.PendingReviewID = pr.Reviews.Nodes[0].ID
			page.PendingComments = pr.Reviews.Nodes[0].Comments.TotalCount
		}
		for _, t := range pr.ReviewThreads.Nodes {
			page.Threads = append(page.Threads, t.thread())
		}
		if !pr.ReviewThreads.PageInfo.HasNextPage {
			break
		}
		cursor = pr.ReviewThreads.PageInfo.EndCursor
	}
	return page, nil
}

func (t gqlThread) thread() Thread {
	th := Thread{
		ID: t.ID, Path: t.Path, Line: deref(t.Line), StartLine: deref(t.StartLine), OriginalLine: deref(t.OriginalLine),
		Side: t.DiffSide, Resolved: t.IsResolved, Outdated: t.IsOutdated, FileLevel: t.SubjectType == "FILE",
		ResolvedBy: t.ResolvedBy.Login, CanResolve: t.ViewerCanResolve, CanUnresolve: t.ViewerCanUnresolve,
		CanReply: t.ViewerCanReply, Comments: make([]Comment, 0, len(t.Comments.Nodes)),
	}
	for _, cm := range t.Comments.Nodes {
		th.Comments = append(th.Comments, cm.comment())
	}
	return th
}

func (cm gqlComment) comment() Comment {
	return Comment{
		ID: cm.ID, DatabaseID: cm.DatabaseID, Author: cm.Author.Login, Body: cm.Body, BodyHTML: markdown(cm.Body),
		CreatedAt: cm.CreatedAt, URL: cm.URL, Pending: cm.State == "PENDING",
	}
}

func deref(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}
