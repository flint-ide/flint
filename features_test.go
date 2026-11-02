package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"flint/internal/config"
	"flint/internal/settings"
)

func TestFeatureStateKeepsItsKeys(t *testing.T) {
	if err := registerFeatures(); err != nil {
		t.Fatal(err)
	}
	cfg := t.TempDir()
	a := &App{configDir: cfg}
	// state.json as written before the update feature owned its key.
	old := `{"recentFolders": ["/p"], "update": {"checkedAt": "2026-10-31T12:00:00Z", "skipped": "v1.1.0"}, "stale": 1}`
	if err := os.WriteFile(a.statePath(), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := config.LoadJSON(a.statePath(), &a.state); err != nil {
		t.Fatal(err)
	}
	var st struct{ Skipped string }
	if err := (featureHost{a}).LoadState("update", &st); err != nil || st.Skipped != "v1.1.0" {
		t.Fatalf("LoadState = %+v, %v", st, err)
	}
	// Another part of App saves state.json: the feature's key survives it.
	if err := a.rememberFolder("/q"); err != nil {
		t.Fatal(err)
	}
	if err := (featureHost{a}).SaveState("update", map[string]string{"skipped": "v1.2.0"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(a.statePath())
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if strings.Join(strings.Fields(string(doc["update"])), "") != `{"skipped":"v1.2.0"}` || !strings.Contains(string(doc["recentFolders"]), "/q") {
		t.Errorf("state.json = %s", data)
	}
	if _, ok := doc["stale"]; ok {
		t.Error("an unknown key was kept")
	}
	restarted := &App{configDir: cfg}
	if err := config.LoadJSON(restarted.statePath(), &restarted.state); err != nil {
		t.Fatal(err)
	}
	if err := (featureHost{restarted}).LoadState("update", &st); err != nil || st.Skipped != "v1.2.0" {
		t.Errorf("after restart: %+v, %v", st, err)
	}
	if err := (featureHost{a}).SaveState("nobody", 1); err == nil {
		t.Error("an undeclared state key was saved")
	}
}

func TestFeaturesAreRegistered(t *testing.T) {
	if err := registerFeatures(); err != nil {
		t.Fatal(err)
	}
	keys := settings.SectionKeys()
	for _, want := range []string{"todoExclude", "checkForUpdates", "features", "extensions"} {
		if !slices.Contains(keys, want) {
			t.Errorf("settings section %q not registered (have %v)", want, keys)
		}
	}
	// Defaults are what they were before the keys moved into features.
	data, err := json.Marshal(settings.Default())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"checkForUpdates":true`, `"todoExclude":[]`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("defaults lack %s", want)
		}
	}
}

// The frontend registers the same sections (features/*/settings.ts), and the
// settings reference documents each one (site/reference/_settings/<key>.md).
func TestFeatureSettingsMatchFrontendAndDocs(t *testing.T) {
	if err := registerFeatures(); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob("frontend/src/features/*/settings.ts")
	if err != nil {
		t.Fatal(err)
	}
	defineSection := regexp.MustCompile(`defineSection\('([^']+)'`)
	var frontend []string
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range defineSection.FindAllStringSubmatch(string(src), -1) {
			frontend = append(frontend, m[1])
		}
	}
	goKeys := settings.SectionKeys()
	slices.Sort(frontend)
	slices.Sort(goKeys)
	if !slices.Equal(frontend, goKeys) {
		t.Errorf("frontend sections %v, Go sections %v", frontend, goKeys)
	}
	for _, key := range goKeys {
		doc, err := os.ReadFile(filepath.Join("site", "reference", "_settings", key+".md"))
		if err != nil {
			t.Errorf("settings key %q is not documented: %v", key, err)
			continue
		}
		if !strings.HasPrefix(string(doc), "| `"+key+"` |") {
			t.Errorf("_settings/%s.md does not start with its table row", key)
		}
	}
}
