package vscodeimport

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"flint/internal/settings"
)

func fixtureHome(t *testing.T) string {
	t.Helper()
	home, err := filepath.Abs(filepath.Join("testdata", "home"))
	if err != nil {
		t.Fatal(err)
	}
	return home
}

func TestSources(t *testing.T) {
	got := Sources(fixtureHome(t))
	if len(got) != 2 || got[0].ID != "vscode" || got[1].ID != "cursor" {
		t.Fatalf("Sources() = %+v, want vscode and cursor", got)
	}
	if !strings.HasSuffix(got[0].ExtensionsDir, filepath.Join(".vscode", "extensions")) {
		t.Errorf("extensions dir = %s", got[0].ExtensionsDir)
	}
	if _, ok := FindSource(fixtureHome(t), "vscodium"); ok {
		t.Error("FindSource found an editor that isn't installed")
	}
	if _, ok := FindSource(fixtureHome(t), "../../etc"); ok {
		t.Error("FindSource accepted an unknown id")
	}
	if got := Sources(t.TempDir()); len(got) != 0 {
		t.Errorf("Sources(empty home) = %v", got)
	}
}

func TestBuildPlanVSCode(t *testing.T) {
	src, ok := FindSource(fixtureHome(t), "vscode")
	if !ok {
		t.Fatal("vscode fixture not found")
	}
	plan := BuildPlan(src, settings.Default())
	targets := map[string]string{}
	for _, it := range plan.Items {
		targets[it.Kind+" "+it.Name] = it.Value + " → " + it.Target
	}
	for key, want := range map[string]string{
		"setting editor.fontFamily":              "'Fira Code', Menlo, monospace → Editor ▸ Font family",
		"setting editor.fontSize":                "14 px → Editor ▸ Font size",
		"setting editor.lineHeight":              "1.5× → Editor ▸ Line height",
		"setting terminal.integrated.fontSize":   "12 px → Terminal ▸ Font size",
		"setting terminal.integrated.lineHeight": "1.1× → Terminal ▸ Line height",
		"setting files.autoSave":                 "on → Editor ▸ Save files when Flint loses focus",
		"setting Vim extension (vscodevim.vim)":  "on → Editor ▸ Vim mode",
		"keybinding workbench.action.quickOpen":  "Mod+E → nav.goToFile",
	} {
		if targets[key] != want {
			t.Errorf("%s = %q, want %q", key, targets[key], want)
		}
	}
	if !strings.HasPrefix(targets["theme Acme Night"], "17 colors on the dark palette") {
		t.Errorf("theme item = %q", targets["theme Acme Night"])
	}
	reasons := map[string]string{}
	for _, s := range plan.Skipped {
		reasons[s.Name] = s.Reason
	}
	for name, want := range map[string]string{
		"editor.tabSize":           "indentation",
		"files.exclude":            ".gitignore",
		"[go]":                     "language-specific",
		"telemetry.telemetryLevel": "no equivalent",
		"workbench.action.selectTheme (cmd+k cmd+t)":     "no matching Flint command",
		"git.push (cmd+k cmd+p)":                         "chords",
		"-workbench.action.togglePanel (cmd+j)":          "removes",
		"git.commit (cmd+enter)":                         "when scmRepository",
		"workbench.action.terminal.sendSequence (cmd+i)": "arguments",
		"workbench.action.toggleSidebarVisibility (x)":   "typing",
		"workbench.action.quickOpen (Mod+T)":             "later shortcut",
	} {
		if !strings.Contains(reasons[name], want) {
			t.Errorf("skipped %s: reason %q, want it to mention %q", name, reasons[name], want)
		}
	}

	got := plan.Apply(settings.Default())
	if got.Editor.FontSize != 14 || got.Editor.LineHeight != 1.5 || !got.Editor.VimMode || !got.AutoSave {
		t.Errorf("applied editor = %+v autoSave %v", got.Editor, got.AutoSave)
	}
	if got.Theme != settings.ThemeCustom || got.CustomTheme.Name != "Acme Night" {
		t.Errorf("applied theme = %q %q", got.Theme, got.CustomTheme.Name)
	}
	if got.Keymap["app.commandPalette"] != "Shift+Mod+A" || got.Keymap["view.toggleSidebar"] != "Mod+B" || got.Keymap["debug.selected"] != "F5" {
		t.Errorf("applied keymap = %v", got.Keymap)
	}
	if err := settings.Validate(got); err != nil {
		t.Errorf("applied settings fail validation: %v", err)
	}
}

