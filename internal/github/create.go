package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"unicode"
)

// maxPRTitle bounds a pull request title (GitHub allows 256 characters).
const maxPRTitle = 256

// NewPullRequest is a pull request to open from a pushed branch.
type NewPullRequest struct {
	Base  string // branch to merge into
	Head  string // branch with the changes (pushed already)
	Title string
	Body  string
	// HeadRemote is the remote Head was pushed to: when it is a fork of the
	// repository, the pull request's head is "<fork owner>:<Head>".
	HeadRemote string
}

func (p NewPullRequest) validate() error {
	for _, b := range []string{p.Base, p.Head} {
		if checkBranch(b) != nil {
			return errors.New("invalid branch name " + b)
		}
	}
	title := strings.TrimSpace(p.Title)
	if title == "" || len(title) > maxPRTitle || strings.ContainsFunc(title, unicode.IsControl) {
		return errors.New("a pull request needs a one-line title of at most 256 characters")
	}
	if len(p.Body) > maxBodyLen {
		return errors.New("the pull request description is too long")
	}
	return nil
}

// CreatePullRequest opens a pull request in the repository of dir and returns
// its URL. Everything travels in the JSON body.
func (c *Client) CreatePullRequest(ctx context.Context, dir string, p NewPullRequest) (string, error) {
	if err := p.validate(); err != nil {
		return "", err
	}
	a, info, err := c.session(ctx, dir)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	head := p.Head
	if p.HeadRemote != "" && p.HeadRemote != info.Remote {
		owner, name, err := c.remoteOwner(ctx, a, dir, p.HeadRemote)
		if err != nil {
			return "", err
		}
		if !strings.EqualFold(owner+"/"+name, info.NameWithOwner) {
			head = owner + ":" + p.Head
		}
	}
	body := map[string]any{"title": strings.TrimSpace(p.Title), "head": head, "base": p.Base, "body": p.Body}
	var out struct {
		HTMLURL string `json:"html_url"`
	}
	err = c.getJSON(ctx, a, call{method: http.MethodPost, url: rest(a, "/repos/%s/%s/pulls", info.Owner, info.Name), body: body}, &out)
	if err != nil {
		return "", err
	}
	c.lists.clear()
	if u, perr := url.Parse(out.HTMLURL); perr != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return "", fmt.Errorf("GitHub did not return the pull request's URL")
	}
	return out.HTMLURL, nil
}
