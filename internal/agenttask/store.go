package agenttask

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"slices"
)

// Bounds of what state.json keeps: a few dozen tasks is a busy repository,
// and the list is read on every refresh of the panel.
const (
	MaxPerRepo   = 50
	maxRepos     = 100
	MaxPrompt    = 8000 // characters of a description that are kept
	idBytes      = 8
	idHexLength  = 2 * idBytes
	errDuplicate = "a task with this id already exists"
)

// Task is one remembered agent task.
type Task struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Prompt is the description the user wrote (redacted, cut to MaxPrompt):
	// the first message of the chat and the body of the pull request.
	Prompt string `json:"prompt"`
	Agent  string `json:"agent"`
	Branch string `json:"branch"`
	// Base is the branch the task started from and merges back into.
	Base string `json:"base"`
	// BaseCommit is where Branch started, to tell "merged" from "no commits yet".
	BaseCommit string `json:"baseCommit"`
	Worktree   string `json:"worktree"`
	// Checkout is the repository's main checkout, where git runs once the
	// worktree is gone.
	Checkout  string `json:"checkout"`
	CreatedAt int64  `json:"createdAt"` // unix seconds
	// State is the settled state (finished / failed) shown after a restart.
	State string `json:"state,omitempty"`
	Error string `json:"error,omitempty"`
	// PullRequest is the URL of the pull request created for the branch.
	PullRequest string `json:"pullRequest,omitempty"`
}

// Store holds the tasks per repository, keyed by the repository's common git
// dir (shared by all its worktrees). Methods never change the receiver:
// they return a new Store, so a failed save leaves the old one in place.
type Store map[string][]Task

// NewID returns a random task id.
func NewID() string {
	b := make([]byte, idBytes)
	_, _ = rand.Read(b) // never fails (crypto/rand)
	return hex.EncodeToString(b)
}

// ValidID reports an id NewID could have made (ids come back from the UI).
func ValidID(id string) bool {
	if len(id) != idHexLength {
		return false
	}
	for _, r := range id {
		if !('0' <= r && r <= '9' || 'a' <= r && r <= 'f') {
			return false
		}
	}
	return true
}

// List returns repo's tasks, oldest first.
func (s Store) List(repo string) []Task {
	return slices.Clone(s[repo])
}

// Find returns the task with id and its repository.
func (s Store) Find(id string) (repo string, task Task, ok bool) {
	for r, tasks := range s {
		if i := slices.IndexFunc(tasks, func(t Task) bool { return t.ID == id }); i >= 0 {
			return r, tasks[i], true
		}
	}
	return "", Task{}, false
}

// ByWorktree returns the id of the task whose worktree is path, "" for none.
func (s Store) ByWorktree(path string) string {
	for _, tasks := range s {
		for _, t := range tasks {
			if t.Worktree == path {
				return t.ID
			}
		}
	}
	return ""
}

// Add appends a task to repo.
func (s Store) Add(repo string, t Task) (Store, error) {
	if _, _, dup := s.Find(t.ID); dup {
		return nil, errors.New(errDuplicate)
	}
	if len(s[repo]) >= MaxPerRepo {
		return nil, fmt.Errorf("this repository already has %d tasks: remove finished ones first", MaxPerRepo)
	}
	if _, known := s[repo]; !known && len(s) >= maxRepos {
		return nil, fmt.Errorf("tasks are kept for at most %d repositories: remove tasks of another one first", maxRepos)
	}
	out := maps.Clone(s)
	if out == nil {
		out = Store{}
	}
	out[repo] = append(slices.Clone(s[repo]), t)
	return out, nil
}

// Update replaces the task with id by fn's result (no-op when it is unknown).
func (s Store) Update(id string, fn func(Task) Task) Store {
	repo, _, ok := s.Find(id)
	if !ok {
		return s
	}
	out := maps.Clone(s)
	tasks := slices.Clone(s[repo])
	for i := range tasks {
		if tasks[i].ID == id {
			tasks[i] = fn(tasks[i])
		}
	}
	out[repo] = tasks
	return out
}

// Remove drops the task with id; a repository left without tasks goes too.
func (s Store) Remove(id string) Store {
	repo, _, ok := s.Find(id)
	if !ok {
		return s
	}
	out := maps.Clone(s)
	tasks := slices.DeleteFunc(slices.Clone(s[repo]), func(t Task) bool { return t.ID == id })
	if len(tasks) == 0 {
		delete(out, repo)
	} else {
		out[repo] = tasks
	}
	return out
}
