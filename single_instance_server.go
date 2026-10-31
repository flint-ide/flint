//go:build server

package main

import (
	"log/slog"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// The browser-only server build runs beside the desktop app.
func singleInstance(*App) *application.SingleInstanceOptions { return nil }

// The server build is for UI checks during development: every binding
// (terminals included) answers on localhost without authentication.
func init() {
	slog.Warn("server build: DEVELOPMENT ONLY — every Flint binding (including terminals) is reachable on localhost without authentication; never run it on a shared machine")
}
