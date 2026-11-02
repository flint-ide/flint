package githubtest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
)

// graphql answers the queries and mutations Flint sends, recognised by the
// fields they select (a fake, not a GraphQL server).
func (f *Fake) graphql(w http.ResponseWriter, body []byte) {
	var req struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"message": "Problems parsing JSON"})
		return
	}
	q, v := req.Query, req.Variables
	f.mu.Lock()
	defer f.mu.Unlock()
	if strings.HasPrefix(strings.TrimSpace(q), "mutation") {
		data, err := f.mutate(q, v)
		if err != "" {
			writeJSON(w, http.StatusOK, map[string]any{"data": nil, "errors": []any{map[string]any{"type": "UNPROCESSABLE", "message": err}}})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": data})
		return
	}
	switch {
	case strings.Contains(q, "reviewThreads("):
		p := f.pull(num(v["number"]))
		if p == nil {
			writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"repository": map[string]any{"pullRequest": nil}},
				"errors": []any{map[string]any{"type": "NOT_FOUND", "message": "Could not resolve to a PullRequest with the number of " + fmt.Sprint(v["number"]) + "."}}})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"repository": map[string]any{"pullRequest": f.threadsJSON(p)}}})
	case strings.Contains(q, "search("):
		query, _ := v["q"].(string)
		nodes := []any{}
		for _, p := range f.pulls {
			if strings.Contains(query, "review-requested:@me") && p.ReviewRequested ||
				strings.Contains(query, "author:@me") && p.AuthoredByMe {
				nodes = append(nodes, f.listNode(p))
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"search": map[string]any{"nodes": nodes}}})
	case strings.Contains(q, "pullRequests("):
		var states []string
		if s, ok := v["states"].([]any); ok {
			for _, x := range s {
				states = append(states, fmt.Sprint(x))
			}
		}
		nodes := []any{}
		for _, p := range f.pulls {
			if len(states) == 0 || slices.Contains(states, p.State) {
				nodes = append(nodes, f.listNode(p))
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"repository": map[string]any{"pullRequests": map[string]any{"nodes": nodes}}}})
	case strings.Contains(q, "headRefOid"):
		p := f.pull(num(v["number"]))
		var view any
		if p != nil {
			view = f.viewJSON(p)
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"repository": map[string]any{"pullRequest": view}}})
	default:
		writeJSON(w, http.StatusOK, map[string]any{"errors": []any{map[string]any{"message": "fake GitHub: unknown query"}}})
	}
}

func num(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case string:
		i, _ := strconv.Atoi(n)
		return i
	}
	return 0
}

func (f *Fake) rollupJSON(p *pull) map[string]any {
	contexts := []any{}
	if p.HeadSHA == HeadSHA {
		base := fmt.Sprintf("%s/actions/runs/%d/job/", f.RepoURL(), RunID)
		wf := map[string]any{"workflowRun": map[string]any{"workflow": map[string]any{"name": "CI"}}}
		contexts = append(contexts,
			map[string]any{"__typename": "CheckRun", "name": "Docs", "status": "COMPLETED", "conclusion": "SUCCESS", "detailsUrl": base + strconv.Itoa(PassedJob), "checkSuite": wf},
			map[string]any{"__typename": "CheckRun", "name": "Build and test (macOS)", "status": "COMPLETED", "conclusion": "FAILURE", "detailsUrl": base + strconv.Itoa(FailedJob), "checkSuite": wf},
			map[string]any{"__typename": "StatusContext", "context": "deploy/preview", "state": "PENDING", "targetUrl": "https://deploy.example/7"},
		)
	}
	return map[string]any{"nodes": []any{map[string]any{"commit": map[string]any{"statusCheckRollup": map[string]any{"contexts": map[string]any{"nodes": contexts}}}}}}
}

func (f *Fake) listNode(p *pull) map[string]any {
	return map[string]any{
		"number": p.Number, "title": p.Title, "author": map[string]any{"login": p.Author}, "headRefName": p.Head, "baseRefName": "main",
		"isDraft": p.Draft, "state": p.State, "reviewDecision": p.Decision, "updatedAt": "2026-10-30T10:00:00Z",
		"url": fmt.Sprintf("%s/pull/%d", f.RepoURL(), p.Number), "rollup": f.rollupJSON(p),
	}
}

