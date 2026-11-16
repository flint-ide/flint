package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"flint/internal/agenttask"
	"flint/internal/git"
)

// Comparing the tasks of a group (the same description given to several
// agents, or several attempts): the files any of them changed with each
// task's line counts, and one file's changes per task as unified diff lines,
// all against the commit where each task left its base. The diffs are made
// here; the UI only lists them.

const (
	compareContext  = 3       // unchanged lines around a change
	compareMaxLines = 3000    // lines of one file's diff per task
	compareMaxFile  = 1 << 20 // bytes of a file that is diffed
	compareMaxPath  = 4096
)

// ComparedTask is one task of a comparison with the totals of its changes.
type ComparedTask struct {
	Task TaskView `json:"task"`
	// MergeBase is where the task's branch left its base.
	MergeBase string `json:"mergeBase"`
	Count     int    `json:"count"`
	Added     int    `json:"added"`
	Deleted   int    `json:"deleted"`
	// Error says why the task's changes could not be read (its worktree is gone).
	Error string `json:"error"`
}

// TaskComparison is what the tasks of a group changed.
type TaskComparison struct {
	Group string         `json:"group"`
	Tasks []ComparedTask `json:"tasks"`
	// Files are the files changed by any task, sorted by path; each row's
	// changes are in the order of Tasks.
	Files []agenttask.FileRow `json:"files"`
	// Truncated: more files were changed than are listed.
	Truncated bool `json:"truncated"`
}

// TaskFileDiff is one file's diff for one task (or between two tasks).
type TaskFileDiff struct {
	TaskID string `json:"taskId"`
	// OrigPath is the path the task renamed the file from.
	OrigPath string `json:"origPath"`
	// Changed is false when the two sides are the same.
	Changed bool `json:"changed"`
	// Note says why no lines are shown (a binary or very large file, an error).
	Note      string               `json:"note"`
	Lines     []agenttask.DiffLine `json:"lines"`
	Added     int                  `json:"added"`
	Deleted   int                  `json:"deleted"`
	Truncated bool                 `json:"truncated"`
}

// FileComparison is one file's diff in every task of a group, in the order
// of TaskComparison.Tasks.
type FileComparison struct {
	Path  string         `json:"path"`
	Tasks []TaskFileDiff `json:"tasks"`
}

// groupTasks looks a group up by an id from the web view.
func (t *TaskAPI) groupTasks(group string) ([]agenttask.Task, error) {
	if !agenttask.ValidID(group) {
		return nil, errors.New("invalid group id")
	}
	tasks := t.app.taskStore().Group(group)
	if len(tasks) == 0 {
		return nil, errors.New("no such group of tasks (they were removed)")
	}
	return tasks, nil
}

// Compare lists the files changed by any task of a group, with what each
// task did to them since it left the base (committed or not, new files
// included).
func (t *TaskAPI) Compare(group string) (TaskComparison, error) {
	tasks, err := t.groupTasks(group)
	if err != nil {
		return TaskComparison{}, err
	}
	ctx, cancel := t.ctx(gitTimeout)
	defer cancel()
	out := TaskComparison{Group: group, Tasks: make([]ComparedTask, len(tasks))}
	perTask := make([]map[string]agenttask.FileChange, len(tasks))
	views := make([]TaskView, len(tasks))
	for i, task := range tasks {
		c := ComparedTask{}
		if !isDir(task.Worktree) {
			c.Error = "the worktree is gone"
			if r, err := t.git.repo(ctx, task.Checkout); err == nil {
				views[i] = t.view(ctx, r, task)
			} else {
				views[i] = TaskView{ID: task.ID, Title: task.Title, Agent: task.Agent, Branch: task.Branch, Base: task.Base, Stale: true, Group: task.Group}
			}
			out.Tasks[i] = c
			continue
		}
		r, err := t.git.repo(ctx, task.Worktree)
		if err != nil {
			return TaskComparison{}, err
		}
		views[i] = t.view(ctx, r, task)
		st, err := r.DiffStat(ctx, task.Base, taskChangesLimit)
		if err != nil {
			c.Error = err.Error()
			out.Tasks[i] = c
			continue
		}
		c.MergeBase, c.Count, c.Added, c.Deleted = st.MergeBase, st.Count, st.Added, st.Deleted
		out.Truncated = out.Truncated || st.Truncated
		perTask[i] = make(map[string]agenttask.FileChange, len(st.Files))
		for _, f := range st.Files {
			perTask[i][f.Path] = agenttask.FileChange{OrigPath: f.OrigPath, Added: f.Added, Deleted: f.Deleted, Binary: f.Binary, Untracked: f.Untracked}
		}
		out.Tasks[i] = c
	}
	t.addUsage(views)
	for i := range out.Tasks {
		out.Tasks[i].Task = views[i]
	}
	var truncated bool
	out.Files, truncated = agenttask.Union(perTask, taskChangesLimit)
	out.Truncated = out.Truncated || truncated
	return out, nil
}

// checkComparePath accepts a path relative to a worktree that stays inside
// it and is not git's own.
func checkComparePath(path string) error {
	switch {
	case path == "" || len(path) > compareMaxPath || strings.ContainsRune(path, 0):
		return errors.New("invalid file path")
	case !filepath.IsLocal(path):
		return fmt.Errorf("%q is not a path inside the task's folder", path)
	case path == ".git" || strings.HasPrefix(filepath.ToSlash(path), ".git/"):
		return errors.New("git's own files are not compared")
	}
	return nil
}

