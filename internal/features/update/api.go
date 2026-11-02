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
}

func newAPI(h feature.Host) *API {
	return &API{host: h, updates: update.New(h.Version())}
}

// Check asks GitHub now, ignoring the daily limit and the checkForUpdates
// setting (the user asked).
func (u *API) Check() (update.Result, error) {
	ctx := u.host.Context()
	if ctx == nil {
		ctx = context.Background()
	}
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
