// Package githubtest is a fake GitHub (REST, GraphQL, OAuth device flow and a
// separate host for redirected Actions logs) for Flint's tests and for trying
// Flint in server mode without GitHub (cmd/fakegithub). It serves one
// scenario (see data.go), laid out like GitHub Enterprise Server: the API at
// /api/v3, GraphQL at /api/graphql, OAuth at /login. It never talks to GitHub.
package githubtest

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// DefaultToken is the token the fake accepts and issues.
const DefaultToken = "gho_fakeFlintTestToken0123456789abcdef"

// Request is one request the fake saw.
type Request struct {
	Method, Path, Query string
	// Auth is the Authorization header ("" when none came).
	Auth string
	// Conditional: the request carried If-None-Match.
	Conditional bool
	Body        string
}

// Fake is the fake GitHub's state.
type Fake struct {
	// Token is accepted on the API and issued by the device flow.
	Token string
	// Web is this fake's base URL (http://127.0.0.1:port); LogBase the log host's.
	Web, LogBase string
	// DevicePolls answer "authorization_pending" until the device is approved.
	approved bool

	mu          sync.Mutex
	pulls       []*pull
	requests    []Request
	logRequests []Request
	mutations   []string
	created     []map[string]any
	reruns      int
	notModified int
	failNext    map[string]failure
	nextID      int
}

type failure struct {
	status int
	header map[string]string
	body   string
}

// New returns a fake with the scenario loaded; Web and LogBase must be set
// before it serves (Start does it).
func New() *Fake {
	return &Fake{Token: DefaultToken, pulls: scenario(), failNext: map[string]failure{}, nextID: 1000}
}

// Start serves the fake and its log host on two httptest servers.
func Start(t testing.TB) *Fake {
	f := New()
	api := httptest.NewServer(f.Handler())
	logs := httptest.NewServer(f.LogHandler())
	t.Cleanup(api.Close)
	t.Cleanup(logs.Close)
	f.Web, f.LogBase = api.URL, logs.URL
	return f
}

// ApproveDevice is the user entering the code in the browser.
func (f *Fake) ApproveDevice() {
	f.mu.Lock()
	f.approved = true
	f.mu.Unlock()
}

// FailNext makes the next request whose path starts with prefix fail.
func (f *Fake) FailNext(prefix string, status int, header map[string]string, body string) {
	f.mu.Lock()
	f.failNext[prefix] = failure{status, header, body}
	f.mu.Unlock()
}

// Requests returns the API requests seen so far.
func (f *Fake) Requests() []Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Request{}, f.requests...)
}

// LogRequests returns the requests the log host saw.
func (f *Fake) LogRequests() []Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Request{}, f.logRequests...)
}

// Mutations lists the GraphQL mutations run, by name, in order.
func (f *Fake) Mutations() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.mutations...)
}

// Created lists the bodies of pull requests created through REST.
func (f *Fake) Created() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]any{}, f.created...)
}

// Reruns counts rerun-failed-jobs requests; NotModified counts 304 answers.
func (f *Fake) Reruns() int      { f.mu.Lock(); defer f.mu.Unlock(); return f.reruns }
func (f *Fake) NotModified() int { f.mu.Lock(); defer f.mu.Unlock(); return f.notModified }

// RepoURL is the scenario repository's web URL.
func (f *Fake) RepoURL() string { return f.Web + "/" + Owner + "/" + Repo }

// LogHandler serves redirected job logs; it records whether a token came.
func (f *Fake) LogHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.logRequests = append(f.logRequests, Request{Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, Auth: r.Header.Get("Authorization")})
		f.mu.Unlock()
		if r.URL.Path != "/logs/"+strconv.Itoa(FailedJob) || r.URL.Query().Get("sig") == "" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, JobLog)
	})
}

// Handler serves the web, OAuth, REST and GraphQL routes.
func (f *Fake) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		r.Body = io.NopCloser(bytes.NewReader(body))
		f.mu.Lock()
		f.requests = append(f.requests, Request{
			Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, Auth: r.Header.Get("Authorization"),
			Conditional: r.Header.Get("If-None-Match") != "", Body: string(body),
		})
		for prefix, fail := range f.failNext {
			if strings.HasPrefix(r.URL.Path, prefix) {
				delete(f.failNext, prefix)
				f.mu.Unlock()
				for k, v := range fail.header {
					w.Header().Set(k, v)
				}
				w.WriteHeader(fail.status)
				_, _ = io.WriteString(w, fail.body)
				return
			}
		}
		f.mu.Unlock()
		switch {
		case r.URL.Path == "/login/device/code" && r.Method == http.MethodPost:
			f.deviceCode(w, r)
		case r.URL.Path == "/login/oauth/access_token" && r.Method == http.MethodPost:
			f.accessToken(w, r)
		case r.URL.Path == "/login/device":
			f.ApproveDevice()
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, "<!doctype html><title>Device activated</title><h1>Fake GitHub</h1><p>Code "+UserCode+" accepted. Return to Flint.</p>")
		case strings.HasPrefix(r.URL.Path, "/api/"):
			if !f.authorized(w, r) {
				return
			}
			if r.URL.Path == "/api/graphql" {
				f.graphql(w, body)
				return
			}
			f.rest(w, r, body)
		default:
			http.NotFound(w, r)
		}
	})
}

