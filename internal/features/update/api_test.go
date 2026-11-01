package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"flint/internal/feature/featuretest"
	"flint/internal/settings"
	"flint/internal/update"
)

// newTestAPI is the feature with a fake host, its update client talking to a
// local server that answers with tag; requests counts the calls. No test
// reaches GitHub.
func newTestAPI(t *testing.T, tag string) (*API, *featuretest.Host, *atomic.Int32) {
	t.Helper()
	settings.Register(Enabled)
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte(`{"tag_name": "` + tag + `", "html_url": "https://github.com/flint-ide/flint/releases/tag/` + tag + `"}`))
	}))
	t.Cleanup(srv.Close)
	h := featuretest.New(t.Context())
	api := newAPI(h)
	api.updates.Base = srv.URL
	return api, h, &requests
}

func TestCheckForUpdate(t *testing.T) {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	ctx := context.Background()

	t.Run("notifies about a newer release once a day", func(t *testing.T) {
		a, h, requests := newTestAPI(t, "v1.1.0")
		a.checkForUpdate(ctx, "v1.0.0", now)
		if requests.Load() != 1 {
			t.Fatalf("requests = %d, want 1", requests.Load())
		}
		pending := a.Pending()
		if pending == nil || pending.Latest.Tag != "v1.1.0" {
			t.Fatalf("Pending() = %+v, want v1.1.0", pending)
		}
		if ev := h.Events(); len(ev) != 1 || ev[0].Name != eventUpdateAvailable {
			t.Errorf("events = %+v", ev)
		}
		// Within a day the remembered answer is used: no new request, same notice.
		a.setPending(nil)
		a.checkForUpdate(ctx, "v1.0.0", now.Add(time.Hour))
		if requests.Load() != 1 {
			t.Errorf("requests = %d after a second check within a day, want 1", requests.Load())
		}
		if a.Pending() == nil {
			t.Error("the remembered release is not offered again")
		}
		a.checkForUpdate(ctx, "v1.0.0", now.Add(update.Interval))
		if requests.Load() != 2 {
			t.Errorf("requests = %d a day later, want 2", requests.Load())
		}
	})

	t.Run("up to date", func(t *testing.T) {
		a, _, _ := newTestAPI(t, "v1.0.0")
		a.checkForUpdate(ctx, "v1.0.0", now)
		if p := a.Pending(); p != nil {
			t.Errorf("Pending() = %+v, want nil", p)
		}
	})

	t.Run("turned off: no request", func(t *testing.T) {
		a, h, requests := newTestAPI(t, "v1.1.0")
		h.SetSettings(Enabled.With(settings.Default(), false))
		a.checkForUpdate(ctx, "v1.0.0", now)
		if requests.Load() != 0 || a.Pending() != nil {
			t.Errorf("requests = %d, pending %v; want nothing", requests.Load(), a.Pending())
		}
	})

	t.Run("development build: no request", func(t *testing.T) {
		a, _, requests := newTestAPI(t, "v1.1.0")
		for _, dev := range []string{"dev", "dev-0123456789ab+dirty"} {
			a.checkForUpdate(ctx, dev, now)
		}
		if requests.Load() != 0 {
			t.Errorf("requests = %d, want 0", requests.Load())
		}
	})

	t.Run("skipped release stays quiet, the next one notifies", func(t *testing.T) {
		a, _, _ := newTestAPI(t, "v1.1.0")
		if err := a.Skip("v1.1.0"); err != nil {
			t.Fatal(err)
		}
		a.checkForUpdate(ctx, "v1.0.0", now)
		if a.Pending() != nil {
			t.Error("a skipped release is offered")
		}
		if err := a.saveState(func(s *update.State) { s.Latest = &update.Release{Tag: "v1.2.0"} }); err != nil {
			t.Fatal(err)
		}
		a.checkForUpdate(ctx, "v1.0.0", now.Add(time.Minute))
		if p := a.Pending(); p == nil || p.Latest.Tag != "v1.2.0" {
			t.Errorf("Pending() = %+v, want v1.2.0", p)
		}
		if err := a.Skip("nightly"); err == nil {
			t.Error("Skip accepted a non-release tag")
		}
	})

	t.Run("failure is remembered, so GitHub is asked once a day", func(t *testing.T) {
		a, _, requests := newTestAPI(t, "garbage")
		a.checkForUpdate(ctx, "v1.0.0", now)
		a.checkForUpdate(ctx, "v1.0.0", now.Add(time.Hour))
		if requests.Load() != 1 || a.Pending() != nil {
			t.Errorf("requests = %d, pending %v; want 1 and nothing", requests.Load(), a.Pending())
		}
	})

	t.Run("state is kept under its state.json key", func(t *testing.T) {
		a, h, _ := newTestAPI(t, "v1.1.0")
		a.checkForUpdate(ctx, "v1.0.0", now)
		restarted := newAPI(h)
		st := restarted.state()
		if !st.CheckedAt.Equal(now) || st.Latest == nil || st.Latest.Tag != "v1.1.0" {
			t.Errorf("state after restart = %+v (raw %s)", st, h.State(stateKey))
		}
	})

	t.Run("the release page comes from Go", func(t *testing.T) {
		a, h, _ := newTestAPI(t, "v1.1.0")
		if err := a.OpenRelease(); err != nil {
			t.Fatal(err)
		}
		a.checkForUpdate(ctx, "v1.0.0", now)
		if err := a.OpenRelease(); err != nil {
			t.Fatal(err)
		}
		opened := h.Opened()
		if len(opened) != 2 || opened[0] != update.ReleasesURL || opened[1] != "https://github.com/flint-ide/flint/releases/tag/v1.1.0" {
			t.Errorf("opened %q", opened)
		}
	})
}
