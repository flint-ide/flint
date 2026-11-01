package github

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Security limits of the API client (constants, not settings: they bound what
// a server can make Flint hold or do).
const (
	// maxResponseBytes caps one JSON answer.
	maxResponseBytes = 32 << 20
	// maxPages caps the pages one listing follows.
	maxPages = 30
	// maxRedirects within the API host (renamed repositories answer 301).
	maxRedirects = 5
	// etagEntries and maxETagBody bound the conditional-request cache.
	etagEntries = 128
	maxETagBody = 2 << 20
	// apiVersion is the REST API version Flint is written against.
	apiVersion = "2022-11-28"
)

// Auth is who Flint talks to GitHub as, and where.
type Auth struct {
	Endpoints
	// Token is sent only to Endpoints.API and Endpoints.GraphQL's origin.
	Token string
}

// RateLimitError: GitHub refused because a rate limit is used up.
type RateLimitError struct {
	// Secondary: GitHub asked to slow down (abuse protection), not the hourly quota.
	Secondary  bool
	RetryAfter time.Duration
	Reset      time.Time
}

func (e *RateLimitError) Error() string {
	if e.Secondary || e.Reset.IsZero() {
		wait := max(e.RetryAfter, time.Second).Round(time.Second)
		return fmt.Sprintf("GitHub asked Flint to slow down (secondary rate limit): try again in %s", wait)
	}
	return fmt.Sprintf("GitHub's API rate limit is used up until %s: try again then", e.Reset.Local().Format("15:04"))
}

// APIError is an error answer of the API, with GitHub's own message.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("GitHub: %s (HTTP %d)", e.Message, e.Status)
}

func isStatus(err error, status int) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.Status == status
}

// call is one request.
type call struct {
	method string
	url    string
	body   any
	accept string
	// conditional sends If-None-Match with the ETag of the last answer, so a
	// poll that finds nothing new costs no rate limit (GitHub answers 304).
	conditional bool
	// limit caps the body (0: maxResponseBytes).
	limit int64
	// truncate keeps the first limit bytes of a longer body instead of failing.
	truncate bool
}

// response is what a call returns.
type response struct {
	status int
	header http.Header
	body   []byte
}

// etagCache remembers the last answer per token and URL for conditional requests.
type etagCache struct {
	mu    sync.Mutex
	items map[string]etagEntry
	order []string
}

type etagEntry struct {
	etag string
	body []byte
}

func (c *etagCache) get(key string) (etagEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[key]
	return e, ok
}

