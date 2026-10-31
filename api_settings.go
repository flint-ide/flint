package main

import (
	"github.com/wailsapp/wails/v3/pkg/application"
	"slices"

	"flint/internal/settings"
)

// Window background behind the web view per appearance (the --bg of each palette
// in style.css; alpha as in the Wails template Flint started from).
var (
	darkBackground  = application.RGBA{Red: 24, Green: 25, Blue: 28, Alpha: 255}
	lightBackground = application.RGBA{Red: 244, Green: 244, Blue: 246, Alpha: 255}
)

// SettingsAPI reads and writes settings.json; the frontend applies the values live.
type SettingsAPI struct {
	app *App
}

// Get returns the current settings (defaults for anything not configured).
func (s *SettingsAPI) Get() settings.Settings { return s.app.currentSettings() }

// Defaults returns the built-in settings, for "reset to default".
func (s *SettingsAPI) Defaults() settings.Settings { return settings.Default() }

// Save validates and stores the settings, returning what was written. The native
// window follows a theme change right away.
func (s *SettingsAPI) Save(next settings.Settings) (settings.Settings, error) {
	a := s.app
	a.settingsMu.Lock()
	prevTheme := a.settings.Theme
	saved, err := settings.Save(settings.Path(a.configDir), next)
	if err == nil {
		a.settings = saved
	}
	a.settingsMu.Unlock()
	if err != nil {
		return settings.Settings{}, err
	}
	if saved.Theme != prevTheme && a.ctx != nil {
		dark := setNativeAppearance(saved.Theme)
		bg := lightBackground
		if dark {
			bg = darkBackground
		}
		a.setWindowBackground(bg)
	}
	return saved, nil
}

// currentSettings returns a copy safe to hand out (the keymap is cloned).
func (a *App) currentSettings() settings.Settings {
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	out := a.settings
	out.Keymap = make(map[string]string, len(a.settings.Keymap))
	for id, keys := range a.settings.Keymap {
		out.Keymap[id] = keys
	}
	out.TodoExclude = slices.Clone(a.settings.TodoExclude)
	return out
}

// startupAppearance picks the native appearance and window background for the
// configured theme before the window exists, so a light theme opens without a
// dark flash.
func (a *App) startupAppearance() (application.MacAppearanceType, application.RGBA) {
	switch a.currentSettings().Theme {
	case settings.ThemeLight:
		bg := lightBackground
		return application.NSAppearanceNameAqua, bg
	case settings.ThemeSystem:
		bg := darkBackground
		if !systemPrefersDark() {
			bg = lightBackground
		}
		return application.DefaultAppearance, bg
	default:
		bg := darkBackground
		return application.NSAppearanceNameDarkAqua, bg
	}
}
