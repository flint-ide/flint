package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestMain lets the test binary stand in for a language server: started
// with FLINT_FAKE_LSP set, it speaks LSP on stdin/stdout instead of running
// the tests.
func TestMain(m *testing.M) {
	if os.Getenv("FLINT_FAKE_LSP") != "" {
		runFakeServer()
		return
	}
	os.Exit(m.Run())
}

const fakeEdit = `{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":3}},"newText":"new"}`

// runFakeServer answers the requests the feature tests send. With
// FLINT_FAKE_LSP=bare it announces no capabilities.
func runFakeServer() {
	var c *conn
	docVersion := 0
	handle := func(method string, params json.RawMessage) (any, error) {
		raw := func(s string) (any, error) { return json.RawMessage(s), nil }
		switch method {
		case "initialize":
			if os.Getenv("FLINT_FAKE_LSP") == "bare" {
				return raw(`{"capabilities":{}}`)
			}
			return raw(`{"capabilities":{
				"renameProvider":{"prepareProvider":true},
				"codeActionProvider":{"resolveProvider":true},
				"documentFormattingProvider":true,
				"documentSymbolProvider":true,
				"workspaceSymbolProvider":true,
				"implementationProvider":true,
				"typeDefinitionProvider":false,
				"signatureHelpProvider":{"triggerCharacters":["(",","],"retriggerCharacters":[",","xx"]}}}`)
		case "textDocument/didOpen", "textDocument/didChange":
			var p struct {
				TextDocument struct {
					URI     string `json:"uri"`
					Version int    `json:"version"`
				} `json:"textDocument"`
			}
			_ = json.Unmarshal(params, &p)
			docVersion = p.TextDocument.Version
			if method == "textDocument/didOpen" {
				_ = c.Notify("textDocument/publishDiagnostics", json.RawMessage(`{"uri":"`+p.TextDocument.URI+`","diagnostics":[
					{"range":{"start":{"line":2,"character":0},"end":{"line":2,"character":5}},"severity":1,"message":"unused","code":"U1000","data":{"k":1}},
					{"range":{"start":{"line":9,"character":0},"end":{"line":9,"character":1}},"severity":2,"message":"elsewhere"}]}`))
			}
			return nil, nil
		case "textDocument/prepareRename":
			return raw(`{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":3}},"placeholder":"old"}`)
		case "textDocument/rename":
			var p struct {
				TextDocument struct{ URI string } `json:"textDocument"`
				NewName      string               `json:"newName"`
			}
			_ = json.Unmarshal(params, &p)
			other := strings.TrimSuffix(p.TextDocument.URI, "main.go") + "other.go"
			edit := `{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":3}},"newText":"` + p.NewName + `"}`
			return raw(`{"documentChanges":[
				{"textDocument":{"uri":"` + p.TextDocument.URI + `","version":` + itoa(docVersion) + `},"edits":[` + edit + `]},
				{"textDocument":{"uri":"` + other + `","version":null},"edits":[` + edit + `]}]}`)
		case "textDocument/formatting":
			return raw(`[` + fakeEdit + `]`)
		case "textDocument/codeAction":
			var p struct {
				TextDocument struct{ URI string } `json:"textDocument"`
				Context      struct {
					Diagnostics []json.RawMessage `json:"diagnostics"`
					Only        []string          `json:"only"`
				} `json:"context"`
			}
			_ = json.Unmarshal(params, &p)
			uri := p.TextDocument.URI
			if len(p.Context.Only) == 1 && p.Context.Only[0] == "source.organizeImports" {
				return raw(`[{"title":"Organize Imports","kind":"source.organizeImports","edit":{"changes":{"` + uri + `":[` + fakeEdit + `]}}}]`)
			}
			// The diagnostics Flint sends back prove it kept the server's own (code, data).
			echo, _ := json.Marshal(string(mustJSON(p.Context.Diagnostics)))
			return raw(`[
				{"title":"Inline edit","kind":"quickfix","isPreferred":true,"edit":{"changes":{"` + uri + `":[` + fakeEdit + `]}}},
				{"title":"Needs resolve","kind":"refactor.extract","data":{"uri":"` + uri + `"}},
				{"title":"Edit then command","kind":"quickfix","edit":{"changes":{"` + uri + `":[` + fakeEdit + `]}},"command":{"title":"c","command":"fake.apply","arguments":["` + uri + `"]}},
				{"title":"Bare command","command":"fake.apply","arguments":["` + uri + `"]},
				{"title":"Disabled","kind":"refactor","disabled":{"reason":"not here"}},
				{"title":"Echo","kind":"quickfix","data":` + string(echo) + `}]`)
		case "codeAction/resolve":
			var p struct {
				Title string          `json:"title"`
				Data  json.RawMessage `json:"data"`
			}
			_ = json.Unmarshal(params, &p)
			var d struct{ URI string }
			_ = json.Unmarshal(p.Data, &d)
			return raw(`{"title":"Needs resolve","edit":{"changes":{"` + d.URI + `":[` + fakeEdit + `]}}}`)
		case "workspace/executeCommand":
			var p struct {
				Arguments []string `json:"arguments"`
			}
			_ = json.Unmarshal(params, &p)
			var res struct {
				Applied bool `json:"applied"`
			}
			err := c.Call(context.Background(), "workspace/applyEdit", json.RawMessage(
				`{"edit":{"changes":{"`+p.Arguments[0]+`":[{"range":{"start":{"line":1,"character":0},"end":{"line":1,"character":0}},"newText":"cmd"}]}}}`), &res)
			if err != nil || !res.Applied {
				return nil, errors.New("edit not applied")
			}
			return nil, nil
		case "textDocument/documentSymbol":
			return raw(`[{"name":"T","kind":23,"range":{"start":{"line":0,"character":0},"end":{"line":5,"character":1}},"selectionRange":{"start":{"line":0,"character":5},"end":{"line":0,"character":6}},
				"children":[{"name":"field","detail":"int","kind":8,"range":{"start":{"line":1,"character":1},"end":{"line":1,"character":10}},"selectionRange":{"start":{"line":1,"character":1},"end":{"line":1,"character":6}}}]},
				{"name":"main","kind":12,"range":{"start":{"line":7,"character":0},"end":{"line":9,"character":1}},"selectionRange":{"start":{"line":7,"character":5},"end":{"line":7,"character":9}}}]`)
		case "workspace/symbol":
			return raw(`[{"name":"main","kind":12,"containerName":"demo","location":{"uri":"file:///w/main.go","range":{"start":{"line":7,"character":5},"end":{"line":7,"character":9}}}},
				{"name":"nofile","kind":12,"location":{"uri":"untitled:1"}}]`)
		case "textDocument/signatureHelp":
			return raw(`{"signatures":[{"label":"f(a int, a string)","documentation":{"kind":"markdown","value":"docs"},"parameters":[{"label":"a int"},{"label":[9,17],"documentation":"second"}]}],"activeSignature":0,"activeParameter":1}`)
		case "textDocument/implementation":
			return raw(`[{"uri":"file:///w/impl.go","range":{"start":{"line":3,"character":0},"end":{"line":3,"character":4}}}]`)
		case "shutdown":
			return nil, nil
		case "exit":
			os.Exit(0)
		}
		return nil, nil
	}
	c = newConn(os.Stdin, os.Stdout, handle)
	<-c.done
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

// fakeManager starts a manager whose Go server is the fake above, with
// main.go open in a temporary folder.
func fakeManager(t *testing.T, mode string) (m *Manager, root, path string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "gopls")
	script := "#!/bin/sh\nFLINT_FAKE_LSP=" + mode + " exec " + exe + "\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	root = t.TempDir()
	path = filepath.Join(root, "main.go")
	m = NewManager()
	m.lookPath = func(name string) (string, error) {
		if name == "gopls" {
			return bin, nil
		}
		return "", os.ErrNotExist
	}
	t.Cleanup(m.Shutdown)
	m.Open(root, path, "old text\nline two\n")
	return m, root, path
}

