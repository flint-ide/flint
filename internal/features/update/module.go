// Package update is the release check feature: "Check for Updates…", the
// daily background check (a Start hook), the notice the status bar shows, the
// "checkForUpdates" setting and its part of state.json. The release client is
// internal/update; the frontend half is frontend/src/features/update.
package update

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"flint/internal/feature"
	"flint/internal/settings"
)

// Module declares the feature (listed in package main's features.go).
var Module = feature.Module{
	ID:       "update",
	Settings: []settings.AnySection{Enabled},
	State:    []string{stateKey},
	New: func(h feature.Host) feature.Instance {
		api := newAPI(h)
		return feature.Instance{
			Services: []application.Service{application.NewService(api)},
			Start:    func(ctx context.Context) { go api.watch(ctx) },
		}
	},
}

// Enabled is the "checkForUpdates" key of settings.json: ask GitHub once a
// day whether a newer Flint release exists. Off means Flint never asks on its
// own; "Check for Updates…" still works.
var Enabled = &settings.Section[bool]{
	Key:     "checkForUpdates",
	Default: func() bool { return true },
}
