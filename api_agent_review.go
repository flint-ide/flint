package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"flint/internal/git"
	"flint/internal/review"
	"flint/internal/settings"
)

// Limits of a review request. They protect Flint (the WebView sends the file
// list), not preferences: the diff size and the timeout are settings.
const (
	maxReviewFiles     = 1000
	maxReviewPath      = 1024
	maxReviewFileBytes = 2 << 20 // larger new versions keep their findings where the agent put them
)

// ReviewFile is one change to review, as the Commit panel lists it. Go takes
// only the path from it: the kind and a rename's source come from git status.
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

// ReviewChanges asks an agent (settings: agentReview.agent, else the default
// one) to review req.Files against HEAD, as the Commit panel shows them, and
// returns its findings on lines of the new versions. Only files git status
// reports as changed in a repository of an open folder are reviewed. The run
// is headless like commit messages: Claude gets only Read, Grep and Glob and
// may not read credential files, Codex a read-only sandbox (which can still
// read any file the user can), a custom agent a session in which Flint
// declines every permission request and refuses writes. In Restricted Mode
// Claude loads only the user's settings and a custom agent refuses to start.
// Secrets are redacted in the input and in the findings, and files that hold
// credentials are not sent. Cancelling the call stops the agent and what it
// started.
func (a *AgentAPI) ReviewChanges(ctx context.Context, req ReviewRequest) (ReviewResult, error) {
	res, err := a.review(ctx, req)
	if err != nil && errors.Is(ctx.Err(), context.Canceled) {
		return ReviewResult{}, ctx.Err() // whichever step was running when it was cancelled
	}
	return res, err
}

func (a *AgentAPI) review(ctx context.Context, req ReviewRequest) (ReviewResult, error) {
	if err := checkReviewFiles(req.Files); err != nil {
		return ReviewResult{}, err
	}
	name, err := a.reviewAgent()
	if err != nil {
		return ReviewResult{}, err
	}
	s := a.app.currentSettings()
	cfg := s.AgentReview
	ctx, cancel := context.WithTimeout(ctx, time.Duration(cfg.TimeoutSeconds)*time.Second)
	defer cancel()
	if a.app.ctx != nil {
		stop := context.AfterFunc(a.app.ctx, cancel) // Flint quits
		defer stop()
	}
	r, err := a.reviewRepo(ctx, req.Root)
	if err != nil {
		return ReviewResult{}, err
	}
	files, err := changedFiles(ctx, r, req.Files)
	if err != nil {
		return ReviewResult{}, err
	}
	changes, err := reviewChanges(ctx, r, files)
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
	deleted := map[string]bool{}
	for _, f := range files {
		deleted[f.Path] = f.Kind == "deleted"
	}
	sent := make(map[string][]string, len(in.Files))
	for _, p := range in.Files {
		sent[p] = nil // lines are read below, only for files with findings
		if deleted[p] {
			sent[p] = []string{}
		}
	}
	parsed, err := review.Parse(answer, sent)
	if err != nil {
		return ReviewResult{}, fmt.Errorf("%s: %w", name, err)
	}
	notes := in.Notes
	if c, ok := s.CustomAgent(name); ok {
		if flags := settings.AutoApproveArgs(c.Args); len(flags) > 0 {
			notes = append(notes, fmt.Sprintf("%s was started with %s: it runs its own tools without asking, so this review was not read-only. Remove the flag in Settings ▸ Agents to keep reviews read-only.", name, strings.Join(flags, " ")))
		}
	}
	if parsed.Partial {
		notes = append(notes, "The agent's answer was cut off; only its complete findings are shown.")
	}
	if parsed.Dropped > 0 {
		notes = append(notes, fmt.Sprintf("%d finding(s) left out: malformed, about files outside the review, or past the limit of %d.", parsed.Dropped, review.MaxFindings))
	}
	findings := placeFindings(r.Root, parsed.Findings, deleted)
	return ReviewResult{Agent: name, Files: in.Files, Findings: findings, Notes: notes}, nil
}

// checkReviewFiles refuses lists the Commit panel never sends.
func checkReviewFiles(files []ReviewFile) error {
	switch {
	case len(files) == 0:
		return errors.New("no changes to review")
	case len(files) > maxReviewFiles:
		return fmt.Errorf("more than %d files to review; tick fewer in the Commit panel", maxReviewFiles)
	}
	for _, f := range files {
		if !validReviewPath(f.Path) {
			return fmt.Errorf("invalid path %q", f.Path)
		}
	}
	return nil
}

func validReviewPath(p string) bool {
	return len(p) <= maxReviewPath && !strings.ContainsAny(p, "\x00\r\n") && filepath.IsLocal(filepath.FromSlash(p))
}

