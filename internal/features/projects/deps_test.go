package projects

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestParseAfter(t *testing.T) {
	c := Config{Name: "x", Repositories: []RepositoryDecl{}}
	many := make([]string, MaxAfter+1)
	for i := range many {
		many[i] = fmt.Sprintf("i%d", i)
	}
	for _, tc := range []struct {
		value string
		want  []string
		err   string
	}{
		{"", []string{}, ""},
		{"a", []string{"a"}, ""},
		{" a , b_2,,c-3 ", []string{"a", "b_2", "c-3"}, ""},
		{strings.Join(many[:MaxAfter], ","), many[:MaxAfter], ""},
		{strings.Join(many, ","), nil, "more than 20"},
		{"a, a", nil, `"a" is listed twice`},
		{"a b", nil, "not an item id"},
		{"../x", nil, "not an item id"},
		{"x.md", nil, "not an item id"},
	} {
		it, err := ParseItem("item", []byte("---\ntitle: T\nafter: "+tc.value+"\n---\n"), c)
		if tc.err != "" {
			wantErr(t, err, tc.err)
			continue
		}
		if err != nil || !slices.Equal(it.After, tc.want) {
			t.Errorf("after %q: %v %v", tc.value, it.After, err)
		}
	}
	// No after key: an empty list.
	if it, err := ParseItem("item", []byte("---\ntitle: T\n---\n"), c); err != nil || it.After == nil || len(it.After) != 0 {
		t.Errorf("no after: %#v %v", it.After, err)
	}
}

func TestDependencies(t *testing.T) {
	it := func(id, status string, after ...string) Item {
		return Item{ID: id, Title: id, Status: status, After: after}
	}
	for _, tc := range []struct {
		name  string
		items []Item
		want  map[string]Deps
	}{
		{"none", []Item{it("a", "todo")}, map[string]Deps{"a": {}}},
		{"waits", []Item{it("a", "todo", "b", "c"), it("b", "doing"), it("c", "done")},
			map[string]Deps{"a": {WaitingFor: []string{"b"}}, "b": {}, "c": {}}},
		{"ready", []Item{it("a", "todo", "b", "c"), it("b", "done"), it("c", "done")},
			map[string]Deps{"a": {}, "b": {}, "c": {}}},
		{"unknown", []Item{it("a", "todo", "b", "zz", "yy")},
			map[string]Deps{"a": {Problem: "after: no item b, zz, yy in this project (or its file has a problem)"}}},
		{"self", []Item{it("a", "todo", "a")}, map[string]Deps{"a": {Problem: "after: the item names itself"}}},
		{"cycle of two", []Item{it("a", "todo", "b"), it("b", "todo", "a")}, map[string]Deps{
			"a": {WaitingFor: []string{"b"}, Problem: "after: a cycle: a → b → a"},
			"b": {WaitingFor: []string{"a"}, Problem: "after: a cycle: b → a → b"},
		}},
		{"cycle of three, and one waiting on it", []Item{it("a", "done", "c"), it("b", "todo", "a"), it("c", "todo", "b"), it("d", "todo", "c")}, map[string]Deps{
			"a": {WaitingFor: []string{"c"}, Problem: "after: a cycle: a → c → b → a"},
			"b": {Problem: "after: a cycle: b → a → c → b"},
			"c": {WaitingFor: []string{"b"}, Problem: "after: a cycle: c → b → a → c"},
			"d": {WaitingFor: []string{"c"}},
		}},
		{"diamond is no cycle", []Item{it("a", "todo", "b", "c"), it("b", "todo", "d"), it("c", "todo", "d"), it("d", "done")}, map[string]Deps{
			"a": {WaitingFor: []string{"b", "c"}}, "b": {}, "c": {}, "d": {},
		}},
	} {
		got := Dependencies(tc.items)
		for id, want := range tc.want {
			g := got[id]
			if !slices.Equal(g.WaitingFor, want.WaitingFor) || g.Problem != want.Problem {
				t.Errorf("%s: %s = %+v, want %+v", tc.name, id, g, want)
			}
			if g.Ready() != (want.Problem == "" && len(want.WaitingFor) == 0) {
				t.Errorf("%s: %s ready = %v", tc.name, id, g.Ready())
			}
		}
	}
}

