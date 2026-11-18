package projects

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetProjectTool(t *testing.T) {
	f := newFixture(t, map[string]string{
		"a": item("Alpha", "doing", "priority: high", "agent: backend", "repos: api, web"),
		"b": item("Bravo", "todo"),
		"c": "---\ntitle: Broken\nstatus: nope\n---\n",
	})
	tools := f.api.agentTools()
	if len(tools) != 1 || tools[0].Name != ToolName || tools[0].Acts || tools[0].Call == nil {
		t.Fatalf("tools %+v", tools)
	}
	var schema map[string]any
	if err := json.Unmarshal(tools[0].InputSchema, &schema); err != nil || schema["additionalProperties"] != false {
		t.Fatalf("schema %s: %v", tools[0].InputSchema, err)
	}
	call := func(args string) (string, error) {
		return tools[0].Call(context.Background(), f.root, json.RawMessage(args))
	}
	if out, err := call(`{}`); err != nil || out != "The user has no Flint project open." {
		t.Errorf("nothing open: %q, %v", out, err)
	}
	f.open()
	out, err := call(`null`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`# Project "Acme" (` + f.plan + `)`, "- api: git@github.com:acme/api.git",
		"## a: Alpha\nstatus: doing; priority: high; agent: backend; repositories: api, web\n\nDo Alpha.",
		"## b: Bravo\nstatus: todo; priority: normal\n", "1 file(s) of this project could not be read.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "## a:") > strings.Index(out, "## b:") {
		t.Error("items are not in the panel's order")
	}
	if out, _ := call(`{"status":"todo"}`); strings.Contains(out, "Alpha") || !strings.Contains(out, "Bravo") {
		t.Errorf("status filter: %s", out)
	}
	if out, _ := call(`{"project":"Other"}`); out != `No open project is named "Other".` {
		t.Errorf("name filter: %s", out)
	}
	for args, want := range map[string]string{
		`{"status":"started"}`: "status must be one of",
		`{"limit":3}`:          "invalid arguments",
		`[1]`:                  "invalid arguments",
	} {
		_, err := call(args)
		wantErr(t, err, want)
	}
}

func TestGetProjectToolIsBounded(t *testing.T) {
	f := newFixture(t, nil)
	f.open()
	body := strings.Repeat("é", 3000) // longer than an item's share, cut on a rune
	for i := range 80 {
		write(t, filepath.Join(f.plan, ItemsDir, fmt.Sprintf("i%02d.md", i)), "---\ntitle: Item "+fmt.Sprint(i)+"\n---\n"+body+"\n")
	}
	out, err := f.api.callTool(context.Background(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) > maxToolOutput+500 || !strings.Contains(out, "[…text cut]") || !strings.Contains(out, "more item(s) not shown") {
		t.Errorf("%d bytes, tail %q", len(out), out[max(0, len(out)-200):])
	}
	if !strings.Contains(out, "## i00: Item 0") {
		t.Error("the first item is missing")
	}
}

// Headers and repository lists count against the budget too: with many large
// projects the answer stays under the MCP server's limit and ends with the
// note about what was left out.
func TestGetProjectToolBoundsHeaders(t *testing.T) {
	f := newFixture(t, nil)
	f.h.SetTrusted(f.base)
	var repos []string
	for i := range MaxRepositories {
		repos = append(repos, fmt.Sprintf(`{"name":"r%d","url":"https://github.com/acme/%s%d"}`, i, strings.Repeat("a", 900), i))
	}
	config := `{"name":"Big","repositories":[` + strings.Join(repos, ",") + `]}`
	for i := range 4 {
		dir := filepath.Join(f.base, fmt.Sprintf("big%d", i))
		write(t, filepath.Join(dir, ConfigFile), config)
		write(t, filepath.Join(dir, ItemsDir, "a.md"), item("A", "todo"))
		if _, err := f.api.Open(dir); err != nil {
			t.Fatal(err)
		}
	}
	out := f.api.describe("", "")
	if len(out) > maxToolOutput || !strings.HasSuffix(out, "(3 more item(s) not shown: the answer is limited to 60 KB; ask for one status or one project.)") {
		t.Errorf("%d bytes, tail %q", len(out), out[max(0, len(out)-200):])
	}
	if strings.Count(out, "# Project ") != 1 {
		t.Errorf("%d projects shown", strings.Count(out, "# Project "))
	}
}
