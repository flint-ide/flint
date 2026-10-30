package main

import (
	"os"
	"path/filepath"
	"testing"

	"flint/internal/runconfig"
	"flint/internal/testrun"
)

func TestProfileName(t *testing.T) {
	for in, want := range map[string]string{
		"http://localhost:6060/debug/pprof/profile?seconds=10": "cpu.pprof",
		"http://localhost:6060/debug/pprof/heap":               "heap.pprof",
		"http://localhost:6060/":                               "cpu.pprof",
	} {
		if got := profileName(in); got != want {
			t.Errorf("profileName(%s) = %s, want %s", in, got, want)
		}
	}
}

func TestProfileAPIRejects(t *testing.T) {
	app := &App{profiles: newProfiles()}
	api := &ProfileAPI{app: app}
	if _, err := api.Collect("x", "run", runconfig.Config{Name: "api"}); err == nil {
		t.Error("collect without profileURL")
	}
	if _, err := api.Collect("x", "run", runconfig.Config{Name: "api", ProfileURL: "http://example.com/debug/pprof/profile"}); err == nil {
		t.Error("collect from a remote host")
	}
	if _, err := api.Open("relative.pprof", ""); err == nil {
		t.Error("relative path opened")
	}

	// A plan error (JS tests) must not leave the temporary directory behind.
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{}`), 0o644)
	if _, err := api.StartTests("x", testrun.Request{Lang: testrun.LangJS, Dirs: []string{dir}}, false); err == nil {
		t.Fatal("JS tests profiled")
	}
	app.profiles.mu.Lock()
	defer app.profiles.mu.Unlock()
	if len(app.profiles.collecting) != 0 {
		t.Error("recordings left registered")
	}
}

func TestDiscardKeepsForeignFiles(t *testing.T) {
	api := &ProfileAPI{app: &App{profiles: newProfiles()}}
	dir := t.TempDir()
	if err := api.Discard(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatal("Discard removed a directory Flint did not create")
	}
}
