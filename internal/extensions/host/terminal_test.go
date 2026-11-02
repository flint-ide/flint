package host

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"flint/internal/extensions/manifest"
)

func terminalExt(t *testing.T, perms string) (*ext, *fakeApp, string) {
	t.Helper()
	m, err := manifest.Parse([]byte(`{"contract":1,"id":"flint.kubernetes","name":"Kubernetes","version":"0.1.0",
	  "run":{"command":"bin/k"},"permissions":[` + perms + `]}`))
	if err != nil {
		t.Fatal(err)
	}
	root := realDir(t)
	app := &fakeApp{roots: []string{root}, trusted: map[string]bool{root: true}, disabled: map[string]bool{}, settings: map[string]json.RawMessage{}}
	h := New(Options{Dir: filepath.Join(t.TempDir(), "extensions"), App: app, Environ: MinimalEnv})
	t.Cleanup(h.Close)
	return newExt(h, m.ID, t.TempDir(), m, nil), app, root
}

func TestOpenTerminal(t *testing.T) {
	e, app, root := terminalExt(t, `"terminal"`)
	err := e.openTerminal(json.RawMessage(`{"title":"logs: web","cwd":"` + root + `","env":{"KUBECONFIG":"/tmp/k"},"command":["kubectl","logs","-f","web; rm -rf"]}`))
	if err != nil {
		t.Fatal(err)
	}
	ev := app.eventsLike(EventOpenTerminal)
	if len(ev) != 1 || !strings.Contains(ev[0], `"command":["kubectl","logs","-f","web; rm -rf"]`) || !strings.Contains(ev[0], `"KUBECONFIG":"/tmp/k"`) {
		t.Fatalf("events = %v", ev)
	}

	for name, params := range map[string]string{
		"reserved env":   `{"env":{"PATH":"/evil"}}`,
		"flint env":      `{"env":{"FLINT_EXTENSION_ID":"x"}}`,
		"bad env name":   `{"env":{"A=B":"x"}}`,
		"control char":   `{"command":["echo","a\u001b[2J"]}`,
		"empty command":  `{"command":[""]}`,
		"outside folder": `{"cwd":"` + t.TempDir() + `"}`,
	} {
		if err := e.openTerminal(json.RawMessage(params)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if n := len(app.eventsLike(EventOpenTerminal)); n != 1 {
		t.Errorf("%d terminals opened, want 1", n)
	}
}

func TestOpenTerminalNeedsThePermission(t *testing.T) {
	e, _, _ := terminalExt(t, ``)
	err := e.openTerminal(json.RawMessage(`{"command":["ls"]}`))
	if err == nil || !strings.Contains(err.Error(), `"terminal" permission`) {
		t.Errorf("openTerminal without permission = %v", err)
	}
}

func TestDataDir(t *testing.T) {
	e, _, _ := terminalExt(t, ``)
	dir, err := e.dataDir()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(dir) != "flint.kubernetes" || filepath.Base(filepath.Dir(dir)) != "extension-data" {
		t.Errorf("data dir = %s", dir)
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() || st.Mode().Perm() != 0o700 {
		t.Errorf("data dir not created private: %v %v", st, err)
	}
}