func (f *Fake) authorized(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Authorization") != "Bearer "+f.Token {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"message":"Bad credentials","documentation_url":"https://docs.github.com/rest","status":"401"}`)
		return false
	}
	if r.Header.Get("X-GitHub-Api-Version") == "" || r.Header.Get("User-Agent") == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"message":"missing API version or User-Agent"}`)
		return false
	}
	return true
}

func (f *Fake) deviceCode(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	if r.Form.Get("client_id") == "" {
		writeJSON(w, http.StatusOK, map[string]any{"error": "unauthorized_client", "error_description": "client_id missing"})
		return
	}
	f.mu.Lock()
	f.approved = false
	f.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"device_code": deviceCode, "user_code": UserCode, "verification_uri": f.Web + "/login/device",
		"expires_in": 900, "interval": 1,
	})
}

func (f *Fake) accessToken(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	if r.Form.Get("device_code") != deviceCode || r.Form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:device_code" {
		writeJSON(w, http.StatusOK, map[string]any{"error": "incorrect_device_code"})
		return
	}
	f.mu.Lock()
	ok := f.approved
	f.mu.Unlock()
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"error": "authorization_pending", "error_description": "The authorization request is still pending."})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"access_token": f.Token, "token_type": "bearer", "scope": "repo,read:org,workflow"})
}

var (
	repoPath  = regexp.MustCompile(`^/api/v3/repos/([^/]+)/([^/]+)(/.*)?$`)
	numberSeg = regexp.MustCompile(`^\d+$`)
)

func (f *Fake) rest(w http.ResponseWriter, r *http.Request, body []byte) {
	if r.URL.Path == "/api/v3/user" {
		w.Header().Set("X-OAuth-Scopes", "repo, read:org, workflow")
		f.answer(w, r, map[string]any{
			"login": Login, "id": 583231, "name": "The Octocat", "type": "User",
			"avatar_url": "https://avatars.githubusercontent.com/u/583231?v=4", "html_url": f.Web + "/" + Login,
		})
		return
	}
	m := repoPath.FindStringSubmatch(r.URL.Path)
	if m == nil || m[1] != Owner || m[2] != Repo {
		notFound(w)
		return
	}
	parts := strings.Split(strings.Trim(m[3], "/"), "/")
	get, post := r.Method == http.MethodGet, r.Method == http.MethodPost
	switch {
	case m[3] == "" && get:
		f.answer(w, r, f.repoJSON())
	case match(parts, "pulls") && get:
		f.listPulls(w, r)
	case match(parts, "pulls") && post:
		f.createPull(w, body)
	case match(parts, "pulls", "#", "files") && get:
		f.pullFiles(w, r, parts[1])
	case match(parts, "pulls", "#", "comments") && post:
		f.mu.Lock()
		f.mutations = append(f.mutations, "rest:pulls.comments")
		f.mu.Unlock()
		writeJSON(w, http.StatusCreated, map[string]any{"id": 2001, "body": "ok"})
	case match(parts, "git", "blobs", "*") && get:
		content, ok := blobs()[parts[2]]
		if !ok {
			notFound(w)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.github.raw+json")
		_, _ = io.WriteString(w, content)
	case len(parts) >= 2 && parts[0] == "contents" && get:
		if strings.Join(parts[1:], "/") == "main.go" && r.URL.Query().Get("ref") == MergeBase {
			_, _ = io.WriteString(w, MainBase)
			return
		}
		notFound(w)
	case match(parts, "compare", "*") && get:
		f.answer(w, r, map[string]any{"status": "ahead", "merge_base_commit": map[string]any{"sha": MergeBase}})
	case match(parts, "commits", "*", "check-runs") && get:
		f.answer(w, r, f.checkRuns(parts[1]))
	case match(parts, "commits", "*", "status") && get:
		f.answer(w, r, f.statuses(parts[1]))
	case match(parts, "actions", "runs") && get:
		f.answer(w, r, f.workflowRuns(r.URL.Query().Get("head_sha")))
	case match(parts, "actions", "runs", "#") && get && parts[2] == strconv.Itoa(RunID):
		f.answer(w, r, f.runJSON())
	case match(parts, "actions", "runs", "#", "jobs") && get && parts[2] == strconv.Itoa(RunID):
		f.answer(w, r, map[string]any{"total_count": 2, "jobs": []any{f.jobJSON(FailedJob), f.jobJSON(PassedJob)}})
	case match(parts, "actions", "runs", "#", "rerun-failed-jobs") && post && parts[2] == strconv.Itoa(RunID):
		f.mu.Lock()
		f.reruns++
		f.mu.Unlock()
		writeJSON(w, http.StatusCreated, map[string]any{})
	case match(parts, "actions", "jobs", "#") && get:
		if job := f.jobJSON(atoi(parts[2])); job != nil {
			f.answer(w, r, job)
			return
		}
		notFound(w)
	case match(parts, "actions", "jobs", "#", "logs") && get:
		if atoi(parts[2]) != FailedJob {
			writeJSON(w, http.StatusGone, map[string]any{"message": "Gone"})
			return
		}
		http.Redirect(w, r, fmt.Sprintf("%s/logs/%d?sig=fake-signature&se=%d", f.LogBase, FailedJob, time.Now().Add(time.Minute).Unix()), http.StatusFound)
	default:
		notFound(w)
	}
}

// match compares path segments; "#" is a number, "*" anything.
func match(parts []string, pattern ...string) bool {
	if len(parts) != len(pattern) {
		return false
	}
	for i, p := range pattern {
		switch p {
		case "#":
			if !numberSeg.MatchString(parts[i]) {
				return false
			}
		case "*":
		default:
			if parts[i] != p {
				return false
			}
		}
	}
	return true
}

// answer writes JSON with an ETag, and 304 when the client already has it.
func (f *Fake) answer(w http.ResponseWriter, r *http.Request, v any) {
	data, _ := json.Marshal(v)
	sum := sha256.Sum256(data)
	etag := `W/"` + hex.EncodeToString(sum[:8]) + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("X-RateLimit-Limit", "5000")
	w.Header().Set("X-RateLimit-Remaining", "4999")
	if r.Header.Get("If-None-Match") == etag {
		f.mu.Lock()
		f.notModified++
		f.mu.Unlock()
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(data)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func notFound(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotFound, map[string]any{"message": "Not Found", "documentation_url": "https://docs.github.com/rest"})
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func (f *Fake) repoJSON() map[string]any {
	return map[string]any{
		"id": 1296269, "node_id": "MDEwOlJlcG9zaXRvcnkxMjk2MjY5", "name": Repo, "full_name": Owner + "/" + Repo,
		"owner": map[string]any{"login": Owner, "type": "Organization"}, "private": false,
		"html_url": f.RepoURL(), "default_branch": "main",
	}
}

func (f *Fake) pull(n int) *pull {
	for _, p := range f.pulls {
		if p.Number == n {
			return p
		}
	}
	return nil
}

func (f *Fake) pullREST(p *pull) map[string]any {
	headRepo := map[string]any{"full_name": p.HeadOwner + "/" + Repo}
	return map[string]any{
		"number": p.Number, "title": p.Title, "state": lower(p.State), "html_url": fmt.Sprintf("%s/pull/%d", f.RepoURL(), p.Number),
		"merged_at": nil, "draft": p.Draft, "updated_at": "2026-10-30T10:00:00Z", "user": map[string]any{"login": p.Author},
		"head": map[string]any{"ref": p.Head, "sha": p.HeadSHA, "label": p.HeadOwner + ":" + p.Head, "repo": headRepo},
		"base": map[string]any{"ref": "main", "sha": BaseSHA, "repo": map[string]any{"full_name": Owner + "/" + Repo}},
	}
}

func (f *Fake) listPulls(w http.ResponseWriter, r *http.Request) {
	head := r.URL.Query().Get("head")
	f.mu.Lock()
	out := []any{}
	for _, p := range f.pulls {
		if head == "" || head == p.HeadOwner+":"+p.Head {
			out = append(out, f.pullREST(p))
		}
	}
	f.mu.Unlock()
	f.answer(w, r, out)
}

func (f *Fake) createPull(w http.ResponseWriter, body []byte) {
	var in map[string]any
	if err := json.Unmarshal(body, &in); err != nil || in["title"] == nil || in["head"] == nil || in["base"] == nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"message": "Validation Failed"})
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, in)
	for _, p := range f.pulls {
		if in["head"] == p.HeadOwner+":"+p.Head || in["head"] == p.Head && p.HeadOwner == Owner {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"message": "Validation Failed",
				"errors":  []any{map[string]any{"resource": "PullRequest", "code": "custom", "message": "A pull request already exists for " + p.HeadOwner + ":" + p.Head + "."}},
			})
			return
		}
	}
	n := 10 + len(f.created)
	writeJSON(w, http.StatusCreated, map[string]any{"number": n, "html_url": fmt.Sprintf("%s/pull/%d", f.RepoURL(), n), "state": "open"})
}

