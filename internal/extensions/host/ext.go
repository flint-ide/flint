package host

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"runtime"
	"sync"
	"time"

	"flint/internal/extensions/manifest"
)

// States of an extension, as Settings ▸ Extensions shows them.
const (
	StateInvalid  = "invalid"  // the manifest or the folder is wrong
	StateDisabled = "disabled" // turned off in Settings
	StateStopped  = "stopped"  // not running (not used yet, or idle)
	StateStarting = "starting"
	StateRunning  = "running"
	StateCrashed  = "crashed" // exited on its own; the next use starts it again
	StateFailed   = "failed"  // crashed too often; stays off until restarted
)

// Crash policy: maxCrashes within crashWindow stop the extension for the
// session; each crash delays the next start by a growing backoff.
const (
	maxCrashes  = 3
	crashWindow = 10 * time.Minute
)

// backoffBase is the delay after the first crash (a variable for tests).
var backoffBase = time.Second

// ext is one installed extension and its process, if any.
type ext struct {
	h       *Host
	id, dir string
	m       *manifest.Manifest // nil when invalid
	loadErr error
	log     *ring

	mu       sync.Mutex
	proc     *proc         // the running process, else nil
	starting chan struct{} // closed when the start in progress ends
	crashes  []time.Time
	lastErr  string // why it last stopped on its own
	failed   bool
	visible  map[string]bool // its panels on screen
	// docs are the open documents it was told about (editor/opened), with
	// the sets that apply (documentKey); they keep it running like a panel.
	docs     map[string]string
	busy     int // requests in flight, both ways
	idle     *time.Timer
	restart  *time.Timer
	status   map[string]StatusText
	origins  map[string]bool // links the user let it open this session
	messages []time.Time     // recent showMessage, for the rate limit
}

func newExt(h *Host, id, dir string, m *manifest.Manifest, err error) *ext {
	return &ext{
		h: h, id: id, dir: dir, m: m, loadErr: err, log: newRing(),
		visible: map[string]bool{}, docs: map[string]string{}, status: map[string]StatusText{}, origins: map[string]bool{},
	}
}

// running returns the live process, if any.
func (e *ext) running() *proc {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.starting != nil {
		return nil
	}
	return e.proc
}

func (e *ext) state() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	switch {
	case e.m == nil:
		return StateInvalid
	case !e.h.o.App.Enabled(e.id):
		return StateDisabled
	case e.failed:
		return StateFailed
	case e.starting != nil:
		return StateStarting
	case e.proc != nil:
		return StateRunning
	case e.lastErr != "":
		return StateCrashed
	}
	return StateStopped
}