func (c *etagCache) put(key string, e etagEntry) {
	if e.etag == "" || len(e.body) > maxETagBody {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.items == nil {
		c.items = map[string]etagEntry{}
	}
	if _, ok := c.items[key]; !ok {
		c.order = append(c.order, key)
		if len(c.order) > etagEntries {
			delete(c.items, c.order[0])
			c.order = c.order[1:]
		}
	}
	c.items[key] = e
}

func (c *etagCache) clear() {
	c.mu.Lock()
	c.items, c.order = nil, nil
	c.mu.Unlock()
}

// sameOrigin reports whether raw is on the scheme and host of base.
func sameOrigin(raw, base string) bool {
	u, err1 := url.Parse(raw)
	b, err2 := url.Parse(base)
	return err1 == nil && err2 == nil && u.Scheme == b.Scheme && strings.EqualFold(u.Host, b.Host)
}

// newAPIHTTP is the client for API calls: redirects are followed only within
// the same origin (a renamed repository), never to another host, so the
// token can't follow a redirect elsewhere.
func newAPIHTTP() *http.Client {
	return &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return errors.New("too many redirects")
			}
			if !sameOrigin(req.URL.String(), via[0].URL.String()) {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
}

// do sends one call with the token (to the API's origin only) and maps error
// answers. A read refused with "retry after" is retried once when the wait is
// within maxRetryWait.
func (c *Client) do(ctx context.Context, a Auth, k call) (response, error) {
	resp, err := c.send(ctx, a, k)
	var rl *RateLimitError
	if errors.As(err, &rl) && rl.RetryAfter > 0 && rl.RetryAfter <= c.maxRetryWait() && idempotent(k) {
		t := time.NewTimer(rl.RetryAfter)
		select {
		case <-ctx.Done():
			t.Stop()
			return response{}, err
		case <-t.C:
		}
		return c.send(ctx, a, k)
	}
	return resp, err
}

// idempotent: GETs, and GraphQL queries (POSTs that only read).
func idempotent(k call) bool {
	if k.method == http.MethodGet {
		return true
	}
	if q, ok := k.body.(graphqlRequest); ok {
		return !strings.HasPrefix(strings.TrimSpace(q.Query), "mutation")
	}
	return false
}

func (c *Client) send(ctx context.Context, a Auth, k call) (response, error) {
	if !sameOrigin(k.url, a.API) && !sameOrigin(k.url, a.GraphQL) {
		return response{}, fmt.Errorf("refusing to call %s: not the configured GitHub API", hostOf(k.url))
	}
	var body io.Reader
	if k.body != nil {
		data, err := json.Marshal(k.body)
		if err != nil {
			return response{}, err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, k.method, k.url, body)
	if err != nil {
		return response{}, err
	}
	accept := k.accept
	if accept == "" {
		accept = "application/vnd.github+json"
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	req.Header.Set("User-Agent", c.userAgent)
	if k.body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if a.Token != "" {
		req.Header.Set("Authorization", "Bearer "+a.Token)
	}
	key := ""
	if k.conditional {
		key = tokenID(a.Token) + " " + k.url
		if e, ok := c.etags.get(key); ok {
			req.Header.Set("If-None-Match", e.etag)
		}
	}
	res, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return response{}, fmt.Errorf("GitHub did not answer in time (%s)", hostOf(k.url))
		}
		return response{}, fmt.Errorf("could not reach GitHub at %s: %w", hostOf(k.url), stripURL(err))
	}
	defer res.Body.Close()
	limit := k.limit
	if limit <= 0 {
		limit = maxResponseBytes
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return response{}, fmt.Errorf("reading GitHub's answer: %w", stripURL(err))
	}
	if int64(len(data)) > limit {
		if !k.truncate || res.StatusCode >= 300 {
			return response{}, fmt.Errorf("GitHub's answer is larger than Flint accepts (%d MB)", limit>>20)
		}
		data = data[:limit]
	}
	if res.StatusCode == http.StatusNotModified && key != "" {
		if e, ok := c.etags.get(key); ok {
			return response{status: http.StatusOK, header: res.Header, body: e.body}, nil
		}
	}
	if err := statusError(res, data); err != nil {
		return response{}, err
	}
	if key != "" {
		c.etags.put(key, etagEntry{etag: res.Header.Get("ETag"), body: data})
	}
	return response{status: res.StatusCode, header: res.Header, body: data}, nil
}

// statusError maps an error answer: 401 means sign in again, rate limits get
// their own error, the rest carries GitHub's message.
func statusError(res *http.Response, body []byte) error {
	if res.StatusCode < 300 {
		return nil
	}
	var e struct {
		Message string `json:"message"`
		Errors  []struct {
			Message string `json:"message"`
			Code    string `json:"code"`
			Field   string `json:"field"`
		} `json:"errors"`
	}
	_ = json.Unmarshal(body, &e)
	msg := oneLine(e.Message)
	for _, d := range e.Errors {
		switch {
		case d.Message != "":
			msg += ": " + oneLine(d.Message)
		case d.Code != "":
			msg += ": " + oneLine(d.Field+" "+d.Code)
		}
	}
	if msg == "" {
		msg = http.StatusText(res.StatusCode)
	}
	h := res.Header
	switch {
	case res.StatusCode == http.StatusUnauthorized:
		return ErrNotAuthenticated
	case res.StatusCode == http.StatusForbidden || res.StatusCode == http.StatusTooManyRequests:
		if ra := h.Get("Retry-After"); ra != "" {
			secs, _ := strconv.Atoi(ra)
			return &RateLimitError{Secondary: true, RetryAfter: time.Duration(max(secs, 1)) * time.Second}
		}
		if h.Get("X-RateLimit-Remaining") == "0" {
			reset, _ := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64)
			return &RateLimitError{Reset: time.Unix(reset, 0), RetryAfter: time.Until(time.Unix(reset, 0))}
		}
		if strings.Contains(strings.ToLower(msg), "secondary rate limit") {
			return &RateLimitError{Secondary: true, RetryAfter: time.Minute}
		}
		if sso := h.Get("X-GitHub-SSO"); sso != "" {
			return &APIError{Status: res.StatusCode, Message: "the organization requires single sign-on: authorize the token for it on GitHub, then retry"}
		}
	case res.StatusCode >= 300 && res.StatusCode < 400:
		return &APIError{Status: res.StatusCode, Message: "GitHub redirected to another host"}
	}
	return &APIError{Status: res.StatusCode, Message: msg}
}

