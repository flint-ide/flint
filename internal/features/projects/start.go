package projects

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"time"

	"flint/internal/prompts"
)

// EventAutoStart is emitted after Flint tried to start a waiting item by
// itself; its data is an AutoStart.
const EventAutoStart = "projects:auto-start"

// AutoStart tells the UI how an automatic start went.
type AutoStart struct {
	Project string `json:"project"`
	ID      string `json:"id"`
	Title   string `json:"title"`
	// Tasks is how many agent tasks started (0 when it failed).
	Tasks int `json:"tasks"`
	// Error says why nothing started ("" when it did); the item keeps
	// waiting with this reason. Warning is DelegateResult.Warning.
	Error   string `json:"error"`
	Warning string `json:"warning"`
	// Unanswered: nobody answered the start's confirmation; it is asked
	// again when the user is back (Waiting.Unanswered).
	Unanswered bool `json:"unanswered"`
}

// unansweredReason is a waiting start's reason after n unanswered
// confirmations.
func unansweredReason(n int) string {
	if n >= MaxUnanswered {
		return fmt.Sprintf("nobody answered Flint's confirmation %d times: Delegate it by hand, or cancel the start.", n)
	}
	return "nobody answered Flint's confirmation: Flint asks again when you are back."
}

// foreground is the Foreground hook: the user is back, so the waiting
// starts whose confirmation went unanswered (fewer than MaxUnanswered times)
// are asked again.
func (a *API) foreground() {
	a.mu.Lock()
	w := a.waiting()
	again := false
	for folder, items := range w {
		for id, cur := range items {
			if cur.Unanswered > 0 && cur.Unanswered < MaxUnanswered && cur.Tried != "" {
				cur.Tried = "" // not "nothing changed since": it is tried again
				w[folder][id] = cur
				again = true
			}
		}
	}
	if again {
		_ = a.host.SaveState(stateWaiting, w)
	}
	a.mu.Unlock()
	if again {
		a.kickStarter()
	}
}

// waiting reads the waiting starts. Callers hold mu.
func (a *API) waiting() waits {
	w := waits{}
	_ = a.host.LoadState(stateWaiting, &w)
	if w == nil {
		w = waits{}
	}
	return w
}

// kickStarter asks the starter to look at the waiting starts.
func (a *API) kickStarter() {
	select {
	case a.startKick <- struct{}{}:
	default:
	}
}

