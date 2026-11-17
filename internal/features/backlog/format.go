package backlog

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"flint/internal/agenttask"
	"flint/internal/prompts"
)

// The backlog folder's format (docs/design/backlog.md). Its limits are fixed:
// a backlog may come from someone else's repository.
const (
	// ConfigFile marks a folder as a backlog; ItemsDir holds its items.
	ConfigFile = "flint-backlog.json"
	ItemsDir   = "items"

	MaxFileSize     = prompts.MaxFileSize // bytes of an item or of ConfigFile
	MaxItems        = 500
	MaxRepositories = 50
	maxBacklogName  = 80
	maxRepoName     = 40
	MaxTitle        = 120
	maxAgentName    = 64
	// MaxDescription bounds title and body together: they become an agent
	// task's description.
	MaxDescription = agenttask.MaxPrompt
)

// Statuses, in the order the panel groups them.
const (
	StatusDoing  = "doing"
	StatusReview = "review"
	StatusTodo   = "todo"
	StatusDone   = "done"
)

// Statuses lists the statuses in the panel's order.
var Statuses = []string{StatusDoing, StatusReview, StatusTodo, StatusDone}

// Priorities.
const (
	PriorityHigh   = "high"
	PriorityNormal = "normal"
	PriorityLow    = "low"
)

var priorities = []string{PriorityHigh, PriorityNormal, PriorityLow}

var (
	repoNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	agentPattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
)

// Config is flint-backlog.json.
type Config struct {
	Name         string           `json:"name"`
	Repositories []RepositoryDecl `json:"repositories"`
}

// RepositoryDecl is one entry of Config.Repositories.
type RepositoryDecl struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// Repository returns the declared repository called name.
func (c Config) Repository(name string) (RepositoryDecl, bool) {
	i := slices.IndexFunc(c.Repositories, func(r RepositoryDecl) bool { return r.Name == name })
	if i < 0 {
		return RepositoryDecl{}, false
	}
	return c.Repositories[i], true
}

// ParseConfig reads flint-backlog.json; folderName is the default name.
// Unknown fields are ignored.
func ParseConfig(data []byte, folderName string) (Config, error) {
	if len(data) > MaxFileSize {
		return Config{}, fmt.Errorf("larger than %d KB", MaxFileSize>>10)
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return Config{}, fmt.Errorf("not valid JSON: %v", err)
	}
	c.Name = strings.TrimSpace(c.Name)
	switch {
	case c.Name == "":
		c.Name = folderName
	case utf8.RuneCountInString(c.Name) > maxBacklogName:
		return Config{}, fmt.Errorf("name is longer than %d characters", maxBacklogName)
	case strings.ContainsFunc(c.Name, unicode.IsControl):
		return Config{}, errors.New("name contains control characters")
	}
	if len(c.Repositories) > MaxRepositories {
		return Config{}, fmt.Errorf("more than %d repositories", MaxRepositories)
	}
	seen := map[string]bool{}
	for i, r := range c.Repositories {
		if len(r.Name) > maxRepoName || !repoNamePattern.MatchString(r.Name) {
			return Config{}, fmt.Errorf("repository %d: name %q: use lowercase letters, digits and dashes (at most %d)", i+1, r.Name, maxRepoName)
		}
		if seen[r.Name] {
			return Config{}, fmt.Errorf("repository %q is listed twice", r.Name)
		}
		seen[r.Name] = true
		c.Repositories[i].URL = strings.TrimSpace(r.URL)
		if _, ok := RepoKey(r.URL); !ok {
			return Config{}, fmt.Errorf("repository %q: %q is not a git remote URL", r.Name, r.URL)
		}
	}
	if c.Repositories == nil {
		c.Repositories = []RepositoryDecl{}
	}
	return c, nil
}

