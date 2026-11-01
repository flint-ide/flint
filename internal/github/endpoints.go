package github

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"flint/internal/settings"
)

// Endpoints are the URLs of one GitHub host.
type Endpoints struct {
	// Host is what remotes are matched against: github.com, ghe.example.com.
	Host string
	// Web is the site (https://github.com): device-flow sign-in, settings pages.
	Web string
	// API is the REST base (https://api.github.com, https://<host>/api/v3).
	API string
	// GraphQL is the GraphQL endpoint.
	GraphQL string
}

// EndpointsFor returns the URLs of host: github.com, a GitHub Enterprise Cloud
// host with data residency (<name>.ghe.com, API at api.<name>.ghe.com) or a
// GitHub Enterprise Server (API under https://<host>/api/).
func EndpointsFor(host string) (Endpoints, error) {
	host = strings.ToLower(host)
	if err := settings.CheckGitHubHost(host); err != nil {
		return Endpoints{}, err
	}
	web := "https://" + host
	switch {
	case host == "github.com":
		return Endpoints{Host: host, Web: web, API: "https://api.github.com", GraphQL: "https://api.github.com/graphql"}, nil
	case strings.HasSuffix(host, ".ghe.com"):
		return Endpoints{Host: host, Web: web, API: "https://api." + host, GraphQL: "https://api." + host + "/graphql"}, nil
	default:
		return Endpoints{Host: host, Web: web, API: web + "/api/v3", GraphQL: web + "/api/graphql"}, nil
	}
}

// LocalEndpoints serves a fake GitHub on a loopback address over plain http,
// laid out like GitHub Enterprise Server. Only development builds use it (see
// github_dev_server.go); anything but a loopback address is refused, so it can
// never send a real token to another machine.
func LocalEndpoints(base string) (Endpoints, error) {
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "http" || u.Path != "" && u.Path != "/" || u.User != nil || u.RawQuery != "" {
		return Endpoints{}, fmt.Errorf("a local GitHub must be http://127.0.0.1:<port>, not %q", base)
	}
	if h := u.Hostname(); h != "127.0.0.1" && h != "::1" && h != "localhost" {
		return Endpoints{}, fmt.Errorf("a local GitHub must be on a loopback address, not %q", h)
	}
	if u.Port() == "" {
		return Endpoints{}, fmt.Errorf("a local GitHub needs a port: %q", base)
	}
	web := "http://" + u.Host
	return Endpoints{Host: u.Host, Web: web, API: web + "/api/v3", GraphQL: web + "/api/graphql"}, nil
}

// DeviceCodeURL and AccessTokenURL are the OAuth device-flow endpoints.
func (e Endpoints) DeviceCodeURL() string  { return e.Web + "/login/device/code" }
func (e Endpoints) AccessTokenURL() string { return e.Web + "/login/oauth/access_token" }

// AuthorizedAppsURL lists the OAuth apps the user authorized (where a token
// is revoked: that needs the app's secret, which Flint doesn't have).
func (e Endpoints) AuthorizedAppsURL() string { return e.Web + "/settings/applications" }

// EnvToken returns a token from the environment for host, as the GitHub CLI
// reads it: GH_TOKEN or GITHUB_TOKEN for github.com and *.ghe.com,
// GH_ENTERPRISE_TOKEN or GITHUB_ENTERPRISE_TOKEN for an Enterprise Server.
// name says which variable it came from ("" when none is set).
func EnvToken(host string, getenv func(string) string) (name, token string) {
	if getenv == nil {
		getenv = os.Getenv
	}
	names := []string{"GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN"}
	if host == "github.com" || strings.HasSuffix(host, ".ghe.com") {
		names = []string{"GH_TOKEN", "GITHUB_TOKEN"}
	}
	for _, n := range names {
		if v := strings.TrimSpace(getenv(n)); v != "" {
			return n, v
		}
	}
	return "", ""
}
