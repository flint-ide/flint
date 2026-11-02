package extensions

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"flint/internal/extensions/host"
	"flint/internal/feature"
)

// API is the extensions service the frontend binds: the installed
// extensions, their install and removal, and the requests their UI makes.
// The web view decides nothing here: every install and uninstall is
// confirmed in a native dialog, and the host checks every id against the
// extension's manifest.
type API struct {
	host    *host.Host
	fh      feature.Host
	started sync.Once
}

func (a *API) ctx() context.Context {
	if ctx := a.fh.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
}

// List describes the installed extensions. The first call, made by the
// frontend once the window is up, also starts the onStartup ones.
func (a *API) List() []host.Info {
	list := a.host.List()
	a.started.Do(func() { go a.host.StartAtLaunch() })
	return list
}

// Rescan reads the extensions folder again (an extension changed on disk).
func (a *API) Rescan() []host.Info {
	a.host.Discover()
	return a.host.List()
}

// Install copies the extension in folder dir into Flint after a native
// confirmation; nil when the user cancelled.
func (a *API) Install(dir string) (*host.Info, error) {
	info, err := a.host.Install(dir)
	if errors.Is(err, host.ErrCancelled) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &info, nil
}

// Uninstall removes an extension after a native confirmation; false when
// the user cancelled.
func (a *API) Uninstall(id string) (bool, error) {
	err := a.host.Uninstall(id)
	if errors.Is(err, host.ErrCancelled) {
		return false, nil
	}
	return err == nil, err
}

// Restart clears a stopped-after-crashes extension; the next use starts it.
func (a *API) Restart(id string) error { return a.host.Restart(id) }

// Log is the extension's log (stderr, log messages, process events).
func (a *API) Log(id string) (string, error) { return a.host.Log(id) }

// Execute runs one of the extension's commands; args is JSON or null.
func (a *API) Execute(id, command string, args json.RawMessage) error {
	return a.host.Execute(a.ctx(), id, command, args)
}

// Resolve returns a panel's content, or a tree node's children (parent).
func (a *API) Resolve(id, view, parent string) (host.View, error) {
	return a.host.Resolve(a.ctx(), id, view, parent)
}

// Action sends the choice of an item's action ("" item: a message's).
func (a *API) Action(id, view, item, action string) error {
	return a.host.Action(a.ctx(), id, view, item, action)
}

// SetVisible tells the host a panel was shown or hidden.
func (a *API) SetVisible(id, view string, visible bool) error {
	return a.host.SetVisible(id, view, visible)
}