// A long chain and a wide project stay quick (500 items, 20 each).
func TestDependenciesAtTheLimits(t *testing.T) {
	var items []Item
	for i := range MaxItems {
		it := Item{ID: fmt.Sprintf("i%d", i), Status: StatusTodo}
		for j := max(0, i-MaxAfter); j < i; j++ {
			it.After = append(it.After, fmt.Sprintf("i%d", j))
		}
		items = append(items, it)
	}
	got := Dependencies(items)
	if d := got["i499"]; d.Problem != "" || len(d.WaitingFor) != MaxAfter {
		t.Errorf("i499 = %+v", d)
	}
	if d := got["i0"]; !d.Ready() {
		t.Errorf("i0 = %+v", d)
	}
}

func TestBuildItemFile(t *testing.T) {
	c, err := ParseConfig([]byte(testConfigJSON), "plan")
	if err != nil {
		t.Fatal(err)
	}
	data, err := BuildItemFile(ItemSpec{Title: " 'Quoted title' ", Text: "Do it.\r\n\nDone when:\n- ok\n", Priority: "high", Agent: "backend", Repos: []string{"api", "web"}, After: []string{"a", "b"}}, "quoted-title", c)
	if err != nil {
		t.Fatal(err)
	}
	want := "---\ntitle: \"'Quoted title'\"\nstatus: todo\npriority: high\nagent: backend\nrepos: api, web\nafter: a, b\n---\nDo it.\n\nDone when:\n- ok\n"
	if string(data) != want {
		t.Errorf("file\n%s\nwant\n%s", data, want)
	}
	if data, err := BuildItemFile(ItemSpec{Title: "T"}, "t", c); err != nil || string(data) != "---\ntitle: T\nstatus: todo\npriority: normal\n---\n" {
		t.Errorf("minimal: %q %v", data, err)
	}
	for _, tc := range []struct {
		spec ItemSpec
		err  string
	}{
		{ItemSpec{}, "give the item a title"},
		{ItemSpec{Title: strings.Repeat("x", MaxTitle+1)}, "longer than 120"},
		{ItemSpec{Title: "a\nstatus: done"}, "one line"},
		{ItemSpec{Title: "a​b"}, "invisible"},
		{ItemSpec{Title: "T", Text: "x‮evil"}, "invisible characters (U+202E)"},
		{ItemSpec{Title: "T", Text: "bell\a"}, "control character"},
		{ItemSpec{Title: "T", Text: strings.Repeat("x", MaxDescription)}, "longer than 8000 bytes"},
		{ItemSpec{Title: "T", Priority: "urgent"}, "priority"},
		{ItemSpec{Title: "T", Priority: "high\nstatus: done"}, "one word"},
		{ItemSpec{Title: "T", Agent: "a b"}, "not an agent name"},
		{ItemSpec{Title: "T", Agent: "x\nstatus: done"}, "one word"},
		{ItemSpec{Title: "T", Repos: []string{"db"}}, `repository "db" is not in`},
		{ItemSpec{Title: "T", Repos: []string{"api, web"}}, "is not a name"},
		{ItemSpec{Title: "T", Repos: []string{"api", "api"}}, "listed twice"},
		{ItemSpec{Title: "T", After: []string{"a\nstatus: done"}}, "is not a name"},
		{ItemSpec{Title: "T", After: []string{" a"}}, "is not a name"},
		{ItemSpec{Title: "T", After: []string{"../a"}}, "not an item id"},
	} {
		_, err := BuildItemFile(tc.spec, "t", c)
		wantErr(t, err, tc.err)
	}
}

// Flint's status rewrite keeps after, together and unknown keys byte for byte.
func TestSetStatusKeepsDependencies(t *testing.T) {
	in := "---\ntitle: T\nafter:  a, b \ntogether: true\nx-owner: me\nstatus: todo\n---\nBody\n"
	out, err := SetStatus([]byte(in), StatusDoing)
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.Replace(in, "status: todo", "status: doing", 1); string(out) != want {
		t.Errorf("got %q", out)
	}
}
