package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"flint/internal/accounts"
	"flint/internal/config"
	"flint/internal/github"
)

// eventAccountsChanged: an account was signed in or out (the views that use it reload).
const eventAccountsChanged = "accounts:changed"

var (
	errNoClientID = errors.New("no GitHub OAuth App is configured for signing in: register one with Device Flow enabled " +
		"and put its client id in Settings ▸ Accounts, or use the token of the GitHub CLI")
	errHostChanged  = errors.New("the GitHub host changed while signing in: start again")
	errNoSignIn     = errors.New("this sign-in is over: start again")
	errCLIDeclined  = errors.New("import cancelled")
	errUnknownProvr = errors.New("unknown account provider")
)

// AccountsAPI signs in to and out of the registered providers. Tokens stay in
// Go and the keyring: no binding returns one, and the web view can't choose
// where one is sent.
type AccountsAPI struct {
	app *App

	mu      sync.Mutex
	pending map[string]*pendingSignIn // by sign-in id; one per provider
}

type pendingSignIn struct {
	provider accounts.Provider
	host     string
	flow     accounts.DeviceFlow
	code     accounts.DeviceCode
	cancel   context.CancelFunc
	ctx      context.Context
}

// ProviderInfo is what Settings ▸ Accounts shows about a provider.
type ProviderInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Host string `json:"host"`
	// Account is the signed-in account on Host (null when none).
	Account *accounts.Account `json:"account"`
	// EnvToken names the environment variable whose token is used while no
	// account is signed in ("" when none is set).
	EnvToken string `json:"envToken"`
	// SignIn is "" when "Sign in" can start, else why not (no OAuth client…).
	SignIn string `json:"signIn"`
}

// SignInCode is a started sign-in: the code to enter on the page.
type SignInCode struct {
	ID              string `json:"id"`
	UserCode        string `json:"userCode"`
	VerificationURI string `json:"verificationUri"`
	ExpiresIn       int    `json:"expiresIn"` // seconds
}

func (s *AccountsAPI) provider(id string) (accounts.Provider, error) {
	if s.app.registry == nil {
		return accounts.Provider{}, errUnknownProvr
	}
	p, ok := s.app.registry.Get(id)
	if !ok {
		return accounts.Provider{}, fmt.Errorf("%w %q", errUnknownProvr, id)
	}
	return p, nil
}

// Providers lists the providers with their signed-in accounts.
func (s *AccountsAPI) Providers() []ProviderInfo {
	out := []ProviderInfo{}
	if s.app.registry == nil {
		return out
	}
	list := s.app.accountList()
	for _, p := range s.app.registry.List() {
		host := p.Host()
		info := ProviderInfo{ID: p.ID, Name: p.Name, Host: host}
		if a, ok := accounts.Find(list, p.ID, host); ok {
			info.Account = &a
		}
		if p.EnvToken != nil {
			info.EnvToken = p.EnvToken(host)
		}
		if _, err := p.Flow(); err != nil {
			info.SignIn = err.Error()
		}
		out = append(out, info)
	}
	return out
}

// StartSignIn asks the provider for a device code. The user enters it on the
// verification page (OpenSignInPage); FinishSignIn waits for that.
func (s *AccountsAPI) StartSignIn(providerID string) (SignInCode, error) {
	p, err := s.provider(providerID)
	if err != nil {
		return SignInCode{}, err
	}
	flow, err := p.Flow()
	if err != nil {
		return SignInCode{}, err
	}
	host := p.Host()
	ctx, cancel := context.WithCancel(s.app.context())
	code, err := flow.Start(ctx)
	if err != nil {
		cancel()
		return SignInCode{}, err
	}
	id := randomID()
	s.mu.Lock()
	for old, ps := range s.pending { // one sign-in per provider at a time
		if ps.provider.ID == p.ID {
			ps.cancel()
			delete(s.pending, old)
		}
	}
	if s.pending == nil {
		s.pending = map[string]*pendingSignIn{}
	}
	s.pending[id] = &pendingSignIn{provider: p, host: host, flow: flow, code: code, cancel: cancel, ctx: ctx}
	s.mu.Unlock()
	return SignInCode{
		ID: id, UserCode: code.UserCode, VerificationURI: code.VerificationURI,
		ExpiresIn: int(time.Until(code.Expires).Round(time.Second).Seconds()),
	}, nil
}

func (s *AccountsAPI) signIn(id string) (*pendingSignIn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ps, ok := s.pending[id]
	if !ok {
		return nil, errNoSignIn
	}
	return ps, nil
}

// OpenSignInPage opens the verification page in the browser. The URL is the
// one the provider sent (checked to be on its host), never one from the UI.
func (s *AccountsAPI) OpenSignInPage(id string) error {
	ps, err := s.signIn(id)
	if err != nil {
		return err
	}
	return s.app.openURL(ps.code.VerificationURI)
}

