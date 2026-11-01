package mcptools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"flint/internal/kube"
	"flint/internal/lsp"
	"flint/internal/mcpserver"
	"flint/internal/testrun"
)

var allOn = Config{Diagnostics: true, Editor: true, Terminals: true, Tests: true, Kubernetes: true, TerminalLines: 200}

func toolNamed(t *testing.T, s *Sources, cfg Config, name string) mcpserver.Tool {
	t.Helper()
	tools := s.Tools(cfg)
	i := slices.IndexFunc(tools, func(x mcpserver.Tool) bool { return x.Name == name })
	if i < 0 {
		t.Fatalf("tool %s not offered", name)
	}
	return tools[i]
}

func run(t *testing.T, s *Sources, name, root, args string) (string, error) {
	t.Helper()
	tool := toolNamed(t, s, allOn, name)
	return tool.Call(context.Background(), root, json.RawMessage(args))
}

func TestToolsFollowConfigAndHaveSchemas(t *testing.T) {
	s := NewSources()
	s.Kube = func() (Kube, error) { return nil, errors.New("no kubectl") }
	tools := s.Tools(allOn)
	if len(tools) != len(Names) {
		t.Fatalf("%d tools", len(tools))
	}
	for _, tool := range tools {
		var schema map[string]any
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil || schema["type"] != "object" {
			t.Errorf("%s: schema %s (%v)", tool.Name, tool.InputSchema, err)
		}
		if tool.Description == "" || !slices.Contains(Names, tool.Name) {
			t.Errorf("%s: no description or unknown name", tool.Name)
		}
	}
	if got := s.Tools(Config{Tests: true}); len(got) != 1 || got[0].Name != ToolTests {
		t.Fatalf("only tests: %+v", got)
	}
	s.Kube = nil
	if got := s.Tools(Config{Kubernetes: true}); len(got) != 0 {
		t.Fatal("kubernetes offered without a reader")
	}
}

func TestDiagnosticsTool(t *testing.T) {
	s := NewSources()
	at := func(line, col int) lsp.Range { return lsp.Range{Start: lsp.Position{Line: line, Character: col}} }
	s.Diagnostics.Set("/p/b/x.go", []lsp.Diagnostic{
		{Range: at(9, 0), Severity: 2, Message: "unused\nvariable", Source: "compiler"},
		{Range: at(1, 4), Severity: 1, Message: "undefined: foo"},
	})
	s.Diagnostics.Set("/p/a.go", []lsp.Diagnostic{{Range: at(0, 0), Severity: 4, Message: "hint"}})
	s.Diagnostics.Set("/q/z.go", []lsp.Diagnostic{{Severity: 1, Message: "other project"}})

	out, err := run(t, s, ToolDiagnostics, "/p", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"3 problems in 2 files", "a.go:1:1: hint: hint", "b/x.go:2:5: error: undefined: foo", "b/x.go:10:1: warning: unused variable (compiler)"}
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in:\n%s", w, out)
		}
	}
	if strings.Contains(out, "other project") || strings.Index(out, "x.go:2:5") > strings.Index(out, "x.go:10:1") {
		t.Errorf("leak or order:\n%s", out)
	}
	out, _ = run(t, s, ToolDiagnostics, "/p", `{"path":"b","severity":"error"}`)
	if !strings.Contains(out, "1 problems in 1 files of b") || strings.Contains(out, "warning:") {
		t.Errorf("filtered:\n%s", out)
	}
	for _, bad := range []string{`{"path":"../q"}`, `{"path":"/q/z.go"}`, `{"severity":"fatal"}`, `{"path":3}`} {
		if _, err := run(t, s, ToolDiagnostics, "/p", bad); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
	out, _ = run(t, s, ToolDiagnostics, "/empty", `null`)
	if !strings.HasPrefix(out, "No problems reported") {
		t.Errorf("empty: %s", out)
	}
}

