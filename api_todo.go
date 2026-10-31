package main

import (
	"context"
	"time"

	"flint/internal/settings"
	"flint/internal/todo"
)

// todoTimeout bounds a scan of a very large workspace.
const todoTimeout = 30 * time.Second

// TodoAPI lists TODO / FIXME / HACK / XXX / BUG markers of the workspace.
type TodoAPI struct {
	app *App
}

// Scan finds the markers in every open folder, skipping the exclude patterns
// (the frontend passes the setting as edited, before it is saved).
func (t *TodoAPI) Scan(exclude []string) (todo.Result, error) {
	ctx, cancel := context.WithTimeout(t.app.ctx, todoTimeout)
	defer cancel()
	return todo.Scan(ctx, t.app.projectRoots(), settings.CleanExcludes(exclude))
}

// AutoExcluded lists the folder names the scan always skips (for Settings).
func (t *TodoAPI) AutoExcluded() []string { return todo.AutoExcluded() }
