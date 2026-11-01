package main

import (
	"os"
	"path/filepath"
	"testing"

	"flint/internal/settings"
)

func useImportHome(t *testing.T) {
	t.Helper()
	home, err := filepath.Abs(filepath.Join("internal", "vscodeimport", "testdata", "home"))
	if err != nil {
		t.Fatal(err)
	}
	prev := importHome
	importHome = func() (string, error) { return home, nil }
	t.Cleanup(func() { importHome = prev })
}

func TestImportPreviewThenApply(t *testing.T) {
	useImportHome(t)
	a := &App{configDir: t.TempDir(), settings: settings.Default()}
	api := &ImportAPI{app: a}

	sources, err := api.Sources()
	if err != nil || len(sources) != 2 {
		t.Fatalf("Sources() = %v, %v", sources, err)
	}
	if _, err := api.Preview("nope"); err == nil {
		t.Error("Preview(unknown id) succeeded")
	}
	plan, err := api.Preview("vscode")
	if err != nil || plan.ID == "" || len(plan.Items) == 0 {
		t.Fatalf("Preview() = %+v, %v", plan, err)
	}
	// Previewing writes nothing.
	if onDisk, _ := settings.Load(settings.Path(a.configDir)); onDisk.Editor.FontSize != settings.Default().Editor.FontSize {
		t.Fatal("Preview() changed settings.json")
	}
	if _, err := api.Apply("other"); err == nil {
		t.Error("Apply(wrong id) succeeded")
	}
	saved, err := api.Apply(plan.ID)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if saved.Editor.FontSize != 14 || saved.Theme != settings.ThemeCustom || saved.Keymap["nav.goToFile"] != "Mod+E" {
		t.Errorf("Apply() = %+v", saved)
	}
	onDisk, err := settings.Load(settings.Path(a.configDir))
	if err != nil || onDisk.Editor.FontSize != 14 || onDisk.CustomTheme.Name != "Acme Night" {
		t.Errorf("settings.json = %+v, %v", onDisk.Editor, err)
	}
	if _, err := api.Apply(plan.ID); err == nil {
		t.Error("a plan was applied twice")
	}
}

func TestImportApplyKeepsPlanWhenSaveFails(t *testing.T) {
	useImportHome(t)
	dir := t.TempDir()
	blocked := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(blocked, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	a := &App{configDir: blocked, settings: settings.Default()} // settings.json can't be written under a file
	api := &ImportAPI{app: a}
	plan, err := api.Preview("vscode")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := api.Apply(plan.ID); err == nil {
		t.Fatal("Apply() succeeded although settings.json can't be written")
	}
	a.configDir = dir
	if saved, err := api.Apply(plan.ID); err != nil || saved.Editor.FontSize != 14 {
		t.Errorf("retried Apply() = %v, %v; want the plan still there", saved.Editor, err)
	}
}
