package agentsession

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

// Additional folders: a chat that works in more than its cwd (a joint agent
// task works in the worktrees of several repositories). Flint decides them,
// never the UI or the agent: they travel in the context of Start and Resume
// (WithFolders), so the Driver interface stays as it is, and a driver that
// cannot give its agent more folders refuses a context that carries some
// (RefuseFolders) instead of silently starting in cwd alone.

// MaxFolders bounds the additional folders of one chat.
const MaxFolders = 16

type foldersKey struct{}

// WithFolders returns ctx carrying folders, the directories a chat started or
// resumed with ctx may read and write besides its cwd. No folders: ctx as is.
func WithFolders(ctx context.Context, folders []string) context.Context {
	if len(folders) == 0 {
		return ctx
	}
	return context.WithValue(ctx, foldersKey{}, slices.Clone(folders))
}

// Folders returns the additional folders WithFolders stored in ctx (nil: none).
func Folders(ctx context.Context) []string {
	f, _ := ctx.Value(foldersKey{}).([]string)
	return slices.Clone(f)
}

// ErrNoFolders is wrapped by RefuseFolders.
var ErrNoFolders = errors.New("cannot work in several folders at once")

// RefuseFolders is the check of a driver that cannot give its agent
// additional folders: an error when ctx carries some.
func RefuseFolders(ctx context.Context, agent string) error {
	if len(Folders(ctx)) > 0 {
		return fmt.Errorf("%s %w", agent, ErrNoFolders)
	}
	return nil
}

// CheckFolders validates the additional folders of a chat in cwd: at most
// MaxFolders, each an absolute, clean path of an existing directory, none
// twice and none equal to cwd. untrusted (nil: everything is trusted) must
// trust cwd and every folder: a chat's settings come from its cwd, and a
// trusted session must not write into a Restricted Mode folder (nor an
// untrusted one load nothing but write into trusted folders).
func CheckFolders(cwd string, folders []string, untrusted func(string) bool) error {
	if len(folders) == 0 {
		return nil
	}
	if len(folders) > MaxFolders {
		return fmt.Errorf("too many additional folders (max %d)", MaxFolders)
	}
	if untrusted != nil && untrusted(cwd) {
		return fmt.Errorf("%s is not trusted (Restricted Mode): a chat there gets no additional folders", cwd)
	}
	seen := map[string]bool{filepath.Clean(cwd): true}
	for _, f := range folders {
		if !filepath.IsAbs(f) || filepath.Clean(f) != f {
			return fmt.Errorf("additional folder %q is not a clean absolute path", f)
		}
		if seen[f] {
			return fmt.Errorf("additional folder %s is named twice (or is the chat's own folder)", f)
		}
		seen[f] = true
		if info, err := os.Stat(f); err != nil || !info.IsDir() {
			return fmt.Errorf("additional folder %s is not a directory", f)
		}
		if untrusted != nil && untrusted(f) {
			return fmt.Errorf("additional folder %s is not trusted (Restricted Mode)", f)
		}
	}
	return nil
}
