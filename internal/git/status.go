package git

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// Change kinds shown in the Commit view.
const (
	Modified   = "modified"
	Added      = "added"
	Deleted    = "deleted"
	Renamed    = "renamed"
	Copied     = "copied"
	TypeChange = "typechange"
	Untracked  = "untracked"
	Conflicted = "conflicted"
)

// FileChange is one entry of `git status`.
type FileChange struct {
	Path     string `json:"path"`     // relative to the repo root
	OrigPath string `json:"origPath"` // source path of a rename/copy
	Kind     string `json:"kind"`
	Index    string `json:"index"`    // X column: staged state ("." when clean)
	Worktree string `json:"worktree"` // Y column: unstaged state
}

// Status summarises the work tree.
type Status struct {
	Branch   string       `json:"branch"`   // "" when detached
	Head     string       `json:"head"`     // commit hash, "" before the first commit
	Upstream string       `json:"upstream"` // e.g. "origin/main"
	Ahead    int          `json:"ahead"`
	Behind   int          `json:"behind"`
	Files    []FileChange `json:"files"`
	// Operation is "merging", "rebasing", "cherry-picking" or "" — shown like IntelliJ's banner.
	Operation string `json:"operation"`
}

// Status runs `git status --porcelain=v2`.
func (r *Repo) Status(ctx context.Context) (Status, error) {
	out, err := r.git(ctx, "status", "--porcelain=v2", "--branch", "-z", "--untracked-files=all")
	if err != nil {
		return Status{}, err
	}
	st, err := parseStatus(out)
	if err != nil {
		return Status{}, err
	}
	st.Operation = r.operation()
	return st, nil
}

func parseStatus(out []byte) (Status, error) {
	st := Status{Files: []FileChange{}}
	fields := splitNUL(out)
	for i := 0; i < len(fields); i++ {
		line := fields[i]
		switch {
		case strings.HasPrefix(line, "# "):
			parseHeader(&st, line[2:])
		case strings.HasPrefix(line, "1 "):
			parts := strings.SplitN(line, " ", 9)
			if len(parts) < 9 {
				return st, fmt.Errorf("malformed status line %q", line)
			}
			st.Files = append(st.Files, newChange(parts[1], parts[8], ""))
		case strings.HasPrefix(line, "2 "):
			parts := strings.SplitN(line, " ", 10)
			if len(parts) < 10 || i+1 >= len(fields) {
				return st, fmt.Errorf("malformed rename line %q", line)
			}
			i++ // -z puts the original path in the next field
			st.Files = append(st.Files, newChange(parts[1], parts[9], fields[i]))
		case strings.HasPrefix(line, "u "):
			parts := strings.SplitN(line, " ", 11)
			if len(parts) < 11 {
				return st, fmt.Errorf("malformed conflict line %q", line)
			}
			fc := newChange(parts[1], parts[10], "")
			fc.Kind = Conflicted
			st.Files = append(st.Files, fc)
		case strings.HasPrefix(line, "? "):
			st.Files = append(st.Files, FileChange{Path: line[2:], Kind: Untracked, Index: "?", Worktree: "?"})
		}
	}
	return st, nil
}

func parseHeader(st *Status, h string) {
	key, val, _ := strings.Cut(h, " ")
	switch key {
	case "branch.oid":
		if val != "(initial)" {
			st.Head = val
		}
	case "branch.head":
		if val != "(detached)" {
			st.Branch = val
		}
	case "branch.upstream":
		st.Upstream = val
	case "branch.ab":
		for _, f := range strings.Fields(val) {
			n, _ := strconv.Atoi(f[1:])
			if f[0] == '+' {
				st.Ahead = n
			} else {
				st.Behind = n
			}
		}
	}
}

func newChange(xy, path, orig string) FileChange {
	x, y := string(xy[0]), string(xy[1])
	return FileChange{Path: path, OrigPath: orig, Kind: kindOf(x, y), Index: x, Worktree: y}
}

// kindOf picks the most relevant state: the index for renames/additions, else the worktree.
func kindOf(x, y string) string {
	switch {
	case x == "R":
		return Renamed
	case x == "C":
		return Copied
	case x == "A":
		return Added
	case x == "D" || y == "D":
		return Deleted
	case x == "T" || y == "T":
		return TypeChange
	}
	return Modified
}

// operation detects an in-progress merge/rebase/cherry-pick from marker files.
func (r *Repo) operation() string {
	markers := []struct{ file, name string }{
		{"rebase-merge", "rebasing"},
		{"rebase-apply", "rebasing"},
		{"MERGE_HEAD", "merging"},
		{"CHERRY_PICK_HEAD", "cherry-picking"},
		{"REVERT_HEAD", "reverting"},
	}
	for _, m := range markers {
		if exists(r.GitDir + "/" + m.file) {
			return m.name
		}
	}
	return ""
}
