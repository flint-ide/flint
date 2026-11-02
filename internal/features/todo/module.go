// Package todo is the TODO list feature: the scan behind the TODO panel
// (internal/todo) and its "todoExclude" setting. The frontend half is
// frontend/src/features/todo.
package todo

import (
	"slices"

	"github.com/wailsapp/wails/v3/pkg/application"

	"flint/internal/feature"
	"flint/internal/settings"
)

// Module declares the feature (listed in package main's features.go).
var Module = feature.Module{
	ID:       "todo",
	Settings: []settings.AnySection{Exclude},
	New: func(h feature.Host) feature.Instance {
		return feature.Instance{Services: []application.Service{application.NewService(&API{host: h})}}
	},
}

// Exclude is the "todoExclude" key of settings.json: extra folders or globs
// the TODO list skips ("docs/legacy", "*.min.js"), on top of the dependency
// folders it recognises itself.
var Exclude = &settings.Section[[]string]{
	Key:       "todoExclude",
	Default:   func() []string { return []string{} },
	Normalize: settings.CleanExcludes,
	Check:     func(p []string) error { return settings.CheckExcludes("TODO", p) },
	Clone: func(p []string) []string {
		if p == nil {
			return []string{}
		}
		return slices.Clone(p)
	},
}
