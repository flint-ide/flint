package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"flint/internal/accounts"
	"flint/internal/config"
	"flint/internal/github"
	"flint/internal/github/githubtest"
	"flint/internal/settings"
)

// accountsApp is an App with a fake keyring (never the real Keychain).
func accountsApp(t *testing.T) (*App, *memSecrets) {
	t.Helper()
	sec := &memSecrets{}
	a := &App{configDir: t.TempDir(), settings: settings.Default(), ask: func(string, string, string) bool { return false }}
	a.accounts = accounts.NewStore(sec)
	a.github = newGitHub(a)
	a.registry = &accounts.Registry{}
	return a, sec
}

func TestGitHubAuthPrefersTheSignedInAccount(t *testing.T) {
	a, _ := accountsApp(t)
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	if _, err := a.githubAuth(); !errors.Is(err, github.ErrNotAuthenticated) {
		t.Fatalf("no token: %v", err)
	}
	t.Setenv("GITHUB_TOKEN", "ghp_fromEnvironment")
	auth, err := a.githubAuth()
	if err != nil || auth.Token != "ghp_fromEnvironment" || auth.API != "https://api.github.com" {
		t.Fatalf("env token: %+v %v", auth.Endpoints, err)
	}
	if err := a.accounts.Save("github", "github.com", accounts.Token{Access: "gho_signedIn"}); err != nil {
		t.Fatal(err)
	}
	if auth, _ := a.githubAuth(); auth.Token != "gho_signedIn" {
		t.Error("the signed-in account doesn't win over the environment")
	}
	// Another host: other account, other variables.
	a.settings.GitHub.Host = "ghe.corp"
	if auth, err := a.githubAuth(); err == nil || auth.API != "https://ghe.corp/api/v3" {
		t.Errorf("ghe: %+v %v", auth.Endpoints, err)
	}
	if err := a.accounts.Save("github", "github.com", accounts.Token{Access: "gho_old", Expiry: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	a.settings.GitHub.Host = "github.com"
	if auth, _ := a.githubAuth(); auth.Token != "ghp_fromEnvironment" {
		t.Error("an expired token was used")
	}
}

func TestProvidersExplainAMissingClientID(t *testing.T) {
	a, _ := accountsApp(t)
	if err := a.registry.Register(githubProvider(a)); err != nil {
		t.Fatal(err)
	}
	api := &AccountsAPI{app: a}
	list := api.Providers()
	if len(list) != 1 || list[0].ID != "github" || list[0].Host != "github.com" || !strings.Contains(list[0].SignIn, "OAuth App") {
		t.Fatalf("providers = %+v", list)
	}
	if _, err := api.StartSignIn("github"); !errors.Is(err, errNoClientID) {
		t.Errorf("StartSignIn without a client id: %v", err)
	}
	if _, err := api.StartSignIn("google"); err == nil {
		t.Error("unknown provider accepted")
	}
}

// fakeProvider is GitHub's provider pointed at githubtest's fake.
func fakeProvider(a *App, fake *githubtest.Fake) accounts.Provider {
	e, _ := github.LocalEndpoints(fake.Web)
	return accounts.Provider{
		ID: "github", Name: "GitHub", Host: func() string { return e.Host },
		Flow: func() (accounts.DeviceFlow, error) {
			return accounts.DeviceFlow{
				CodeURL: e.DeviceCodeURL(), TokenURL: e.AccessTokenURL(), ClientID: "Ov23liTest", Scopes: []string{"repo"},
				Sleep: func(ctx context.Context, _ time.Duration) error { fake.ApproveDevice(); return ctx.Err() },
			}, nil
		},
		Identify: func(ctx context.Context, host string, tok accounts.Token) (accounts.Account, error) {
			return a.github.Identify(ctx, e, tok.Access)
		},
	}
}

func TestDeviceFlowSignInAndOut(t *testing.T) {
	a, sec := accountsApp(t)
	fake := githubtest.Start(t)
	if err := a.registry.Register(fakeProvider(a, fake)); err != nil {
		t.Fatal(err)
	}
	api := &AccountsAPI{app: a}
	code, err := api.StartSignIn("github")
	if err != nil {
		t.Fatal(err)
	}
	if code.UserCode != githubtest.UserCode || !strings.HasSuffix(code.VerificationURI, "/login/device") || code.ExpiresIn < 800 {
		t.Fatalf("code = %+v", code)
	}
	acct, err := api.FinishSignIn(context.Background(), code.ID)
	if err != nil {
		t.Fatal(err)
	}
	if acct.Login != "octocat" || acct.Source != accounts.SourceDevice || acct.SignedInAt == "" {
		t.Fatalf("account = %+v", acct)
	}
	host := acct.Host
	if v := sec.v["github:"+host]; !strings.Contains(v, fake.Token) {
		t.Fatalf("keyring = %v", sec.v)
	}
	// The token is in the keyring only: not in state.json, not in what the UI gets.
	state, _ := os.ReadFile(a.statePath())
	providers, _ := json.Marshal(api.Providers())
	for what, data := range map[string][]byte{"state.json": state, "Providers": providers} {
		if strings.Contains(string(data), fake.Token) {
			t.Errorf("token in %s", what)
		}
	}
	var saved appState
	if err := config.LoadJSON(a.statePath(), &saved); err != nil || len(saved.Accounts) != 1 || saved.Accounts[0].Login != "octocat" {
		t.Fatalf("state = %+v, %v", saved.Accounts, err)
	}
	if _, err := api.FinishSignIn(context.Background(), code.ID); !errors.Is(err, errNoSignIn) {
		t.Errorf("a finished sign-in ran again: %v", err)
	}
	if err := api.SignOut("github"); err != nil {
		t.Fatal(err)
	}
	if sec.v["github:"+host] != "" || len(a.accountList()) != 0 {
		t.Error("sign out left the token or the account")
	}
}

func TestSignInCanBeCancelled(t *testing.T) {
	a, _ := accountsApp(t)
	fake := githubtest.Start(t)
	p := fakeProvider(a, fake)
	p.Flow = func() (accounts.DeviceFlow, error) {
		e, _ := github.LocalEndpoints(fake.Web)
		return accounts.DeviceFlow{CodeURL: e.DeviceCodeURL(), TokenURL: e.AccessTokenURL(), ClientID: "x",
			Sleep: func(ctx context.Context, d time.Duration) error { <-ctx.Done(); return ctx.Err() }}, nil
	}
	_ = a.registry.Register(p)
	api := &AccountsAPI{app: a}
	code, err := api.StartSignIn("github")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := api.FinishSignIn(context.Background(), code.ID); done <- err }()
	time.Sleep(50 * time.Millisecond)
	api.CancelSignIn(code.ID)
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled sign-in succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("CancelSignIn did not stop the wait")
	}
}

func TestImportGitHubCLINeedsConsent(t *testing.T) {
	a, sec := accountsApp(t)
	if err := a.registry.Register(githubProvider(a)); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir()) // no gh: nothing may run anyway
	var asked string
	a.ask = func(title, msg, ok string) bool { asked = msg; return false }
	if _, err := (&AccountsAPI{app: a}).ImportGitHubCLI(); !errors.Is(err, errCLIDeclined) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(asked, "gh auth token") || !strings.Contains(asked, "github.com") || len(sec.v) != 0 {
		t.Errorf("asked %q, keyring %v", asked, sec.v)
	}
}
