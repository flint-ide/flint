package git

import (
	"context"
	"strconv"
	"strings"
)

// Commit is one log entry.
type Commit struct {
	Hash    string   `json:"hash"`
	Parents []string `json:"parents"`
	Author  string   `json:"author"`
	Email   string   `json:"email"`
	Time    int64    `json:"time"` // author date, unix seconds
	Refs    string   `json:"refs"` // decorations, e.g. "HEAD -> main, origin/main"
	Subject string   `json:"subject"`
}

// LogOptions select which commits to list.
type LogOptions struct {
	Limit int    `json:"limit"`
	Skip  int    `json:"skip"`
	All   bool   `json:"all"`  // all branches, like IntelliJ's log default
	Path  string `json:"path"` // limit to a file or directory (relative)
}

// Log lists commits newest first.
func (r *Repo) Log(ctx context.Context, opts LogOptions) ([]Commit, error) {
	if opts.Limit <= 0 {
		opts.Limit = 200
	}
	args := []string{"log", "--date-order", "-z",
		"--format=%H%x1f%P%x1f%an%x1f%ae%x1f%at%x1f%D%x1f%s",
		"-n", strconv.Itoa(opts.Limit), "--skip", strconv.Itoa(opts.Skip)}
	if opts.All {
		args = append(args, "--all")
	}
	if opts.Path != "" {
		args = append(args, "--", opts.Path)
	}
	out, err := r.git(ctx, args...)
	if err != nil {
		if strings.Contains(err.Error(), "does not have any commits") {
			return []Commit{}, nil
		}
		return nil, err
	}
	commits := []Commit{}
	for _, rec := range splitNUL(out) {
		f := strings.Split(rec, "\x1f")
		if len(f) < 7 {
			continue
		}
		c := Commit{Hash: f[0], Author: f[2], Email: f[3], Refs: f[5], Subject: f[6], Parents: strings.Fields(f[1])}
		c.Time, _ = strconv.ParseInt(f[4], 10, 64)
		commits = append(commits, c)
	}
	return commits, nil
}

// CommitFiles lists files changed by a commit (against its first parent).
func (r *Repo) CommitFiles(ctx context.Context, hash string) ([]FileChange, error) {
	out, err := r.git(ctx, "diff-tree", "--root", "--no-commit-id", "-r", "-M", "-z", "--name-status", "--diff-merges=first-parent", hash)
	if err != nil {
		return nil, err
	}
	files := []FileChange{}
	fields := splitNUL(out)
	for i := 0; i < len(fields); i++ {
		code := fields[i]
		if code == "" || i+1 >= len(fields) {
			continue
		}
		fc := FileChange{Index: code[:1], Worktree: "."}
		switch code[0] {
		case 'R', 'C':
			if i+2 >= len(fields) {
				return files, nil
			}
			fc.OrigPath, fc.Path = fields[i+1], fields[i+2]
			fc.Kind = Renamed
			if code[0] == 'C' {
				fc.Kind = Copied
			}
			i += 2
		default:
			fc.Path = fields[i+1]
			fc.Kind = map[byte]string{'A': Added, 'D': Deleted, 'T': TypeChange}[code[0]]
			if fc.Kind == "" {
				fc.Kind = Modified
			}
			i++
		}
		files = append(files, fc)
	}
	return files, nil
}
