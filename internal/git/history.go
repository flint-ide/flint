package git

import (
	"context"
	"strconv"
	"strings"
)

// FileCommit is a commit in a file's history, with the file's name in that commit.
type FileCommit struct {
	Commit Commit `json:"commit"`
	// Path is the file's name after the commit; OrigPath its name before a rename.
	Path     string `json:"path"`
	OrigPath string `json:"origPath"`
	Kind     string `json:"kind"` // Added, Modified, Deleted, Renamed…
}

// FileHistory lists commits touching rel newest first, following renames (`git log
// --follow`), so each entry knows what the file was called at that point.
func (r *Repo) FileHistory(ctx context.Context, rel string, limit int) ([]FileCommit, error) {
	if limit <= 0 {
		limit = 500
	}
	out, err := r.git(ctx, "log", "--follow", "-z", "--name-status", "-M",
		"--format=%x1e%H%x1f%P%x1f%an%x1f%ae%x1f%at%x1f%D%x1f%s",
		"-n", strconv.Itoa(limit), "--", rel)
	if err != nil {
		if strings.Contains(err.Error(), "does not have any commits") {
			return []FileCommit{}, nil
		}
		return nil, err
	}
	return parseFileHistory(string(out), rel), nil
}

// parseFileHistory reads records "\x1e<format>\x00\n<status>\x00<path>[\x00<path>]\x00".
// Merge commits come without a status, keeping the name of the newer entry.
func parseFileHistory(out, rel string) []FileCommit {
	commits := []FileCommit{}
	path := rel
	for _, rec := range strings.Split(out, "\x1e") {
		if rec == "" {
			continue
		}
		fields := strings.Split(rec, "\x00")
		f := strings.Split(fields[0], "\x1f")
		if len(f) < 7 {
			continue
		}
		c := Commit{Hash: f[0], Author: f[2], Email: f[3], Refs: f[5], Subject: f[6], Parents: strings.Fields(f[1])}
		c.Time, _ = strconv.ParseInt(f[4], 10, 64)
		fc := FileCommit{Commit: c, Path: path, Kind: Modified}
		if len(fields) >= 3 {
			code := strings.TrimSpace(fields[1])
			if code != "" {
				fc.Kind = changeKind(code[0])
				fc.Path = fields[2]
				if (code[0] == 'R' || code[0] == 'C') && len(fields) >= 4 {
					fc.OrigPath, fc.Path = fields[2], fields[3]
				}
			}
		}
		commits = append(commits, fc)
		// Older commits know the file by its name before the rename.
		path = fc.Path
		if fc.OrigPath != "" {
			path = fc.OrigPath
		}
	}
	return commits
}

// changeKind maps a --name-status letter to a change kind.
func changeKind(code byte) string {
	switch code {
	case 'A':
		return Added
	case 'D':
		return Deleted
	case 'R':
		return Renamed
	case 'C':
		return Copied
	case 'T':
		return TypeChange
	}
	return Modified
}
