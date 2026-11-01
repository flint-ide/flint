package git

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// ConflictSide names one side of a conflict for the merge editor.
type ConflictSide struct {
	// Label is short: a branch name or an abbreviated commit.
	Label string `json:"label"`
	// Detail says what the side is in this operation, in a sentence.
	Detail string `json:"detail"`
}

// ConflictSides says what "current" (ours, index stage 2) and "incoming" (theirs,
// stage 3) are. They depend on the operation: in a rebase, current is the branch
// being rebased onto and incoming is the user's own commit being replayed.
type ConflictSides struct {
	// Operation as in Status ("merging", "rebasing", "cherry-picking", "reverting");
	// "" when no operation is recorded, e.g. after git stash pop.
	Operation string       `json:"operation"`
	Current   ConflictSide `json:"current"`
	Incoming  ConflictSide `json:"incoming"`
}

// ConflictSides describes both sides of the conflicts of the operation in progress.
func (r *Repo) ConflictSides(ctx context.Context) (ConflictSides, error) {
	op := r.operation()
	head := r.headName(ctx)
	s := ConflictSides{Operation: op, Current: ConflictSide{Label: head, Detail: "HEAD, your branch"}}
	switch op {
	case "merging":
		in := r.mergeName(ctx)
		s.Incoming = ConflictSide{Label: in, Detail: fmt.Sprintf("MERGE_HEAD, the branch being merged into %s", head)}
	case "rebasing":
		onto, branch := r.rebaseNames(ctx)
		s.Current = ConflictSide{
			Label:  onto,
			Detail: fmt.Sprintf("HEAD: %s, the branch you are rebasing onto, with the commits replayed so far", onto),
		}
		detail := fmt.Sprintf("your commit from %s, being replayed onto %s", branch, onto)
		if commit := r.commitLine(ctx, "REBASE_HEAD"); commit != "" {
			detail = fmt.Sprintf("your commit %s from %s, being replayed onto %s", commit, branch, onto)
		}
		s.Incoming = ConflictSide{Label: branch, Detail: detail}
	case "cherry-picking":
		s.Incoming = ConflictSide{Label: r.shortRev(ctx, "CHERRY_PICK_HEAD"), Detail: "the commit being cherry-picked: " + r.commitLine(ctx, "CHERRY_PICK_HEAD")}
	case "reverting":
		s.Incoming = ConflictSide{Label: "revert " + r.shortRev(ctx, "REVERT_HEAD"), Detail: "the reverse of commit " + r.commitLine(ctx, "REVERT_HEAD")}
	default:
		s.Incoming = ConflictSide{Label: "applied changes", Detail: "the changes being applied to HEAD, for example by git stash pop or git apply -3"}
	}
	return s, nil
}

// headName is the checked-out branch, else "detached HEAD at <hash>".
func (r *Repo) headName(ctx context.Context) string {
	if out, err := r.git(ctx, "symbolic-ref", "--short", "-q", "HEAD"); err == nil && strings.TrimSpace(string(out)) != "" {
		return strings.TrimSpace(string(out))
	}
	return "detached HEAD at " + r.shortRev(ctx, "HEAD")
}

// mergeMsg matches the first line git writes to MERGE_MSG for one merged ref.
var mergeMsg = regexp.MustCompile(`^Merge (?:remote-tracking branch|branch|tag|commit) '([^']+)'`)

// mergeName is what is being merged: as the user named it (from MERGE_MSG), else a
// branch at MERGE_HEAD, else its abbreviated hash.
func (r *Repo) mergeName(ctx context.Context) string {
	if b, err := os.ReadFile(r.GitDir + "/MERGE_MSG"); err == nil {
		if m := mergeMsg.FindStringSubmatch(string(b)); m != nil {
			return m[1]
		}
	}
	return r.refName(ctx, "MERGE_HEAD")
}

// rebaseNames returns the branch being rebased onto and the branch being rebased,
// from the state git keeps in rebase-merge (or rebase-apply for the apply backend).
func (r *Repo) rebaseNames(ctx context.Context) (onto, branch string) {
	read := func(name string) string {
		for _, dir := range []string{"rebase-merge", "rebase-apply"} {
			if b, err := os.ReadFile(r.GitDir + "/" + dir + "/" + name); err == nil {
				return strings.TrimSpace(string(b))
			}
		}
		return ""
	}
	onto, branch = "upstream", "your branch"
	if sha := read("onto"); sha != "" {
		onto = r.refName(ctx, sha)
	}
	if head := read("head-name"); head != "" && head != "detached HEAD" {
		branch = strings.TrimPrefix(head, "refs/heads/")
	}
	return onto, branch
}

// refName is a branch pointing at rev (local branches first), else its short hash.
func (r *Repo) refName(ctx context.Context, rev string) string {
	sha, err := r.git(ctx, "rev-parse", "--verify", "-q", rev+"^{commit}")
	if err != nil {
		return rev
	}
	out, err := r.git(ctx, "for-each-ref", "--points-at="+strings.TrimSpace(string(sha)), "--format=%(refname:short)", "refs/heads", "refs/remotes")
	if err == nil {
		for _, name := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if name != "" && !strings.HasSuffix(name, "/HEAD") {
				return name
			}
		}
	}
	return r.shortRev(ctx, rev)
}

// shortRev abbreviates rev's hash, else returns rev unchanged.
func (r *Repo) shortRev(ctx context.Context, rev string) string {
	out, err := r.git(ctx, "rev-parse", "--short", "--verify", "-q", rev)
	if s := strings.TrimSpace(string(out)); err == nil && s != "" {
		return s
	}
	return rev
}

// commitLine is `<short hash> "<subject>"`, or "" when rev doesn't resolve.
func (r *Repo) commitLine(ctx context.Context, rev string) string {
	out, err := r.git(ctx, "log", "-1", "--no-show-signature", "--format=%h%x00%s", rev, "--")
	hash, subject, ok := strings.Cut(strings.TrimSpace(string(out)), "\x00")
	if err != nil || !ok {
		return ""
	}
	return fmt.Sprintf("%s “%s”", hash, subject)
}
