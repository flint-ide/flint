package extensions

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"flint/internal/extensions/catalog"
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
	catalog *catalog.Catalog
	langs   *languageSync
	index   *indexStore
	fh      feature.Host
	started sync.Once

	// pending is the folder `flint --install-extension <dir>` named when
	// this Flint was started with it; the frontend takes it once.
	pendingMu sync.Mutex
	pending   *InstallRequest
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
	a.started.Do(func() {
		go a.host.StartAtLaunch()
		go a.langs.sync() // the first discovery sends no change event
	})
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

// PendingInstall is the folder this Flint was started to install
// (`flint --install-extension <dir>`), handed out once; an empty request
// when there is none. The frontend then calls Install with it, so the
// native confirmation is the same as for "Install Extension from Folder…".
func (a *API) PendingInstall() InstallRequest {
	a.pendingMu.Lock()
	defer a.pendingMu.Unlock()
	if a.pending == nil {
		return InstallRequest{}
	}
	req := *a.pending
	a.pending = nil
	return req
}

// Catalog is what the extensions repository offers; the first call clones
// the repository, later ones read the clone.
func (a *API) Catalog() (catalog.Listing, error) { return a.catalog.List(a.ctx()) }

// RefreshCatalog fetches the extensions repository again.
func (a *API) RefreshCatalog() (catalog.Listing, error) { return a.catalog.Refresh(a.ctx()) }

// InstallFromCatalog builds the extension in the repository's folder if it
// is a Go program, then installs it like Install (native confirmation); nil
// when the user cancelled. The repository isn't refreshed meanwhile, so what
// is copied is what was built and confirmed.
func (a *API) InstallFromCatalog(folder string) (*host.Info, error) {
	dir, release, err := a.catalog.Prepare(a.ctx(), folder)
	if err != nil {
		return nil, err
	}
	defer release()
	return a.Install(dir)
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

// ResolveView returns the content of a panel (key "") or of a tab view's tab
// (its key and saved data) that Flint draws from data: a table, a form, a
// chart, or a tree or list.
func (a *API) ResolveView(id, view, key string, data json.RawMessage) (host.View, error) {
	return a.host.ResolveView(a.ctx(), id, view, key, data, "")
}

// ViewAction sends the choice of a row's or a message's action in a panel
// (key "") or a tab.
func (a *API) ViewAction(id, view, key, item, action string) error {
	return a.host.ViewAction(a.ctx(), id, view, key, item, action)
}

// SubmitForm sends a form view's values (a JSON object by field id) to the
// extension once Go's checks of them pass; the result says what was refused.
func (a *API) SubmitForm(id, view, key string, values json.RawMessage) (host.FormResult, error) {
	return a.host.SubmitForm(a.ctx(), id, view, key, values)
}

// SetViewVisible tells the host a declarative panel or tab was shown or hidden.
func (a *API) SetViewVisible(id, view, key string, visible bool) error {
	return a.host.SetViewVisible(id, view, key, visible)
}

// SetVisible tells the host a panel was shown or hidden.
func (a *API) SetVisible(id, view string, visible bool) error {
	return a.host.SetVisible(id, view, visible)
}

// EditorInterest reports whether an extension's decoration set, turned on,
// is for the file: only then does the editor report the document.
func (a *API) EditorInterest(path string) host.DocumentInterest { return a.host.EditorInterest(path) }

// EditorOpened reports a document open in an editor, at version; text is its
// content when hasText (asked for by EditorInterest). Extensions with a
// decoration set for the file hear of it, and start.
func (a *API) EditorOpened(path string, version int, text string, hasText bool) error {
	return a.host.EditorOpened(path, version, text, hasText)
}

// EditorChanged reports an open document's new version, debounced.
func (a *API) EditorChanged(path string, version int, text string, hasText bool) {
	a.host.EditorChanged(path, version, text, hasText)
}

// EditorClosed reports that a document's editor closed.
func (a *API) EditorClosed(path string) { a.host.EditorClosed(path) }

// EditorAction asks the extension for the edits of a quick fix the user
// chose on one of its decorations.
func (a *API) EditorAction(id string, req host.ActionRequest) (host.ActionResult, error) {
	return a.host.EditorAction(a.ctx(), id, req)
}

// WebviewHTML returns a webview's HTML file, to show in a sandboxed frame.
func (a *API) WebviewHTML(id, view string) (string, error) { return a.host.WebviewHTML(id, view) }

// WebviewRequest relays a webview's flint.request to its extension's process.
func (a *API) WebviewRequest(id, view, key, method string, params json.RawMessage) (json.RawMessage, error) {
	return a.host.WebviewRequest(a.ctx(), id, view, key, method, params)
}

// SetWebviewVisible tells the host a webview panel or tab was shown or hidden.
func (a *API) SetWebviewVisible(id, view, key string, visible bool) error {
	return a.host.SetWebviewVisible(id, view, key, visible)
}
