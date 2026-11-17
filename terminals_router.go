package main

import (
	"sync"

	as "flint/internal/agentsession"
	"flint/internal/sessions"
	"flint/internal/terminal"
)

// Both managers must keep satisfying the interfaces the app depends on.
var (
	_ sessions.Terminals = (*terminal.Manager)(nil)
	_ sessions.Agents    = (*as.Manager)(nil)
)

// terminalsRouter sends each terminal id to the manager that owns it.
// Docker and extension stream sessions need local pipes, so they always live in
// the streams manager; everything else goes to main, which a later step can
// replace with a client of a background host.
type terminalsRouter struct {
	main    sessions.Terminals
	streams streamTerminals

	mu        sync.Mutex
	streamIDs map[string]struct{}
}

// streamTerminals is the local manager that hosts stream sessions.
type streamTerminals interface {
	sessions.Terminals
	StartStream(id string, st terminal.Stream) error
}

func newTerminalsRouter(main sessions.Terminals, streams streamTerminals) *terminalsRouter {
	return &terminalsRouter{main: main, streams: streams, streamIDs: map[string]struct{}{}}
}

// StartStream starts a stream session and remembers that id belongs to the
// streams manager.
func (r *terminalsRouter) StartStream(id string, st terminal.Stream) error {
	if err := r.streams.StartStream(id, st); err != nil {
		return err
	}
	r.mu.Lock()
	r.streamIDs[id] = struct{}{}
	r.mu.Unlock()
	return nil
}

// pick returns the manager for id. An unknown id goes to main, which reports
// it as not running.
func (r *terminalsRouter) pick(id string) sessions.Terminals {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.streamIDs[id]; ok {
		return r.streams
	}
	return r.main
}

func (r *terminalsRouter) Start(id string, opts terminal.Options) error {
	return r.main.Start(id, opts)
}

func (r *terminalsRouter) Write(id string, data []byte) error { return r.pick(id).Write(id, data) }

func (r *terminalsRouter) Resize(id string, cols, rows int) error {
	return r.pick(id).Resize(id, cols, rows)
}

func (r *terminalsRouter) Cwd(id string) (string, error) { return r.pick(id).Cwd(id) }

func (r *terminalsRouter) Resume(id string) (terminal.Resume, error) { return r.pick(id).Resume(id) }

// Close forgets the id and closes it in the manager that owned it.
func (r *terminalsRouter) Close(id string) {
	m := r.pick(id)
	r.mu.Lock()
	delete(r.streamIDs, id)
	r.mu.Unlock()
	m.Close(id)
}

// CloseAll closes every session in both managers.
func (r *terminalsRouter) CloseAll() {
	r.mu.Lock()
	r.streamIDs = map[string]struct{}{}
	r.mu.Unlock()
	r.main.CloseAll()
	r.streams.CloseAll()
}

// streamStarter starts stream sessions; featureHost.StartStream needs it
// where the rest of the app only needs sessions.Terminals.
type streamStarter interface {
	StartStream(id string, st terminal.Stream) error
}
