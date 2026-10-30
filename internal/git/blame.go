package git

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
)

// uncommittedHash is what blame reports for lines that differ from HEAD.
const uncommittedHash = "0000000000000000000000000000000000000000"

// BlameCommit describes a commit that last touched some lines.
type BlameCommit struct {
	Hash    string `json:"hash"`
	Author  string `json:"author"`
	Email   string `json:"email"`
	Time    int64  `json:"time"` // author date, unix seconds
	Summary string `json:"summary"`
	// Path is the file's name in that commit (blame follows renames).
	Path string `json:"path"`
	// Previous and PrevPath locate the file before this commit ("" for a root commit),
	// i.e. the left side of the commit's change.
	Previous string `json:"previous"`
	PrevPath string `json:"prevPath"`
	// Uncommitted marks lines changed in the working tree (or the editor buffer).
	Uncommitted bool `json:"uncommitted"`
}

// BlameRange attributes Count lines starting at Line (1-based) to Commits[Commit].
type BlameRange struct {
	Line   int `json:"line"`
	Count  int `json:"count"`
	Commit int `json:"commit"`
}

// Blame annotates every line of a file. Commits are listed once and referenced by
// index from line ranges, which keeps the payload small for large files.
type Blame struct {
	// Root is the work tree the file belongs to (may be a nested worktree).
	Root string `json:"root"`
	// Untracked is set for files git does not know yet: every line is uncommitted.
	Untracked bool          `json:"untracked"`
	Commits   []BlameCommit `json:"commits"`
	Ranges    []BlameRange  `json:"ranges"` // sorted by Line
}

// Blame annotates rel. contents, when non-nil, is blamed instead of the file on
// disk (an editor buffer with unsaved edits), so its lines match what is shown.
func (r *Repo) Blame(ctx context.Context, rel string, contents *string) (Blame, error) {
	args := []string{"blame", "--incremental"}
	var stdin []byte
	if contents != nil {
		args = append(args, "--contents", "-")
		stdin = []byte(*contents)
	}
	args = append(args, "--", rel)
	out, err := r.gitInput(ctx, stdin, args...)
	if err != nil {
		var gerr *Error
		// New files (and repositories without commits) have no history yet.
		if errors.As(err, &gerr) && (strings.Contains(gerr.Msg, "no such path") || strings.Contains(gerr.Msg, "no such ref: HEAD")) {
			return Blame{Root: r.Root, Untracked: true, Commits: []BlameCommit{}, Ranges: []BlameRange{}}, nil
		}
		return Blame{}, err
	}
	b := parseBlame(out)
	b.Root = r.Root
	return b, nil
}

// parseBlame reads `git blame --incremental` output: a header line
// "<hash> <orig line> <final line> <count>" followed by key/value lines ending with
// "filename". Commit details are only printed the first time a commit appears.
func parseBlame(out []byte) Blame {
	b := Blame{Commits: []BlameCommit{}, Ranges: []BlameRange{}}
	index := map[string]int{}
	var cur *BlameCommit
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if cur == nil {
			f := strings.Fields(line)
			if len(f) != 4 || len(f[0]) < 40 {
				continue
			}
			final, err1 := strconv.Atoi(f[2])
			count, err2 := strconv.Atoi(f[3])
			if err1 != nil || err2 != nil {
				continue
			}
			i, ok := index[f[0]]
			if !ok {
				i = len(b.Commits)
				index[f[0]] = i
				b.Commits = append(b.Commits, BlameCommit{Hash: f[0], Uncommitted: f[0] == uncommittedHash})
			}
			b.Ranges = append(b.Ranges, BlameRange{Line: final, Count: count, Commit: i})
			cur = &b.Commits[i]
			continue
		}
		key, val, _ := strings.Cut(line, " ")
		switch key {
		case "author":
			cur.Author = val
		case "author-mail":
			cur.Email = strings.TrimSuffix(strings.TrimPrefix(val, "<"), ">")
		case "author-time":
			cur.Time, _ = strconv.ParseInt(val, 10, 64)
		case "summary":
			cur.Summary = val
		case "previous":
			if hash, path, ok := strings.Cut(val, " "); ok && cur.Previous == "" {
				cur.Previous, cur.PrevPath = hash, path
			}
		case "filename":
			if cur.Path == "" {
				cur.Path = val
			}
			cur = nil // a group always ends with its filename
		}
	}
	sort.Slice(b.Ranges, func(i, j int) bool { return b.Ranges[i].Line < b.Ranges[j].Line })
	return b
}
