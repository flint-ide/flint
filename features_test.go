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
	"flint/internal/feature"
	"flint/internal/features/projects"
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

// The Projects feature was called Backlog on main for a short time (never
// released): the old state.json keys and the old features.enabled id are
// migrated to the new names when those are absent, and the old keys are
// dropped on the next save.
func TestOldBacklogKeysAreMigrated(t *testing.T) {
	if err := registerFeatures(); err != nil {
		t.Fatal(err)
	}
	load := func(t *testing.T, text string) settings.Settings {
		t.Helper()
		p := filepath.Join(t.TempDir(), "settings.json")
		if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		s, err := settings.Load(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := settings.Validate(s); err != nil {
			t.Fatalf("settings with the old keys don't validate: %v", err)
		}
		return s
	}
	t.Run("settings", func(t *testing.T) {
		for _, c := range []struct {
			text string
			on   bool
		}{
			{`{"features":{"enabled":{"backlog":false}}}`, false},                // old only
			{`{"features":{"enabled":{"projects":false}}}`, false},               // new only
			{`{"features":{"enabled":{"backlog":false,"projects":true}}}`, true}, // both: the new one wins
			{`{"features":{"enabled":{"backlog":true}},"mcpServer":{"actions":{"backlog":true}}}`, true},
		} {
			s := load(t, c.text)
			toggles := feature.Section.Get(s)
			if toggles.On(projects.Module) != c.on {
				t.Errorf("%s: projects on = %v, want %v", c.text, !c.on, c.on)
			}
			if _, ok := toggles.Enabled["backlog"]; ok {
				t.Errorf("%s: the old id is kept: %v", c.text, toggles.Enabled)
			}
			// mcpServer.actions.backlog never existed on main: nothing to migrate.
			if s.MCPServer.Actions.Projects {
				t.Errorf("%s: actions.projects switched on", c.text)
			}
		}
	})

	for _, c := range []struct {
		name, state string
		projects    []string
		checkout    string
		tasks       string // the task linked to item x of /p/plan
	}{
		{"old only", `{"recentFolders":["/p"],
			"backlogs":["/p/plan"],
			"backlogCheckouts":[{"repo":"github.com/acme/api","folder":"/p/api"}],
			"backlogTasks":{"/p/plan":{"x":[{"task":"0000000000000001","repo":"api"}]}}}`,
			[]string{"/p/plan"}, "/p/api", "0000000000000001"},
		{"new only", `{"projects":["/p/new"],
			"projectCheckouts":[{"repo":"github.com/acme/api","folder":"/p/api2"}],
			"projectTasks":{"/p/plan":{"x":[{"task":"0000000000000002"}]}}}`,
			[]string{"/p/new"}, "/p/api2", "0000000000000002"},
		{"both", `{"backlogs":["/p/plan"],"projects":["/p/new"],
			"backlogCheckouts":[{"repo":"github.com/acme/api","folder":"/p/api"}],
			"backlogTasks":{"/p/plan":{"x":[{"task":"0000000000000001"}]}},
			"projectTasks":{"/p/plan":{"x":[{"task":"0000000000000002"}]}}}`,
			[]string{"/p/new"}, "/p/api", "0000000000000002"},
	} {
		t.Run(c.name, func(t *testing.T) {
			a := &App{configDir: t.TempDir()}
			if err := os.WriteFile(a.statePath(), []byte(c.state), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := config.LoadJSON(a.statePath(), &a.state); err != nil {
				t.Fatal(err)
			}
			h := featureHost{a}
			var remembered []string
			var checkouts []struct{ Repo, Folder string }
			var tasks map[string]map[string][]struct{ Task, Repo string }
			if err := h.LoadState("projects", &remembered); err != nil || !slices.Equal(remembered, c.projects) {
				t.Errorf("projects = %v, %v; want %v", remembered, err, c.projects)
			}
			if err := h.LoadState("projectCheckouts", &checkouts); err != nil || len(checkouts) != 1 || checkouts[0].Folder != c.checkout {
				t.Errorf("projectCheckouts = %+v, %v", checkouts, err)
			}
			if err := h.LoadState("projectTasks", &tasks); err != nil || len(tasks["/p/plan"]["x"]) != 1 || tasks["/p/plan"]["x"][0].Task != c.tasks {
				t.Errorf("projectTasks = %+v, %v", tasks, err)
			}
			for _, key := range []string{"backlogs", "backlogCheckouts", "backlogTasks"} {
				if err := h.LoadState(key, new(any)); err == nil {
					t.Errorf("the old key %s is declared", key)
				}
			}
			if err := a.rememberFolder("/q"); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(a.statePath())
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "backlog") {
				t.Errorf("state.json keeps the old keys: %s", data)
			}
			if !strings.Contains(string(data), `"projects":`) {
				t.Errorf("state.json lost the migrated keys: %s", data)
			}
		})
	}
}
