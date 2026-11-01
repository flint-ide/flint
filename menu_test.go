package main

import (
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// On Linux every menu accelerator becomes an app-wide GTK shortcut that runs
// before the page, so the menu must not carry any (Ctrl+Q, Ctrl+O… belong to
// terminals and the frontend keymap there), including the ones roles bring.
func TestLinuxMenuHasNoAccelerators(t *testing.T) {
	saved := isMac
	isMac = false
	t.Cleanup(func() { isMac = saved })

	m := (&App{}).buildMenu()
	for _, role := range []application.Role{application.Quit, application.About, application.EditMenu, application.WindowMenu, application.ServicesMenu} {
		if m.FindByRole(role) != nil {
			t.Errorf("the Linux menu uses role %v, which may bring its own accelerator", role)
		}
	}
	for _, label := range []string{"Open Folder…", "Add Project to Workspace…", "Settings…", "Quit", "About Flint", "Check Tools…", "Show Logs in File Manager"} {
		item := m.FindByLabel(label)
		if item == nil {
			t.Errorf("no %q item", label)
			continue
		}
		if acc := item.GetAccelerator(); acc != "" {
			t.Errorf("%q has accelerator %q", label, acc)
		}
	}
}
