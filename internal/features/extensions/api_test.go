package extensions

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"flint/internal/extensions/manifest"
	"flint/internal/feature/featuretest"
)

const nord = `{"contract":1,"id":"me.nord","name":"Nord","version":"1.0.0","publisher":"Me",
 "contributes":{"themes":[{"id":"me.nord.dark","label":"Nord","base":"dark",
   "colors":{"--bg-editor":"#2e3440"},"terminal":{"red":"#bf616a"}}]}}`

const tool = `{"contract":1,"id":"me.tool","name":"Tool","version":"0.1.0","run":{"command":"bin/tool"},
 "activation":["onStartup"],"permissions":["secrets"],
 "contributes":{"commands":[{"id":"me.tool.go","title":"Go"}]}}`

func setup(t *testing.T) (*API, *featuretest.Host, string) {
	t.Helper()
	h := featuretest.New(context.Background())
	root := filepath.Join(t.TempDir(), "extensions")
	return &API{host: h, root: func() (string, error) { return root, nil }}, h, root
}

func source(t *testing.T, man string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, manifest.FileName), []byte(man), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestListEmpty(t *testing.T) {
	api, _, _ := setup(t)
	got, err := api.List()
	if err != nil || len(got) != 0 {
		t.Errorf("List() = %v, %v", got, err)
	}
}

func TestInstallFromFolder(t *testing.T) {
	api, h, root := setup(t)
	h.Folder, h.Answer = source(t, nord), true
	ext, err := api.InstallFromFolder()
	if err != nil || ext == nil {
		t.Fatalf("InstallFromFolder() = %v, %v", ext, err)
	}
	if ext.ID != "me.nord" || ext.Dir != filepath.Join(root, "me.nord") || ext.Program {
		t.Errorf("ext = %+v", ext)
	}
	if len(ext.Themes) != 1 || ext.Themes[0].Terminal["red"] != "#bf616a" || ext.Themes[0].Colors["--bg-editor"] != "#2e3440" {
		t.Errorf("Themes = %+v", ext.Themes)
	}
	asked := h.Asked()
	if len(asked) != 1 || !strings.Contains(asked[0], "Nord 1.0.0 (me.nord) by Me") || !strings.Contains(asked[0], "1 color theme") {
		t.Errorf("dialog = %q", asked)
	}
	if ev := h.Events(); len(ev) != 1 || ev[0].Name != EventChanged {
		t.Errorf("events = %+v", ev)
	}
	list, err := api.List()
	if err != nil || len(list) != 1 || list[0].Name != "Nord" {
		t.Errorf("List() = %+v, %v", list, err)
	}
}

func TestInstallDialogDisclosesProgram(t *testing.T) {
	api, h, _ := setup(t)
	h.Folder, h.Answer = source(t, tool), true
	ext, err := api.InstallFromFolder()
	if err != nil || ext == nil || !ext.Program {
		t.Fatalf("InstallFromFolder() = %+v, %v", ext, err)
	}
	msg := h.Asked()[0]
	for _, want := range []string{"Program: bin/tool", "your user's permissions", "doesn't start extension programs yet", "run at startup", "Permissions: secrets"} {
		if !strings.Contains(msg, want) {
			t.Errorf("dialog lacks %q:\n%s", want, msg)
		}
	}
}

func TestInstallCancelled(t *testing.T) {
	api, h, root := setup(t)
	if ext, err := api.InstallFromFolder(); ext != nil || err != nil {
		t.Errorf("no folder picked: %v, %v", ext, err)
	}
	h.Folder = source(t, nord) // Answer stays false
	if ext, err := api.InstallFromFolder(); ext != nil || err != nil {
		t.Errorf("dialog declined: %v, %v", ext, err)
	}
	if _, err := os.Stat(filepath.Join(root, "me.nord")); err == nil {
		t.Error("installed without confirmation")
	}
	if len(h.Events()) != 0 {
		t.Error("a cancelled install emitted an event")
	}
}

func TestInstallBrokenManifest(t *testing.T) {
	api, h, _ := setup(t)
	h.Folder, h.Answer = source(t, `{"contract":1,"id":"x"}`), true
	if _, err := api.InstallFromFolder(); err == nil {
		t.Error("installed a broken manifest")
	}
	if len(h.Asked()) != 0 {
		t.Error("asked to install a broken manifest")
	}
}

func TestListShowsBrokenExtensions(t *testing.T) {
	api, _, root := setup(t)
	dir := filepath.Join(root, "me.bad")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, manifest.FileName), []byte(`{"contract":7}`), 0o644); err != nil {
		t.Fatal(err)
	}
	list, err := api.List()
	if err != nil || len(list) != 1 || list[0].ID != "me.bad" || !strings.Contains(list[0].Error, "contract 7") {
		t.Errorf("List() = %+v, %v", list, err)
	}
}

func TestUninstall(t *testing.T) {
	api, h, root := setup(t)
	h.Folder, h.Answer = source(t, nord), true
	if _, err := api.InstallFromFolder(); err != nil {
		t.Fatal(err)
	}
	h.Answer = false
	if ok, err := api.Uninstall("me.nord"); ok || err != nil {
		t.Errorf("declined Uninstall() = %v, %v", ok, err)
	}
	h.Answer = true
	if ok, err := api.Uninstall("me.nord"); !ok || err != nil {
		t.Errorf("Uninstall() = %v, %v", ok, err)
	}
	if _, err := os.Stat(filepath.Join(root, "me.nord")); err == nil {
		t.Error("the folder is still there")
	}
	if !strings.Contains(h.Asked()[1], "me.nord") {
		t.Errorf("dialog = %q", h.Asked()[1])
	}
	if ok, err := api.Uninstall("../escape"); ok || err == nil {
		t.Errorf("Uninstall(../escape) = %v, %v", ok, err)
	}
}
