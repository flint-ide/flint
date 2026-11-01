// Package update tells whether a newer Flint release exists. It asks GitHub's
// public API for the latest release of flint-ide/flint (one anonymous GET: no
// token, no cookies, no identifiers beyond the User-Agent "Flint/<version>")
// and compares its tag with the running version. It never downloads or
// installs anything: the user gets a link to the release page.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
)

const (
	// DefaultAPIBase is GitHub's REST API.
	DefaultAPIBase = "https://api.github.com"
	// ReleasesURL is where the download link points when the API's link is not
	// one Flint is willing to open.
	ReleasesURL = "https://github.com/flint-ide/flint/releases/latest"
	// releasePrefix is the only place a link from the API may point to.
	releasePrefix = "https://github.com/flint-ide/flint/"
	latestPath    = "/repos/flint-ide/flint/releases/latest"

	// Interval is how often Flint asks on its own.
	Interval = 24 * time.Hour

	requestTimeout = 10 * time.Second
	// A release document is a few KB; the release notes and asset list are the
	// only parts that grow. Anything bigger is not what we asked for.
	maxBody    = 1 << 20
	maxNameLen = 200
)

// Release is what Flint keeps of GitHub's latest release.
type Release struct {
	Tag         string    `json:"tag"`         // "v1.2.3"
	Name        string    `json:"name"`        // release title, may be empty
	URL         string    `json:"url"`         // release page, always under https://github.com/flint-ide/flint/
	PublishedAt time.Time `json:"publishedAt"` // zero when GitHub didn't say
}

// State is remembered between launches (in state.json): when GitHub was last
// asked, what it answered, and the release the user chose to skip.
type State struct {
	CheckedAt time.Time `json:"checkedAt,omitzero"`
	Latest    *Release  `json:"latest,omitempty"`
	Skipped   string    `json:"skipped,omitempty"`
}

// Result is the outcome of a check, for the UI.
type Result struct {
	Current string  `json:"current"` // the running version
	Latest  Release `json:"latest"`
	// Available: Latest is newer than a release build of Current.
	Available bool `json:"available"`
	// Dev: Current is a development build, which is never offered updates.
	Dev bool `json:"dev"`
	// Skipped: the user chose "Skip this version" for Latest.
	Skipped bool `json:"skipped"`
}

// Evaluate compares the running version with rel.
func Evaluate(current string, rel Release, skipped string) Result {
	r := Result{Current: current, Latest: rel, Dev: !IsRelease(current)}
	if order, ok := Compare(rel.Tag, current); ok && order > 0 {
		r.Available = true
	}
	r.Skipped = r.Available && skipped != "" && skipped == rel.Tag
	return r
}

// Notify reports whether the user should hear about r without having asked.
func (r Result) Notify() bool { return r.Available && !r.Dev && !r.Skipped }

// Due reports whether the daily check should run now. A last check in the
// future (the clock was turned back) counts as due, so a wrong clock can't
// switch the check off.
func Due(checkedAt, now time.Time) bool {
	return checkedAt.IsZero() || checkedAt.After(now) || now.Sub(checkedAt) >= Interval
}

// SafeURL returns link when it is an https page of the Flint repository on
// github.com, and the releases page otherwise, so a tampered response can't
// make Flint open an arbitrary site.
func SafeURL(link string) string {
	u, err := url.Parse(link)
	if err != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil || u.Opaque != "" {
		return ReleasesURL
	}
	if !strings.HasPrefix(link, releasePrefix) || strings.Contains(link, `\`) {
		return ReleasesURL
	}
	// Dot segments (also percent-encoded, which Path has decoded) would let a
	// link that starts with the prefix leave the repository.
	for _, seg := range strings.Split(u.Path, "/") {
		if seg == "." || seg == ".." {
			return ReleasesURL
		}
	}
	return link
}

// Client asks GitHub for the latest release.
type Client struct {
	// Base is the API root; tests point it at an httptest server.
	Base    string
	Version string // sent as User-Agent "Flint/<Version>"
	HTTP    *http.Client
}

// New returns a client for GitHub's API.
func New(version string) *Client {
	return &Client{Base: DefaultAPIBase, Version: version, HTTP: newHTTPClient()}
}

// newHTTPClient has no cookie jar and follows redirects only within the same
// scheme and host (GitHub redirects renamed repositories).
func newHTTPClient() *http.Client {
	return &http.Client{
		Timeout: requestTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			first := via[0].URL
			if len(via) >= 5 || req.URL.Scheme != first.Scheme || req.URL.Host != first.Host {
				return errors.New("unexpected redirect")
			}
			return nil
		},
	}
}

// apiRelease is the part of GitHub's release document Flint reads.
type apiRelease struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	HTMLURL     string    `json:"html_url"`
	PublishedAt time.Time `json:"published_at"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
}

// Latest fetches the latest published release. GitHub's "latest" already
// leaves out drafts and pre-releases; they are refused here too in case that
// ever changes.
func (c *Client) Latest(ctx context.Context) (Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.Base, "/")+latestPath, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "Flint/"+c.Version)
	client := c.HTTP
	if client == nil {
		client = newHTTPClient()
	}
	resp, err := client.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBody))
		// GitHub answers 404 both before the first release and for a private repository.
		if resp.StatusCode == http.StatusNotFound {
			return Release{}, errors.New("no published release found on GitHub")
		}
		return Release{}, fmt.Errorf("GitHub answered %s", resp.Status)
	}
	return decodeRelease(io.LimitReader(resp.Body, maxBody))
}

func decodeRelease(r io.Reader) (Release, error) {
	var api apiRelease
	if err := json.NewDecoder(r).Decode(&api); err != nil {
		return Release{}, fmt.Errorf("decode release: %w", err)
	}
	if api.Draft || api.Prerelease {
		return Release{}, errors.New("latest release is a draft or pre-release")
	}
	if !IsRelease(api.TagName) {
		return Release{}, fmt.Errorf("unexpected tag %q", cleanText(api.TagName, 40))
	}
	return Release{
		Tag:         api.TagName,
		Name:        cleanText(api.Name, maxNameLen),
		URL:         SafeURL(api.HTMLURL),
		PublishedAt: api.PublishedAt,
	}, nil
}

// cleanText drops control characters and caps the length of text shown in the UI.
func cleanText(s string, limit int) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.TrimSpace(s))
	if r := []rune(s); len(r) > limit {
		return string(r[:limit-1]) + "…"
	}
	return s
}