// StartWhenReady remembers that the user wants item id started when every
// item it waits for is done (the answer to Delegate's NotReady). Flint then
// delegates it by itself, with every check of Delegate at that time, once,
// and only while the item's file is still what it is now (Waiting.Hash).
// root is the window's active folder, used for an item without
// repositories; it must be an open workspace folder now (its trust is
// checked when the item starts). An item that is ready already starts as
// soon as the starter looks.
func (a *API) StartWhenReady(folder, id, root string) (ItemView, error) {
	real, _, err := a.known(folder)
	if err != nil {
		return ItemView{}, err
	}
	if !prompts.ValidName(id) {
		return ItemView{}, errors.New("invalid item id")
	}
	l, err := Load(real)
	if err != nil {
		return ItemView{}, err
	}
	if err := writable(l.Config); err != nil {
		return ItemView{}, err
	}
	if !slices.ContainsFunc(l.Items, func(it Item) bool { return it.ID == id }) {
		return ItemView{}, fmt.Errorf("the project has no usable item %s: see its problems", id)
	}
	// The file as it is now is what the start may run (Waiting.Hash).
	data, _, err := ReadItemFile(real, id)
	if err != nil {
		return ItemView{}, fmt.Errorf("%s: %w", itemPath(id), err)
	}
	it, err := ParseItem(id, data, l.Config)
	if err != nil {
		return ItemView{}, fmt.Errorf("%s: %w", itemPath(id), err)
	}
	d := Dependencies(append(slices.DeleteFunc(l.Items, func(x Item) bool { return x.ID == id }), it))[id]
	switch {
	case d.Problem != "":
		return ItemView{}, fmt.Errorf("%s can't start: %s: fix the item's after line", itemPath(id), d.Problem)
	case it.Status == StatusDone:
		return ItemView{}, errors.New("the item is done")
	}
	if len(it.Repos) > 0 {
		root = ""
	} else if !slices.Contains(a.host.Roots(), root) {
		return ItemView{}, errors.New("the item names no repository and no folder is open: open the folder it is for")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	w := a.waiting()
	if w[real] == nil {
		w[real] = map[string]Waiting{}
	}
	if _, ok := w[real][id]; !ok && len(w[real]) >= MaxWaiting {
		return ItemView{}, fmt.Errorf("%d items of this project wait to start already: cancel some first", MaxWaiting)
	}
	w[real][id] = Waiting{Root: root, Since: time.Now(), Hash: itemHash(data)}
	w.pruneProjects(func(f string) bool { return slices.Contains(a.knownSet(), f) })
	if err := a.host.SaveState(stateWaiting, w); err != nil {
		return ItemView{}, err
	}
	v := itemView(real, it, a.links()[real][id], nil)
	setDeps(&v, d, w[real])
	a.emit(real)
	a.kickStarter()
	return v, nil
}

// CancelStart forgets the waiting start of item id (Cancel start).
func (a *API) CancelStart(folder, id string) error {
	real, _, err := a.known(folder)
	if err != nil {
		return err
	}
	if !prompts.ValidName(id) {
		return errors.New("invalid item id")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	w := a.waiting()
	if _, ok := w[real][id]; !ok {
		return errors.New("the item is not waiting to start")
	}
	delete(w[real], id)
	if len(w[real]) == 0 {
		delete(w, real)
	}
	if err := a.host.SaveState(stateWaiting, w); err != nil {
		return err
	}
	a.emit(real)
	return nil
}

// starter starts waiting items whose last dependency is done, one pass at a
// time, until ctx ends. It is woken (kickStarter) where statuses change:
// after the watcher's reconcile (TasksChanged), when the user sets a status,
// when the panel reads a project with waiting starts, when the workspace or
// its trust changes, and by StartWhenReady. Nothing polls. It runs apart from
// the watcher: a confirmation it asks may stay open for a while.
func (a *API) starter(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-a.startKick:
			a.startReady(ctx)
		}
	}
}

// startReady delegates every waiting item that is ready now, through the
// same function as a click, once: a start that fails keeps the item waiting
// with the reason, and is tried again only once something it depends on
// changed (fingerprint). An item that is done is not started; its waiting
// start is dropped.
func (a *API) startReady(ctx context.Context) {
	known := a.knownSet()
	a.mu.Lock()
	w := a.waiting()
	a.mu.Unlock()
	for folder, items := range w {
		if ctx.Err() != nil {
			return
		}
		if !slices.Contains(known, folder) {
			continue
		}
		l, err := Load(folder)
		if err != nil || l.Config.OldFormat {
			continue // an old-format project starts nothing until it is renamed
		}
		deps := Dependencies(l.Items)
		for id, entry := range items {
			i := slices.IndexFunc(l.Items, func(it Item) bool { return it.ID == id })
			if i < 0 {
				// The file is gone for now, or a problem: keep waiting. It can
				// start only once the file is back exactly as it was (Hash).
				continue
			}
			it := l.Items[i]
			if it.Status == StatusDone {
				a.dropWaiting(folder, id, entry.Since)
				continue
			}
			if entry.Changed || !deps[id].Ready() {
				continue // refused until the user acts, or not ready
			}
			fp := a.fingerprint(ctx, folder, l.Config, it, entry.Root)
			if entry.Reason != "" && entry.Tried == fp {
				continue // failed before, and nothing it depends on changed
			}
			a.autoStart(folder, it, entry, fp)
		}
	}
}

