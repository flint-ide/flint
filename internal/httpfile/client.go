package httpfile

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultTimeout applies when neither the request (@timeout) nor the caller sets one.
	DefaultTimeout = 30 * time.Second
	// readLimit stops reading a response body; more is reported as incomplete.
	readLimit = 100 << 20
	// maxJars bounds the per-file cookie jars kept for the session.
	maxJars   = 64
	userAgent = "flint-http-client"
)

// Options are the caller's settings for one send.
type Options struct {
	FollowRedirects bool
	// Timeout overrides DefaultTimeout (a request's @timeout overrides both).
	Timeout time.Duration
}

// Response is what the response tab shows. Bodies stay in memory only.
type Response struct {
	ID string `json:"id"`
	// File is the .http file the request came from; Key names the request in it.
	File   string `json:"file"`
	Key    string `json:"key"`
	Name   string `json:"name"`
	Method string `json:"method"`
	URL    string `json:"url"`
	// FinalURL differs from URL after redirects.
	FinalURL   string   `json:"finalUrl"`
	Status     int      `json:"status"`
	StatusText string   `json:"statusText"` // "404 Not Found"
	Proto      string   `json:"proto"`
	Headers    []Header `json:"headers"`
	// TimeMs is from sending the request until the body was read.
	TimeMs float64 `json:"timeMs"`
	// Size is the body's length in bytes (after decompression).
	Size        int64  `json:"size"`
	ContentType string `json:"contentType"`
	Kind        string `json:"kind"`
	// Body is the display text: pretty JSON, cut at DisplayLimit (Truncated).
	Body      string `json:"body"`
	Truncated bool   `json:"truncated"`
	// Incomplete: reading stopped at the read limit, so Size and Save are partial.
	Incomplete bool `json:"incomplete"`
	// At is when the request was sent (Unix milliseconds).
	At int64 `json:"at"`
}

// Client sends requests of .http files: one cookie jar per file, cancellable by id,
// with a bounded in-memory history of responses. Safe for concurrent use.
type Client struct {
	transport http.RoundTripper
	now       func() time.Time

	mu       sync.Mutex
	jars     map[string]*cookiejar.Jar
	jarOrder []string // oldest first, for eviction
	running  map[string]*call
	history  history
}

type call struct {
	cancel    context.CancelFunc
	cancelled bool
}

// NewClient creates a client with its own connection pool (proxy from the environment).
func NewClient() *Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	return newClient(t, time.Now)
}

func newClient(t http.RoundTripper, now func() time.Time) *Client {
	return &Client{transport: t, now: now, jars: map[string]*cookiejar.Jar{}, running: map[string]*call{}}
}

// Send performs p (a request of file) under id, which Cancel accepts while it runs.
// The response is kept in the history; a transport failure is returned as an error.
func (c *Client) Send(ctx context.Context, id, file string, p Prepared, opts Options) (*Response, error) {
	timeout := cmpDuration(p.Timeout, opts.Timeout, DefaultTimeout)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := c.start(id, cancel); err != nil {
		return nil, err
	}
	defer c.finish(id)

	req, err := http.NewRequestWithContext(ctx, p.Method, p.URL, bytes.NewReader(p.Body))
	if err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}
	for _, h := range p.Headers {
		if http.CanonicalHeaderKey(h.Name) == "Host" {
			req.Host = h.Value
			continue
		}
		req.Header.Add(h.Name, h.Value)
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", userAgent)
	}
	client := &http.Client{Transport: c.transport}
	if !opts.FollowRedirects || p.NoRedirect {
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}
	if !p.NoCookieJar {
		client.Jar = c.jar(file)
	}

	started := c.now()
	resp, err := client.Do(req)
	if err != nil {
		return nil, c.explain(ctx, id, err, timeout)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, readLimit+1))
	if err != nil {
		return nil, c.explain(ctx, id, err, timeout)
	}
	elapsed := c.now().Sub(started)
	incomplete := len(raw) > readLimit
	if incomplete {
		raw = raw[:readLimit]
	}
	if !resp.Uncompressed {
		raw = decode(raw, resp.Header.Get("Content-Encoding"), readLimit)
	}

	shown := formatBody(raw, resp.Header.Get("Content-Type"), DisplayLimit)
	out := &Response{
		ID: id, File: file, Key: p.Key, Name: p.Name, Method: p.Method, URL: p.URL,
		FinalURL: resp.Request.URL.String(), Status: resp.StatusCode, StatusText: resp.Status, Proto: resp.Proto,
		Headers: responseHeaders(resp), TimeMs: float64(elapsed.Microseconds()) / 1000, Size: int64(len(raw)),
		ContentType: shown.contentType, Kind: shown.kind, Body: shown.text, Truncated: shown.truncated,
		Incomplete: incomplete, At: started.UnixMilli(),
	}
	c.mu.Lock()
	c.history = c.history.add(entry{resp: out, raw: raw, curl: Curl(p, opts.FollowRedirects && !p.NoRedirect)})
	c.mu.Unlock()
	return out, nil
}