func (f *Fake) viewJSON(p *pull) map[string]any {
	reviews := []any{
		map[string]any{"author": map[string]any{"login": "carol"}, "state": "COMMENTED", "body": "", "submittedAt": "2026-10-29T10:00:00Z"},
		map[string]any{"author": map[string]any{"login": "dave"}, "state": "CHANGES_REQUESTED", "body": "Please fix the *lint*.", "submittedAt": "2026-10-29T11:00:00Z"},
	}
	if p.PendingReview != "" {
		reviews = append(reviews, map[string]any{"author": map[string]any{"login": Login}, "state": "PENDING", "body": "", "submittedAt": nil})
	}
	adds, dels := 0, 0
	for _, fl := range p.Files {
		adds += fl["additions"].(int)
		dels += fl["deletions"].(int)
	}
	return map[string]any{
		"number": p.Number, "title": p.Title, "body": p.Body, "author": map[string]any{"login": p.Author},
		"baseRefName": "main", "headRefName": p.Head, "baseRefOid": BaseSHA, "headRefOid": p.HeadSHA,
		"isCrossRepository": p.HeadOwner != Owner, "headRepositoryOwner": map[string]any{"login": p.HeadOwner},
		"isDraft": p.Draft, "state": p.State, "reviewDecision": p.Decision, "url": fmt.Sprintf("%s/pull/%d", f.RepoURL(), p.Number),
		"createdAt": "2026-10-28T10:00:00Z", "updatedAt": "2026-10-30T10:00:00Z", "additions": adds, "deletions": dels,
		"commits": map[string]any{"nodes": []any{map[string]any{"commit": map[string]any{
			"oid": p.HeadSHA, "messageHeadline": "feat: " + strings.ToLower(p.Title), "committedDate": "2026-10-28T10:00:00Z",
			"authors": map[string]any{"nodes": []any{map[string]any{"name": "Alice", "user": map[string]any{"login": p.Author}}}},
		}}}},
		"reviews": map[string]any{"nodes": reviews},
		"labels":  map[string]any{"nodes": []any{map[string]any{"name": "enhancement"}}},
		"rollup":  f.rollupJSON(p),
	}
}

func (f *Fake) threadsJSON(p *pull) map[string]any {
	threads := []any{}
	for _, t := range p.Threads {
		comments := []any{}
		for _, c := range t.Comments {
			comments = append(comments, map[string]any{
				"id": c.ID, "databaseId": c.DatabaseID, "author": map[string]any{"login": c.Author}, "body": c.Body,
				"createdAt": "2026-10-29T11:00:00Z", "url": fmt.Sprintf("%s/pull/%d#discussion_r%d", f.RepoURL(), p.Number, c.DatabaseID), "state": c.State,
			})
		}
		var line, resolvedBy any
		if t.Line > 0 {
			line = t.Line
		}
		if t.ResolvedBy != "" {
			resolvedBy = map[string]any{"login": t.ResolvedBy}
		}
		threads = append(threads, map[string]any{
			"id": t.ID, "isResolved": t.Resolved, "isOutdated": t.Outdated, "path": t.Path, "line": line, "startLine": nil,
			"originalLine": t.OriginalLine, "diffSide": t.Side, "subjectType": "LINE",
			"viewerCanResolve": t.CanResolve && !t.Resolved, "viewerCanUnresolve": t.CanResolve && t.Resolved || t.Resolved,
			"viewerCanReply": t.CanReply, "resolvedBy": resolvedBy, "comments": map[string]any{"nodes": comments},
		})
	}
	pending := []any{}
	if p.PendingReview != "" {
		pending = append(pending, map[string]any{"id": p.PendingReview, "comments": map[string]any{"totalCount": p.PendingComments}})
	}
	return map[string]any{
		"id":            fmt.Sprintf("PR_kwDOAAABc85%07d", p.Number),
		"reviews":       map[string]any{"nodes": pending},
		"reviewThreads": map[string]any{"pageInfo": map[string]any{"hasNextPage": false, "endCursor": "Y3Vyc29yOjI="}, "nodes": threads},
	}
}

