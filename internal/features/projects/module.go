// Package projects is the Projects feature (docs/design/projects.md):
// projects, folders of planned work items (Markdown files with a status: the
// project's backlog) for one or more repositories, and Delegate, which turns
// an item into agent tasks whose progress moves the item's status. The file format, loading, status
// rewriting and URL matching are pure and tested without Flint; the agent
// tasks come through feature.Host. The frontend half is
// frontend/src/features/projects.
//
// A project is only such a folder. The folders open in the window are
// workspace folders (roots), and the one the user works in is the active
// folder.
//
// Nothing in a project runs by itself: opening one reads files, and only the
// user's Delegate starts agents, never in a folder in Restricted Mode. The
// one start without a click at that moment is a waiting start the user asked
// for with Start When Ready, which Flint makes with every check of Delegate.
package projects

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"flint/internal/feature"
)

// ID is the feature's id, also its key under settings.json's features.enabled.
const ID = "projects"

// Module declares the feature (listed in package main's features.go).
var Module = feature.Module{
	ID:       ID,
	Optional: true,
	State:    []string{stateProjects, stateCheckouts, stateTasks, stateWaiting},
	New: func(h feature.Host) feature.Instance {
		api := newAPI(h)
		return feature.Instance{
			Services:     []application.Service{application.NewService(api)},
			AgentTools:   api.agentTools,
			TasksChanged: api.tasksChanged,
			// The project context every item's agent gets, when its chat starts.
			TaskContext: api.taskContext,
			// A waiting start that failed for a checkout's trust or a
			// missing workspace folder may go now.
			RootsChanged: api.kickStarter,
			// A start whose confirmation went unanswered asks again.
			Foreground: api.foreground,
			Start: func(ctx context.Context) {
				api.tasksChanged() // tasks may have moved on while Flint was closed
				go api.watch(ctx)
				go api.starter(ctx)
			},
			Close: api.clones.closeAll,
		}
	},
}
