// Package backlog is the Backlog feature (docs/design/backlog.md): folders of
// planned work items (Markdown files with a status) for one or more
// repositories, and Delegate, which turns an item into agent tasks whose
// progress moves the item's status. The file format, loading, status
// rewriting and URL matching are pure and tested without Flint; the agent
// tasks come through feature.Host. The frontend half is
// frontend/src/features/backlog.
//
// Nothing in a backlog runs by itself: opening one reads files, and only the
// user's Delegate starts agents, never in a folder in Restricted Mode.
package backlog

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"flint/internal/feature"
)

// ID is the feature's id, also its key under settings.json's features.enabled.
const ID = "backlog"

// Module declares the feature (listed in package main's features.go).
var Module = feature.Module{
	ID:       ID,
	Optional: true,
	State:    []string{stateBacklogs, stateCheckouts, stateTasks},
	New: func(h feature.Host) feature.Instance {
		api := newAPI(h)
		return feature.Instance{
			Services:     []application.Service{application.NewService(api)},
			AgentTools:   api.agentTools,
			TasksChanged: api.tasksChanged,
			Start: func(ctx context.Context) {
				api.tasksChanged() // tasks may have moved on while Flint was closed
				go api.watch(ctx)
			},
		}
	},
}