// CompareFile returns one file's changes in every task of a group, each
// against where that task left its base, as unified diff lines.
func (t *TaskAPI) CompareFile(group, path string) (FileComparison, error) {
	tasks, err := t.groupTasks(group)
	if err != nil {
		return FileComparison{}, err
	}
	if err := checkComparePath(path); err != nil {
		return FileComparison{}, err
	}
	ctx, cancel := t.ctx(gitTimeout)
	defer cancel()
	out := FileComparison{Path: path, Tasks: make([]TaskFileDiff, len(tasks))}
	for i, task := range tasks {
		out.Tasks[i] = t.fileDiff(ctx, task, path)
	}
	return out, nil
}

// fileDiff is path's diff in one task: its merge base → its work tree.
func (t *TaskAPI) fileDiff(ctx context.Context, task agenttask.Task, path string) TaskFileDiff {
	d := TaskFileDiff{TaskID: task.ID, Lines: []agenttask.DiffLine{}}
	if !isDir(task.Worktree) {
		d.Note = "The worktree is gone."
		return d
	}
	r, err := t.git.repo(ctx, task.Worktree)
	if err != nil {
		d.Note = err.Error()
		return d
	}
	st, err := r.DiffStat(ctx, task.Base, taskChangesLimit)
	if err != nil {
		d.Note = err.Error()
		return d
	}
	basePath := path
	if i := slices.IndexFunc(st.Files, func(f git.FileStat) bool { return f.Path == path }); i >= 0 && st.Files[i].OrigPath != "" {
		basePath, d.OrigPath = st.Files[i].OrigPath, st.Files[i].OrigPath
	}
	base, err := r.FileAt(ctx, st.MergeBase, filepath.ToSlash(basePath))
	if err != nil {
		d.Note = err.Error()
		return d
	}
	current, err := readTaskFile(task.Worktree, path)
	if err != nil {
		d.Note = err.Error()
		return d
	}
	return diffSides(d, side{exists: base.Exists, opaque: base.Binary, text: base.Content}, current)
}

// DiffBetween compares one file as it is now in two tasks of the same
// group: what the second task has that the first has not.
func (t *TaskAPI) DiffBetween(idA, idB, path string) (TaskFileDiff, error) {
	a, err := t.liveTask(idA)
	if err != nil {
		return TaskFileDiff{}, err
	}
	b, err := t.liveTask(idB)
	if err != nil {
		return TaskFileDiff{}, err
	}
	if a.Group == "" || a.Group != b.Group || a.ID == b.ID {
		return TaskFileDiff{}, errors.New("choose two different tasks of the same group")
	}
	if err := checkComparePath(path); err != nil {
		return TaskFileDiff{}, err
	}
	left, err := readTaskFile(a.Worktree, path)
	if err != nil {
		return TaskFileDiff{}, err
	}
	right, err := readTaskFile(b.Worktree, path)
	if err != nil {
		return TaskFileDiff{}, err
	}
	return diffSides(TaskFileDiff{TaskID: b.ID, Lines: []agenttask.DiffLine{}}, left, right), nil
}

// side is one side of a file diff.
type side struct {
	exists bool
	opaque bool // binary or too large to diff
	text   string
}

// diffSides fills d with the diff of from → to.
func diffSides(d TaskFileDiff, from, to side) TaskFileDiff {
	switch {
	case from.opaque || to.opaque:
		d.Changed = true // not compared byte by byte: shown as possibly different
		d.Note = "Binary or very large file: contents not shown."
		return d
	case !from.exists && !to.exists:
		d.Note = "The file does not exist here."
		return d
	}
	lines, stat, truncated := agenttask.Unified(from.text, to.text, compareContext, compareMaxLines)
	d.Lines, d.Added, d.Deleted, d.Truncated = lines, stat.Added, stat.Deleted, truncated
	d.Changed = len(lines) > 0 || from.exists != to.exists
	switch {
	case !from.exists:
		d.Note = "New file."
	case !to.exists:
		d.Note = "Deleted."
	case !d.Changed:
		d.Note = "Unchanged."
	}
	return d
}

// readTaskFile reads a file of a task's work tree. os.Root keeps the path,
// which comes from the web view, inside the worktree whatever symlinks an
// agent left there.
func readTaskFile(worktree, rel string) (side, error) {
	root, err := os.OpenRoot(worktree)
	if err != nil {
		return side{}, err
	}
	defer root.Close()
	f, err := root.Open(rel)
	if errors.Is(err, os.ErrNotExist) {
		return side{}, nil
	}
	if err != nil {
		return side{}, fmt.Errorf("read %s: %w", rel, err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return side{}, err
	}
	if !info.Mode().IsRegular() {
		return side{exists: true, opaque: true}, nil
	}
	if info.Size() > compareMaxFile {
		return side{exists: true, opaque: true}, nil
	}
	data, err := io.ReadAll(io.LimitReader(f, compareMaxFile+1))
	if err != nil {
		return side{}, fmt.Errorf("read %s: %w", rel, err)
	}
	if len(data) > compareMaxFile || slices.Contains(data[:min(len(data), 8000)], 0) {
		return side{exists: true, opaque: true}, nil
	}
	return side{exists: true, text: string(data)}, nil
}
