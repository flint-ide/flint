package main

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"flint/internal/config"
	"flint/internal/diag"
	"flint/internal/update"
)

const (
	// eventUpdateAvailable carries an update.Result the user should hear about.
	eventUpdateAvailable = "update:available"
	// The first check waits until launch is over, so it never competes with
	// opening the project.
	updateStartDelay = 5 * time.Second
)

// UpdateAPI tells the user about new Flint releases. Flint only links to the
// release page; it never downloads or installs anything itself.
type UpdateAPI struct {
	app *App
}

// Check asks GitHub now, ignoring the daily limit and the checkForUpdates
// setting (the user asked).
func (u *UpdateAPI) Check() (update.Result, error) {
	a := u.app
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	rel, err := a.fetchRelease(ctx, time.Now())
	if err != nil {
		slog.Info("update check failed", "error", err)
		return update.Result{}, err
	}
	r := update.Evaluate(diag.Version(version), rel, a.updateState().Skipped)
	if r.Available && !r.Dev {
		a.setPendingUpdate(&r)
	}
	return r, nil
}

// Pending is what the background check found and the user hasn't skipped
// (nil when there is nothing to tell), for a UI that subscribed after the
// update:available event went out.
func (u *UpdateAPI) Pending() *update.Result {
	u.app.updateMu.Lock()
	defer u.app.updateMu.Unlock()
	if u.app.pendingUpdate == nil {
		return nil
	}
	r := *u.app.pendingUpdate
	return &r
}

// Skip stops the automatic notice for this release; a later one notifies again.
func (u *UpdateAPI) Skip(tag string) error {
	if !update.IsRelease(tag) {
		return errors.New("not a release version")
	}
	if err := u.app.saveUpdateState(func(s *update.State) { s.Skipped = tag }); err != nil {
		return err
	}
	u.app.setPendingUpdate(nil)
	return nil
}

// OpenRelease opens the latest known release page in the browser. The link
// comes from Go, not the UI, and is checked again (state.json is a plain file).
func (u *UpdateAPI) OpenRelease() error {
	link := update.ReleasesURL
	if latest := u.app.updateState().Latest; latest != nil {
		link = update.SafeURL(latest.URL)
	}
	return u.app.openURL(link)
}

// watchForUpdates checks shortly after launch and then once a day while Flint
// runs, until ctx ends (the application quits).
func (a *App) watchForUpdates(ctx context.Context) {
	delay := updateStartDelay
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		a.checkForUpdate(ctx, diag.Version(version), time.Now())
		delay = update.Interval
	}
}

// checkForUpdate is the automatic check: nothing at all when it is turned off
// or this is a development build, GitHub at most once per update.Interval
// (the last answer is reused in between), and a notice only for a newer,
// not skipped release. Failures are only logged: an offline machine must not
// see errors about a check it didn't ask for.
func (a *App) checkForUpdate(ctx context.Context, current string, now time.Time) {
	if !a.currentSettings().CheckForUpdates || !update.IsRelease(current) {
		return
	}
	st := a.updateState()
	latest := st.Latest
	if update.Due(st.CheckedAt, now) {
		rel, err := a.fetchRelease(ctx, now)
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
		a.setPendingUpdate(&r)
		a.emit(eventUpdateAvailable, r)
	}
}

// fetchRelease asks GitHub and remembers when (also after a failure, so an
// unreachable GitHub is asked at most once a day) and what it answered.
func (a *App) fetchRelease(ctx context.Context, now time.Time) (update.Release, error) {
	rel, err := a.updates.Latest(ctx)
	saveErr := a.saveUpdateState(func(s *update.State) {
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

func (a *App) setPendingUpdate(r *update.Result) {
	a.updateMu.Lock()
	a.pendingUpdate = r
	a.updateMu.Unlock()
}

func (a *App) updateState() update.State {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	return a.state.Update
}

// saveUpdateState changes the update part of state.json.
func (a *App) saveUpdateState(change func(*update.State)) error {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	next := a.state // Latest is replaced, never modified in place, so sharing it is safe
	change(&next.Update)
	if err := config.SaveJSON(a.statePath(), next); err != nil {
		return err
	}
	a.state = next
	return nil
}