// FinishSignIn waits until the code was entered (or denied, or expired),
// checks the token, stores it in the keyring and the account in Flint's
// state. Cancelling the call or CancelSignIn stops the wait.
func (s *AccountsAPI) FinishSignIn(ctx context.Context, id string) (accounts.Account, error) {
	ps, err := s.signIn(id)
	if err != nil {
		return accounts.Account{}, err
	}
	defer s.forget(id)
	ctx, stop := mergeCancel(ctx, ps.ctx)
	defer stop()
	tok, err := ps.flow.Poll(ctx, ps.code)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return accounts.Account{}, errors.New("sign-in cancelled")
		}
		return accounts.Account{}, err
	}
	return s.app.storeAccount(ctx, ps.provider, ps.host, tok, accounts.SourceDevice)
}

// CancelSignIn stops a sign-in in progress.
func (s *AccountsAPI) CancelSignIn(id string) {
	s.forget(id)
}

func (s *AccountsAPI) forget(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ps, ok := s.pending[id]; ok {
		ps.cancel()
		delete(s.pending, id)
	}
}

// SignOut deletes the provider's token for its current host from the keyring
// and forgets the account. Revoking the token at GitHub needs the OAuth App's
// secret, which Flint doesn't have: OpenAuthorizedApps shows where to do it.
func (s *AccountsAPI) SignOut(providerID string) error {
	p, err := s.provider(providerID)
	if err != nil {
		return err
	}
	host := p.Host()
	if err := s.app.accounts.Delete(p.ID, host); err != nil {
		return err
	}
	return s.app.saveAccounts(func(list []accounts.Account) []accounts.Account { return accounts.Without(list, p.ID, host) })
}

// OpenAuthorizedApps opens GitHub's page of authorized OAuth apps, where a
// token is revoked.
func (s *AccountsAPI) OpenAuthorizedApps() error {
	e, _, err := s.app.githubEndpoints()
	if err != nil {
		return err
	}
	return s.app.openURL(e.AuthorizedAppsURL())
}

// ImportGitHubCLI stores the GitHub CLI's token for the configured host as
// the signed-in account, after a native confirmation: `gh auth token` runs
// once, and gh is not needed afterwards.
func (s *AccountsAPI) ImportGitHubCLI() (accounts.Account, error) {
	p, err := s.provider(github.ProviderID)
	if err != nil {
		return accounts.Account{}, err
	}
	host := p.Host()
	msg := fmt.Sprintf("Flint will run `gh auth token` once to read the GitHub CLI's token for %s, check it with GitHub and keep it in the system keyring. "+
		"It is sent only to %s, never shown in Flint.", host, host)
	if !s.app.askUser("Use the GitHub CLI's token?", msg, "Use Token") {
		return accounts.Account{}, errCLIDeclined
	}
	ctx := s.app.context()
	tok, err := github.CLIToken(ctx, host)
	if err != nil {
		return accounts.Account{}, err
	}
	return s.app.storeAccount(ctx, p, host, accounts.Token{Access: tok}, accounts.SourceCLI)
}

// storeAccount checks a token with the provider, then keeps it and the account.
func (a *App) storeAccount(ctx context.Context, p accounts.Provider, host string, tok accounts.Token, source string) (accounts.Account, error) {
	acct, err := p.Identify(ctx, host, tok)
	if err != nil {
		return accounts.Account{}, err
	}
	if len(acct.Scopes) == 0 {
		acct.Scopes = tok.Scopes
	}
	acct.Provider, acct.Host, acct.Source, acct.SignedInAt = p.ID, host, source, accounts.Since(time.Now())
	acct = accounts.Clean(acct)
	if err := a.accounts.Save(p.ID, host, tok); err != nil {
		return accounts.Account{}, err
	}
	if err := a.saveAccounts(func(list []accounts.Account) []accounts.Account { return accounts.Upsert(list, acct) }); err != nil {
		return accounts.Account{}, err
	}
	return acct, nil
}

// accountList is the accounts in Flint's state.
func (a *App) accountList() []accounts.Account {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	return append([]accounts.Account{}, a.state.Accounts...)
}

// saveAccounts changes the account list in state.json, drops what the GitHub
// client cached for the old account and tells the UI.
func (a *App) saveAccounts(change func([]accounts.Account) []accounts.Account) error {
	a.stateMu.Lock()
	next := a.state
	next.Accounts = change(append([]accounts.Account{}, a.state.Accounts...))
	err := config.SaveJSON(a.statePath(), next)
	if err == nil {
		a.state = next
	}
	a.stateMu.Unlock()
	if a.github != nil {
		a.github.Reset()
	}
	a.emit(eventAccountsChanged)
	return err
}

// context is the app's lifetime (Background before startup, in tests).
func (a *App) context() context.Context {
	if a.ctx == nil {
		return context.Background()
	}
	return a.ctx
}

// mergeCancel ends when either context does.
func mergeCancel(a, b context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(a)
	stop := context.AfterFunc(b, cancel)
	return ctx, func() { stop(); cancel() }
}