// getJSON reads one answer into out.
func (c *Client) getJSON(ctx context.Context, a Auth, k call, out any) error {
	if k.method == "" {
		k.method = http.MethodGet
	}
	resp, err := c.do(ctx, a, k)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(resp.body, out); err != nil {
		return fmt.Errorf("unexpected answer from GitHub (%s): %w", pathOf(k.url), err)
	}
	return nil
}

// pages follows a listing's Link: rel="next" (only within the API's origin),
// handing each page's body to each, up to limit pages.
func (c *Client) pages(ctx context.Context, a Auth, first string, limit int, each func([]byte) error) error {
	next := first
	for range min(limit, maxPages) {
		resp, err := c.do(ctx, a, call{method: http.MethodGet, url: next})
		if err != nil {
			return err
		}
		if err := each(resp.body); err != nil {
			return err
		}
		next = nextLink(resp.header.Get("Link"))
		if next == "" {
			return nil
		}
		if !sameOrigin(next, a.API) {
			return fmt.Errorf("GitHub's next page is on another host (%s)", hostOf(next))
		}
	}
	return nil
}

// nextLink reads rel="next" from a Link header.
func nextLink(h string) string {
	for part := range strings.SplitSeq(h, ",") {
		segs := strings.Split(part, ";")
		if len(segs) < 2 {
			continue
		}
		target := strings.TrimSpace(segs[0])
		for _, p := range segs[1:] {
			if strings.ReplaceAll(strings.TrimSpace(p), " ", "") == `rel="next"` && strings.HasPrefix(target, "<") && strings.HasSuffix(target, ">") {
				return target[1 : len(target)-1]
			}
		}
	}
	return ""
}

// rest builds a REST URL from path segments, escaping each one.
func rest(a Auth, format string, args ...any) string {
	esc := make([]any, len(args))
	for i, v := range args {
		if s, ok := v.(string); ok {
			esc[i] = url.PathEscape(s)
		} else {
			esc[i] = v
		}
	}
	return strings.TrimRight(a.API, "/") + fmt.Sprintf(format, esc...)
}

type graphqlRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

// graphql posts a query; GraphQL errors (also sent with 200) become errors.
func (c *Client) graphql(ctx context.Context, a Auth, query string, vars map[string]any, out any) error {
	resp, err := c.do(ctx, a, call{method: http.MethodPost, url: a.GraphQL, body: graphqlRequest{Query: query, Variables: vars}})
	if err != nil {
		return err
	}
	var r struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(resp.body, &r); err != nil {
		return fmt.Errorf("unexpected answer from GitHub's GraphQL API: %w", err)
	}
	if len(r.Errors) > 0 {
		msgs := make([]string, 0, len(r.Errors))
		for _, e := range r.Errors {
			if e.Type == "RATE_LIMITED" {
				return &RateLimitError{Secondary: true, RetryAfter: time.Minute}
			}
			msgs = append(msgs, oneLine(e.Message))
		}
		return &APIError{Status: http.StatusOK, Message: strings.Join(msgs, "; ")}
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(r.Data, out)
}

// tokenID keys caches by token without keeping the token itself in a key.
func tokenID(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:8])
}

func hostOf(raw string) string {
	if u, err := url.Parse(raw); err == nil {
		return u.Host
	}
	return "?"
}

func pathOf(raw string) string {
	if u, err := url.Parse(raw); err == nil {
		return u.Path
	}
	return "?"
}

// stripURL drops the URL from a transport error (it repeats the request).
func stripURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

// oneLine makes a server message safe to show: one line, bounded.
func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 300 {
		return string(r[:300]) + "…"
	}
	return s
}