func TestPlanApplyKeepsOtherSettings(t *testing.T) {
	src, _ := FindSource(fixtureHome(t), "vscode")
	plan := BuildPlan(src, settings.Default())
	current := settings.Default()
	current.DefaultAgent = "codex"
	current.Keymap = map[string]string{"git.pull": "Mod+Alt+U", "nav.goToFile": "Mod+P"}
	got := plan.Apply(current)
	if got.DefaultAgent != "codex" || got.Keymap["git.pull"] != "Mod+Alt+U" || got.Keymap["nav.goToFile"] != "Mod+E" {
		t.Errorf("Apply() = agent %q keymap %v", got.DefaultAgent, got.Keymap)
	}
	if current.Keymap["nav.goToFile"] != "Mod+P" {
		t.Error("Apply() mutated its input")
	}
}

func TestBuildPlanBuiltInThemeAndRanges(t *testing.T) {
	src, _ := FindSource(fixtureHome(t), "cursor")
	plan := BuildPlan(src, settings.Default())
	got := plan.Apply(settings.Default())
	if got.Theme != settings.ThemeLight {
		t.Errorf("theme = %q, want light for a built-in light theme", got.Theme)
	}
	if got.Editor.FontSize != settings.Default().Editor.FontSize {
		t.Errorf("an out-of-range font size was applied: %d", got.Editor.FontSize)
	}
	var skippedColors, skippedSize bool
	for _, s := range plan.Skipped {
		skippedColors = skippedColors || (s.Kind == KindTheme && strings.Contains(s.Reason, "built into"))
		skippedSize = skippedSize || (s.Name == "editor.fontSize" && strings.Contains(s.Reason, "range"))
	}
	if !skippedColors || !skippedSize {
		t.Errorf("skipped = %+v", plan.Skipped)
	}
}

func TestBuildPlanUnreadableFiles(t *testing.T) {
	home := t.TempDir()
	user := filepath.Join(home, "Library", "Application Support", "VSCodium", "User")
	writeFile(t, filepath.Join(user, "settings.json"), `{"editor.fontSize": `)
	writeFile(t, filepath.Join(user, "keybindings.json"), `{"not": "a list"}`)
	src, ok := FindSource(home, "vscodium")
	if !ok {
		t.Fatal("source not found")
	}
	plan := BuildPlan(src, settings.Default())
	if len(plan.Items) != 0 {
		t.Errorf("items = %+v", plan.Items)
	}
	files := 0
	for _, s := range plan.Skipped {
		if s.Kind == KindFile {
			files++
		}
	}
	if files != 2 {
		t.Errorf("skipped = %+v, want both files reported", plan.Skipped)
	}
}

func TestAutoDetectColorScheme(t *testing.T) {
	var c changes
	raw := map[string]json.RawMessage{"window.autoDetectColorScheme": json.RawMessage("true"), "workbench.colorTheme": json.RawMessage(`"Acme Night"`)}
	items, skipped := mapTheme(raw, Source{}, &c)
	if c.theme != settings.ThemeSystem || len(items) != 1 || len(skipped) != 1 {
		t.Errorf("mapTheme() = %q, %v, %v", c.theme, items, skipped)
	}
}

func TestLineHeightConversion(t *testing.T) {
	cur := settings.Default()
	for in, want := range map[string]float64{"1.6": 1.6, "26": 2} {
		c, _, _ := mapSettings(map[string]json.RawMessage{"editor.lineHeight": json.RawMessage(in)}, cur)
		if c.editorLine == nil || *c.editorLine != want {
			t.Errorf("lineHeight %s → %v, want %v", in, c.editorLine, want)
		}
	}
	for _, in := range []string{"0", "3", "60", `"x"`} {
		c, _, skipped := mapSettings(map[string]json.RawMessage{"editor.lineHeight": json.RawMessage(in)}, cur)
		if c.editorLine != nil || len(skipped) != 1 {
			t.Errorf("lineHeight %s → %v, skipped %v", in, c.editorLine, skipped)
		}
	}
}

// Every Flint command the table maps to must exist in the frontend registry,
// so a renamed command fails here instead of importing a dead shortcut.
func TestCommandMapTargetsExist(t *testing.T) {
	var src strings.Builder
	root := filepath.Join("..", "..", "frontend", "src")
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.Contains(path, ".test.") || !(strings.HasSuffix(path, ".ts") || strings.HasSuffix(path, ".tsx")) {
			return err
		}
		data, err := os.ReadFile(path)
		src.Write(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	all := src.String()
	for vs, id := range commandMap {
		want := regexp.QuoteMeta("id: '" + id + "'")
		if panel, ok := strings.CutPrefix(id, "view."); ok && id != "view.toggleSidebar" {
			want = regexp.QuoteMeta("id: '" + panel + "'") // panels get "view.<id>"
		}
		if !regexp.MustCompile(want).MatchString(all) {
			t.Errorf("%s maps to %s, which is not a Flint command", vs, id)
		}
		if regexp.MustCompile(regexp.QuoteMeta("id: '"+id+"'") + `[^}]*hintKeys`).MatchString(all) {
			t.Errorf("%s maps to %s, whose shortcut belongs to the editor and can't be rebound", vs, id)
		}
	}
}
