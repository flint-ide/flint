// Package clickhouse is a minimal ClickHouse client over the HTTP interface, built on the
// standard library: queries are POSTed, rows stream back as JSONCompactEachRowWithNamesAndTypes
// (one JSON array per row) and are decoded one at a time, so a row limit stops the transfer.
package clickhouse

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Config describes one server. The password travels in a header, never in the URL.
type Config struct {
	Host     string
	Port     int
	TLS      bool
	Insecure bool // skip certificate verification (self-signed or private CAs)
	User     string
	Password string
	// Database is sent with every request; "" leaves the user's default_database.
	Database string
	// ReadOnly asks the server to refuse statements that change data or settings.
	ReadOnly bool
}

// Request is one statement to run.
type Request struct {
	SQL string
	// QueryID names the run on the server so Kill can target it; generated when empty
	// or not a plain identifier.
	QueryID string
	// SessionID keeps SET and temporary tables across requests of one console. Only one
	// request may use a session at a time; concurrent ones wait briefly, then fail.
	SessionID string
}

// sessionTimeout is how long the server keeps an idle session (its maximum by default).
const sessionTimeout = 3600

// sessionRetries bounds the wait for a session still locked by the previous request
// (typically one that was just cancelled and not yet killed).
const (
	sessionRetries = 25
	sessionBackoff = 120 * time.Millisecond
)

// Client talks to one server; safe for concurrent use.
type Client struct {
	cfg      Config
	base     string
	http     *http.Client
	compress bool
}

// New validates cfg and prepares a client; nothing is sent until the first request.
func New(cfg Config) (*Client, error) {
	if cfg.Host == "" {
		return nil, errors.New("clickhouse: host is required")
	}
	if cfg.Port <= 0 || cfg.Port > 65535 {
		return nil, fmt.Errorf("clickhouse: invalid port %d", cfg.Port)
	}
	scheme := "http"
	tr := &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       8 * time.Second, // below the server's 10 s keep-alive
		ExpectContinueTimeout: time.Second,
	}
	if cfg.TLS {
		scheme = "https"
		tr.TLSClientConfig = &tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12, InsecureSkipVerify: cfg.Insecure} //nolint:gosec // the user's explicit choice
		// A proxy only tunnels TLS; a plain-HTTP proxy would see the credentials in the headers.
		tr.Proxy = http.ProxyFromEnvironment
	}
	return &Client{
		cfg:  cfg,
		base: scheme + "://" + net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)) + "/",
		http: &http.Client{Transport: tr},
		// Compression pays on a network, not on loopback.
		compress: !isLoopback(cfg.Host),
	}, nil
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Close drops idle connections.
func (c *Client) Close() { c.http.CloseIdleConnections() }

var plainID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)

// NewQueryID returns a fresh query id, for callers that must know it before the request
// is sent (to kill a statement that hasn't produced a response yet).
func NewQueryID() string { return newQueryID() }

func newQueryID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// params builds the URL query: settings that make the output exact, plus the ids.
// Settings go in the URL, the statement in the body, the credentials in headers.
func (c *Client) params(req Request, queryID string) url.Values {
	q := url.Values{}
	q.Set("default_format", "JSONCompactEachRowWithNamesAndTypes")
	q.Set("output_format_json_quote_64bit_integers", "1")
	q.Set("output_format_json_quote_decimals", "1")
	q.Set("output_format_json_quote_denormals", "1") // NaN and Inf as strings, not null
	// Over HTTP, closing the connection only cancels read-only queries with this on.
	q.Set("cancel_http_readonly_queries_on_client_close", "1")
	q.Set("query_id", queryID)
	if c.compress {
		q.Set("enable_http_compression", "1")
	}
	if c.cfg.ReadOnly {
		q.Set("readonly", "1")
	}
	if plainID.MatchString(req.SessionID) {
		q.Set("session_id", req.SessionID)
		q.Set("session_timeout", strconv.Itoa(sessionTimeout))
	}
	return q
}

func (c *Client) newRequest(ctx context.Context, q url.Values, body string) (*http.Request, error) {
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"?"+q.Encode(), strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	r.Header.Set("Content-Type", "text/plain; charset=utf-8")
	r.Header.Set("User-Agent", "flint")
	r.Header.Set("X-ClickHouse-User", c.cfg.User)
	if c.cfg.Password != "" {
		r.Header.Set("X-ClickHouse-Key", c.cfg.Password)
	}
	if c.cfg.Database != "" {
		r.Header.Set("X-ClickHouse-Database", c.cfg.Database)
	}
	return r, nil
}

// Query sends the statement and returns once the column header arrived; rows are then
// read with Next. The caller must Close the result. Cancelling ctx closes the request;
// Kill stops the statement on the server.
func (c *Client) Query(ctx context.Context, req Request) (*Rows, error) {
	if strings.TrimSpace(req.SQL) == "" {
		return nil, errors.New("clickhouse: empty statement")
	}
	if !plainID.MatchString(req.QueryID) {
		req.QueryID = newQueryID()
	}
	for attempt := 0; ; attempt++ {
		rows, err := c.send(ctx, req)
		var ce *Error
		if err == nil || req.SessionID == "" || !errors.As(err, &ce) || ce.Code != codeSessionLocked || attempt >= sessionRetries {
			return rows, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(sessionBackoff):
		}
	}
}

func (c *Client) send(ctx context.Context, req Request) (*Rows, error) {
	hr, err := c.newRequest(ctx, c.params(req, req.QueryID), req.SQL)
	if err != nil {
		return nil, fmt.Errorf("clickhouse request: %w", err)
	}
	resp, err := c.http.Do(hr)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: %w", cleanTransportError(err))
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		if ce := parseError(string(body), resp.Header.Get("X-ClickHouse-Exception-Code")); ce != nil {
			return nil, ce
		}
		return nil, fmt.Errorf("clickhouse: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(clip(string(body), 300)))
	}
	rows, err := newRows(resp, req.QueryID)
	if err != nil {
		_ = resp.Body.Close()
		return nil, err
	}
	return rows, nil
}

// cleanTransportError drops the request URL (which carries query ids and settings) from
// net/http's error text and keeps the cause.
func cleanTransportError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

// Kill asks the server to stop the statement with the given query id. A finished or
// unknown id is not an error. It runs on its own context so it works after the query's
// context was cancelled.
func (c *Client) Kill(queryID string) error {
	if !plainID.MatchString(queryID) {
		return fmt.Errorf("clickhouse: invalid query id")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	q := url.Values{}
	q.Set("default_format", "TabSeparated")
	q.Set("param_qid", queryID)
	hr, err := c.newRequest(ctx, q, "KILL QUERY WHERE query_id = {qid:String}")
	if err != nil {
		return err
	}
	resp, err := c.http.Do(hr)
	if err != nil {
		return fmt.Errorf("clickhouse kill: %w", cleanTransportError(err))
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != http.StatusOK {
		if ce := parseError(string(body), resp.Header.Get("X-ClickHouse-Exception-Code")); ce != nil {
			return ce
		}
		return fmt.Errorf("clickhouse kill: HTTP %d", resp.StatusCode)
	}
	return nil
}

// Ping runs SELECT 1 with the credentials; the server's /ping endpoint doesn't check them.
func (c *Client) Ping(ctx context.Context) error {
	rows, err := c.Query(ctx, Request{SQL: "SELECT 1"})
	if err != nil {
		return err
	}
	for rows.Next() {
	}
	return errors.Join(rows.Err(), rows.Close())
}
