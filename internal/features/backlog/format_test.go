package backlog

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestParseConfig(t *testing.T) {
	repos := func(n int) string {
		var parts []string
		for i := range n {
			parts = append(parts, fmt.Sprintf(`{"name":"r%d","url":"git@github.com:acme/r%d.git"}`, i, i))
		}
		return `{"repositories":[` + strings.Join(parts, ",") + `]}`
	}
	for _, tc := range []struct {
		name, data string
		want       Config
		err        string
	}{
		{"full", `{"name":"Acme Shop","repositories":[{"name":"api","url":"git@github.com:acme/api.git"},{"name":"web-2","url":"https://github.com/acme/web.git"}]}`,
			Config{Name: "Acme Shop", Repositories: []RepositoryDecl{{"api", "git@github.com:acme/api.git"}, {"web-2", "https://github.com/acme/web.git"}}}, ""},
		{"default name, no repositories", `{}`, Config{Name: "plan", Repositories: []RepositoryDecl{}}, ""},
		{"unknown fields ignored", `{"name":" x ","owner":"me","repositories":[{"name":"a","url":"/srv/a.git","branch":"main"}]}`,
			Config{Name: "x", Repositories: []RepositoryDecl{{"a", "/srv/a.git"}}}, ""},
		{"50 repositories", repos(50), Config{}, ""},
		{"51 repositories", repos(51), Config{}, "more than 50"},
		{"name too long", `{"name":"` + strings.Repeat("n", 81) + `"}`, Config{}, "longer than 80"},
		{"name of 80", `{"name":"` + strings.Repeat("ü", 80) + `"}`, Config{Name: strings.Repeat("ü", 80), Repositories: []RepositoryDecl{}}, ""},
		{"control in name", `{"name":"a\u0007b"}`, Config{}, "control"},
		{"upper-case repo", `{"repositories":[{"name":"API","url":"git@h:a/b"}]}`, Config{}, "lowercase"},
		{"repo name too long", `{"repositories":[{"name":"` + strings.Repeat("a", 41) + `","url":"git@h:a/b"}]}`, Config{}, "at most 40"},
		{"empty repo name", `{"repositories":[{"name":"","url":"git@h:a/b"}]}`, Config{}, "lowercase"},
		{"duplicate repo", `{"repositories":[{"name":"a","url":"git@h:a/b"},{"name":"a","url":"git@h:a/c"}]}`, Config{}, "twice"},
		{"bad url", `{"repositories":[{"name":"a","url":"not a url"}]}`, Config{}, "not a git remote URL"},
		{"not json", `{"name":`, Config{}, "not valid JSON"},
		{"wrong type", `{"repositories":{}}`, Config{}, "not valid JSON"},
		{"too large", `{"name":"` + strings.Repeat(" ", MaxFileSize) + `"}`, Config{}, "larger than"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := ParseConfig([]byte(tc.data), "plan")
			if tc.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.err) {
					t.Fatalf("err = %v, want %q", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.want.Name != "" && (c.Name != tc.want.Name || !slices.Equal(c.Repositories, tc.want.Repositories)) {
				t.Errorf("got %+v, want %+v", c, tc.want)
			}
		})
	}
}

var testConfig = Config{Name: "Acme", Repositories: []RepositoryDecl{{"api", "git@github.com:acme/api.git"}, {"web", "https://github.com/acme/web.git"}}}

func TestParseItem(t *testing.T) {
	long := strings.Repeat("x", MaxDescription)
	for _, tc := range []struct {
		name, id, data string
		want           Item
		err            string
	}{
		{"full", "rate-limit", "---\ntitle: Rate limit the checkout API\nstatus: doing\npriority: high\nagent: backend\nrepos: api, web\nowner: me\n# a comment\n---\nLimit it.\n\nDone when: 429.\n",
			Item{ID: "rate-limit", Title: "Rate limit the checkout API", Status: "doing", Priority: "high", Agent: "backend", Repos: []string{"api", "web"}, Body: "Limit it.\n\nDone when: 429."}, ""},
		{"defaults", "a_1", "---\ntitle: 'Quoted'\n---\n",
			Item{ID: "a_1", Title: "Quoted", Status: "todo", Priority: "normal", Repos: []string{}}, ""},
		{"crlf", "c", "---\r\ntitle: T\r\nstatus: review\r\n---\r\nline one\r\nline two\r\n",
			Item{ID: "c", Title: "T", Status: "review", Priority: "normal", Repos: []string{}, Body: "line one\nline two"}, ""},
		{"empty repos and agent", "e", "---\ntitle: T\nrepos:\nagent:\n---\n",
			Item{ID: "e", Title: "T", Status: "todo", Priority: "normal", Repos: []string{}}, ""},
		{"no front matter", "n", "just text\n", Item{}, "title is missing"},
		{"no title", "n", "---\nstatus: todo\n---\nx", Item{}, "title is missing"},
		{"title too long", "n", "---\ntitle: " + strings.Repeat("t", 121) + "\n---\n", Item{}, "longer than 120"},
		{"bad status", "n", "---\ntitle: T\nstatus: started\n---\n", Item{}, `status "started"`},
		{"bad priority", "n", "---\ntitle: T\npriority: urgent\n---\n", Item{}, `priority "urgent"`},
		{"bad agent", "n", "---\ntitle: T\nagent: rm -rf\n---\n", Item{}, "not an agent name"},
		{"unknown repo", "n", "---\ntitle: T\nrepos: api, db\n---\n", Item{}, `repository "db" is not in`},
		{"repo twice", "n", "---\ntitle: T\nrepos: api,api\n---\n", Item{}, "twice"},
		{"no closing", "n", "---\ntitle: T\n", Item{}, "no closing"},
		{"bad id", "../x", "---\ntitle: T\n---\n", Item{}, "file name"},
		{"too long text", "n", "---\ntitle: T\n---\n" + long, Item{}, "longer than 8000"},
		{"nul", "n", "---\ntitle: T\n---\na\x00b", Item{}, "NUL"},
		{"not utf-8", "n", "---\ntitle: \xff\n---\n", Item{}, "UTF-8"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it, err := ParseItem(tc.id, []byte(tc.data), testConfig)
			if tc.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.err) {
					t.Fatalf("err = %v, want %q", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if it.ID != tc.want.ID || it.Title != tc.want.Title || it.Status != tc.want.Status || it.Priority != tc.want.Priority ||
				it.Agent != tc.want.Agent || !slices.Equal(it.Repos, tc.want.Repos) || it.Body != tc.want.Body {
				t.Errorf("got %+v\nwant %+v", it, tc.want)
			}
		})
	}
	// Items name repositories only when the backlog lists some.
	if _, err := ParseItem("n", []byte("---\ntitle: T\nrepos: api\n---\n"), Config{}); err == nil || !strings.Contains(err.Error(), "lists no repositories") {
		t.Errorf("repos without repositories: %v", err)
	}
	// The limit counts title and text together.
	ok := "---\ntitle: T\n---\n" + strings.Repeat("x", MaxDescription-len("T\n\n"))
	if _, err := ParseItem("n", []byte(ok), testConfig); err != nil {
		t.Errorf("at the limit: %v", err)
	}
}