// cmpDuration returns the first positive duration.
func cmpDuration(ds ...time.Duration) time.Duration {
	for _, d := range ds {
		if d > 0 {
			return d
		}
	}
	return 0
}

func (c *Client) start(id string, cancel context.CancelFunc) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, busy := c.running[id]; busy {
		return fmt.Errorf("request %q is already running", id)
	}
	c.running[id] = &call{cancel: cancel}
	return nil
}

func (c *Client) finish(id string) {
	c.mu.Lock()
	delete(c.running, id)
	c.mu.Unlock()
}

// Cancel aborts a running request; unknown ids are ignored.
func (c *Client) Cancel(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if r, ok := c.running[id]; ok {
		r.cancelled = true
		r.cancel()
	}
}

// CancelAll aborts every running request (quitting).
func (c *Client) CancelAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, r := range c.running {
		r.cancelled = true
		r.cancel()
	}
}

// explain turns transport errors into messages a developer can act on. They never
// include header values; the URL's host may appear (it's the user's own request).
func (c *Client) explain(ctx context.Context, id string, err error, timeout time.Duration) error {
	c.mu.Lock()
	cancelled := c.running[id] != nil && c.running[id].cancelled
	c.mu.Unlock()
	switch {
	case cancelled:
		return errors.New("request cancelled")
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return fmt.Errorf("request timed out after %s (set # @timeout to wait longer)", timeout)
	}
	var unknown x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	var record tls.RecordHeaderError
	switch {
	case errors.As(err, &unknown):
		return errors.New("TLS: the server's certificate is signed by an unknown authority (self-signed or a private CA not trusted by macOS)")
	case errors.As(err, &hostname):
		return fmt.Errorf("TLS: the certificate is not valid for this host: %v", hostname.Error())
	case errors.As(err, &invalid):
		return fmt.Errorf("TLS: invalid certificate: %v", invalid.Error())
	case errors.As(err, &record), strings.Contains(err.Error(), "server gave HTTP response to HTTPS client"):
		return errors.New("TLS: the server did not answer with TLS (try http:// instead of https://)")
	}
	return err
}

// jar returns file's cookie jar, creating it on first use.
func (c *Client) jar(file string) *cookiejar.Jar {
	c.mu.Lock()
	defer c.mu.Unlock()
	if j, ok := c.jars[file]; ok {
		return j
	}
	j, _ := cookiejar.New(nil) // never fails without options
	if len(c.jarOrder) >= maxJars {
		delete(c.jars, c.jarOrder[0])
		c.jarOrder = c.jarOrder[1:]
	}
	c.jars[file] = j
	c.jarOrder = append(c.jarOrder, file)
	return j
}

// ClearCookies forgets the cookies collected for file.
func (c *Client) ClearCookies(file string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.jars, file)
	c.jarOrder = deleteString(c.jarOrder, file)
}

func deleteString(list []string, s string) []string {
	out := make([]string, 0, len(list))
	for _, x := range list {
		if x != s {
			out = append(out, x)
		}
	}
	return out
}

func responseHeaders(resp *http.Response) []Header {
	var out []Header
	for name, values := range resp.Header {
		for _, v := range values {
			out = append(out, Header{Name: name, Value: v})
		}
	}
	// By name, so responses read the same every time (values keep their order).
	slices.SortStableFunc(out, func(a, b Header) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })
	return append([]Header{}, out...)
}

// History lists file's kept responses, newest first, without bodies.
func (c *Client) History(file string) []Response {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := []Response{}
	for i := len(c.history.entries) - 1; i >= 0; i-- {
		if r := c.history.entries[i].resp; r.File == file {
			summary := *r
			summary.Body, summary.Headers = "", nil
			out = append(out, summary)
		}
	}
	return out
}

// Response returns a kept response by id.
func (c *Client) Response(id string) (*Response, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.history.get(id)
	return e.resp, ok
}

// Body returns a kept response's whole body (for "Save to file").
func (c *Client) Body(id string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.history.get(id)
	return e.raw, ok
}

// CurlOf returns the request of a kept response as a curl command.
func (c *Client) CurlOf(id string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.history.get(id)
	return e.curl, ok
}
