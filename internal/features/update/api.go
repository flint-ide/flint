package update

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"flint/internal/feature"
	"flint/internal/update"
)

const (
	// eventUpdateAvailable carries an update.Result the user should hear about.
	eventUpdateAvailable = "update:available"
	// The first check waits until launch is over, so it never competes with
	// opening the project.
	updateStartDelay = 5 * time.Second
	// updateRecheck is how often the loop looks whether a day has passed.
	updateRecheck = time.Hour
	// manualCheckReuse: a manual check this soon after the last one reuses its answer.
	manualCheckReuse = 10 * time.Second
	// stateKey is the update part of state.json: when GitHub was last asked,
	// its answer, and the release the user skipped.
	stateKey = "update"
)

// API tells the user about new Flint releases. Flint only links to the
// release page; it never downloads or installs anything itself.
type API struct {
	host    feature.Host
	updates *update.Client // GitHub's latest-release endpoint
	mu      sync.Mutex     // one manual check at a time

	stateMu sync.Mutex // read-modify-write of the state key

	pendingMu sync.Mutex
	pending   *update.Result // a newer release the UI should offer; nil = none

	// launch is the state as the application started, before the first
	// background check wrote to it; nil until rememberLaunch.
	launchMu sync.Mutex
	launch   *update.State
}

func newAPI(h feature.Host) *API {
	return &API{host: h, updates: update.New(h.Version())}
}

// Check asks GitHub now, ignoring the daily limit and the checkForUpdates
// setting (the user asked).
func (u *API) Check() (update.Result, error) {
	ctx := u.context()
	u.mu.Lock()
	defer u.mu.Unlock()
	// Repeated clicks reuse an answer a few seconds old instead of asking
	// GitHub (and rewriting state.json) again.
	st := u.state()
	var rel update.Release
	if st.Latest != nil && time.Since(st.CheckedAt) < manualCheckReuse {
		rel = *st.Latest
	} else {
		var err error
		if rel, err = u.fetchRelease(ctx, time.Now()); err != nil {
			slog.Info("update check failed", "error", err)
			return update.Result{}, err
		}
	}
	r := update.Evaluate(u.host.Version(), rel, u.state().Skipped)
	if r.Available && !r.Dev {
		u.setPending(&r)
	}
	return r, nil
}

// Pending is what the background check found and the user hasn't skipped
// (nil when there is nothing to tell), for a UI that subscribed after the
// update:available event went out.
func (u *API) Pending() *update.Result {
	u.pendingMu.Lock()
	defer u.pendingMu.Unlock()
	if u.pending == nil {
		return nil
	}
	r := *u.pending
	return &r
}

// Skip stops the automatic notice for this release; a later one notifies again.
func (u *API) Skip(tag string) error {
	if !update.IsRelease(tag) {
		return errors.New("not a release version")
	}
	if err := u.saveState(func(s *update.State) { s.Skipped = tag }); err != nil {
		return err
	}
	u.setPending(nil)
	return nil
}

// OpenRelease opens the latest known release page in the browser. The link
// comes from Go, not the UI, and is checked again (state.json is a plain file).
func (u *API) OpenRelease() error {
	link := update.ReleasesURL
	if latest := u.state().Latest; latest != nil {
		link = update.SafeURL(latest.URL)
	}
	return u.host.OpenURL(link)
}

// WhatsNew is the release notes of the running version on its first launch
// after an update, and nil on every other launch. A fresh install and a
// development build show nothing. With the checkForUpdates setting off Flint
// doesn't ask GitHub on its own, so nothing is shown either ("View Release
// Notes" still works).
func (u *API) WhatsNew() (*update.Notes, error) {
	current := u.host.Version()
	st := u.state()
	if !update.IsRelease(current) || st.LastSeen == current {
		return nil, nil
	}
	seen := func() error { return u.saveState(func(s *update.State) { s.LastSeen = current }) }
	if !updatedFrom(u.atLaunch(st), current) || !Enabled.Get(u.host.Settings()) {
		return nil, seen()
	}
	notes, err := u.updates.Notes(u.context(), current)
	if errors.Is(err, update.ErrNoRelease) {
		return nil, seen() // a build GitHub knows nothing about: don't ask at every launch
	}
	if err != nil {
		// Offline: the next launch tries again.
		slog.Info("release notes failed", "error", err)
		return nil, err
	}
	if err := seen(); err != nil {
		return nil, err
	}
	if notes.Empty() {
		return nil, nil
	}
	return &notes, nil
}

