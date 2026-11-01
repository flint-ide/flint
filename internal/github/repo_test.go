package github

import (
	"errors"
	"testing"

	"flint/internal/git"
)

func TestParseRemoteURL(t *testing.T) {
	tests := []struct {
		url, host, owner, name string
	}{
		{"https://github.com/acme/widgets.git", "github.com", "acme", "widgets"},
		{"https://github.com/acme/widgets", "github.com", "acme", "widgets"},
		{"https://github.com/acme/widgets/", "github.com", "acme", "widgets"},
		{"git@github.com:acme/widgets.git", "github.com", "acme", "widgets"},
		{"github.com:acme/widgets", "github.com", "acme", "widgets"},
		{"ssh://git@github.com/acme/widgets.git", "github.com", "acme", "widgets"},
		{"ssh://git@ssh.github.com:443/acme/widgets.git", "github.com", "acme", "widgets"},
		{"https://ghe.corp:8443/acme/widgets.git", "ghe.corp:8443", "acme", "widgets"},
		{"https://user:pass@GitHub.com/Acme/Widgets.js.git", "github.com", "Acme", "Widgets.js"},
	}
	for _, tt := range tests {
		h, o, n, ok := parseRemoteURL(tt.url)
		if !ok || h != tt.host || o != tt.owner || n != tt.name {
			t.Errorf("parseRemoteURL(%q) = %q %q %q %v", tt.url, h, o, n, ok)
		}
	}
	for _, bad := range []string{"", "/local/path/repo.git", "file:///tmp/x.git", "https://github.com/acme", "https://github.com/a/b/c", "https://github.com/../x", "git@github.com:acme/-/"} {
		if _, _, _, ok := parseRemoteURL(bad); ok {
			t.Errorf("parseRemoteURL(%q) accepted", bad)
		}
	}
}

func TestPickRemote(t *testing.T) {
	remotes := []git.Remote{
		{Name: "origin", URL: "git@github.com:me/widgets.git"},
		{Name: "upstream", URL: "https://github.com/acme/widgets.git"},
		{Name: "work", URL: "https://ghe.corp/acme/widgets.git"},
		{Name: "local", URL: "/tmp/x.git"},
	}
	pick, owners, ok := pickRemote(remotes, "github.com")
	if !ok || pick.remote != "upstream" || pick.owner != "acme" {
		t.Fatalf("pick = %+v", pick)
	}
	if len(owners) != 2 || owners[0] != "me" || owners[1] != "acme" {
		t.Errorf("owners = %v", owners)
	}
	// gh repo set-default wins.
	remotes[0].Resolved = "base"
	if pick, _, _ := pickRemote(remotes, "github.com"); pick.remote != "origin" {
		t.Errorf("gh-resolved base ignored: %+v", pick)
	}
	remotes[0].Resolved = "acme/other"
	if pick, _, _ := pickRemote(remotes, "github.com"); pick.remote != "origin" || pick.name != "other" {
		t.Errorf("gh-resolved owner/repo ignored: %+v", pick)
	}
	if pick, _, ok := pickRemote(remotes, "ghe.corp"); !ok || pick.remote != "work" {
		t.Errorf("enterprise host: %+v", pick)
	}
	if pick, _, ok := pickRemote([]git.Remote{{Name: "o", URL: "git@ghe.corp:a/b.git"}}, "ghe.corp:8443"); !ok || pick.remote != "o" {
		t.Errorf("ssh remote against a host with a web port: %+v", pick)
	}
	if _, _, ok := pickRemote(remotes, "gitlab.com"); ok {
		t.Error("picked a remote on another host")
	}
}

func TestRepoResolves(t *testing.T) {
	fx := newFixture(t)
	repo, err := fx.c.Repo(ctx(), fx.dir)
	if err != nil {
		t.Fatal(err)
	}
	if repo.NameWithOwner != "acme/widgets" || repo.DefaultBranch != "main" || repo.Remote != "origin" || repo.URL != fx.fake.RepoURL() {
		t.Fatalf("repo = %+v", repo)
	}
	// Cached: a second call asks GitHub nothing.
	n := len(fx.fake.Requests())
	if _, err := fx.c.Repo(ctx(), fx.dir); err != nil || len(fx.fake.Requests()) != n {
		t.Errorf("not cached (%v)", err)
	}
	st := fx.c.Check(ctx(), fx.dir, true)
	if st.State != "ok" || st.Repo.Name != "widgets" || st.Host != fx.auth.Host {
		t.Errorf("Check = %+v", st)
	}
}

func TestRepoWithoutGitHubRemote(t *testing.T) {
	fx := newFixture(t)
	dir := project(t, "origin", "https://gitlab.com/acme/widgets.git")
	_, err := fx.c.Repo(ctx(), dir)
	if !errors.Is(err, ErrNotGitHub) {
		t.Fatalf("err = %v", err)
	}
	if st := fx.c.Check(ctx(), t.TempDir(), false); st.State != "not-github" {
		t.Errorf("not a repository: %+v", st)
	}
	other := project(t, "origin", fx.fake.Web+"/acme/missing.git")
	if _, err := fx.c.Repo(ctx(), other); !errors.Is(err, ErrNotGitHub) {
		t.Errorf("unknown repository: %v", err)
	}
}