func TestEditorTool(t *testing.T) {
	s := NewSources()
	out, _ := run(t, s, ToolEditor, "/p", `{}`)
	if !strings.HasPrefix(out, "No files") {
		t.Fatalf("nothing open: %s", out)
	}
	_ = s.Editor.Set(EditorSnapshot{
		Files:     []OpenFile{{Path: "/p/a.go", Dirty: true}, {Path: "/p/b.go"}},
		Active:    "/p/a.go",
		Selection: &Selection{StartLine: 3, StartColumn: 2, EndLine: 4, EndColumn: 7, Text: "x := 1\ny := 2"},
	})
	out, _ = run(t, s, ToolEditor, "/p", `{}`)
	for _, w := range []string{"Active file: a.go (unsaved", "line 3, column 2 to line 4, column 7", "x := 1\ny := 2", "- a.go (unsaved changes)", "- b.go"} {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in:\n%s", w, out)
		}
	}
	_ = s.Editor.Set(EditorSnapshot{Files: []OpenFile{{Path: "/p/a.go"}}, Active: "/p/a.go", Selection: &Selection{StartLine: 5, StartColumn: 1, EndLine: 5, EndColumn: 1}})
	if out, _ = run(t, s, ToolEditor, "/p", `{}`); !strings.Contains(out, "Cursor: line 5, column 1") {
		t.Errorf("cursor:\n%s", out)
	}
}

func TestTerminalsTool(t *testing.T) {
	s := NewSources()
	s.Terminals.Open("t1", KindTerminal, "", "/p")
	s.Terminals.SetInfo("t1", "npm run dev", "/p/web")
	s.Terminals.Write("t1", []byte("\x1b[32mready\x1b[0m on :3000\r\nError: boom\r\n"))
	s.Terminals.Open("r1", KindRun, "go test", "/p")
	for i := range 100 {
		s.Terminals.Write("r1", fmt.Appendf(nil, "line %d\n", i))
	}
	s.Terminals.Exit("r1", 1)
	s.Terminals.Open("x", KindTerminal, "", "/q")

	out, err := run(t, s, ToolTerminals, "/p", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{"## terminal t1: npm run dev (in web, running)", "ready on :3000\nError: boom", "## run r1: go test (in ., exited with code 1)", "line 99"} {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in:\n%s", w, out)
		}
	}
	if strings.Contains(out, "line 69\n") || strings.Contains(out, "## terminal x") || strings.Contains(out, "\x1b") {
		t.Errorf("list not bounded to %d lines, or leaked:\n%s", listLines, out)
	}
	out, _ = run(t, s, ToolTerminals, "/p", `{"terminal":"r1"}`)
	if !strings.Contains(out, "line 0\n") || strings.Contains(out, "t1") {
		t.Errorf("one console:\n%s", out)
	}
	out, _ = run(t, s, ToolTerminals, "/p", `{"terminal":"r1","lines":2}`)
	if !strings.Contains(out, "````\nline 98\nline 99\n````") {
		t.Errorf("two lines:\n%s", out)
	}
	if _, err := run(t, s, ToolTerminals, "/p", `{"terminal":"x"}`); err == nil || !strings.Contains(err.Error(), "t1") {
		t.Errorf("other project's console: %v", err)
	}
	limited := toolNamed(t, s, Config{Terminals: true, TerminalLines: 5}, ToolTerminals)
	out, _ = limited.Call(context.Background(), "/p", json.RawMessage(`{"terminal":"r1","lines":500}`))
	if !strings.Contains(out, "````\nline 95\n") {
		t.Errorf("setting does not cap lines:\n%s", out)
	}
	if out, _ := run(t, s, ToolTerminals, "/none", `{}`); !strings.HasPrefix(out, "No terminals") {
		t.Errorf("none: %s", out)
	}
}

func TestTestsTool(t *testing.T) {
	s := NewSources()
	start := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return start.Add(90 * time.Second) }
	if out, _ := run(t, s, ToolTests, "/p", `{}`); !strings.HasPrefix(out, "No test run") {
		t.Fatalf("none: %s", out)
	}
	s.Tests.Begin("r", "go test ./...", "/p")
	s.Tests.mu.Lock()
	s.Tests.runs[0].started = start
	s.Tests.mu.Unlock()
	s.Tests.Update("r", []testrun.Node{
		{ID: "p1", Name: "flint/a", Kind: testrun.KindPackage, Status: testrun.Failed},
		{ID: "t1", Parent: "p1", Name: "TestOK", Test: "TestOK", Kind: testrun.KindTest, Status: testrun.Passed},
		{ID: "t2", Parent: "p1", Name: "TestBad", Test: "TestBad", Kind: testrun.KindTest, Status: testrun.Failed,
			Output: "\x1b[31m    a_test.go:12: want 1, got 2\x1b[0m\n", Location: &testrun.Location{Path: "/p/a/a_test.go", Line: 10}},
		{ID: "p2", Name: "flint/b", Kind: testrun.KindPackage, Status: testrun.Failed, Output: "build failed"},
		{ID: "t3", Parent: "p1", Name: "TestSkip", Kind: testrun.KindTest, Status: testrun.Skipped},
	})
	out, _ := run(t, s, ToolTests, "/p", `{}`)
	if !strings.Contains(out, "still running") || !strings.Contains(out, "started 1m30s ago") {
		t.Errorf("running:\n%s", out)
	}
	s.Tests.Finish("r", 1, false, 2500, "")
	out, _ = run(t, s, ToolTests, "/p", `{}`)
	for _, w := range []string{"exit code 1 in 2.5s", "1 passed, 1 failed, 1 skipped", "Failed (2):", "- flint/a TestBad (a/a_test.go:10)", "a_test.go:12: want 1, got 2", "- flint/b\n````\nbuild failed"} {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in:\n%s", w, out)
		}
	}
	if strings.Contains(out, "\x1b") {
		t.Errorf("escape codes left:\n%s", out)
	}
}

