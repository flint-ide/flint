//go:build !darwin || server

package main

import (
	_ "embed"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"flint/internal/desktop"
)

// Linux (and later Windows) keep the system's own window frame, so there is no
// title bar to line up with and the window shows as soon as the app starts.
// The browser-only server build uses this file on every system.

//go:embed build/linux/icon-512.png
var appIcon []byte

// setNativeAppearance reports whether theme ("dark", "light" or "system") is
// dark. GTK draws the frame from the desktop's own theme, so nothing changes.
func setNativeAppearance(theme string) bool {
	switch theme {
	case "dark":
		return true
	case "light":
		return false
	}
	return systemPrefersDark()
}

// systemPrefersDark reads the desktop's color scheme (dark when unknown).
func systemPrefersDark() bool { return desktop.PrefersDark() }

func titlebarMetrics() TitlebarMetrics { return TitlebarMetrics{} }

// onLaunched runs launched once the app has started. In the server build
// (which uses this file on macOS too) Wails never emits ApplicationStarted and
// its windows are no-ops, so nothing runs there: the login-shell PATH is
// imported on first use (shellenv.Wait), as before.
func (a *App) onLaunched(app *application.App, _ *application.WebviewWindow, launched func()) {
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) { launched() })
}

// linuxWindow sets the window icon (taskbars without a .desktop file show it)
// and puts the menu in a GTK 4 header bar button instead of a menu bar.
func linuxWindow() application.LinuxWindow {
	return application.LinuxWindow{Icon: appIcon, MenuStyle: application.LinuxMenuStylePrimaryMenu}
}

// showWindowEarly shows the window; its background is the theme's colour
// until the page paints.
func (a *App) showWindowEarly() {
	if a.window != nil {
		a.window.Show()
	}
}
