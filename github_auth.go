package main

import (
	"context"
	"os"
	"time"

	"flint/internal/accounts"
	"flint/internal/diag"
	"flint/internal/github"
)

// newGitHub is the GitHub client of the app: it asks githubAuth for the host
// and token on every operation, so signing in or out and changing the host
// apply at once (Reset drops what was cached for the old ones).
func newGitHub(a *App) *github.Client {
	return github.New(github.Config{
		Auth:      a.githubAuth,
		UserAgent: "Flint/" + diag.Version(version),
		MaxRetryWait: func() time.Duration {
			return time.Duration(a.currentSettings().GitHub.MaxRetryWaitSeconds) * time.Second
		},
	})
}

// githubEndpoints are the URLs of the configured host (settings.GitHub.Host),
// or of a local fake GitHub in a development server build (dev is true then).
func (a *App) githubEndpoints() (e github.Endpoints, dev bool, err error) {
	if e, ok, err := devGitHubEndpoints(); ok || err != nil {
		return e, true, err
	}
	e, err = github.EndpointsFor(a.currentSettings().GitHub.Host)
	return e, false, err
}

// githubAuth picks the token for the configured host: the signed-in account's
// (from the keyring), else GH_TOKEN / GITHUB_TOKEN (or the Enterprise
// variables) from Flint's environment, which is used as it is and never stored.
func (a *App) githubAuth() (github.Auth, error) {
	e, dev, err := a.githubEndpoints()
	if err != nil {
		return github.Auth{}, err
	}
	if a.accounts != nil {
		tok, ok, err := a.accounts.Token(github.ProviderID, e.Host)
		if err != nil {
			return github.Auth{Endpoints: e}, err
		}
		if ok && tok.Valid(time.Now()) {
			return github.Auth{Endpoints: e, Token: tok.Access}, nil
		}
	}
	if !dev { // a fake GitHub never gets a real token from the environment
		if _, v := github.EnvToken(e.Host, os.Getenv); accounts.CheckToken(v) == nil {
			return github.Auth{Endpoints: e, Token: v}, nil
		}
	}
	return github.Auth{Endpoints: e}, github.ErrNotAuthenticated
}

// githubProvider registers GitHub with the accounts layer.
func githubProvider(a *App) accounts.Provider {
	host := func() string {
		e, _, err := a.githubEndpoints()
		if err != nil {
			return a.currentSettings().GitHub.Host
		}
		return e.Host
	}
	return accounts.Provider{
		ID:   github.ProviderID,
		Name: "GitHub",
		Host: host,
		Flow: func() (accounts.DeviceFlow, error) {
			e, dev, err := a.githubEndpoints()
			if err != nil {
				return accounts.DeviceFlow{}, err
			}
			cfg := a.currentSettings().GitHub
			clientID := cfg.ClientID
			if dev && clientID == "" {
				clientID = "Iv1.flint-dev-fake"
			}
			if clientID == "" {
				return accounts.DeviceFlow{}, errNoClientID
			}
			return accounts.DeviceFlow{
				CodeURL: e.DeviceCodeURL(), TokenURL: e.AccessTokenURL(), ClientID: clientID,
				Scopes: cfg.Scopes, UserAgent: "Flint/" + diag.Version(version),
			}, nil
		},
		Identify: func(ctx context.Context, h string, tok accounts.Token) (accounts.Account, error) {
			e, _, err := a.githubEndpoints()
			if err != nil {
				return accounts.Account{}, err
			}
			if e.Host != h {
				return accounts.Account{}, errHostChanged
			}
			return a.github.Identify(ctx, e, tok.Access)
		},
		EnvToken: func(h string) string {
			if _, dev, _ := a.githubEndpoints(); dev {
				return ""
			}
			name, _ := github.EnvToken(h, os.Getenv)
			return name
		},
	}
}
