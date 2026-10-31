package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestEvaluate(t *testing.T) {
	rel := Release{Tag: "v1.3.0", URL: ReleasesURL}
	tests := []struct {
		name                 string
		current, skipped     string
		available, dev, skip bool
		notify               bool
	}{
		{name: "older release", current: "v1.2.0", available: true, notify: true},
		{name: "same release", current: "v1.3.0"},
		{name: "newer local build", current: "v1.4.0"},
		{name: "pre-release of the latest", current: "v1.3.0-rc.1", available: true, notify: true},
		{name: "dev build", current: "dev", dev: true},
		{name: "dev build with revision", current: "dev-0123456789ab+dirty", dev: true},
		{name: "skipped", current: "v1.2.0", skipped: "v1.3.0", available: true, skip: true},
		{name: "an older release was skipped", current: "v1.2.0", skipped: "v1.2.5", available: true, notify: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Evaluate(tt.current, rel, tt.skipped)
			if r.Available != tt.available || r.Dev != tt.dev || r.Skipped != tt.skip || r.Notify() != tt.notify {
				t.Errorf("Evaluate() = %+v (notify %v), want available %v dev %v skipped %v notify %v",
					r, r.Notify(), tt.available, tt.dev, tt.skip, tt.notify)
			}
			if r.Current != tt.current || r.Latest != rel {
				t.Errorf("Evaluate() = %+v, want current and release passed through", r)
			}
		})
	}
}

func TestDue(t *testing.T) {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		checkedAt time.Time
		want      bool
	}{
		{"never checked", time.Time{}, true},
		{"just checked", now.Add(-time.Minute), false},
		{"almost a day ago", now.Add(-Interval + time.Second), false},
		{"exactly a day ago", now.Add(-Interval), true},
		{"two days ago", now.Add(-2 * Interval), true},
		{"in the future (clock turned back)", now.Add(time.Hour), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Due(tt.checkedAt, now); got != tt.want {
				t.Errorf("Due(%v, %v) = %v, want %v", tt.checkedAt, now, got, tt.want)
			}
		})
	}
}

func TestSafeURL(t *testing.T) {
	tests := []struct {
		link string
		want string
	}{
		{"https://github.com/ODudek/flint/releases/tag/v1.3.0", "https://github.com/ODudek/flint/releases/tag/v1.3.0"},
		{"https://github.com/ODudek/flint/releases/latest", "https://github.com/ODudek/flint/releases/latest"},
		{"", ReleasesURL},
		{"http://github.com/ODudek/flint/releases/tag/v1.3.0", ReleasesURL},
		{"https://github.com/ODudek/flint-evil/releases/tag/v1.3.0", ReleasesURL},
		{"https://github.com/ODudek/flint", ReleasesURL},
		{"https://github.com/other/flint/releases/tag/v1.3.0", ReleasesURL},
		{"https://github.com.evil.example/ODudek/flint/releases", ReleasesURL},
		{"https://evil.example/https://github.com/ODudek/flint/", ReleasesURL},
		{"https://github.com/ODudek/flint/../../evil/repo", ReleasesURL},
		{"https://github.com/ODudek/flint/%2e%2e/%2e%2e/evil", ReleasesURL},
		{`https://github.com/ODudek/flint/..\..\evil`, ReleasesURL},
		{"https://user@github.com/ODudek/flint/releases", ReleasesURL},
		{"https://github.com:8443/ODudek/flint/releases", ReleasesURL},
		{"javascript:alert(1)//https://github.com/ODudek/flint/", ReleasesURL},
		{"https://github.com/ODudek/flint/releases/tag/v1.3.0\n", ReleasesURL},
	}
	for _, tt := range tests {
		t.Run(tt.link, func(t *testing.T) {
			if got := SafeURL(tt.link); got != tt.want {
				t.Errorf("SafeURL(%q) = %q, want %q", tt.link, got, tt.want)
			}
		})
	}
}

// serve answers the latest-release endpoint with status and body, and records
// the request so tests can check what Flint sends.
func serve(t *testing.T, status int, body string) (*Client, *http.Request) {
	t.Helper()
	seen := new(http.Request)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*seen = *r.Clone(context.Background())
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	c := New("v1.2.0")
	c.Base = srv.URL
	return c, seen
}

