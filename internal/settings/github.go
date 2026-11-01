package settings

import (
	"fmt"
	"net"
	"regexp"
	"slices"
	"strings"
)

// GitHub configures how Flint talks to GitHub (pull requests, CI, signing in).
type GitHub struct {
	// Host is github.com or a GitHub Enterprise Server host (ghe.example.com):
	// the API is https://<host>/api/v3 and /api/graphql there.
	Host string `json:"host"`
	// ClientID is the OAuth App Flint signs in with (Device Flow enabled).
	// Empty until one is configured: "Sign in with GitHub" explains how to
	// register one, and the token of the GitHub CLI or GH_TOKEN still work.
	ClientID string `json:"clientId"`
	// Scopes asked for when signing in.
	Scopes []string `json:"scopes"`
	// MaxRetryWaitSeconds is how long a read waits when GitHub says "retry
	// after" (secondary rate limit) before trying once more; a longer wait,
	// or 0, reports the limit instead.
	MaxRetryWaitSeconds int `json:"maxRetryWaitSeconds"`
}

// Bounds of the GitHub settings.
const (
	DefaultGitHubHost      = "github.com"
	MaxGitHubRetryWait     = 300
	maxGitHubScopes        = 20
	maxGitHubHostLen       = 253
	maxGitHubClientIDLen   = 100
	defaultGitHubRetryWait = 60
)

var (
	// hostLabelRE: one DNS label.
	hostLabelRE = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)
	// clientIDRE: GitHub OAuth App client ids ("Iv1.0123abcd…", "Ov23li…").
	clientIDRE = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	// scopeRE: an OAuth scope ("repo", "read:org", "admin:repo_hook").
	scopeRE = regexp.MustCompile(`^[a-z][a-z0-9_]*(?::[a-z][a-z0-9_]*)?$`)
)

// DefaultGitHubScopes: repositories (pull requests, reviews, checks, re-runs),
// organisation membership (private org repositories) and workflow files.
func DefaultGitHubScopes() []string { return []string{"repo", "read:org", "workflow"} }

func defaultGitHub() GitHub {
	return GitHub{Host: DefaultGitHubHost, Scopes: DefaultGitHubScopes(), MaxRetryWaitSeconds: defaultGitHubRetryWait}
}

// CheckGitHubHost accepts a host name with an optional port: no scheme, path,
// user or spaces, so the API URL built from it can only name that host.
func CheckGitHubHost(host string) error {
	if host == "" || len(host) > maxGitHubHostLen {
		return fmt.Errorf("invalid GitHub host %q", host)
	}
	name, port := host, ""
	if h, p, err := net.SplitHostPort(host); err == nil {
		name, port = h, p
	}
	if port != "" {
		var n int
		if _, err := fmt.Sscanf(port, "%d", &n); err != nil || n < 1 || n > 65535 || fmt.Sprint(n) != port {
			return fmt.Errorf("invalid port in GitHub host %q", host)
		}
	}
	for label := range strings.SplitSeq(name, ".") {
		if !hostLabelRE.MatchString(label) {
			return fmt.Errorf("invalid GitHub host %q: a host name like github.com or ghe.example.com", host)
		}
	}
	return nil
}

// CheckGitHub reports the first invalid GitHub value.
func CheckGitHub(g GitHub) error {
	if err := CheckGitHubHost(g.Host); err != nil {
		return err
	}
	if len(g.ClientID) > maxGitHubClientIDLen || (g.ClientID != "" && !clientIDRE.MatchString(g.ClientID)) {
		return fmt.Errorf("invalid GitHub OAuth client id %q", g.ClientID)
	}
	if len(g.Scopes) > maxGitHubScopes {
		return fmt.Errorf("too many GitHub scopes (max %d)", maxGitHubScopes)
	}
	for _, s := range g.Scopes {
		if !scopeRE.MatchString(s) {
			return fmt.Errorf("invalid GitHub scope %q", s)
		}
	}
	if g.MaxRetryWaitSeconds < 0 || g.MaxRetryWaitSeconds > MaxGitHubRetryWait {
		return fmt.Errorf("GitHub retry wait %d s out of range 0–%d", g.MaxRetryWaitSeconds, MaxGitHubRetryWait)
	}
	return nil
}

// normalizeGitHub replaces invalid values with defaults (hosts are lower case,
// scopes unique; a missing scope list is the default one).
func normalizeGitHub(g GitHub) GitHub {
	def := defaultGitHub()
	out := GitHub{
		Host:                strings.ToLower(strings.TrimSpace(g.Host)),
		ClientID:            strings.TrimSpace(g.ClientID),
		Scopes:              []string{},
		MaxRetryWaitSeconds: g.MaxRetryWaitSeconds,
	}
	if CheckGitHubHost(out.Host) != nil {
		out.Host = def.Host
	}
	if len(out.ClientID) > maxGitHubClientIDLen || (out.ClientID != "" && !clientIDRE.MatchString(out.ClientID)) {
		out.ClientID = ""
	}
	if g.Scopes == nil {
		out.Scopes = def.Scopes
	}
	for _, s := range g.Scopes {
		s = strings.TrimSpace(s)
		if len(out.Scopes) < maxGitHubScopes && scopeRE.MatchString(s) && !slices.Contains(out.Scopes, s) {
			out.Scopes = append(out.Scopes, s)
		}
	}
	if out.MaxRetryWaitSeconds < 0 || out.MaxRetryWaitSeconds > MaxGitHubRetryWait {
		out.MaxRetryWaitSeconds = def.MaxRetryWaitSeconds
	}
	return out
}
