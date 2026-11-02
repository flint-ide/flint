package settings

import (
	"slices"
	"testing"
)

func TestGitHubDefaults(t *testing.T) {
	g := Default().GitHub
	if g.Host != "github.com" || g.ClientID != "" || !slices.Equal(g.Scopes, []string{"repo", "read:org", "workflow"}) || g.MaxRetryWaitSeconds != 60 {
		t.Fatalf("defaults = %+v", g)
	}
	if err := Validate(Default()); err != nil {
		t.Fatal(err)
	}
}

func TestCheckGitHubHost(t *testing.T) {
	for _, h := range []string{"github.com", "ghe.example.com", "GHE.Example.com", "localhost:8443", "127.0.0.1:34230"} {
		if err := CheckGitHubHost(h); err != nil {
			t.Errorf("%q: %v", h, err)
		}
	}
	for _, h := range []string{"", "https://github.com", "github.com/api", "user@github.com", "git hub.com", "github.com:0", "github.com:99999", "github.com:x", "-a.com", "a..com", "github.com:080"} {
		if err := CheckGitHubHost(h); err == nil {
			t.Errorf("%q accepted", h)
		}
	}
}

func TestCheckGitHub(t *testing.T) {
	bad := []GitHub{
		{Host: "github.com", ClientID: "a b"},
		{Host: "github.com", Scopes: []string{"repo", "Repo!"}},
		{Host: "github.com", MaxRetryWaitSeconds: -1},
		{Host: "github.com", MaxRetryWaitSeconds: 301},
		{Host: "evil.com/x"},
	}
	for _, g := range bad {
		if err := CheckGitHub(g); err == nil {
			t.Errorf("%+v accepted", g)
		}
	}
	if err := CheckGitHub(GitHub{Host: "ghe.corp", ClientID: "Ov23liABC.def-1", Scopes: []string{"repo", "admin:repo_hook"}}); err != nil {
		t.Error(err)
	}
}

func TestNormalizeGitHub(t *testing.T) {
	s, err := decode([]byte(`{"github":{"host":" GHE.Corp ","clientId":"bad id","scopes":["repo","repo","x y","read:org"],"maxRetryWaitSeconds":9999}}`))
	if err != nil {
		t.Fatal(err)
	}
	g := s.GitHub
	if g.Host != "ghe.corp" || g.ClientID != "" || !slices.Equal(g.Scopes, []string{"repo", "read:org"}) || g.MaxRetryWaitSeconds != 60 {
		t.Fatalf("normalized = %+v", g)
	}
	s, _ = decode([]byte(`{"github":{"host":"https://evil"}}`))
	if s.GitHub.Host != "github.com" || len(s.GitHub.Scopes) != 3 {
		t.Fatalf("bad host kept: %+v", s.GitHub)
	}
	if err := Validate(s); err != nil {
		t.Fatal(err)
	}
}

func TestCloneGitHubScopes(t *testing.T) {
	s := Default()
	c := s.Clone()
	c.GitHub.Scopes[0] = "changed"
	if s.GitHub.Scopes[0] != "repo" {
		t.Error("Clone shares the scope list")
	}
}
