package main

import (
	"context"

	"flint/internal/git"
	"flint/internal/github"
)

// GitHubAPI reviews pull requests of a project's GitHub repository through gh.
// dir is any folder of the project (the frontend passes the active project's root).
type GitHubAPI struct {
	app *App
	git *GitAPI
	gh  *github.Client
	// ciGH replaces gh for the CI bindings in tests (api_ci.go).
	ciGH ciGitHub
}

// Check reports whether gh is installed and logged in and which repository dir is.
func (g *GitHubAPI) Check(dir string, force bool) github.Status {
	return g.gh.Check(g.app.ctx, dir, force)
}

// List lists pull requests (cached briefly; force refetches).
func (g *GitHubAPI) List(dir string, filter github.ListFilter, force bool) ([]github.PullRequest, error) {
	return g.gh.List(g.app.ctx, dir, filter, force)
}

// Details loads a pull request with its files, checks, reviews and threads.
func (g *GitHubAPI) Details(dir string, number int, force bool) (github.Details, error) {
	return g.gh.Details(g.app.ctx, dir, number, force)
}

// FileDiff returns the merge-base and head versions of a changed file.
func (g *GitHubAPI) FileDiff(dir string, number int, path string) (github.FileDiff, error) {
	return g.gh.FileDiff(g.app.ctx, dir, number, path, g.local(dir))
}

// Reply answers a review thread.
func (g *GitHubAPI) Reply(dir string, number int, threadID, body string) error {
	return g.gh.Reply(g.app.ctx, dir, number, threadID, body)
}

// SetResolved resolves or unresolves a review thread.
func (g *GitHubAPI) SetResolved(dir string, number int, threadID string, resolved bool) error {
	return g.gh.SetResolved(g.app.ctx, dir, number, threadID, resolved)
}

// AddComment comments on lines of a file, at once or in my pending review.
func (g *GitHubAPI) AddComment(dir string, number int, comment github.NewComment) error {
	return g.gh.AddComment(g.app.ctx, dir, number, comment)
}

// SubmitReview submits my review (COMMENT, APPROVE or REQUEST_CHANGES).
func (g *GitHubAPI) SubmitReview(dir string, number int, event, body string) error {
	return g.gh.SubmitReview(g.app.ctx, dir, number, event, body)
}

// DiscardReview deletes my pending review.
func (g *GitHubAPI) DiscardReview(dir string, number int) error {
	return g.gh.DiscardReview(g.app.ctx, dir, number)
}

// Checkout checks the pull request's branch out in the project (gh pr checkout),
// refusing while tracked files have local changes.
func (g *GitHubAPI) Checkout(dir string, number int) error {
	st, err := g.git.Status(dir)
	if err != nil {
		return err
	}
	var dirty []string
	for _, f := range st.Files {
		if f.Kind != git.Untracked {
			dirty = append(dirty, f.Path)
		}
	}
	return g.gh.Checkout(g.app.ctx, dir, number, dirty)
}

// local is the project's clone, used to skip downloads of objects it already has.
func (g *GitHubAPI) local(dir string) github.Local {
	ctx, cancel := context.WithTimeout(g.app.ctx, gitTimeout)
	defer cancel()
	r, err := g.git.repo(ctx, dir)
	if err != nil {
		return nil
	}
	return r
}
