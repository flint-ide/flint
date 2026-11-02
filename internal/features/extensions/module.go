// Package extensions is the external extensions feature (level 2 of
// docs/design/extensions.md): the extensions installed in
// <config>/extensions, Settings ▸ Extensions (install from a folder,
// uninstall) and what they contribute that needs no program, which today is
// color themes. Starting extension programs (JSON-RPC over stdio) is the
// next step and not here yet. The frontend half is
// frontend/src/features/extensions.
package extensions

import (
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/application"

	"flint/internal/config"
	"flint/internal/feature"
)

// Module declares the feature (listed in package main's features.go).
var Module = feature.Module{
	ID: "extensions",
	New: func(h feature.Host) feature.Instance {
		api := &API{host: h, root: defaultRoot}
		return feature.Instance{Services: []application.Service{application.NewService(api)}}
	},
}

// defaultRoot is where global extensions live: <config>/extensions.
func defaultRoot() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "extensions"), nil
}