// NewConfig is the flint-backlog.json New Backlog writes.
func NewConfig(name string) ([]byte, error) {
	name = strings.TrimSpace(name)
	if utf8.RuneCountInString(name) > maxBacklogName || strings.ContainsFunc(name, unicode.IsControl) {
		return nil, fmt.Errorf("the name must be one line of at most %d characters", maxBacklogName)
	}
	data, err := json.MarshalIndent(Config{Name: name, Repositories: []RepositoryDecl{}}, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// Item is one work item, parsed from items/<id>.md.
type Item struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Status   string   `json:"status"`
	Priority string   `json:"priority"`
	Agent    string   `json:"agent"` // "" = the default agent
	Repos    []string `json:"repos"` // names from Config; none = the active project
	Body     string   `json:"body"`
}

// ParseItem reads an item file of backlog c. Unknown keys are ignored.
func ParseItem(id string, data []byte, c Config) (Item, error) {
	if !prompts.ValidName(id) {
		return Item{}, fmt.Errorf("file name %q: use letters, digits, - and _", id+".md")
	}
	if len(data) > MaxFileSize {
		return Item{}, fmt.Errorf("larger than %d KB", MaxFileSize>>10)
	}
	fields, body, err := prompts.FrontMatter(data)
	if err != nil {
		return Item{}, err
	}
	it := Item{ID: id, Status: StatusTodo, Priority: PriorityNormal, Repos: []string{}}
	for _, f := range fields {
		switch f.Key {
		case "title":
			it.Title = f.Value
		case "status":
			if !slices.Contains(Statuses, f.Value) {
				return Item{}, fmt.Errorf("status %q: use todo, doing, review or done", f.Value)
			}
			it.Status = f.Value
		case "priority":
			if !slices.Contains(priorities, f.Value) {
				return Item{}, fmt.Errorf("priority %q: use low, normal or high", f.Value)
			}
			it.Priority = f.Value
		case "agent":
			if f.Value != "" && (len(f.Value) > maxAgentName || !agentPattern.MatchString(f.Value)) {
				return Item{}, fmt.Errorf("agent %q is not an agent name", f.Value)
			}
			it.Agent = f.Value
		case "repos":
			if it.Repos, err = parseRepos(f.Value, c); err != nil {
				return Item{}, err
			}
		}
	}
	switch {
	case it.Title == "":
		return Item{}, errors.New("title is missing")
	case utf8.RuneCountInString(it.Title) > MaxTitle:
		return Item{}, fmt.Errorf("title is longer than %d characters", MaxTitle)
	}
	it.Body = strings.TrimSpace(strings.ReplaceAll(body, "\r\n", "\n"))
	if strings.ContainsRune(it.Body, 0) {
		return Item{}, errors.New("the text contains a NUL character")
	}
	if len(it.Description()) > MaxDescription {
		return Item{}, fmt.Errorf("title and text are longer than %d characters", MaxDescription)
	}
	return it, nil
}

func parseRepos(v string, c Config) ([]string, error) {
	out := []string{}
	for name := range strings.SplitSeq(v, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := c.Repository(name); !ok {
			if len(c.Repositories) == 0 {
				return nil, fmt.Errorf("repository %q: %s lists no repositories", name, ConfigFile)
			}
			return nil, fmt.Errorf("repository %q is not in %s", name, ConfigFile)
		}
		if slices.Contains(out, name) {
			return nil, fmt.Errorf("repository %q is listed twice", name)
		}
		out = append(out, name)
	}
	return out, nil
}

// Description is what an agent task of the item is told: title and text.
func (it Item) Description() string {
	if it.Body == "" {
		return it.Title
	}
	return it.Title + "\n\n" + it.Body
}

// TaskDescription is the description of the item's task in repository repo:
// with several repositories it names the others and says which one this
// task works in.
func (it Item) TaskDescription(repo string) string {
	d := it.Description()
	if len(it.Repos) < 2 {
		return d
	}
	var others []string
	for _, r := range it.Repos {
		if r != repo {
			others = append(others, r)
		}
	}
	return fmt.Sprintf("%s\n\nThis work item spans the repositories %s. This task works in %s only; %s %s handled by separate tasks.",
		d, strings.Join(it.Repos, ", "), repo, strings.Join(others, ", "), plural(len(others), "is", "are"))
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// SortItems orders items by status (Statuses), then priority (high first),
// then title, then id.
func SortItems(items []Item) {
	rank := func(list []string, v string) int { return slices.Index(list, v) }
	slices.SortStableFunc(items, func(a, b Item) int {
		if d := rank(Statuses, a.Status) - rank(Statuses, b.Status); d != 0 {
			return d
		}
		if d := rank(priorities, a.Priority) - rank(priorities, b.Priority); d != 0 {
			return d
		}
		if d := strings.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title)); d != 0 {
			return d
		}
		return strings.Compare(a.ID, b.ID)
	})
}

// ItemID derives an item's id from its title (agenttask.Slug: lowercase
// letters, digits and dashes, at most 40).
func ItemID(title string) string {
	id := agenttask.Slug(title)
	if id == "task" && !strings.Contains(strings.ToLower(title), "task") {
		return "item"
	}
	return id
}

// NewItemFile is the file New Item writes for title.
func NewItemFile(title string) ([]byte, error) {
	title = strings.TrimSpace(title)
	switch {
	case title == "":
		return nil, errors.New("give the item a title")
	case utf8.RuneCountInString(title) > MaxTitle:
		return nil, fmt.Errorf("the title is longer than %d characters", MaxTitle)
	case strings.ContainsFunc(title, unicode.IsControl):
		return nil, errors.New("the title must be one line")
	}
	if len(title) >= 2 && (title[0] == '"' || title[0] == '\'') && title[len(title)-1] == title[0] {
		title = `"` + title + `"` // FrontMatter takes one pair of quotes off
	}
	return []byte("---\n" +
		"title: " + title + "\n" +
		"status: todo\n" +
		"priority: normal\n" +
		"# agent: backend     the agent that works on it (default: the default agent)\n" +
		"# repos: api, web    repositories from " + ConfigFile + " (default: the active project)\n" +
		"---\n" +
		"What to do.\n\n" +
		"Done when:\n" +
		"- \n"), nil
}
