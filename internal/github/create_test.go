package github

import (
	"strings"
	"testing"

	"flint/internal/accounts"
)

func TestCreatePullRequest(t *testing.T) {
	fx := newFixture(t)
	url, err := fx.c.CreatePullRequest(ctx(), fx.dir, NewPullRequest{
		Base: "main", Head: "agent/fix", Title: "  Fix the bug ", Body: "--body-file=/etc/passwd\nDo it", HeadRemote: "origin",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(url, fx.fake.RepoURL()+"/pull/") {
		t.Errorf("url = %q", url)
	}
	got := fx.fake.Created()
	if len(got) != 1 || got[0]["head"] != "agent/fix" || got[0]["title"] != "Fix the bug" || got[0]["body"] != "--body-file=/etc/passwd\nDo it" {
		t.Fatalf("created = %+v", got)
	}
}

func TestCreatePullRequestFromFork(t *testing.T) {
	fx := newFixture(t)
	runGit(t, fx.dir, "remote", "add", "mine", fx.fake.Web+"/octocat/widgets.git")
	if _, err := fx.c.CreatePullRequest(ctx(), fx.dir, NewPullRequest{Base: "main", Head: "agent/fix", Title: "t", HeadRemote: "mine"}); err != nil {
		t.Fatal(err)
	}
	if got := fx.fake.Created(); got[0]["head"] != "octocat:agent/fix" {
		t.Fatalf("head = %v", got[0]["head"])
	}
}

func TestCreatePullRequestValidates(t *testing.T) {
	fx := newFixture(t)
	for _, pr := range []NewPullRequest{
		{Base: "main", Head: "x", Title: ""},
		{Base: "main", Head: "x", Title: "a\nb"},
		{Base: "-main", Head: "x", Title: "t"},
		{Base: "main", Head: "a b", Title: "t"},
		{Base: "main", Head: "x", Title: strings.Repeat("t", 257)},
	} {
		if _, err := fx.c.CreatePullRequest(ctx(), fx.dir, pr); err == nil {
			t.Errorf("%+v accepted", pr)
		}
	}
	if len(fx.fake.Requests()) != 0 {
		t.Error("GitHub asked for an invalid pull request")
	}
}

func TestCreatePullRequestError(t *testing.T) {
	fx := newFixture(t)
	_, err := fx.c.CreatePullRequest(ctx(), fx.dir, NewPullRequest{Base: "main", Head: "feat/rate-limit", Title: "t"})
	if err == nil || !strings.Contains(err.Error(), "A pull request already exists for acme:feat/rate-limit") {
		t.Fatalf("err = %v", err)
	}
}

func TestIdentify(t *testing.T) {
	fx := newFixture(t)
	a, err := fx.c.Identify(ctx(), fx.auth.Endpoints, fx.fake.Token)
	if err != nil {
		t.Fatal(err)
	}
	want := accounts.Account{Provider: "github", Host: fx.auth.Host, Login: "octocat", Name: "The Octocat",
		AvatarURL: "https://avatars.githubusercontent.com/u/583231?v=4", Scopes: []string{"repo", "read:org", "workflow"}}
	if a.Login != want.Login || a.Host != want.Host || a.AvatarURL != want.AvatarURL || strings.Join(a.Scopes, ",") != "repo,read:org,workflow" {
		t.Fatalf("account = %+v", a)
	}
	if _, err := fx.c.Identify(ctx(), fx.auth.Endpoints, "wrong-token"); err == nil || strings.Contains(err.Error(), "wrong-token") {
		t.Errorf("bad token: %v", err)
	}
}
