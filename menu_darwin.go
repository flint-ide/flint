//go:build darwin

package main

import "C"

import "github.com/wailsapp/wails/v2/pkg/runtime"

// flintMenuOpenSettings is called by the app menu's "Settings…" item
// (window_darwin.go). Kept in its own file: a cgo file with //export may not
// define C functions in its preamble.
//
//export flintMenuOpenSettings
func flintMenuOpenSettings() {
	if menuApp != nil && menuApp.ctx != nil {
		runtime.EventsEmit(menuApp.ctx, eventMenuSettings)
	}
}
