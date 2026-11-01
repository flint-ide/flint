package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"flint/internal/git"
	"flint/internal/review"
)

// Limits of a review request. They protect Flint (the WebView sends the file
// list), not preferences: the diff size and the timeout are settings.
const (
	maxReviewFiles     = 1000
	maxReviewPath      = 1024
	maxReviewFileBytes = 2 << 20 // larger new versions are reviewed from the diff alone
)

// ReviewFile is one change to review, as the Commit panel lists it.
type ReviewFile struct {
	Path     string `json:"path"`     // relative to the repository root
	OrigPath string `json:"origPath"` // a rename's source
	Kind     string `json:"kind"`     // git.FileChange kind
}

// ReviewRequest asks for a review of files in the repository at Root.
type ReviewRequest struct {
	Root  string       `json:"root"`
	Files []ReviewFile `json:"files"`
}

// ReviewResult is the agent's findings and what the user should know about
// the run (files left out, a shortened diff, a cut-off answer).
type ReviewResult struct {
	Agent    string           `json:"agent"`
	Files    []string         `json:"files"`
	Findings []review.Finding `json:"findings"`
	Notes    []string         `json:"notes"`
}

var reviewKinds = []string{"modified", "added", "deleted", "renamed", "copied", "typechange", "untracked"}

// ReviewChanges asks an agent (settings: agentReview.agent, else the default
// one) to review req.Files against HEAD, as the Commit panel shows them, and
// returns its findings on lines of the new versions. The run is headless and
// read-only like commit messages: Claude gets only Read, Grep and Glob, Codex
// a read-only sandbox, a custom agent a session that refuses writes; in
// Restricted Mode Claude loads only the user's settings and a custom agent
// refuses to start. Secrets are redacted and files that hold credentials are
// not sent. Cancelling the call stops the agent and what it started.
func (a *AgentAPI) ReviewChanges(ctx context.Context, req ReviewRequest) (ReviewResult, error) {
	res, err := a.review(ctx, req)
	if err != nil && errors.Is(ctx.Err(), context.Canceled) {
		return ReviewResult{}, ctx.Err() // whichever step was running when it was cancelled
	}
	return res, err
}

func (a *AgentAPI) review(ctx context.Context, req ReviewRequest) (ReviewResult, error) {
	if err := a.checkReviewRequest(req); err != nil {
		return ReviewResult{}, err
	}
	name, err := a.reviewAgent()
	if err != nil {
		return ReviewResult{}, err
	}
	cfg := a.app.currentSettings().AgentReview
	ctx, cancel := context.WithTimeout(ctx, time.Duration(cfg.TimeoutSeconds)*time.Second)
	defer cancel()
	if a.app.ctx != nil {
		stop := context.AfterFunc(a.app.ctx, cancel) // Flint quits
		defer stop()
	}
	r, err := git.Open(ctx, req.Root)
	if err != nil {
		return ReviewResult{}, err
	}
	changes, contents, err := reviewChanges(ctx, r, req.Files)
	if err != nil {
		return ReviewResult{}, err
	}
	in := review.BuildInput(changes, cfg.MaxDiffKB<<10)
	if len(in.Files) == 0 {
		return ReviewResult{}, errors.New("nothing to review: " + strings.Join(in.Notes, " "))
	}
	answer, err := a.headless(ctx, name, r.Root, review.Prompt(), in.Text)
	if err != nil {
		return ReviewResult{}, err
	}
	sent := make(map[string][]string, len(in.Files))
	for _, p := range in.Files {
		sent[p] = contents[p]
	}
	parsed, err := review.Parse(answer, sent)
	if err != nil {
		return ReviewResult{}, fmt.Errorf("%s: %w", name, err)
	}
	notes := in.Notes
	if parsed.Partial {
		notes = append(notes, "The agent's answer was cut off; only its complete findings are shown.")
	}
	if parsed.Dropped > 0 {
		notes = append(notes, fmt.Sprintf("%d finding(s) left out: malformed, about files outside the review, or past the limit of %d.", parsed.Dropped, review.MaxFindings))
	}
	findings := parsed.Findings
	if findings == nil {
		findings = []review.Finding{}
	}
	return ReviewResult{Agent: name, Files: in.Files, Findings: findings, Notes: notes}, nil
}

