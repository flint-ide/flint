package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"flint/internal/config"
)

// Events the native menu sends to the frontend.
const (
	eventMenuOpenRecent = "menu:open-recent" // show the Recent Projects popup
	eventMenuOpenFolder = "menu:open-folder" // open the given folder
	eventMenuAddProject = "menu:add-project" // pick a folder to attach to the workspace
	eventMenuSettings   = "menu:settings"    // open the Settings tab (app menu, ⌘,)
)

// menuApp is the app that native menu items added outside Wails' menu talk to
// (the app menu's Settings… item, see window_darwin.go).
var menuApp *App

// buildMenu creates the native macOS menu bar. File ▸ Open Recent mirrors the
// recent-folders list and is rebuilt whenever it changes.
func (a *App) buildMenu() *menu.Menu {
	m := menu.NewMenu()
	m.Append(menu.AppMenu())

	file := m.AddSubmenu("File")
	file.AddText("Open Folder…", keys.CmdOrCtrl("o"), func(*menu.CallbackData) {
		runtime.EventsEmit(a.ctx, eventMenuOpenRecent)
	})
	file.AddText("Add Project to Workspace…", keys.Combo("o", keys.CmdOrCtrlKey, keys.ShiftKey), func(*menu.CallbackData) {
		runtime.EventsEmit(a.ctx, eventMenuAddProject)
	})
	recentMenu := file.AddSubmenu("Open Recent")
	recent := a.recentFolders()
	for _, path := range recent {
		recentMenu.AddText(recentLabel(path), nil, func(*menu.CallbackData) {
			runtime.EventsEmit(a.ctx, eventMenuOpenFolder, path)
		})
	}
	if len(recent) == 0 {
		recentMenu.AddText("No Recent Folders", nil, nil).Disabled = true
	} else {
		recentMenu.AddSeparator()
		recentMenu.AddText("Clear Recently Opened", nil, func(*menu.CallbackData) {
			_ = a.clearRecentFolders()
		})
	}

	m.Append(menu.EditMenu())
	m.Append(menu.WindowMenu())
	return m
}

// refreshMenu re-installs the menu after the recent list changed.
func (a *App) refreshMenu() {
	if a.ctx == nil {
		return // menu is installed by wails.Run at startup
	}
	runtime.MenuSetApplicationMenu(a.ctx, a.buildMenu())
	runtime.MenuUpdateApplicationMenu(a.ctx)
	addSettingsMenuItem() // the rebuilt app menu lost it
}

// recentLabel is "name — ~/parent/path", like Xcode's Open Recent.
func recentLabel(path string) string {
	name := filepath.Base(path)
	dir := filepath.Dir(path)
	if home, err := os.UserHomeDir(); err == nil {
		if rest, ok := strings.CutPrefix(dir, home); ok {
			dir = "~" + rest
		}
	}
	return name + " — " + dir
}

// forgetRecentFolder removes one entry from the recent list.
func (a *App) forgetRecentFolder(path string) error {
	a.stateMu.Lock()
	next := a.state
	next.RecentFolders = slices.DeleteFunc(slices.Clone(a.state.RecentFolders), func(p string) bool { return p == path })
	err := config.SaveJSON(a.statePath(), next)
	if err == nil {
		a.state = next
	}
	a.stateMu.Unlock()
	if err == nil {
		a.refreshMenu()
	}
	return err
}

// clearRecentFolders empties the recent list.
func (a *App) clearRecentFolders() error {
	a.stateMu.Lock()
	next := a.state
	next.RecentFolders = nil
	err := config.SaveJSON(a.statePath(), next)
	if err == nil {
		a.state = next
	}
	a.stateMu.Unlock()
	if err == nil {
		a.refreshMenu()
	}
	return err
}
