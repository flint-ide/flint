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
	// MaxAfter bounds the items an item waits for (its after key).
	MaxAfter = 20
	// MaxDescription bounds title and body together, in bytes (UTF-8), as
	// New Agent Task bounds a description: they become an agent task's.
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
	if r, ok := hiddenRune(c.Name); ok {
		return Config{}, fmt.Errorf("name contains invisible characters (U+%04X)", r)
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
	// After are the ids of the items that must be done first (same backlog).
	After []string `json:"after"`
	Body  string   `json:"body"`
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
	it := Item{ID: id, Status: StatusTodo, Priority: PriorityNormal, Repos: []string{}, After: []string{}}
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
		case "after":
			if it.After, err = parseAfter(f.Value); err != nil {
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
	for _, part := range []struct{ name, text string }{{"title", it.Title}, {"the text", it.Body}} {
		if r, ok := hiddenRune(part.text); ok {
			if unicode.IsControl(r) {
				return Item{}, fmt.Errorf("%s contains a control character (U+%04X)", part.name, r)
			}
			return Item{}, fmt.Errorf("%s contains invisible characters (U+%04X)", part.name, r)
		}
	}
	if len(it.Description()) > MaxDescription {
		return Item{}, fmt.Errorf("title and text are longer than %d bytes", MaxDescription)
	}
	return it, nil
}

// hiddenRune finds a character an item must not contain because the user
// reading it does not see it, while the agent it is sent to does: a control
// character other than tab and newline, a Unicode tag (U+E0000–U+E007F), a
// bidirectional control (U+202A–U+202E, U+2066–U+2069), a zero-width space
// (U+200B), a word joiner (U+2060) or a zero-width no-break space (U+FEFF; a
// file that starts with one has no front matter, so no title). Zero-width
// joiner and non-joiner (U+200D, U+200C) are allowed: emoji and several
// scripts need them.
func hiddenRune(s string) (rune, bool) {
	for _, r := range s {
		switch {
		case r == '\t' || r == '\n':
		case unicode.IsControl(r),
			r >= 0xE0000 && r <= 0xE007F,
			r >= 0x202A && r <= 0x202E,
			r >= 0x2066 && r <= 0x2069,
			r == 0x200B, r == 0x2060, r == 0xFEFF:
			return r, true
		}
	}
	return 0, false
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

// parseAfter reads the after key: comma-separated item ids, at most
// MaxAfter, none twice. Whether they exist, and cycles, are checked against
// the whole backlog (Dependencies).
func parseAfter(v string) ([]string, error) {
	out := []string{}
	for id := range strings.SplitSeq(v, ",") {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if !prompts.ValidName(id) {
			return nil, fmt.Errorf("after: %q is not an item id (the file name without .md)", id)
		}
		if slices.Contains(out, id) {
			return nil, fmt.Errorf("after: %q is listed twice", id)
		}
		if len(out) == MaxAfter {
			return nil, fmt.Errorf("after: more than %d items", MaxAfter)
		}
		out = append(out, id)
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

// checkTitle checks a title for a new item and returns it trimmed.
func checkTitle(title string) (string, error) {
	title = strings.TrimSpace(title)
	switch {
	case title == "":
		return "", errors.New("give the item a title")
	case utf8.RuneCountInString(title) > MaxTitle:
		return "", fmt.Errorf("the title is longer than %d characters", MaxTitle)
	case strings.ContainsFunc(title, unicode.IsControl):
		return "", errors.New("the title must be one line")
	}
	if r, ok := hiddenRune(title); ok {
		return "", fmt.Errorf("the title contains invisible characters (U+%04X)", r)
	}
	return title, nil
}

// titleValue is title as a front matter value: FrontMatter takes one pair of
// quotes off, so a quoted title is quoted once more.
func titleValue(title string) string {
	if len(title) >= 2 && (title[0] == '"' || title[0] == '\'') && title[len(title)-1] == title[0] {
		return `"` + title + `"`
	}
	return title
}

// NewItemFile is the file New Item writes for title.
func NewItemFile(title string) ([]byte, error) {
	title, err := checkTitle(title)
	if err != nil {
		return nil, err
	}
	return []byte("---\n" +
		"title: " + titleValue(title) + "\n" +
		"status: todo\n" +
		"priority: normal\n" +
		"# agent: backend     the agent that works on it (default: the default agent)\n" +
		"# repos: api, web    repositories from " + ConfigFile + " (default: the active project)\n" +
		"---\n" +
		"What to do.\n\n" +
		"Done when:\n" +
		"- \n"), nil
}

// ItemSpec is an item to write in full (add_backlog_item): what an agent
// planned. The status of a new item is always todo.
type ItemSpec struct {
	Title    string
	Text     string
	Priority string // "" = normal
	Agent    string
	Repos    []string
	After    []string
}

// BuildItemFile writes spec as an item file of backlog c and checks it with the
// parser (ParseItem, as id): the file says exactly what spec says, or it is
// refused with the reason. Whether the items of After exist is checked
// against the backlog (Dependencies).
func BuildItemFile(spec ItemSpec, id string, c Config) ([]byte, error) {
	title, err := checkTitle(spec.Title)
	if err != nil {
		return nil, err
	}
	if spec.Priority == "" {
		spec.Priority = PriorityNormal
	}
	list := func(key string, v []string) string {
		if len(v) == 0 {
			return ""
		}
		return key + ": " + strings.Join(v, ", ") + "\n"
	}
	for _, v := range [][]string{spec.Repos, spec.After} {
		for _, s := range v {
			if s == "" || strings.ContainsAny(s, ",\n\r") || strings.TrimSpace(s) != s {
				return nil, fmt.Errorf("%q is not a name", s)
			}
		}
	}
	if strings.ContainsAny(spec.Agent+spec.Priority, "\n\r") {
		return nil, errors.New("agent and priority are one word each")
	}
	text := strings.TrimSpace(strings.ReplaceAll(spec.Text, "\r\n", "\n"))
	var b strings.Builder
	b.WriteString("---\ntitle: " + titleValue(title) + "\nstatus: todo\npriority: " + spec.Priority + "\n")
	if spec.Agent != "" {
		b.WriteString("agent: " + spec.Agent + "\n")
	}
	b.WriteString(list("repos", spec.Repos))
	b.WriteString(list("after", spec.After))
	b.WriteString("---\n")
	if text != "" {
		b.WriteString(text + "\n")
	}
	data := []byte(b.String())
	it, err := ParseItem(id, data, c)
	if err != nil {
		return nil, err
	}
	// What was meant is what is written.
	if it.Title != title || it.Body != text || it.Status != StatusTodo || it.Priority != spec.Priority || it.Agent != spec.Agent ||
		!slices.Equal(it.Repos, nonNil(spec.Repos)) || !slices.Equal(it.After, nonNil(spec.After)) {
		return nil, errors.New("the item can't be written as given: check the title and the names")
	}
	return data, nil
}

func nonNil(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}