// rememberLaunch keeps the state the application started with. The background
// check stamps CheckedAt a few seconds later, which would make a fresh
// install look like one that has run before.
func (u *API) rememberLaunch() {
	st := u.state()
	u.launchMu.Lock()
	u.launch = &st
	u.launchMu.Unlock()
}

// atLaunch is the state at launch, or now when it was not recorded.
func (u *API) atLaunch(now update.State) update.State {
	u.launchMu.Lock()
	defer u.launchMu.Unlock()
	if u.launch == nil {
		return now
	}
	return *u.launch
}

// updatedFrom reports whether current is newer than the version that ran
// last. An installation older than the LastSeen record has none but did check
// for updates before, which tells it apart from a fresh install.
func updatedFrom(st update.State, current string) bool {
	if st.LastSeen == "" {
		return !st.CheckedAt.IsZero()
	}
	order, ok := update.Compare(current, st.LastSeen)
	return ok && order > 0
}

// ReleaseNotes is the release notes of the running version, or of the latest
// release for a development build (the user asked: "View Release Notes").
func (u *API) ReleaseNotes() (update.Notes, error) {
	ctx := u.context()
	tag := u.host.Version()
	if !update.IsRelease(tag) {
		rel, err := u.updates.Latest(ctx)
		if err != nil {
			return update.Notes{}, err
		}
		tag = rel.Tag
	}
	return u.updates.Notes(ctx, tag)
}

// OpenLink opens a link of the release notes in the browser: only pages of
// the Flint repository, anything else leads to the releases page.
func (u *API) OpenLink(link string) error {
	return u.host.OpenURL(update.SafeURL(link))
}

func (u *API) context() context.Context {
	if ctx := u.host.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
}

// watch checks shortly after launch and then whenever the last check is a
// day old, until ctx ends (the application quits). It looks every hour rather
// than sleeping a whole day, so a manual check or a setting turned on later
// can't push the next check a day further.
func (u *API) watch(ctx context.Context) {
	delay, first := updateStartDelay, true
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		if now := time.Now(); first || update.Due(u.state().CheckedAt, now) {
			u.checkForUpdate(ctx, u.host.Version(), now)
		}
		delay, first = updateRecheck, false
	}
}

// checkForUpdate is the automatic check: nothing at all when it is turned off
// or this is a development build, GitHub at most once per update.Interval
// (the last answer is reused in between), and a notice only for a newer,
// not skipped release. Failures are only logged: an offline machine must not
// see errors about a check it didn't ask for.
func (u *API) checkForUpdate(ctx context.Context, current string, now time.Time) {
	if !Enabled.Get(u.host.Settings()) || !update.IsRelease(current) {
		return
	}
	st := u.state()
	latest := st.Latest
	if update.Due(st.CheckedAt, now) {
		rel, err := u.fetchRelease(ctx, now)
		if err != nil {
			slog.Info("update check failed", "error", err)
			return
		}
		latest = &rel
	}
	if latest == nil {
		return
	}
	if r := update.Evaluate(current, *latest, st.Skipped); r.Notify() {
		u.setPending(&r)
		u.host.Emit(eventUpdateAvailable, r)
	}
}

// fetchRelease asks GitHub and remembers when (also after a failure, so an
// unreachable GitHub is asked at most once a day) and what it answered.
func (u *API) fetchRelease(ctx context.Context, now time.Time) (update.Release, error) {
	rel, err := u.updates.Latest(ctx)
	saveErr := u.saveState(func(s *update.State) {
		s.CheckedAt = now
		if err == nil {
			s.Latest = &rel
		}
	})
	if saveErr != nil {
		slog.Warn("save update state", "error", saveErr)
	}
	return rel, err
}

func (u *API) setPending(r *update.Result) {
	u.pendingMu.Lock()
	u.pending = r
	u.pendingMu.Unlock()
}

// state is the update part of state.json (zero when there is none yet).
func (u *API) state() update.State {
	var st update.State
	if err := u.host.LoadState(stateKey, &st); err != nil {
		slog.Warn("read update state", "error", err)
		return update.State{}
	}
	return st
}

// saveState changes the update part of state.json.
func (u *API) saveState(change func(*update.State)) error {
	u.stateMu.Lock()
	defer u.stateMu.Unlock()
	st := u.state() // Latest is replaced, never modified in place
	change(&st)
	return u.host.SaveState(stateKey, st)
}
