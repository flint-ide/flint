package todo

import (
	"context"
	"time"

	"flint/internal/feature"
	"flint/internal/settings"
	"flint/internal/todo"
)

// todoTimeout bounds a scan of a very large workspace.
const todoTimeout = 30 * time.Second

// API lists TODO / FIXME / HACK / XXX / BUG markers of the workspace.
type API struct {
	host feature.Host
}

// Scan finds the markers in every open folder, skipping the exclude patterns
// (the frontend passes the setting as edited, before it is saved).
func (t *API) Scan(exclude []string) (todo.Result, error) {
	ctx, cancel := context.WithTimeout(t.host.Context(), todoTimeout)
	defer cancel()
	return todo.Scan(ctx, t.host.Roots(), settings.CleanExcludes(exclude))
}

// AutoExcluded lists the folder names the scan always skips (for Settings).
func (t *API) AutoExcluded() []string { return todo.AutoExcluded() }
