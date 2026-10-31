package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func write(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), FileName)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadMissingFileGivesDefaults(t *testing.T) {
	got, err := Load(filepath.Join(t.TempDir(), FileName))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !reflect.DeepEqual(got, Default()) {
		t.Errorf("Load() = %+v, want defaults", got)
	}
}

func TestLoadFillsMissingKeysAndIgnoresUnknown(t *testing.T) {
	path := write(t, `{"theme":"light","editor":{"fontSize":15},"future":{"x":1},"keymap":{"git.push":"Mod+Alt+P"}}`)
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	def := Default()
	if got.Theme != ThemeLight {
		t.Errorf("Theme = %q, want light", got.Theme)
	}
	if got.Editor.FontSize != 15 || got.Editor.FontFamily != def.Editor.FontFamily || got.Editor.LineHeight != def.Editor.LineHeight {
		t.Errorf("Editor = %+v, want size 15 with default family and line height", got.Editor)
	}
	if got.Terminal != def.Terminal {
		t.Errorf("Terminal = %+v, want defaults", got.Terminal)
	}
	if got.Keymap["git.push"] != "Mod+Alt+P" {
		t.Errorf("Keymap = %v", got.Keymap)
	}
	if got.Version != Version {
		t.Errorf("Version = %d, want %d", got.Version, Version)
	}
}

func TestLoadReplacesInvalidValues(t *testing.T) {
	path := write(t, `{
		"theme": "neon",
		"editor": {"fontFamily": "x; } body { display:none", "fontSize": 99, "lineHeight": 0.2},
		"terminal": {"fontSize": "big", "lineHeight": 1.4},
		"defaultAgent": "gpt",
		"keymap": {"a.ok": "Mod+K", "a.removed": "", "a.bad": "Mod+", "": "Mod+J", "a.spaces": "Mod+Shift+ X"}
	}`)
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	def := Default()
	if got.Theme != def.Theme {
		t.Errorf("Theme = %q, want default", got.Theme)
	}
	if got.Editor != def.Editor {
		t.Errorf("Editor = %+v, want defaults", got.Editor)
	}
	if got.Terminal.FontSize != def.Terminal.FontSize || got.Terminal.LineHeight != 1.4 {
		t.Errorf("Terminal = %+v, want default size and line height 1.4", got.Terminal)
	}
	if got.DefaultAgent != "" {
		t.Errorf("DefaultAgent = %q, want empty", got.DefaultAgent)
	}
	want := map[string]string{"a.ok": "Mod+K", "a.removed": ""}
	if !reflect.DeepEqual(got.Keymap, want) {
		t.Errorf("Keymap = %v, want %v", got.Keymap, want)
	}
	if err := Validate(got); err != nil {
		t.Errorf("normalized settings fail Validate: %v", err)
	}
}

func TestLoadMalformedFileGivesDefaultsAndError(t *testing.T) {
	got, err := Load(write(t, `{"theme": `))
	if err == nil {
		t.Fatal("Load() error = nil, want decode error")
	}
	if !reflect.DeepEqual(got, Default()) {
		t.Errorf("Load() = %+v, want defaults", got)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*Settings)
		want   string // error substring; "" means valid
	}{
		{"defaults", func(*Settings) {}, ""},
		{"system theme and agent", func(s *Settings) { s.Theme = ThemeSystem; s.DefaultAgent = "codex" }, ""},
		{"theme", func(s *Settings) { s.Theme = "Dark" }, "theme"},
		{"font size", func(s *Settings) { s.Editor.FontSize = 7 }, "editor: font size"},
		{"line height", func(s *Settings) { s.Terminal.LineHeight = 3 }, "terminal: line height"},
		{"empty family", func(s *Settings) { s.Editor.FontFamily = "  " }, "empty"},
		{"family injection", func(s *Settings) { s.Terminal.FontFamily = "Menlo; color: red" }, "invalid characters"},
		{"agent", func(s *Settings) { s.DefaultAgent = "gpt" }, "agent"},
		{"plus key", func(s *Settings) { s.Keymap = map[string]string{"zoom.in": "Mod++"} }, ""},
		{"bare key", func(s *Settings) { s.Keymap = map[string]string{"debug.step": "F8"} }, ""},
		{"unknown modifier", func(s *Settings) { s.Keymap = map[string]string{"x.y": "Hyper+K"} }, "invalid shortcut"},
		{"empty id", func(s *Settings) { s.Keymap = map[string]string{"": "Mod+K"} }, "command id"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := Default()
			tt.modify(&s)
			err := Validate(s)
			if tt.want == "" {
				if err != nil {
					t.Errorf("Validate() error = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Validate() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestSaveRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", FileName)
	in := Default()
	in.Version = 0 // stamped on save
	in.Theme = ThemeSystem
	in.Editor.FontFamily = "  Menlo  "
	in.Terminal.FontSize = 15
	in.DefaultAgent = "claude"
	in.AutoSave = true
	in.Keymap = map[string]string{"git.push": "", "app.settings": "Mod+,"}
	saved, err := Save(path, in)
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if saved.Version != Version || saved.Editor.FontFamily != "Menlo" {
		t.Errorf("Save() = %+v, want version stamped and family trimmed", saved)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !reflect.DeepEqual(loaded, saved) {
		t.Errorf("Load() = %+v, want %+v", loaded, saved)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("permissions = %o, want 600", perm)
	}
}

func TestSaveRejectsInvalidAndKeepsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	if _, err := Save(path, Default()); err != nil {
		t.Fatal(err)
	}
	bad := Default()
	bad.Editor.FontSize = 100
	if _, err := Save(path, bad); err == nil {
		t.Fatal("Save() error = nil, want validation error")
	}
	got, err := Load(path)
	if err != nil || got.Editor.FontSize != Default().Editor.FontSize {
		t.Errorf("file changed after a rejected save: %+v, %v", got.Editor, err)
	}
}

func TestSaveWritesEmptyKeymapObject(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	in := Default()
	in.Keymap = nil
	if _, err := Save(path, in); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if string(raw["keymap"]) != "{}" {
		t.Errorf("keymap = %s, want {}", raw["keymap"])
	}
}

func TestNormalizeDoesNotMutateInput(t *testing.T) {
	in := Default()
	in.Keymap = map[string]string{"ok": "Mod+K", "bad": "Hyper+K"}
	_ = Normalize(in)
	if len(in.Keymap) != 2 {
		t.Errorf("input keymap changed: %v", in.Keymap)
	}
}
