package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"flint/internal/dap"
	"flint/internal/lsp"
	"flint/internal/testrun"
)

// extensionToolsApp has a Ruby language, debugger and test runner from
// extensions, and a project folder (not trusted yet) with app.rb.
func extensionToolsApp(t *testing.T) (a *App, root, file, adapter string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file = filepath.Join(root, "lib", "app.rb")
	adapter = filepath.Join(t.TempDir(), "adapter")
	if err := os.WriteFile(adapter, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	lsp.SetExtensionLanguages([]lsp.ExtensionLanguage{{Language: "ruby", Label: "Ruby", Extensions: []string{".rb"}, Source: "me.ruby"}})
	dap.SetExtensionDebuggers([]dap.ExtensionDebugger{{
		Type: "rdbg", Label: "Ruby (rdbg)", Languages: []string{"ruby"}, Source: "me.ruby",
		Candidates: [][]string{{"flint-no-such-rdbg"}, {adapter, "--file=${fileBasename}"}}, Install: "gem install debug",
		Launch: []byte(`{"script":"${file}","cwd":"${workspaceFolder}"}`),
	}})
	testrun.SetExtensionRunners([]testrun.ExtensionRunner{{
		ID: "rspec", Label: "RSpec", Languages: []string{"ruby"}, Command: []string{adapter, "${file}"}, Format: testrun.FormatTAP, Source: "me.ruby",
	}})
	t.Cleanup(func() {
		lsp.SetExtensionLanguages(nil)
		dap.SetExtensionDebuggers(nil)
		testrun.SetExtensionRunners(nil)
	})
	return &App{configDir: t.TempDir()}, root, file, adapter
}

func wantError(t *testing.T, err error, parts ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("no error, want one with %q", parts)
	}
	for _, p := range parts {
		if !strings.Contains(err.Error(), p) {
			t.Errorf("error %q lacks %q", err, p)
		}
	}
}

func TestExtensionDebuggerOnlyInTrustedFolders(t *testing.T) {
	a, root, file, adapter := extensionToolsApp(t)
	_, _, _, err := a.extensionAdapter(root, file)
	wantError(t, err, "Debugging app.rb is off in Restricted Mode", "Ruby (rdbg) (from the extension me.ruby) runs this project's code", "Run Trust Folder")

	if err := a.setTrust(root, true); err != nil {
		t.Fatal(err)
	}
	ad, launch, label, err := a.extensionAdapter(root, file)
	if err != nil {
		t.Fatal(err)
	}
	if label != "Ruby (rdbg)" || ad.ID != "rdbg" || ad.Dir != root || len(ad.Argv) != 2 || ad.Argv[0] != adapter || ad.Argv[1] != "--file=app.rb" {
		t.Errorf("adapter = %+v, label %q", ad, label)
	}
	if want := `{"cwd":"` + root + `","script":"` + file + `"}`; string(launch) != want {
		t.Errorf("launch = %s, want %s", launch, want)
	}
}

func TestExtensionDebuggerMessages(t *testing.T) {
	a, root, _, _ := extensionToolsApp(t)
	if err := a.setTrust(root, true); err != nil {
		t.Fatal(err)
	}
	_, _, _, err := a.extensionAdapter(root, filepath.Join(root, "notes.xyz"))
	wantError(t, err, "Can't debug notes.xyz: Flint doesn't know its language", "Settings ▸ Extensions ▸ Repository")

	_, _, _, err = a.extensionAdapter(root, filepath.Join(root, "main.py"))
	wantError(t, err, "Can't debug main.py: no extension that debugs Python files is installed and on", "Settings ▸ Extensions ▸ Repository")

	_, _, _, err = a.extensionAdapter(root, filepath.Join(root, "main.go"))
	wantError(t, err, "Flint debugs Go itself", "click Debug")

	_, _, _, err = a.extensionAdapter(root, "/elsewhere/app.rb")
	wantError(t, err, "app.rb is not inside the project folder")

	dap.SetExtensionDebuggers([]dap.ExtensionDebugger{{Type: "rdbg", Label: "Ruby (rdbg)", Languages: []string{"ruby"}, Candidates: [][]string{{"flint-no-such-rdbg", "--open"}}, Install: "gem install debug"}})
	_, _, _, err = a.extensionAdapter(root, filepath.Join(root, "app.rb"))
	wantError(t, err, "Can't debug app.rb: Ruby (rdbg) needs flint-no-such-rdbg, which isn't installed or isn't on your PATH. Install it with `gem install debug`, then try again.")
}

func TestExtensionTestRunnerOnlyInTrustedFolders(t *testing.T) {
	a, root, file, adapter := extensionToolsApp(t)
	req := testrun.Request{Lang: testrun.LangExt, Dirs: []string{root}, File: file}
	_, err := a.planExtensionTests(req)
	wantError(t, err, "Running the tests of app.rb is off in Restricted Mode", "RSpec", "Run Trust Folder")

	if err := a.setTrust(root, true); err != nil {
		t.Fatal(err)
	}
	plan, err := a.planExtensionTests(req)
	if err != nil {
		t.Fatal(err)
	}
	if s := plan.Steps[0]; s.Program != adapter || len(s.Args) != 1 || s.Args[0] != file || plan.Format != testrun.FormatTAP {
		t.Errorf("plan = %+v", plan)
	}

	_, err = a.planExtensionTests(testrun.Request{Lang: testrun.LangExt, Dirs: []string{root}, File: filepath.Join(root, "x.py")})
	wantError(t, err, "Can't run the tests of x.py: no extension that runs Python tests is installed and on", "Settings ▸ Extensions ▸ Repository")
}
