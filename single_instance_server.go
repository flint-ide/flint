//go:build server

package main

import "github.com/wailsapp/wails/v3/pkg/application"

// The browser-only server build runs beside the desktop app.
func singleInstance(*App) *application.SingleInstanceOptions { return nil }
