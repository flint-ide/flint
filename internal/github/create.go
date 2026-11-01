package github

import (
	"context"
	"errors"
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
}

func (p NewPullRequest) validate() error {
	for _, b := range []string{p.Base, p.Head} {
		if b == "" || strings.HasPrefix(b, "-") || strings.ContainsFunc(b, unicode.IsSpace) {
			return errors.New("invalid branch name " + b)
		}
	}
	title := strings.TrimSpace(p.Title)
	if title == "" || len(title) > maxPRTitle || strings.ContainsFunc(title, unicode.IsControl) {
		return errors.New("a pull request needs a one-line title of at most 256 characters")
	}
	return nil
}

// CreatePullRequest opens a pull request with gh from the repository in dir
// and returns its URL. Every value is passed as --flag=value and the body on
// stdin, so nothing in them is read as another flag or an @file reference.
func (c *Client) CreatePullRequest(ctx context.Context, dir string, p NewPullRequest) (string, error) {
	if err := p.validate(); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	out, err := c.run(ctx, dir, []byte(p.Body), "pr", "create",
		"--base="+p.Base, "--head="+p.Head, "--title="+strings.TrimSpace(p.Title), "--body-file=-")
	if err != nil {
		return "", err
	}
	c.lists.clear()
	// gh prints progress lines, then the new pull request's URL.
	lines := strings.Fields(string(out))
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(lines[i], "https://") || strings.HasPrefix(lines[i], "http://") {
			return lines[i], nil
		}
	}
	return "", errors.New("gh pr create did not print the pull request's URL")
}