// checkReviewRequest refuses what the Commit panel never sends: a repository
// that has nothing to do with the open folders, or paths that leave it.
func (a *AgentAPI) checkReviewRequest(req ReviewRequest) error {
	if !filepath.IsAbs(req.Root) {
		return errors.New("the repository path must be absolute")
	}
	root := filepath.Clean(req.Root)
	if _, err := a.app.rootOf(root); err != nil && !slices.ContainsFunc(a.app.projectRoots(), func(p string) bool {
		return strings.HasPrefix(p, root+string(filepath.Separator))
	}) {
		return errors.New("the repository is not part of an open folder")
	}
	switch {
	case len(req.Files) == 0:
		return errors.New("no changes to review")
	case len(req.Files) > maxReviewFiles:
		return fmt.Errorf("more than %d files to review; tick fewer in the Commit panel", maxReviewFiles)
	}
	for _, f := range req.Files {
		if !validReviewPath(f.Path) || (f.OrigPath != "" && !validReviewPath(f.OrigPath)) {
			return fmt.Errorf("invalid path %q", f.Path)
		}
		if !slices.Contains(reviewKinds, f.Kind) {
			return fmt.Errorf("unsupported change kind %q", f.Kind)
		}
	}
	return nil
}

func validReviewPath(p string) bool {
	return len(p) <= maxReviewPath && !strings.ContainsAny(p, "\x00\r\n") && filepath.IsLocal(filepath.FromSlash(p))
}

// reviewAgent is the agent from settings, else the default one.
func (a *AgentAPI) reviewAgent() (string, error) {
	name := a.app.currentSettings().AgentReview.Agent
	if name == "" {
		return a.preferred()
	}
	if !slices.Contains(a.Info().Available, name) {
		return "", fmt.Errorf("%s, the agent set for reviews in Settings ▸ Agents, is not installed", name)
	}
	return name, nil
}

// reviewChanges reads each file's diff against HEAD (or, untracked, its
// content) and the lines of its new version, which anchor the findings.
func reviewChanges(ctx context.Context, r *git.Repo, files []ReviewFile) ([]review.Change, map[string][]string, error) {
	changes := make([]review.Change, 0, len(files))
	contents := make(map[string][]string, len(files))
	for _, f := range files {
		c := review.Change{Path: f.Path, OrigPath: f.OrigPath, Kind: f.Kind}
		content, binary, ok := readNewVersion(r.Root, f.Path)
		switch {
		case f.Kind == "deleted":
			contents[f.Path] = []string{}
		case ok && !binary:
			contents[f.Path] = review.Lines(content)
		}
		if f.Kind == "untracked" {
			c.Content, c.Binary = content, binary || !ok
		} else if f.Kind != "deleted" {
			paths := []string{f.Path}
			if f.OrigPath != "" {
				paths = append(paths, f.OrigPath)
			}
			diff, err := r.WorkingDiff(ctx, paths...)
			if err != nil {
				return nil, nil, err
			}
			c.Diff = diff
		}
		changes = append(changes, c)
	}
	return changes, contents, nil
}

// readNewVersion reads the working-tree file rel of root: a regular file
// that resolves inside root, up to maxReviewFileBytes. A symlink is not
// followed (it could point at a secret outside the project).
func readNewVersion(root, rel string) (content string, binary, ok bool) {
	p := filepath.Join(root, filepath.FromSlash(rel))
	fi, err := os.Lstat(p)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() > maxReviewFileBytes {
		return "", false, false
	}
	realRoot, err1 := filepath.EvalSymlinks(root)
	real, err2 := filepath.EvalSymlinks(p)
	if err1 != nil || err2 != nil || !strings.HasPrefix(real, realRoot+string(filepath.Separator)) {
		return "", false, false
	}
	data, err := os.ReadFile(real)
	if err != nil {
		return "", false, false
	}
	head := data[:min(len(data), 8000)]
	return string(data), bytes.IndexByte(head, 0) >= 0, true
}