func testCtx(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestRename(t *testing.T) {
	m, root, path := fakeManager(t, "full")
	ctx := testCtx(t)

	target, err := m.PrepareRename(ctx, path, Position{})
	if err != nil || !target.Known || target.Placeholder != "old" || target.Range.End.Character != 3 {
		t.Fatalf("prepare = %+v, %v", target, err)
	}
	edit, err := m.Rename(ctx, path, Position{}, "fresh")
	if err != nil || len(edit.Files) != 2 {
		t.Fatalf("rename = %+v, %v", edit, err)
	}
	if f := edit.Files[0]; f.Path != path || !f.Open || f.Edits[0].NewText != "fresh" {
		t.Errorf("open file edit = %+v", f)
	}
	if f := edit.Files[1]; f.Path != filepath.Join(root, "other.go") || f.Open {
		t.Errorf("closed file edit = %+v", f)
	}
	for _, name := range []string{"", "two\nlines", strings.Repeat("x", 600)} {
		if _, err := m.Rename(ctx, path, Position{}, name); err == nil {
			t.Errorf("name %q accepted", name)
		}
	}
}

func TestFormat(t *testing.T) {
	m, _, path := fakeManager(t, "full")
	edits, err := m.Format(testCtx(t), path, 4, false)
	if err != nil || len(edits) != 1 || edits[0].NewText != "new" {
		t.Fatalf("format = %+v, %v", edits, err)
	}
}

func TestCodeActions(t *testing.T) {
	m, _, path := fakeManager(t, "full")
	ctx := testCtx(t)
	var mu sync.Mutex
	var pushed []WorkspaceEdit
	m.OnApplyEdit = func(e WorkspaceEdit) { mu.Lock(); pushed = append(pushed, e); mu.Unlock() }

	// The diagnostic on line 2 arrives after didOpen; ask until it is kept.
	var actions []CodeAction
	waitFor(t, func() bool {
		var err error
		actions, err = m.CodeActions(ctx, path, r(2, 1, 2, 1), nil)
		if err != nil {
			t.Fatal(err)
		}
		return len(actions) == 6 && strings.Contains(m.actionRaw(path, actions[5].ID), "U1000")
	})
	echo := m.actionRaw(path, actions[5].ID)
	if !strings.Contains(echo, `\"data\":{\"k\":1}`) || strings.Contains(echo, "elsewhere") {
		t.Errorf("diagnostics sent with the request = %s", echo)
	}
	if !actions[0].Preferred || actions[0].Kind != "quickfix" || actions[4].Disabled != "not here" {
		t.Errorf("actions = %+v", actions)
	}

	apply := func(i int) WorkspaceEdit {
		t.Helper()
		e, err := m.ApplyCodeAction(ctx, path, actions[i].ID)
		if err != nil {
			t.Fatalf("apply %q: %v", actions[i].Title, err)
		}
		return e
	}
	if e := apply(0); len(e.Files) != 1 || e.Files[0].Edits[0].NewText != "new" || !e.Files[0].Open {
		t.Errorf("inline edit = %+v", e)
	}
	if e := apply(1); len(e.Files) != 1 || e.Files[0].Edits[0].NewText != "new" {
		t.Errorf("resolved edit = %+v", e)
	}
	// The action's own edit comes first, then what its command applied.
	if e := apply(2); len(e.Files) != 2 || e.Files[0].Edits[0].NewText != "new" || e.Files[1].Edits[0].NewText != "cmd" {
		t.Errorf("edit then command = %+v", e)
	}
	if e := apply(3); len(e.Files) != 1 || e.Files[0].Edits[0].NewText != "cmd" {
		t.Errorf("bare command = %+v", e)
	}
	mu.Lock()
	if len(pushed) != 0 {
		t.Errorf("edits of a code action's command were pushed to the UI: %+v", pushed)
	}
	mu.Unlock()

	// A new list replaces the old ids.
	if _, err := m.CodeActions(ctx, path, r(2, 1, 2, 1), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ApplyCodeAction(ctx, path, actions[0].ID); err == nil || !strings.Contains(err.Error(), "out of date") {
		t.Errorf("stale id: %v", err)
	}

	e, err := m.SourceAction(ctx, path, "source.organizeImports")
	if err != nil || len(e.Files) != 1 {
		t.Errorf("organize imports = %+v, %v", e, err)
	}
}

// actionRaw is the stored server action, for tests.
func (m *Manager) actionRaw(path string, id int) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return string(m.actions[path][id].raw)
}

func TestSymbolsAndSignatures(t *testing.T) {
	m, root, path := fakeManager(t, "full")
	ctx := testCtx(t)

	syms, err := m.DocumentSymbols(ctx, path)
	if err != nil || len(syms) != 3 {
		t.Fatalf("symbols = %+v, %v", syms, err)
	}
	if syms[1].Name != "field" || syms[1].Depth != 1 || syms[1].Container != "T" || syms[1].Detail != "int" || syms[2].Depth != 0 || syms[0].Selection.Start.Character != 5 {
		t.Errorf("symbols = %+v", syms)
	}

	ws, err := m.WorkspaceSymbols(ctx, []string{root}, "ma")
	if err != nil || len(ws) != 1 || ws[0].Path != "/w/main.go" || ws[0].Container != "demo" {
		t.Errorf("workspace symbols = %+v, %v", ws, err)
	}
	if ws, _ := m.WorkspaceSymbols(ctx, []string{"/elsewhere"}, "ma"); len(ws) != 0 {
		t.Errorf("symbols from a folder that wasn't asked for: %+v", ws)
	}

	help, err := m.SignatureHelp(ctx, path, Position{})
	if err != nil || len(help.Signatures) != 1 || help.ActiveParameter != 1 {
		t.Fatalf("signature help = %+v, %v", help, err)
	}
	ps := help.Signatures[0].Parameters
	if ps[0].Start != 2 || ps[0].End != 7 || ps[1].Start != 9 || ps[1].End != 17 || ps[1].Documentation != "second" || help.Signatures[0].Documentation != "docs" {
		t.Errorf("parameters = %+v", ps)
	}
	if got := m.SignatureTriggers(ctx, path); strings.Join(got, "") != "(," {
		t.Errorf("triggers = %q", got)
	}

	impl, err := m.Implementation(ctx, path, Position{})
	if err != nil || len(impl) != 1 || impl[0].Path != "/w/impl.go" {
		t.Errorf("implementation = %+v, %v", impl, err)
	}
	if _, err := m.TypeDefinition(ctx, path, Position{}); !errors.Is(err, ErrUnsupported) {
		t.Errorf("type definition with the capability off: %v", err)
	}
}

func TestServerWithoutCapabilities(t *testing.T) {
	m, root, path := fakeManager(t, "bare")
	ctx := testCtx(t)
	if _, err := m.Rename(ctx, path, Position{}, "x"); !errors.Is(err, ErrUnsupported) {
		t.Errorf("rename: %v", err)
	}
	if _, err := m.PrepareRename(ctx, path, Position{}); !errors.Is(err, ErrUnsupported) {
		t.Errorf("prepare rename: %v", err)
	}
	if _, err := m.Format(ctx, path, 4, true); !errors.Is(err, ErrUnsupported) {
		t.Errorf("format: %v", err)
	}
	if _, err := m.CodeActions(ctx, path, Range{}, nil); !errors.Is(err, ErrUnsupported) {
		t.Errorf("code actions: %v", err)
	}
	if _, err := m.DocumentSymbols(ctx, path); !errors.Is(err, ErrUnsupported) {
		t.Errorf("document symbols: %v", err)
	}
	if help, err := m.SignatureHelp(ctx, path, Position{}); err != nil || len(help.Signatures) != 0 {
		t.Errorf("signature help: %+v, %v", help, err)
	}
	if ws, err := m.WorkspaceSymbols(ctx, []string{root}, "x"); err != nil || len(ws) != 0 {
		t.Errorf("workspace symbols: %+v, %v", ws, err)
	}
}

func TestApplyToDisk(t *testing.T) {
	m, root, path := fakeManager(t, "full")
	if _, err := m.DocumentSymbols(testCtx(t), path); err != nil { // the server is up
		t.Fatal(err)
	}
	other := filepath.Join(root, "other.go")
	write := func(p, s string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(other, "old name\r\nrest\r\n")
	edits := []TextEdit{{r(0, 0, 0, 3), "new"}}
	if err := m.ApplyToDisk(other, edits); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(other)
	if string(data) != "new name\r\nrest\r\n" {
		t.Errorf("file = %q", data)
	}
	if info, _ := os.Stat(other); info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v", info.Mode())
	}

	outside := filepath.Join(t.TempDir(), "x.go")
	write(outside, "old")
	git := filepath.Join(root, ".git", "config")
	write(git, "old")
	link := filepath.Join(root, "link.go")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "bin.go")
	write(bin, "old\xff\xfe")
	for name, p := range map[string]string{
		"open in an editor": path, "outside the folder": outside, "inside .git": git, "a symlink": link,
		"not UTF-8": bin, "relative": "other.go", "unclean": root + "/sub/../other.go", "missing": filepath.Join(root, "none.go"),
	} {
		if err := m.ApplyToDisk(p, edits); err == nil {
			t.Errorf("%s: edit applied to %s", name, p)
		}
	}
	for _, p := range []string{outside, git} {
		if data, _ := os.ReadFile(p); string(data) != "old" {
			t.Errorf("%s was changed", p)
		}
	}
}

// An edit the server sends on its own goes to the UI; one made for an older
// version of an open file is refused.
func TestServerInitiatedEdit(t *testing.T) {
	m, _, path := fakeManager(t, "full")
	var got []WorkspaceEdit
	m.OnApplyEdit = func(e WorkspaceEdit) { got = append(got, e) }
	uri := PathToURI(path)
	edit := func(version string) json.RawMessage {
		return json.RawMessage(`{"documentChanges":[{"textDocument":{"uri":"` + uri + `","version":` + version + `},"edits":[` + fakeEdit + `]}]}`)
	}
	if err := m.applyEdit(edit("1")); err != nil || len(got) != 1 || !got[0].Files[0].Open {
		t.Fatalf("edit = %+v, %v", got, err)
	}
	m.Change(path, "newer text")
	if err := m.applyEdit(edit("1")); !errors.Is(err, ErrStaleEdit) || len(got) != 1 {
		t.Fatalf("stale edit: %v, %d pushed", err, len(got))
	}
}
