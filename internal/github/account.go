package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"flint/internal/accounts"
	"flint/internal/shellenv"
)

// ProviderID names GitHub in the accounts registry.
const ProviderID = "github"

// cliTimeout bounds `gh auth token`.
const cliTimeout = 15 * time.Second

// ErrCLINotInstalled: the GitHub CLI isn't on the login-shell PATH.
var ErrCLINotInstalled = errors.New("the GitHub CLI (gh) is not installed")

// Identify reads who a token belongs to (GET /user) and the scopes it has
// (X-OAuth-Scopes; fine-grained tokens have none to report). It is also the
// check that a token works before Flint stores it.
func (c *Client) Identify(ctx context.Context, e Endpoints, tok string) (accounts.Account, error) {
	if err := accounts.CheckToken(tok); err != nil {
		return accounts.Account{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	a := Auth{Endpoints: e, Token: tok}
	resp, err := c.do(ctx, a, call{method: http.MethodGet, url: rest(a, "/user")})
	if errors.Is(err, ErrNotAuthenticated) {
		return accounts.Account{}, fmt.Errorf("%s did not accept the token", e.Host)
	}
	if err != nil {
		return accounts.Account{}, err
	}
	var u struct {
		Login     string `json:"login"`
		Name      string `json:"name"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := json.Unmarshal(resp.body, &u); err != nil || !validName(u.Login) {
		return accounts.Account{}, errors.New("unexpected answer from GitHub (user)")
	}
	return accounts.Clean(accounts.Account{
		Provider: ProviderID, Host: e.Host, Login: u.Login, Name: u.Name, AvatarURL: u.AvatarURL,
		Scopes: accounts.ParseScopes(resp.header.Get("X-OAuth-Scopes")),
	}), nil
}

// CLIToken reads the GitHub CLI's token for host (`gh auth token`). It runs
// once, when the user asks to import it; the token never reaches an error
// message (gh's output isn't quoted).
func CLIToken(ctx context.Context, host string) (string, error) {
	shellenv.Wait()
	bin, err := exec.LookPath("gh")
	if err != nil {
		return "", ErrCLINotInstalled
	}
	ctx, cancel := context.WithTimeout(ctx, cliTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "auth", "token", "--hostname", host)
	cmd.Env = append(cmd.Environ(), "GH_PROMPT_DISABLED=1", "GH_NO_UPDATE_NOTIFIER=1", "NO_COLOR=1")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", errors.New("the GitHub CLI did not answer in time")
		}
		return "", fmt.Errorf("the GitHub CLI is not signed in to %s (run `gh auth login` first)", host)
	}
	tok := strings.TrimSpace(stdout.String())
	if accounts.CheckToken(tok) != nil {
		return "", errors.New("the GitHub CLI printed no usable token")
	}
	return tok, nil
}
