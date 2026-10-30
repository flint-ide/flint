package main

import (
	"context"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	eventBeforeClose = "app:before-close"
	// If the frontend doesn't answer (e.g. it crashed), a second quit within this
	// window closes the app anyway.
	closeAnswerTimeout = 10 * time.Second
)

// beforeClose lets the frontend flush scratchpads and confirm unsaved files first.
// Returning true keeps the window open until the frontend calls Quit.
func (a *App) beforeClose(ctx context.Context) bool {
	a.closeMu.Lock()
	defer a.closeMu.Unlock()
	if a.quitting {
		return false
	}
	if !a.closeAskedAt.IsZero() && time.Since(a.closeAskedAt) < closeAnswerTimeout {
		return false
	}
	a.closeAskedAt = time.Now()
	runtime.EventsEmit(ctx, eventBeforeClose)
	return true
}

// AppAPI is the frontend's side of the quit handshake.
type AppAPI struct {
	app *App
}

// Titlebar reports where the native window buttons are, so the title bar can be
// laid out around them (0s on platforms without them).
func (q *AppAPI) Titlebar() TitlebarMetrics { return titlebarMetrics() }

// Quit closes the app after the frontend saved what it had to.
func (q *AppAPI) Quit() {
	q.app.closeMu.Lock()
	q.app.quitting = true
	q.app.closeMu.Unlock()
	runtime.Quit(q.app.ctx)
}

// CancelQuit records that the user chose to keep working.
func (q *AppAPI) CancelQuit() {
	q.app.closeMu.Lock()
	q.app.closeAskedAt = time.Time{}
	q.app.closeMu.Unlock()
}