// reviewRepo opens the repository at root, which must be an open folder or
// the work tree an open folder belongs to (the Commit panel's repository).
func (a *AgentAPI) reviewRepo(ctx context.Context, root string) (*git.Repo, error) {
	if !filepath.IsAbs(root) {
		return nil, errors.New("the repository path must be absolute")
	}
	root = filepath.Clean(root)
	for _, p := range a.app.projectRoots() {
		if p != root && !strings.HasPrefix(p, root+string(filepath.Separator)) {
			continue
		}
		r, err := git.Open(ctx, p)
		if err == nil && filepath.Clean(r.Root) == root {
			return r, nil
		}
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return nil, errors.New("the repository is not an open folder or the repository of one")
}

// changedFiles keeps the requested paths that git status reports as changed
// (and not conflicted), with git's kind and rename source.
func changedFiles(ctx context.Context, r *git.Repo, requested []ReviewFile) ([]ReviewFile, error) {
	st, err := r.Status(ctx)
	if err != nil {
		return nil, err
	}
	changed := make(map[string]git.FileChange, len(st.Files))
	for _, f := range st.Files {
		if f.Kind != git.Conflicted {
			changed[f.Path] = f
		}
	}
	var out []ReviewFile
	for _, f := range requested {
		c, ok := changed[f.Path]
		if !ok {
			continue
		}
		delete(changed, f.Path) // once each
		out = append(out, ReviewFile{Path: c.Path, OrigPath: c.OrigPath, Kind: c.Kind})
	}
	if len(out) == 0 {
		return nil, errors.New("none of these files has changes to review")
	}
	return out, nil
}

// reviewChanges collects each file's diff against HEAD in batched git calls;
// an untracked file is read later, only as far as its share of the limit.
func reviewChanges(ctx context.Context, r *git.Repo, files []ReviewFile) ([]review.Change, error) {
	var groups [][]string
	for _, f := range files {
		if f.Kind == "untracked" || f.Kind == "deleted" {
			continue
		}
		g := []string{f.Path}
		if f.OrigPath != "" {
			g = append(g, f.OrigPath)
		}
		groups = append(groups, g)
	}
	diffs, err := r.WorkingDiffs(ctx, groups)
	if err != nil {
		return nil, err
	}
	changes := make([]review.Change, 0, len(files))
	for _, f := range files {
		c := review.Change{Path: f.Path, OrigPath: f.OrigPath, Kind: f.Kind, Diff: diffs[f.Path]}
		if f.Kind == "untracked" {
			if fi, err := os.Lstat(filepath.Join(r.Root, filepath.FromSlash(f.Path))); err == nil && fi.Mode().IsRegular() {
				c.Size = int(min(fi.Size(), 1<<40))
			}
			c.Read = func(limit int) (string, bool, bool) { return readNewVersion(r.Root, f.Path, limit) }
		}
		changes = append(changes, c)
	}
	return changes, nil
}

// placeFindings checks the findings' lines against their files' new versions
// and keeps each line's text for re-anchoring. Only files with findings are
// read, up to maxReviewFileBytes; findings in larger or unreadable files keep
// the agent's lines.
func placeFindings(root string, findings []review.Finding, deleted map[string]bool) []review.Finding {
	out := make([]review.Finding, 0, len(findings))
	lines := map[string][]string{}
	for _, f := range findings {
		if f.Line > 0 && !deleted[f.Path] {
			l, read := lines[f.Path]
			if !read {
				if content, binary, ok := readNewVersion(root, f.Path, maxReviewFileBytes+1); ok && !binary && len(content) <= maxReviewFileBytes {
					l = review.Lines(content)
					if l == nil {
						l = []string{}
					}
				}
				lines[f.Path] = l
			}
			f = review.Place(f, l)
		}
		out = append(out, f)
	}
	return out
}

// readNewVersion reads up to limit bytes of the working-tree file rel of
// root: a regular file that resolves inside root. A symlink is not followed
// (it could point at a secret outside the project). binary is set when the
// bytes read look binary.
func readNewVersion(root, rel string, limit int) (content string, binary, ok bool) {
	p := filepath.Join(root, filepath.FromSlash(rel))
	fi, err := os.Lstat(p)
	if err != nil || !fi.Mode().IsRegular() {
		return "", false, false
	}
	realRoot, err1 := filepath.EvalSymlinks(root)
	real, err2 := filepath.EvalSymlinks(p)
	if err1 != nil || err2 != nil || !strings.HasPrefix(real, realRoot+string(filepath.Separator)) {
		return "", false, false
	}
	f, err := os.Open(real)
	if err != nil {
		return "", false, false
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, int64(max(0, limit))))
	if err != nil {
		return "", false, false
	}
	head := data[:min(len(data), 8000)]
	return string(data), bytes.IndexByte(head, 0) >= 0, true
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