type fakeKube struct {
	contexts []kube.Context
	ov       kube.Overview
	asked    []string
}

func (f *fakeKube) Contexts(context.Context) ([]kube.Context, error) { return f.contexts, nil }
func (f *fakeKube) Overview(_ context.Context, c, ns string) (kube.Overview, error) {
	f.asked = append(f.asked, c+"/"+ns)
	return f.ov, nil
}

func TestKubernetesTool(t *testing.T) {
	fk := &fakeKube{
		contexts: []kube.Context{{Name: "other"}, {Name: "dev", Cluster: "kind-dev", Namespace: "shop", Current: true}},
		ov: kube.Overview{
			Apps: []kube.App{
				{Name: "web", Source: "helm", Health: kube.HealthBad, Problems: []string{"0/2 pods ready"}},
				{Name: "db", Source: "label", Health: kube.HealthOK},
			},
			Workloads: []kube.Workload{
				{Kind: "Deployment", Name: "web", Ready: 0, Desired: 2, Rollout: kube.RolloutFailed, RolloutMessage: "deadline exceeded"},
				{Kind: "Deployment", Name: "db", Ready: 1, Desired: 1, Rollout: kube.RolloutComplete},
			},
			Pods: []kube.Pod{
				{Name: "web-1", Status: "CrashLoopBackOff", Ready: 0, Total: 1, Restarts: 7, Problem: "OOMKilled (exit 137) in app"},
				{Name: "db-0", Status: "Running", Healthy: true, Ready: 1, Total: 1},
			},
			Jobs:    []kube.Job{{Kind: "Job", Name: "migrate", Status: "failed (3)"}, {Kind: "Job", Name: "ok", Status: "complete"}},
			Events:  []kube.Event{{Kind: "Pod", Name: "web-1", Reason: "BackOff", Message: "Back-off restarting", Count: 12, LastSeen: "1m"}},
			Partial: []string{"ingresses"},
		},
	}
	s := NewSources()
	s.Kube = func() (Kube, error) { return fk, nil }
	out, err := run(t, s, ToolKubernetes, "/p", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{
		"Context: dev (cluster kind-dev), namespace: shop", "Apps: 2 (1 ok, 0 warning, 1 failing)",
		"- web (helm, bad): 0/2 pods ready", "- Deployment/web: 0/2 ready, rollout failed deadline exceeded",
		"- web-1: CrashLoopBackOff, 0/1 ready, 7 restarts — OOMKilled", "- Job/migrate: failed (3)",
		"- Pod/web-1 BackOff (×12, 1m): Back-off restarting", "Not readable (permissions or missing CRDs): ingresses",
	} {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in:\n%s", w, out)
		}
	}
	if strings.Contains(out, "db-0") || strings.Contains(out, "Deployment/db") || strings.Contains(out, "Job/ok") {
		t.Errorf("healthy objects listed:\n%s", out)
	}
	if _, err := run(t, s, ToolKubernetes, "/p", `{"namespace":"kube-system"}`); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(fk.asked, []string{"dev/shop", "dev/kube-system"}) {
		t.Errorf("asked %v", fk.asked)
	}
	if _, err := run(t, s, ToolKubernetes, "/p", `{"namespace":"--all"}`); err == nil {
		t.Error("invalid namespace accepted")
	}
	fk.contexts = []kube.Context{{Name: "x"}}
	if _, err := run(t, s, ToolKubernetes, "/p", `{}`); err == nil {
		t.Error("no current context accepted")
	}
}
