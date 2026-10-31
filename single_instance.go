//go:build !server

package main

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// singleInstance makes a second `flint <folder>` hand the folder to the running
// Flint (which switches to it and comes to the front) instead of starting
// another app. Instances with their own config directory (FLINT_CONFIG_DIR, as
// in development) stay separate.
func singleInstance(a *App) *application.SingleInstanceOptions {
	sum := sha256.Sum256([]byte(a.configDir))
	return &application.SingleInstanceOptions{
		UniqueID: "com.wails.flint." + hex.EncodeToString(sum[:6]),
		OnSecondInstanceLaunch: func(data application.SecondInstanceData) {
			a.secondLaunch(data.Args, data.WorkingDir)
		},
	}
}
