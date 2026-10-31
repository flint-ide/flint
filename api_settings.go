package main

import (
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"flint/internal/settings"
)

// Window background behind the web view per appearance (the --bg of each palette
// in style.css; alpha as in the Wails template Flint started from).
var (
	darkBackground  = options.RGBA{R: 24, G: 25, B: 28, A: 1}
	lightBackground = options.RGBA{R: 244, G: 244, B: 246, A: 1}
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
		runtime.WindowSetBackgroundColour(a.ctx, bg.R, bg.G, bg.B, bg.A)
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
	return out
}

// startupAppearance picks the native appearance and window background for the
// configured theme before the window exists, so a light theme opens without a
// dark flash.
func (a *App) startupAppearance() (mac.AppearanceType, *options.RGBA) {
	switch a.currentSettings().Theme {
	case settings.ThemeLight:
		bg := lightBackground
		return mac.NSAppearanceNameAqua, &bg
	case settings.ThemeSystem:
		bg := darkBackground
		if !systemPrefersDark() {
			bg = lightBackground
		}
		return mac.DefaultAppearance, &bg
	default:
		bg := darkBackground
		return mac.NSAppearanceNameDarkAqua, &bg
	}
}
