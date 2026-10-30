package profiling

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/google/pprof/profile"
)

// Fetching a profile from a program's net/http/pprof endpoint.
const (
	// DefaultSeconds is how long a CPU profile records when profileURL names
	// no ?seconds= (net/http/pprof itself defaults to 30).
	DefaultSeconds = 10
	// startupWait bounds how long Fetch waits for the program to listen
	// (a run configuration builds first).
	startupWait = 2 * time.Minute
	retryEvery  = 300 * time.Millisecond
	// responseSlack is added to the recording time for the request timeout.
	responseSlack = 30 * time.Second
)

// Endpoint completes a profileURL: a CPU profile endpoint (…/profile) without
// ?seconds= records DefaultSeconds. It returns the URL to request and how long
// the endpoint will record (0 for snapshots such as /heap).
func Endpoint(raw string) (string, time.Duration, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", 0, err
	}
	q := u.Query()
	seconds := 0
	if s := q.Get("seconds"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n <= 0 {
			return "", 0, fmt.Errorf("invalid seconds=%q in %s", s, raw)
		}
		seconds = n
	} else if strings.HasSuffix(strings.TrimSuffix(u.Path, "/"), "/profile") {
		seconds = DefaultSeconds
		q.Set("seconds", strconv.Itoa(seconds))
		u.RawQuery = q.Encode()
	}
	return u.String(), time.Duration(seconds) * time.Second, nil
}

// Fetch downloads a profile from endpoint (see Endpoint) into path. While the
// program refuses connections (still building or starting) it retries, until
// alive reports the program gone, startupWait passes or ctx ends.
func Fetch(ctx context.Context, endpoint string, recording time.Duration, path string, alive func() bool) error {
	client := &http.Client{Timeout: recording + responseSlack}
	deadline := time.Now().Add(startupWait)
	for {
		err := fetchOnce(ctx, client, endpoint, path)
		if err == nil || !errors.Is(err, syscall.ECONNREFUSED) {
			return err
		}
		if !alive() {
			return fmt.Errorf("the program ended before %s answered", endpoint)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%s did not answer within %s (does the program import net/http/pprof and listen there?)", endpoint, startupWait)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(retryEvery):
		}
	}
}

func fetchOnce(ctx context.Context, client *http.Client, endpoint, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFileSize+1))
	if err != nil {
		return fmt.Errorf("read profile: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(body[:min(len(body), 300)]))
		return fmt.Errorf("%s: %s %s", endpoint, resp.Status, msg)
	}
	if len(body) > maxFileSize {
		return fmt.Errorf("%s: profile larger than %d MB", endpoint, maxFileSize>>20)
	}
	if _, err := profile.ParseData(body); err != nil {
		return fmt.Errorf("%s did not return a pprof profile: %w", endpoint, err)
	}
	return os.WriteFile(path, body, 0o600)
}