// session returns the running process, starting it (and waiting for its
// initialize handshake) when needed. Concurrent callers share one start.
func (e *ext) session(ctx context.Context) (*proc, error) {
	for {
		if err := e.usable(); err != nil {
			return nil, err
		}
		e.mu.Lock()
		if e.starting == nil && e.proc != nil {
			p := e.proc
			e.mu.Unlock()
			return p, nil
		}
		if ch := e.starting; ch != nil {
			e.mu.Unlock()
			select {
			case <-ch:
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		wait := e.backoff(time.Now())
		ch := make(chan struct{})
		e.starting = ch
		e.stopTimer(&e.restart)
		e.mu.Unlock()
		e.h.changed()

		p, err := e.startAfter(ctx, wait)
		e.mu.Lock()
		if err == nil && p.hasExited() {
			err = errors.New("exited right after starting")
		}
		e.starting = nil
		close(ch)
		var docs map[string]string
		if err == nil {
			e.proc, e.lastErr = p, ""
			docs = maps.Clone(e.docs) // in the same step: a later one is announced by syncDocument
		}
		e.mu.Unlock()
		if err != nil {
			e.log.add("flint", "start failed: "+err.Error())
			if ctx.Err() == nil || !errors.Is(err, ctx.Err()) { // not the caller giving up during the backoff
				e.crashed(err.Error())
			}
			e.h.changed()
			return nil, fmt.Errorf("%s did not start: %w", e.name(), err)
		}
		e.h.changed()
		e.announceDocuments(p, docs)
		e.touch()
		return p, nil
	}
}

func (e *ext) startAfter(ctx context.Context, wait time.Duration) (*proc, error) {
	if wait > 0 {
		t := time.NewTimer(wait)
		defer t.Stop()
		select {
		case <-t.C:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return e.start()
}

// usable: the extension may be started at all.
func (e *ext) usable() error {
	switch {
	case e.m == nil:
		return fmt.Errorf("extension %s is broken: %v", e.id, e.loadErr)
	case !e.h.o.App.Enabled(e.id):
		return fmt.Errorf("%s is turned off in Settings ▸ Extensions", e.name())
	}
	e.h.mu.Lock()
	closed := e.h.closed
	e.h.mu.Unlock()
	if closed {
		return errors.New("Flint is quitting")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.failed {
		return fmt.Errorf("%s stopped after crashing %d times; restart it in Settings ▸ Extensions", e.name(), maxCrashes)
	}
	return nil
}

// backoff is how long a start must still wait after the last crash.
func (e *ext) backoff(now time.Time) time.Duration {
	n := len(e.crashes)
	if n == 0 {
		return 0
	}
	delay := backoffBase << (2 * (n - 1)) // 1 s, 4 s, 16 s
	return e.crashes[n-1].Add(delay).Sub(now)
}

// crashed records an unexpected end: after too many the extension stays off,
// otherwise one that should be running (onStartup, a panel on screen) is
// started again after the backoff.
func (e *ext) crashed(reason string) {
	now := time.Now()
	e.mu.Lock()
	e.lastErr = reason
	recent := e.crashes[:0:0]
	for _, t := range e.crashes {
		if now.Sub(t) < crashWindow {
			recent = append(recent, t)
		}
	}
	e.crashes = append(recent, now)
	failed := len(e.crashes) >= maxCrashes && !e.failed
	if failed {
		e.failed = true
	}
	wanted := !e.failed && (e.m.RunsAtStartup() || len(e.visible) > 0 || len(e.docs) > 0)
	if wanted {
		e.stopTimer(&e.restart)
		e.restart = time.AfterFunc(e.backoff(now), func() {
			ctx, cancel := context.WithTimeout(context.Background(), e.h.o.InitTimeout+time.Second)
			defer cancel()
			_, _ = e.session(ctx)
		})
	}
	e.mu.Unlock()
	if failed {
		e.log.add("flint", fmt.Sprintf("stopped after %d crashes in %s", maxCrashes, crashWindow))
		e.h.o.App.Emit(EventMessage, MessageEvent{
			Extension: e.id, Name: e.name(), Type: "error",
			Message: fmt.Sprintf("stopped after crashing %d times; restart it in Settings ▸ Extensions", maxCrashes),
		})
	}
}

func (e *ext) resetCrashes() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.crashes, e.failed, e.lastErr = nil, false, ""
}

// exited is called once the process p is gone.
func (e *ext) exited(p *proc, reason string) {
	e.mu.Lock()
	current := e.proc == p
	if current {
		e.proc = nil
		e.stopTimer(&e.idle)
	}
	e.mu.Unlock()
	if !current {
		return
	}
	// Nobody keeps its decorations up to date any more.
	e.h.o.App.Emit(EventDecorationsCleared, ClearDecorationsEvent{Extension: e.id})
	if p.stopping.Load() {
		e.log.add("flint", "stopped")
	} else {
		e.log.add("flint", "exited: "+reason)
		e.crashed(reason)
	}
	e.h.changed()
}

// stop ends the process, if one runs (idle, turned off, uninstalled, quit).
func (e *ext) stop(why string) {
	e.mu.Lock()
	p := e.proc
	e.stopTimer(&e.idle)
	e.stopTimer(&e.restart)
	e.mu.Unlock()
	if p != nil {
		e.log.add("flint", "stopping: "+why)
		p.stop(e.h.o.StopTimeout)
	}
}

// begin and end bracket a request in either direction; touch restarts the
// idle countdown once nothing is in flight.
func (e *ext) begin() {
	e.mu.Lock()
	e.busy++
	e.stopTimer(&e.idle)
	e.mu.Unlock()
}

func (e *ext) end() {
	e.mu.Lock()
	e.busy--
	e.mu.Unlock()
	e.touch()
}

func (e *ext) touch() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.stopTimer(&e.idle)
	if e.proc == nil || e.busy > 0 || len(e.visible) > 0 || len(e.docs) > 0 || e.m.RunsAtStartup() {
		return
	}
	p := e.proc
	e.idle = time.AfterFunc(e.h.o.IdleTimeout, func() {
		e.mu.Lock()
		still := e.proc == p && e.busy == 0 && len(e.visible) == 0 && len(e.docs) == 0
		e.mu.Unlock()
		if still {
			e.stop(fmt.Sprintf("idle for %s", e.h.o.IdleTimeout))
			e.h.changed()
		}
	})
}

// stopTimer stops *t; the caller holds e.mu.
func (e *ext) stopTimer(t **time.Timer) {
	if *t != nil {
		(*t).Stop()
		*t = nil
	}
}

// dropDocuments forgets the documents of an extension that was removed or
// replaced: it gets no editor/* notifications any more.
func (e *ext) dropDocuments() {
	e.mu.Lock()
	defer e.mu.Unlock()
	clear(e.docs)
}

func (e *ext) name() string {
	if e.m != nil {
		return e.m.Name
	}
	return e.id
}

// settings are the values the extension receives (schema defaults filled in).
func (e *ext) settings() map[string]any {
	return e.m.Contributes.Settings.Values(e.h.o.App.Settings(e.id))
}

// Root is a workspace folder as an extension sees it.
type Root struct {
	Path    string `json:"path"`
	Trusted bool   `json:"trusted"`
}

// roots are the trusted workspace folders: an extension never learns of an
// untrusted one (Restricted Mode).
func (e *ext) roots() []Root {
	out := []Root{}
	for _, r := range e.h.o.App.Roots() {
		if e.h.o.App.Trusted(r) {
			out = append(out, Root{Path: r, Trusted: true})
		}
	}
	return out
}

func (e *ext) initializeParams() map[string]any {
	return map[string]any{
		"contract":     manifest.Contract,
		"flintVersion": e.h.o.App.Version(),
		"extension":    map[string]string{"id": e.id, "version": e.m.Version, "dir": e.dir},
		"settings":     e.settings(),
		"roots":        e.roots(),
		"platform":     runtime.GOOS,
		"capabilities": map[string]any{
			"views": []string{"tree", "list", "message"}, "secrets": true, "agent": true, "openUrl": true,
			"decorations": DecorationKinds, "quickFixes": true,
		},
	}
}

// setVisible records whether one of its panels is on screen; a shown panel
// keeps the process from stopping when idle.
func (e *ext) setVisible(view, key string, visible bool) {
	e.mu.Lock()
	if visible {
		e.visible[view+"\x00"+key] = true
	} else {
		delete(e.visible, view+"\x00"+key)
	}
	p := e.proc
	e.mu.Unlock()
	if p != nil {
		params := map[string]any{"view": view, "visible": visible}
		if key != "" {
			params["key"] = key
		}
		_ = p.conn.Notify("view/visible", params)
	}
	e.touch()
}
