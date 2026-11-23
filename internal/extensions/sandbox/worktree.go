package sandbox

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// What a script step may not change in its worktree. A script step runs a
// run configuration on code an agent wrote, and later steps of the same run
// start agents in that worktree: an agent CLI there runs the hooks of
// .claude/settings.json, reads CLAUDE.md and AGENTS.md as standing
// instructions, loads .codex's configuration, and git follows the
// worktree's .git file to its configuration and hooks. A test that writes
// one of those plants what the next agent (or the reviewer) runs.
//
// Two lines guard them. On macOS the sandbox profile denies writes to them
// (Policy.DenyWrite, after the worktree's write allowance). Landlock cannot
// deny inside an allowed folder, so on every system, sandboxed or not, Flint
// takes a fingerprint of them before the script and compares it after
// (TakeFingerprint, Fingerprint.Changed): a change fails the step.

// WorktreeProtected are the names, at the top of a worktree, a script step
// may not change. Matched in any letter case: macOS file systems ignore it,
// so .CLAUDE is .claude there.
var WorktreeProtected = []string{
	".git", ".claude", ".codex", ".vscode", ".idea", ".flint", ".mcp.json",
	"CLAUDE.md", "CLAUDE.local.md", "AGENTS.md", "AGENTS.override.md",
}

// worktreeDenyWrite are the paths of WorktreeProtected in worktree, for
// Policy.DenyWrite.
func worktreeDenyWrite(worktree string) []string {
	out := make([]string, len(WorktreeProtected))
	for i, name := range WorktreeProtected {
		out[i] = path.Join(worktree, name)
	}
	return out
}

// Bounds of a fingerprint. Past maxHashBytes for one file, or
// maxHashTotal for all of them, a file is known by its size and its
// inode's change time (which a process cannot set back); past
// maxFingerprintEntries the fingerprint cannot be taken at all.
const (
	maxFingerprintEntries = 4096
	maxHashBytes          = 1 << 20
	maxHashTotal          = 16 << 20
)

// Fingerprint is what WorktreeProtected holds in one worktree: an entry
// per file, folder and link, by path relative to the worktree.
type Fingerprint map[string]string

// TakeFingerprint reads what root holds at the top under the names of
// WorktreeProtected (any letter case): for each file, folder and link
// below them (links are not followed), its kind, permissions and, for a
// file, its content's hash (or size and change time past the bounds), for
// a link its target. A tree of more than maxFingerprintEntries entries is
// an error: it cannot be checked.
func TakeFingerprint(root string) (Fingerprint, error) {
	names, err := DirNames(root)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", root, err)
	}
	fp := Fingerprint{}
	budget := maxHashTotal
	for _, name := range names {
		if !protectedName(name) {
			continue
		}
		err := filepath.WalkDir(filepath.Join(root, name), func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if len(fp) >= maxFingerprintEntries {
				return fmt.Errorf("more than %d entries", maxFingerprintEntries)
			}
			rel, _ := filepath.Rel(root, p)
			entry, err := fingerprintEntry(p, d, &budget)
			if err != nil {
				return err
			}
			fp[filepath.ToSlash(rel)] = entry
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("cannot check %s: %w", name, err)
		}
	}
	return fp, nil
}

// protectedName reports whether a top-level name is one of
// WorktreeProtected, in any letter case.
func protectedName(name string) bool {
	return slices.ContainsFunc(WorktreeProtected, func(p string) bool { return strings.EqualFold(p, name) })
}

// fingerprintEntry describes one entry; budget is what is left of
// maxHashTotal.
func fingerprintEntry(p string, d fs.DirEntry, budget *int) (string, error) {
	info, err := d.Info()
	if err != nil {
		return "", err
	}
	mode := info.Mode()
	switch {
	case mode&fs.ModeSymlink != 0:
		target, err := os.Readlink(p)
		if err != nil {
			return "", err
		}
		return "link " + target, nil
	case mode.IsDir():
		return fmt.Sprintf("dir %o", mode.Perm()), nil
	case !mode.IsRegular():
		return fmt.Sprintf("other %s", mode.Type()), nil
	}
	size := info.Size()
	if size > maxHashBytes || size > int64(*budget) {
		return fmt.Sprintf("file %o %d changed %d", mode.Perm(), size, changeTime(info)), nil
	}
	sum, err := hashFile(p, size)
	if err != nil {
		return "", err
	}
	*budget -= int(size)
	return fmt.Sprintf("file %o %s", mode.Perm(), sum), nil
}

// hashFile hashes at most size+1 bytes of a regular file (a file that grew
// while it was read hashes differently, which is a change).
func hashFile(p string, size int64) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, io.LimitReader(f, size+1)); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Changed lists, sorted, the paths whose entry differs between before and
// after: added, removed or changed.
func (before Fingerprint) Changed(after Fingerprint) []string {
	var out []string
	for p, e := range before {
		if a, ok := after[p]; !ok || a != e {
			out = append(out, p)
		}
	}
	for p := range after {
		if _, ok := before[p]; !ok {
			out = append(out, p)
		}
	}
	slices.Sort(out)
	return out
}

// errNoFingerprint is ChangedSince's answer when the fingerprint after the
// script cannot be taken.
var errNoFingerprint = errors.New("cannot be checked")

// ChangedSince takes root's fingerprint again and lists what changed since
// before; when it cannot be taken, that is reported as a change of root's
// protected places.
func ChangedSince(root string, before Fingerprint) []string {
	after, err := TakeFingerprint(root)
	if err != nil {
		return []string{fmt.Sprintf("%s (%v: %v)", strings.Join(WorktreeProtected, ", "), errNoFingerprint, err)}
	}
	return before.Changed(after)
}
