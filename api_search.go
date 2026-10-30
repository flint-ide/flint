package main

import (
	"context"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"flint/internal/search"
)

// Events emitted while a search runs.
const (
	eventSearchBatch = "search:batch"
	eventSearchDone  = "search:done"
)

// SearchBatch carries matches for one running search.
type SearchBatch struct {
	ID      string         `json:"id"`
	Matches []search.Match `json:"matches"`
}

// SearchDone reports the end of a search.
type SearchDone struct {
	ID      string         `json:"id"`
	Summary search.Summary `json:"summary"`
	Error   string         `json:"error"`
}

// SearchAPI runs one project search at a time, streaming results as events.
type SearchAPI struct {
	app *App

	mu     sync.Mutex
	cancel context.CancelFunc
}

// Start cancels any running search and starts a new one. The caller picks the id so it
// can match events that may arrive before this call returns.
// Results arrive as "search:batch" events followed by one "search:done".
func (s *SearchAPI) Start(id string, opts search.Options) {
	ctx, cancel := context.WithCancel(s.app.ctx)

	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	s.cancel = cancel
	s.mu.Unlock()

	go func() {
		defer cancel()
		sum, err := search.Run(ctx, opts, func(matches []search.Match) {
			if ctx.Err() == nil {
				runtime.EventsEmit(s.app.ctx, eventSearchBatch, SearchBatch{ID: id, Matches: matches})
			}
		})
		done := SearchDone{ID: id, Summary: sum}
		if err != nil && ctx.Err() == nil {
			done.Error = err.Error()
		}
		runtime.EventsEmit(s.app.ctx, eventSearchDone, done)
	}()
}

// Cancel stops the running search, if any.
func (s *SearchAPI) Cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
}
