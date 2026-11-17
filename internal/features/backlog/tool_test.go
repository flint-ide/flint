package backlog

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetBacklogTool(t *testing.T) {
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
		return tools[0].Call(context.Background(), f.project, json.RawMessage(args))
	}
	if out, err := call(`{}`); err != nil || out != "No backlog is open in Flint." {
		t.Errorf("nothing open: %q, %v", out, err)
	}
	f.open()
	out, err := call(`null`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`# Backlog "Acme" (` + f.plan + `)`, "- api: git@github.com:acme/api.git",
		"## a: Alpha\nstatus: doing; priority: high; agent: backend; repositories: api, web\n\nDo Alpha.",
		"## b: Bravo\nstatus: todo; priority: normal\n", "1 file(s) of this backlog could not be read.",
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
	if out, _ := call(`{"backlog":"Other"}`); out != `No open backlog is named "Other".` {
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

func TestGetBacklogToolIsBounded(t *testing.T) {
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
