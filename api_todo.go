package main

import (
	"context"
	"time"

	"flint/internal/todo"
)

// todoTimeout bounds a scan of a very large workspace.
const todoTimeout = 30 * time.Second

// TodoAPI lists TODO / FIXME / HACK / XXX / BUG markers of the workspace.
type TodoAPI struct {
	app *App
}

// Scan finds the markers in every open folder.
func (t *TodoAPI) Scan() (todo.Result, error) {
	ctx, cancel := context.WithTimeout(t.app.ctx, todoTimeout)
	defer cancel()
	return todo.Scan(ctx, t.app.projectRoots())
}