func (f *Fake) pullFiles(w http.ResponseWriter, r *http.Request, num string) {
	f.mu.Lock()
	p := f.pull(atoi(num))
	f.mu.Unlock()
	if p == nil {
		notFound(w)
		return
	}
	per := atoi(r.URL.Query().Get("per_page"))
	if per <= 0 {
		per = 30
	}
	page := max(1, atoi(r.URL.Query().Get("page")))
	from := min(len(p.Files), (page-1)*per)
	to := min(len(p.Files), from+per)
	if to < len(p.Files) {
		q := r.URL.Query()
		q.Set("page", strconv.Itoa(page+1))
		w.Header().Set("Link", fmt.Sprintf(`<%s%s?%s>; rel="next", <%s%s?page=99>; rel="last"`, f.Web, r.URL.Path, q.Encode(), f.Web, r.URL.Path))
	}
	f.answer(w, r, p.Files[from:to])
}

func (f *Fake) checkRuns(sha string) map[string]any {
	if sha != HeadSHA {
		return map[string]any{"total_count": 0, "check_runs": []any{}}
	}
	run := func(id int, name, status string, conclusion any, details string) map[string]any {
		return map[string]any{"id": id, "name": name, "status": status, "conclusion": conclusion, "head_sha": HeadSHA,
			"details_url": details, "html_url": details, "check_suite": map[string]any{"id": SuiteID},
			"app": map[string]any{"slug": "github-actions"}}
	}
	base := fmt.Sprintf("%s/actions/runs/%d/job/", f.RepoURL(), RunID)
	return map[string]any{"total_count": 2, "check_runs": []any{
		run(PassedJob, "Docs", "completed", "success", base+strconv.Itoa(PassedJob)),
		run(FailedJob, "Build and test (macOS)", "completed", "failure", base+strconv.Itoa(FailedJob)),
	}}
}

