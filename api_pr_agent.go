package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"time"

	"flint/internal/git"
	"flint/internal/github"
	"flint/internal/prfeedback"
	"flint/internal/redact"
	"flint/internal/review"
	"flint/internal/workspace"
)

// prDraftTimeout bounds the local work on a draft (git, reading files,
// redaction); the GitHub read has its own timeout.
const prDraftTimeout = 30 * time.Second

// threadIDRE: a GraphQL node id, as the web view sends it back.
var threadIDRE = regexp.MustCompile(`^[A-Za-z0-9_=-]{1,200}$`)

// oidRE: a commit id GitHub names, before it reaches git.
var oidRE = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

// PRReviewDraft is the agent chat draft for a pull request's review feedback
// and what the UI says about it.
type PRReviewDraft struct {
	// Prompt is the draft: reviewers' text quoted, redacted and bounded.
	Prompt string `json:"prompt"`
	Number int    `json:"number"`
	// Threads and Reviews are how many the draft quotes; LeftOut how many
	// unresolved threads did not fit.
	Threads int `json:"threads"`
	Reviews int `json:"reviews"`
	LeftOut int `json:"leftOut"`
	// HeadBranch is the branch Check out puts the pull request on; Branch the
	// one checked out in dir ("" when detached).
	HeadBranch string `json:"headBranch"`
	Branch     string `json:"branch"`
	// OnBranch: dir has the pull request's branch checked out. When false the
	// draft tells the agent not to edit files there.
	OnBranch bool `json:"onBranch"`
	// Open: the pull request is open (Check out makes sense).
	Open bool `json:"open"`
}

// ReviewDraft builds the agent chat draft for the review feedback of pull
// request number: one thread (threadID), or with "" all unresolved,
// non-outdated threads and the reviews that request changes. dir is a folder
// of the workspace or an agent task's worktree, where the agent would work.
// The pull request comes from Go's cache when it was loaded in the last
// seconds; nothing is written to GitHub and nothing is sent to an agent.
func (g *GitHubAPI) ReviewDraft(dir string, number int, threadID string) (PRReviewDraft, error) {
	if err := g.checkCIDir(dir); err != nil {
		return PRReviewDraft{}, err
	}
	if threadID != "" && !threadIDRE.MatchString(threadID) {
		return PRReviewDraft{}, errors.New("invalid review thread id")
	}
	d, patches, err := g.gh.DetailsWithPatches(g.app.ctx, dir, number, false)
	if err != nil {
		return PRReviewDraft{}, err
	}
	if threadID != "" && !hasThread(d, threadID) { // posted after the cached read
		if d, patches, err = g.gh.DetailsWithPatches(g.app.ctx, dir, number, true); err != nil {
			return PRReviewDraft{}, err
		}
	}
	ctx, cancel := context.WithTimeout(g.app.ctx, prDraftTimeout)
	defer cancel()
	in := prfeedback.Input{PR: d, Patches: patches, ThreadID: threadID}
	if r, err := git.Open(ctx, dir); err == nil {
		branch, oid := r.Head(ctx)
		in.Branch = branch
		in.OnBranch = prCheckedOut(ctx, r, d, branch, oid)
		in.ReadFile = g.workingTreeReader(r.Root, dir)
	}
	cfg := g.app.currentSettings().PRReview
	draft, err := prfeedback.Build(in, prfeedback.Options{MaxThreads: cfg.MaxThreads, ContextLines: cfg.ContextLines})
	if err != nil {
		if errors.Is(err, prfeedback.ErrNothing) {
			return PRReviewDraft{}, fmt.Errorf("%w on #%d", err, number)
		}
		return PRReviewDraft{}, err
	}
	return PRReviewDraft{
		Prompt: draft.Prompt, Number: d.Number, Threads: draft.Threads, Reviews: draft.Reviews, LeftOut: draft.LeftOut,
		HeadBranch: oneLine(d.LocalBranch()), Branch: oneLine(in.Branch), OnBranch: in.OnBranch, Open: d.State == "OPEN",
	}, nil
}

func hasThread(d github.Details, id string) bool {
	for _, t := range d.Threads {
		if t.ID == id {
			return true
		}
	}
	return false
}

// prCheckedOut reports whether the work tree has the pull request's code:
// HEAD is its head commit, or the branch is the one Check out names. A fork's
// branch under its own name (the fork author's clone) counts only when it
// contains the head commit: another repository's "main" is not this one's.
func prCheckedOut(ctx context.Context, r *git.Repo, d github.Details, branch, oid string) bool {
	switch {
	case oid != "" && oid == d.HeadOID:
		return true
	case branch == "":
		return false
	case branch == d.LocalBranch() && (!d.CrossRepository || d.HeadOwner != ""):
		return true
	}
	return d.CrossRepository && branch == d.HeadRef && oidRE.MatchString(d.HeadOID) && r.IsAncestor(ctx, d.HeadOID, "HEAD")
}

// workingTreeReader reads a file of the repository at root for the draft's
// code excerpts: only a regular file inside an open folder (or inside dir,
// an agent task's worktree), never a credential file, a symlink, a binary
// or a file larger than maxReviewFileBytes.
func (g *GitHubAPI) workingTreeReader(root, dir string) func(string) ([]string, bool) {
	roots := g.app.projectRoots()
	if g.app.taskByFolder(dir) != "" {
		roots = append(roots, dir)
	}
	return func(rel string) ([]string, bool) {
		if !validReviewPath(rel) || redact.SecretFile(rel) {
			return nil, false
		}
		abs := filepath.Join(root, filepath.FromSlash(rel))
		folder, ok := workspace.RootOf(roots, abs)
		if !ok {
			return nil, false
		}
		// A symlinked folder may not lead out of the open folder.
		realFolder, err1 := filepath.EvalSymlinks(folder)
		real, err2 := filepath.EvalSymlinks(abs)
		if err1 != nil || err2 != nil || !workspace.Within(realFolder, real) {
			return nil, false
		}
		content, binary, ok := readNewVersion(root, rel, maxReviewFileBytes+1)
		if !ok || binary || len(content) > maxReviewFileBytes {
			return nil, false
		}
		return review.Lines(content), true
	}
}
