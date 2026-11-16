package lsp

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var errNotFound = errors.New("not found")

const sample = `package main

import "fmt"

func greet() string { return "hi" }

func main() {
	fmt.Println(greet())
	fmt.
}
`

// TestGoplsEndToEnd drives a real gopls (skipped when it isn't installed).
func TestGoplsEndToEnd(t *testing.T) {
	gopls, err := exec.LookPath("gopls")
	if err != nil {
		if home, _ := os.UserHomeDir(); home != "" {
			gopls = filepath.Join(home, "go", "bin", "gopls")
		}
		if _, err := os.Stat(gopls); err != nil {
			t.Skip("gopls not installed")
		}
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module demo\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "main.go")
	if err := os.WriteFile(path, []byte(sample), 0o644); err != nil {
		t.Fatal(err)
	}

	m := NewManager()
	m.lookPath = func(name string) (string, error) {
		if name == "gopls" {
			return gopls, nil
		}
		return "", errNotFound
	}
	var mu sync.Mutex
	diags := map[string][]Diagnostic{}
	m.OnDiagnostics = func(p string, d []Diagnostic) {
		mu.Lock()
		diags[p] = d
		mu.Unlock()
	}
	defer m.Shutdown()

	if st := m.Open(root, path, sample); st.State != StateStarting && st.State != StateReady {
		t.Fatalf("open status = %+v", st)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// "fmt." is on line 8 (0-based), cursor after the dot.
	list, err := m.Completion(ctx, path, Position{Line: 8, Character: 5})
	if err != nil {
		t.Fatalf("completion: %v", err)
	}
	if !hasLabel(list, "Println") {
		t.Errorf("completion lacks Println: %d items", len(list.Items))
	}

	// Definition of greet() called on line 7.
	defs, err := m.Definition(ctx, path, Position{Line: 7, Character: 15})
	if err != nil || len(defs) != 1 || defs[0].Range.Start.Line != 4 || !strings.Contains(defs[0].Preview, "func greet") {
		t.Errorf("definition = %+v, %v", defs, err)
	}

	refs, err := m.References(ctx, path, Position{Line: 4, Character: 6})
	if err != nil || len(refs) != 1 || refs[0].Range.Start.Line != 7 {
		t.Errorf("references = %+v, %v", refs, err)
	}

	hover, err := m.Hover(ctx, path, Position{Line: 7, Character: 6})
	if err != nil || !strings.Contains(hover, "Println") {
		t.Errorf("hover = %q, %v", hover, err)
	}

	// The outline lists both functions, in order.
	syms, err := m.DocumentSymbols(ctx, path)
	if err != nil || len(syms) != 2 || syms[0].Name != "greet" || syms[1].Name != "main" {
		t.Errorf("document symbols = %+v, %v", syms, err)
	}
	ws, err := m.WorkspaceSymbols(ctx, []string{root}, "greet")
	if err != nil || len(ws) == 0 || ws[0].Name != "greet" || ws[0].Path != path {
		t.Errorf("workspace symbols = %+v, %v", ws, err)
	}
	// Renaming greet changes its declaration and its call, in this open file.
	target, err := m.PrepareRename(ctx, path, Position{Line: 4, Character: 6})
	if err != nil || !target.Known || target.Range.Start.Character != 5 {
		t.Errorf("prepare rename = %+v, %v", target, err)
	}
	edit, err := m.Rename(ctx, path, Position{Line: 4, Character: 6}, "salute")
	if err != nil || len(edit.Files) != 1 || !edit.Files[0].Open || len(edit.Files[0].Edits) != 2 {
		t.Errorf("rename = %+v, %v", edit, err)
	} else if renamed, err := ApplyTextEdits(sample, edit.Files[0].Edits); err != nil || strings.Count(renamed, "salute()") != 2 || strings.Contains(renamed, "greet") {
		t.Errorf("renamed text = %q, %v", renamed, err)
	}
	help, err := m.SignatureHelp(ctx, path, Position{Line: 7, Character: 13})
	if err != nil || len(help.Signatures) == 0 || help.Signatures[0].Label == "" {
		t.Errorf("signature help = %+v, %v", help, err)
	}

	// The dangling "fmt." is a syntax error gopls must report.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(diags[path])
		mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	mu.Lock()
	if len(diags[path]) == 0 {
		t.Error("no diagnostics for a file with a syntax error")
	}
	mu.Unlock()

	// After fixing the file the error disappears.
	fixed := strings.Replace(sample, "\tfmt.\n", "", 1)
	m.Change(path, fixed)
	deadline = time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(diags[path])
		mu.Unlock()
		if n == 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	mu.Lock()
	if len(diags[path]) != 0 {
		t.Errorf("diagnostics not cleared after fix: %+v", diags[path])
	}
	mu.Unlock()

	// gofmt through gopls: badly indented code gets its tab back, and a
	// quick fix removes an unused variable.
	messy := strings.Replace(fixed, "\tfmt.Println", "      var unused int\n      fmt.Println", 1)
	m.Change(path, messy)
	edits, err := m.Format(ctx, path, 4, false)
	if err != nil {
		t.Fatalf("format: %v", err)
	}
	if formatted, err := ApplyTextEdits(messy, edits); err != nil || !strings.Contains(formatted, "\n\tfmt.Println(greet())\n") {
		t.Errorf("formatted = %q, %v", formatted, err)
	}
	var removed string
	deadline = time.Now().Add(30 * time.Second)
	for removed == "" && time.Now().Before(deadline) {
		actions, err := m.CodeActions(ctx, path, Range{Start: Position{Line: 7, Character: 12}, End: Position{Line: 7, Character: 12}}, []string{"quickfix"})
		if err != nil {
			t.Fatalf("code actions: %v", err)
		}
		for _, a := range actions {
			if strings.Contains(a.Title, "Remove") {
				edit, err := m.ApplyCodeAction(ctx, path, a.ID)
				if err != nil || len(edit.Files) != 1 {
					t.Fatalf("apply %q = %+v, %v", a.Title, edit, err)
				}
				if removed, err = ApplyTextEdits(messy, edit.Files[0].Edits); err != nil {
					t.Fatal(err)
				}
			}
		}
		time.Sleep(100 * time.Millisecond) // the diagnostic the fix belongs to isn't published yet
	}
	if removed == "" || strings.Contains(removed, "unused") {
		t.Errorf("after the quick fix: %q", removed)
	}

	if sts := m.Statuses(); len(sts) != 1 || sts[0].State != StateReady {
		t.Errorf("statuses = %+v", sts)
	}
	m.Close(path)
	if _, err := m.Completion(ctx, path, Position{}); err == nil {
		t.Error("requests on a closed document must fail")
	}
}

func TestUnavailableServer(t *testing.T) {
	m := NewManager()
	m.lookPath = func(string) (string, error) { return "", errNotFound }
	var mu sync.Mutex
	var last Status
	m.OnStatus = func(s Status) { mu.Lock(); last = s; mu.Unlock() }

	path := filepath.Join(t.TempDir(), "x.py")
	m.Open(filepath.Dir(path), path, "print(1)\n")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := m.Completion(ctx, path, Position{})
	if err == nil || !strings.Contains(err.Error(), "pipx install basedpyright") {
		t.Errorf("err = %v, want install hint", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if last.State != StateUnavailable || last.Language != "python" {
		t.Errorf("status = %+v", last)
	}
}

func hasLabel(list CompletionList, label string) bool {
	for _, it := range list.Items {
		if it.Label == label {
			return true
		}
	}
	return false
}