func TestLatest(t *testing.T) {
	const release = `{
		"tag_name": "v1.3.0",
		"name": "Flint 1.3\u0007",
		"html_url": "https://github.com/ODudek/flint/releases/tag/v1.3.0",
		"published_at": "2026-10-30T09:15:00Z",
		"draft": false,
		"prerelease": false,
		"body": "notes",
		"assets": [{"name": "Flint.dmg"}]
	}`
	tests := []struct {
		name    string
		status  int
		body    string
		want    Release
		wantErr string
	}{
		{
			name: "release", status: 200, body: release,
			want: Release{Tag: "v1.3.0", Name: "Flint 1.3", URL: "https://github.com/ODudek/flint/releases/tag/v1.3.0",
				PublishedAt: time.Date(2026, 10, 30, 9, 15, 0, 0, time.UTC)},
		},
		{
			name: "foreign link falls back", status: 200,
			body: `{"tag_name": "v1.3.0", "html_url": "https://evil.example/flint.dmg", "published_at": null}`,
			want: Release{Tag: "v1.3.0", URL: ReleasesURL},
		},
		{name: "no releases", status: 404, body: `{"message": "Not Found"}`, wantErr: "no published release"},
		{name: "rate limited", status: 403, body: `{"message": "API rate limit exceeded"}`, wantErr: "403"},
		{name: "not JSON", status: 200, body: `<html>`, wantErr: "decode"},
		{name: "bad tag", status: 200, body: `{"tag_name": "nightly"}`, wantErr: "unexpected tag"},
		{name: "pre-release", status: 200, body: `{"tag_name": "v2.0.0-rc.1", "prerelease": true}`, wantErr: "pre-release"},
		{name: "draft", status: 200, body: `{"tag_name": "v2.0.0", "draft": true}`, wantErr: "draft"},
		{name: "too big", status: 200, body: `{"tag_name": "v1.3.0", "body": "` + strings.Repeat("x", maxBody) + `"}`, wantErr: "decode"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, seen := serve(t, tt.status, tt.body)
			got, err := c.Latest(context.Background())
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Latest() error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Latest() error = %v", err)
			}
			if !got.PublishedAt.Equal(tt.want.PublishedAt) {
				t.Errorf("PublishedAt = %v, want %v", got.PublishedAt, tt.want.PublishedAt)
			}
			got.PublishedAt, tt.want.PublishedAt = time.Time{}, time.Time{}
			if got != tt.want {
				t.Errorf("Latest() = %+v, want %+v", got, tt.want)
			}
			if seen.URL.Path != latestPath {
				t.Errorf("path = %q, want %q", seen.URL.Path, latestPath)
			}
		})
	}
}

func TestLatestSendsOnlyWhatIsDocumented(t *testing.T) {
	c, seen := serve(t, 200, `{"tag_name": "v1.3.0"}`)
	if _, err := c.Latest(context.Background()); err != nil {
		t.Fatalf("Latest() error = %v", err)
	}
	if seen.Method != http.MethodGet || seen.URL.RawQuery != "" {
		t.Errorf("request = %s %s, want a plain GET", seen.Method, seen.URL)
	}
	want := map[string]string{
		"Accept":               "application/vnd.github+json",
		"User-Agent":           "Flint/v1.2.0",
		"X-Github-Api-Version": "2022-11-28",
	}
	for name, value := range want {
		if got := seen.Header.Get(name); got != value {
			t.Errorf("%s = %q, want %q", name, got, value)
		}
	}
	for _, name := range []string{"Authorization", "Cookie"} {
		if seen.Header.Get(name) != "" {
			t.Errorf("request carries %s", name)
		}
	}
}

func TestLatestRefusesRedirectToAnotherHost(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("followed a redirect to another host")
	}))
	t.Cleanup(other.Close)
	srv := httptest.NewServer(http.RedirectHandler(other.URL+latestPath, http.StatusFound))
	t.Cleanup(srv.Close)
	c := New("v1.2.0")
	c.Base = srv.URL
	if _, err := c.Latest(context.Background()); err == nil || !strings.Contains(err.Error(), "redirect") {
		t.Errorf("Latest() error = %v, want a refused redirect", err)
	}
}

func TestLatestTimesOutWithContext(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-block:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() {
		close(block)
		srv.Close()
	})
	c := New("v1.2.0")
	c.Base = srv.URL
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := c.Latest(ctx); err == nil {
		t.Error("Latest() error = nil, want a timeout")
	}
}
