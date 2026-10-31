package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"flint/internal/config"
)

func TestToolCheckOpensOnce(t *testing.T) {
	cfg := t.TempDir()
	d := &DiagnosticsAPI{app: &App{configDir: cfg}}
	if !d.ToolCheckOnFirstRun() {
		t.Fatal("first launch: the tool check should open")
	}
	if d.ToolCheckOnFirstRun() {
		t.Fatal("asked twice in one run: opened again")
	}
	raw, err := os.ReadFile(filepath.Join(cfg, "state.json"))
	if err != nil || !strings.Contains(string(raw), `"toolCheckShown": true`) {
		t.Fatalf("state.json %s, %v", raw, err)
	}

	// The next launch reads the flag from state.json.
	next := &App{configDir: cfg}
	if err := config.LoadJSON(next.statePath(), &next.state); err != nil {
		t.Fatal(err)
	}
	if (&DiagnosticsAPI{app: next}).ToolCheckOnFirstRun() {
		t.Fatal("next launch: opened again")
	}
}
