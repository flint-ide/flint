package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// emptyTree is git's well-known hash of the empty tree, used as the base before the
// first commit.
const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

const maxUntrackedPreview = 4 << 10

// ChangesDiff renders the changes of tracked paths against HEAD plus a preview of
// untracked files, capped at maxBytes — context for an agent writing a commit message.
func (r *Repo) ChangesDiff(ctx context.Context, tracked, untracked []string, maxBytes int) (string, error) {
	var b strings.Builder
	if len(tracked) > 0 {
		base := "HEAD"
		if _, err := r.git(ctx, "rev-parse", "--verify", "--quiet", "HEAD"); err != nil {
			base = emptyTree
		}
		args := append([]string{"diff", base, "--no-color", "--no-ext-diff", "--find-renames", "--"}, tracked...)
		out, err := r.git(ctx, args...)
		if err != nil {
			return "", err
		}
		b.Write(out)
	}
	for _, p := range untracked {
		if b.Len() >= maxBytes {
			break
		}
		data, err := os.ReadFile(filepath.Join(r.Root, filepath.FromSlash(p)))
		if err != nil {
			continue
		}
		fmt.Fprintf(&b, "\nnew file: %s\n", p)
		if isBinary(data) {
			b.WriteString("(binary)\n")
			continue
		}
		if len(data) > maxUntrackedPreview {
			data = append(data[:maxUntrackedPreview], []byte("\n…(truncated)")...)
		}
		b.Write(data)
		b.WriteByte('\n')
	}
	out := b.String()
	if len(out) > maxBytes {
		out = out[:maxBytes] + "\n…(diff truncated)"
	}
	return out, nil
}

// RecentSubjects returns the last n commit subjects (style reference for agents).
func (r *Repo) RecentSubjects(ctx context.Context, n int) []string {
	commits, err := r.Log(ctx, LogOptions{Limit: n})
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(commits))
	for _, c := range commits {
		out = append(out, c.Subject)
	}
	return out
}