func (f *Fake) statuses(sha string) map[string]any {
	out := []any{}
	if sha == HeadSHA {
		out = append(out, map[string]any{"context": "deploy/preview", "state": "pending", "target_url": "https://deploy.example/7"})
	}
	return map[string]any{"state": "pending", "sha": sha, "total_count": len(out), "statuses": out}
}

func (f *Fake) workflowRuns(sha string) map[string]any {
	runs := []any{}
	if sha == HeadSHA {
		runs = append(runs, map[string]any{"id": RunID, "name": "CI", "check_suite_id": SuiteID, "head_sha": HeadSHA})
	}
	return map[string]any{"total_count": len(runs), "workflow_runs": runs}
}

func (f *Fake) runJSON() map[string]any {
	return map[string]any{
		"id": RunID, "name": "CI", "display_title": "Add rate limiting", "head_branch": "feat/rate-limit", "head_sha": HeadSHA,
		"event": "pull_request", "status": "completed", "conclusion": "failure", "run_attempt": 1,
		"html_url": fmt.Sprintf("%s/actions/runs/%d", f.RepoURL(), RunID), "check_suite_id": SuiteID,
	}
}

func (f *Fake) jobJSON(id int) map[string]any {
	url := fmt.Sprintf("%s/actions/runs/%d/job/%d", f.RepoURL(), RunID, id)
	switch id {
	case FailedJob:
		return map[string]any{"id": id, "run_id": RunID, "name": "Build and test (macOS)", "status": "completed", "conclusion": "failure",
			"started_at": "2026-11-01T12:41:35Z", "completed_at": "2026-11-01T12:42:51Z", "html_url": url, "steps": failedJobSteps()}
	case PassedJob:
		return map[string]any{"id": id, "run_id": RunID, "name": "Docs", "status": "completed", "conclusion": "success", "html_url": url,
			"steps": []any{map[string]any{"name": "build", "status": "completed", "conclusion": "success", "number": 1,
				"started_at": "2026-11-01T12:41:35Z", "completed_at": "2026-11-01T12:42:01Z"}}}
	}
	return nil
}
