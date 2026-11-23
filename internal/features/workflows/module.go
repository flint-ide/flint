// Package workflows is the Workflows feature (docs/design/workflows.md): a
// workflow is a Markdown file of steps (agent, script, review, gate) that
// one agent task takes in order. The file format, the runs and their
// transitions, the prompts and the loading are pure and tested without
// Flint; the engine takes the steps through feature.Host: the first step
// is the agent task's own first chat (TaskContext), later steps are chats
// (StartStepChat) and run configurations (RunConfiguration). The frontend
// half is frontend/src/features/workflows.
//
// Nothing runs by itself: a run starts on the user's click, an agent's call
// the user approved, or a schedule the user wrote into their own settings,
// only from a trusted folder, and a repository's workflows are read only
// when it is trusted. The steps are copied into the run when it starts.
package workflows

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"flint/internal/feature"
	"flint/internal/settings"
)

// ID is the feature's id, also its key under settings.json's features.enabled.
const ID = "workflows"

// Module declares the feature (listed in package main's features.go).
var Module = feature.Module{
	ID:       ID,
	Optional: true,
	Settings: []settings.AnySection{Section},
	State:    []string{stateRuns, stateSchedules, stateUnsandboxed},
	New: func(h feature.Host) feature.Instance {
		api := newAPI(h)
		e := api.engine
		return feature.Instance{
			Services: []application.Service{application.NewService(api)},
			// start_workflow and its kin, while mcpServer.actions.workflows is on.
			AgentTools: api.agentTools,
			// flint workflow … and flint run … (cli.go).
			CLI: api.cliCommands(),
			// A project item's task that names a workflow is taken by a run.
			TaskWorkflow: api.taskWorkflow,
			// The same checks, planning nothing (an agent's delegation,
			// before the user is asked).
			TaskWorkflowCheck: api.taskWorkflowCheck,
			TaskRun:           e.taskRun,
			// The first step's prompt, when Flint starts a run's task chat.
			TaskContext: e.taskContext,
			// The chat Flint opened for a run's first step; and no chat for
			// the task of a run that is over.
			TaskChatStarted: e.taskChatStarted,
			TaskStartCheck:  e.taskStartCheck,
			TasksChanged:    e.kickTasks,
			// A step refused for a folder's trust may go now.
			// A schedule whose folder opened is fingerprinted.
			RootsChanged: func() { e.kick(); api.sched.sync() },
			// The runs load, then the schedules' timer is armed.
			Start: func(ctx context.Context) { e.start(ctx); api.sched.arm(); api.sched.sync() },
			// The schedules' timer follows workflows.schedules.
			SettingsChanged: func(_, _ settings.Settings) { api.sched.arm(); api.sched.sync() },
			// Script waits end, the schedules' timer stops; chats go on
			// (quitting stops no agent).
			Close: func() { api.sched.close(); e.close() },
		}
	},
}