// autoStart delegates a waiting item and records how it went.
func (a *API) autoStart(folder string, it Item, entry Waiting, fp string) {
	still := func() bool {
		cur, ok := a.waiting()[folder][it.ID]
		return ok && cur.Since.Equal(entry.Since)
	}
	res, err := a.delegate(folder, it.ID, entry.Root, &autoStart{waitedFor: it.After, still: still, hash: entry.Hash})
	switch {
	case errors.Is(err, errStarting):
		return // a click is starting it, or the start was cancelled
	case err == nil && res.NotReady:
		return // a dependency moved back meanwhile
	}
	ev := AutoStart{Project: folder, ID: it.ID, Title: it.Title}
	if err != nil {
		ev.Error = err.Error()
		a.mu.Lock()
		w := a.waiting()
		if cur, ok := w[folder][it.ID]; ok && cur.Since.Equal(entry.Since) {
			cur.Reason, cur.Tried = err.Error(), fp
			cur.Changed = errors.Is(err, errItemChanged)
			if errors.Is(err, errUnanswered) {
				cur.Unanswered++
				cur.Reason = unansweredReason(cur.Unanswered)
				ev.Error, ev.Unanswered = cur.Reason, true
			}
			w[folder][it.ID] = cur
			_ = a.host.SaveState(stateWaiting, w)
		}
		a.mu.Unlock()
		a.emit(folder)
	} else {
		ev.Tasks, ev.Warning = len(res.Tasks), res.Warning
	}
	a.host.Emit(EventAutoStart, ev)
}

// itemHash identifies the content of an item's file (Waiting.Hash).
func itemHash(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// rehashWaiting follows Flint's own rewrite of an item's status line (from
// old to now) in the item's waiting start, when the start was made for old:
// Flint's change is not one the user has to confirm again. Callers hold mu.
func (a *API) rehashWaiting(folder, id string, old, now []byte) {
	w := a.waiting()
	cur, ok := w[folder][id]
	if !ok || cur.Hash != itemHash(old) {
		return
	}
	cur.Hash = itemHash(now)
	w[folder][id] = cur
	_ = a.host.SaveState(stateWaiting, w)
}

// dropWaiting forgets a waiting start (the one asked for at since).
func (a *API) dropWaiting(folder, id string, since time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	w := a.waiting()
	if cur, ok := w[folder][id]; !ok || !cur.Since.Equal(since) {
		return
	}
	delete(w[folder], id)
	if len(w[folder]) == 0 {
		delete(w, folder)
	}
	_ = a.host.SaveState(stateWaiting, w)
	a.emit(folder)
}

// fingerprint sums up what Delegate's checks of item it depend on: the
// item's file and the project's, the agents, the checkouts of its
// repositories (folder, workspace, trust, problem), the active folder and the
// trust of the project. A failed start is tried again only when it changed.
func (a *API) fingerprint(ctx context.Context, folder string, c Config, it Item, root string) string {
	data, _, _ := ReadItemFile(folder, it.ID)
	config, _ := readInside(folder, ConfigFile)
	var repos []RepositoryView
	if len(it.Repos) > 0 {
		roots := a.roots(ctx)
		checkouts := a.checkouts()
		for _, name := range it.Repos {
			if decl, ok := c.Repository(name); ok {
				repos = append(repos, a.resolveRepo(ctx, decl, roots, checkouts))
			}
		}
	}
	_, rootErr := a.activeFolder(root)
	if len(it.Repos) > 0 {
		rootErr = nil
	}
	_, statErr := os.Stat(root)
	raw, _ := json.Marshal(struct {
		Item, Config []byte
		Agents       any
		Repos        []RepositoryView
		Root         string
		RootOK       bool
		RootExists   bool
		Trusted      bool
	}{data, config, a.host.Agents(), repos, root, rootErr == nil, statErr == nil, a.host.Trusted(folder)})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