// mutate applies a mutation to the scenario; a non-empty string is a GraphQL error.
func (f *Fake) mutate(q string, v map[string]any) (map[string]any, string) {
	str := func(k string) string { s, _ := v[k].(string); return s }
	findThread := func(id string) (*pull, *thread) {
		for _, p := range f.pulls {
			for i := range p.Threads {
				if p.Threads[i].ID == id {
					return p, &p.Threads[i]
				}
			}
		}
		return nil, nil
	}
	pullByID := func(id string) *pull {
		for _, p := range f.pulls {
			if id == fmt.Sprintf("PR_kwDOAAABc85%07d", p.Number) {
				return p
			}
		}
		return nil
	}
	pullByReview := func(id string) *pull {
		for _, p := range f.pulls {
			if p.PendingReview != "" && p.PendingReview == id {
				return p
			}
		}
		return nil
	}
	f.nextID++
	switch {
	case strings.Contains(q, "addPullRequestReviewThreadReply"):
		_, t := findThread(str("thread"))
		if t == nil {
			return nil, "Could not resolve to a node with the global id of '" + str("thread") + "'"
		}
		f.mutations = append(f.mutations, "addPullRequestReviewThreadReply")
		t.Comments = append(t.Comments, comment{ID: fmt.Sprintf("PRRC_new%d", f.nextID), DatabaseID: int64(f.nextID), Author: Login, Body: str("body"), State: "SUBMITTED"})
		return map[string]any{"addPullRequestReviewThreadReply": map[string]any{"comment": map[string]any{"id": "PRRC_x"}}}, ""
	case strings.Contains(q, "unresolveReviewThread"), strings.Contains(q, "resolveReviewThread"):
		_, t := findThread(str("thread"))
		if t == nil {
			return nil, "Could not resolve to a node with the global id of '" + str("thread") + "'"
		}
		resolve := !strings.Contains(q, "unresolveReviewThread")
		name := "resolveReviewThread"
		if !resolve {
			name = "unresolveReviewThread"
		}
		f.mutations = append(f.mutations, name)
		t.Resolved = resolve
		if resolve {
			t.ResolvedBy = Login
		} else {
			t.ResolvedBy = ""
		}
		return map[string]any{name: map[string]any{"thread": map[string]any{"id": t.ID, "isResolved": resolve}}}, ""
	case strings.Contains(q, "addPullRequestReviewThread("):
		p := pullByReview(str("review"))
		if p == nil {
			return nil, "review not found"
		}
		f.mutations = append(f.mutations, "addPullRequestReviewThread")
		p.PendingComments++
		return map[string]any{"addPullRequestReviewThread": map[string]any{"thread": map[string]any{"id": "PRRT_new"}}}, ""
	case strings.Contains(q, "submitPullRequestReview"):
		p := pullByReview(str("review"))
		if p == nil {
			return nil, "review not found"
		}
		f.mutations = append(f.mutations, "submitPullRequestReview:"+str("event"))
		p.PendingReview, p.PendingComments = "", 0
		return map[string]any{"submitPullRequestReview": map[string]any{"pullRequestReview": map[string]any{"id": "PRR_done", "state": "COMMENTED"}}}, ""
	case strings.Contains(q, "deletePullRequestReview"):
		p := pullByReview(str("review"))
		if p == nil {
			return nil, "review not found"
		}
		f.mutations = append(f.mutations, "deletePullRequestReview")
		p.PendingReview, p.PendingComments = "", 0
		return map[string]any{"deletePullRequestReview": map[string]any{"pullRequestReview": map[string]any{"id": "PRR_gone"}}}, ""
	case strings.Contains(q, "addPullRequestReview("):
		p := pullByID(str("pr"))
		if p == nil {
			return nil, "pull request not found"
		}
		if ev := str("event"); ev != "" {
			f.mutations = append(f.mutations, "addPullRequestReview:"+ev)
			return map[string]any{"addPullRequestReview": map[string]any{"pullRequestReview": map[string]any{"id": "PRR_new", "state": ev}}}, ""
		}
		f.mutations = append(f.mutations, "addPullRequestReview")
		p.PendingReview = fmt.Sprintf("PRR_pending%d", f.nextID)
		return map[string]any{"addPullRequestReview": map[string]any{"pullRequestReview": map[string]any{"id": p.PendingReview}}}, ""
	}
	return nil, "fake GitHub: unknown mutation"
}
