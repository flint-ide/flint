package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"

	"flint/internal/config"
)

// Events the native menu sends to the frontend.
const (
	eventMenuOpenRecent = "menu:open-recent" // show the Recent Projects popup
	eventMenuOpenFolder = "menu:open-folder" // open the given folder
	eventMenuAddProject = "menu:add-project" // pick a folder to attach to the workspace
	eventMenuSettings   = "menu:settings"    // open the Settings tab (app menu, ⌘,)
	// Help menu: the frontend runs these so they share code with the palette.
	eventMenuReportIssue     = "menu:report-issue"
	eventMenuCopyDiagnostics = "menu:copy-diagnostics"
	eventMenuShowLogs        = "menu:show-logs"
	eventMenuCheckTools      = "menu:check-tools"
)

// buildMenu creates the native macOS menu bar. File ▸ Open Recent mirrors the
// recent-folders list and is rebuilt whenever it changes.
func (a *App) buildMenu() *application.Menu {
	m := application.NewMenu()

	// The first submenu is the app menu on macOS.
	appMenu := m.AddSubmenu("flint")
	appMenu.AddRole(application.About)
	appMenu.AddSeparator()
	appMenu.Add("Settings…").SetAccelerator("CmdOrCtrl+,").OnClick(func(*application.Context) { a.emit(eventMenuSettings) })
	appMenu.AddSeparator()
	appMenu.AddRole(application.ServicesMenu)
	appMenu.AddSeparator()
	appMenu.AddRole(application.Hide)
	appMenu.AddRole(application.HideOthers)
	appMenu.AddRole(application.ShowAll)
	appMenu.AddSeparator()
	appMenu.AddRole(application.Quit)

	file := m.AddSubmenu("File")
	file.Add("Open Folder…").SetAccelerator("CmdOrCtrl+O").OnClick(func(*application.Context) { a.emit(eventMenuOpenRecent) })
	file.Add("Add Project to Workspace…").SetAccelerator("CmdOrCtrl+Shift+O").OnClick(func(*application.Context) { a.emit(eventMenuAddProject) })
	recentMenu := file.AddSubmenu("Open Recent")
	recent := a.recentFolders()
	for _, path := range recent {
		recentMenu.Add(recentLabel(path)).OnClick(func(*application.Context) { a.emit(eventMenuOpenFolder, path) })
	}
	if len(recent) == 0 {
		recentMenu.Add("No Recent Folders").SetEnabled(false)
	} else {
		recentMenu.AddSeparator()
		recentMenu.Add("Clear Recently Opened").OnClick(func(*application.Context) { _ = a.clearRecentFolders() })
	}

	// Without an Edit menu macOS webviews ignore Cmd+C / Cmd+V / Cmd+Z.
	m.AddRole(application.EditMenu)
	m.AddRole(application.WindowMenu)

	help := m.AddSubmenu("Help")
	help.Add("Check Tools…").OnClick(func(*application.Context) { a.emit(eventMenuCheckTools) })
	help.AddSeparator()
	help.Add("Report an Issue…").OnClick(func(*application.Context) { a.emit(eventMenuReportIssue) })
	help.Add("Copy Diagnostics").OnClick(func(*application.Context) { a.emit(eventMenuCopyDiagnostics) })
	help.Add("Show Logs in Finder").OnClick(func(*application.Context) { a.emit(eventMenuShowLogs) })
	return m
}

// refreshMenu re-installs the menu after the recent list changed.
func (a *App) refreshMenu() {
	if a.wails == nil {
		return // installed by main at startup
	}
	a.wails.Menu.Set(a.buildMenu())
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