func TestTaskDescription(t *testing.T) {
	it := Item{Title: "T", Body: "Do it."}
	if got := it.TaskDescription(""); got != "T\n\nDo it." {
		t.Errorf("no repos: %q", got)
	}
	it.Repos = []string{"api"}
	if got := it.TaskDescription("api"); got != "T\n\nDo it." {
		t.Errorf("one repo: %q", got)
	}
	it.Repos = []string{"api", "web", "db"}
	got := it.TaskDescription("web")
	if !strings.HasPrefix(got, "T\n\nDo it.\n\n") || !strings.Contains(got, "spans the repositories api, web, db") ||
		!strings.Contains(got, "works in web only; api, db are handled by separate tasks") {
		t.Errorf("several repos: %q", got)
	}
	if got := (Item{Title: "T", Repos: []string{"a", "b"}}).TaskDescription("a"); !strings.Contains(got, "b is handled") {
		t.Errorf("two repos: %q", got)
	}
}

func TestSortItems(t *testing.T) {
	items := []Item{
		{ID: "d", Title: "a", Status: "done", Priority: "high"},
		{ID: "t2", Title: "b", Status: "todo", Priority: "normal"},
		{ID: "t1", Title: "Z", Status: "todo", Priority: "high"},
		{ID: "t3", Title: "a", Status: "todo", Priority: "normal"},
		{ID: "t4", Title: "a", Status: "todo", Priority: "low"},
		{ID: "r", Title: "x", Status: "review", Priority: "low"},
		{ID: "g", Title: "y", Status: "doing", Priority: "low"},
	}
	SortItems(items)
	var ids []string
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	if want := []string{"g", "r", "t1", "t3", "t2", "t4", "d"}; !slices.Equal(ids, want) {
		t.Errorf("order %v, want %v", ids, want)
	}
}

func TestNewItemFileAndID(t *testing.T) {
	for _, tc := range []struct{ title, id string }{
		{"Rate limit the checkout API", "rate-limit-the-checkout-api"},
		{"Ünïcode: café", "unicode-cafe"},
		{"???", "item"},
		{"Write the task list", "write-the-task-list"},
	} {
		if got := ItemID(tc.title); got != tc.id {
			t.Errorf("ItemID(%q) = %q, want %q", tc.title, got, tc.id)
		}
	}
	for _, title := range []string{"Rate limit: checkout", `"quoted"`, "'single'", strings.Repeat("é", MaxTitle)} {
		data, err := NewItemFile(title)
		if err != nil {
			t.Fatal(err)
		}
		it, err := ParseItem("x", data, Config{})
		if err != nil || it.Title != title || it.Status != StatusTodo || it.Priority != PriorityNormal {
			t.Errorf("NewItemFile(%q) parses as %+v, %v", title, it, err)
		}
	}
	for _, bad := range []string{"", "  ", "a\nb", strings.Repeat("t", MaxTitle+1)} {
		if _, err := NewItemFile(bad); err == nil {
			t.Errorf("NewItemFile(%q) accepted", bad)
		}
	}
}

func TestNewConfig(t *testing.T) {
	data, err := NewConfig(" Acme ")
	if err != nil {
		t.Fatal(err)
	}
	c, err := ParseConfig(data, "folder")
	if err != nil || c.Name != "Acme" || len(c.Repositories) != 0 {
		t.Errorf("NewConfig: %s → %+v, %v", data, c, err)
	}
	if _, err := NewConfig(strings.Repeat("n", 81)); err == nil {
		t.Error("a long name was accepted")
	}
	if _, err := NewConfig("a\nb"); err == nil {
		t.Error("two lines were accepted")
	}
}
