package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"flint/internal/config"
	"flint/internal/settings"
	"flint/internal/update"
)

// newUpdateTestApp is an App whose update client talks to a local server that
// answers with tag; requests counts the calls. No test reaches GitHub.
func newUpdateTestApp(t *testing.T, tag string) (*App, *atomic.Int32) {
	t.Helper()
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte(`{"tag_name": "` + tag + `", "html_url": "https://github.com/flint-ide/flint/releases/tag/` + tag + `"}`))
	}))
	t.Cleanup(srv.Close)
	c := update.New("v1.0.0")
	c.Base = srv.URL
	return &App{configDir: t.TempDir(), settings: settings.Default(), updates: c}, &requests
}

func TestCheckForUpdate(t *testing.T) {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	ctx := context.Background()

	t.Run("notifies about a newer release once a day", func(t *testing.T) {
		a, requests := newUpdateTestApp(t, "v1.1.0")
		a.checkForUpdate(ctx, "v1.0.0", now)
		if requests.Load() != 1 {
			t.Fatalf("requests = %d, want 1", requests.Load())
		}
		pending := (&UpdateAPI{app: a}).Pending()
		if pending == nil || pending.Latest.Tag != "v1.1.0" {
			t.Fatalf("Pending() = %+v, want v1.1.0", pending)
		}
		// Within a day the remembered answer is used: no new request, same notice.
		a.setPendingUpdate(nil)
		a.checkForUpdate(ctx, "v1.0.0", now.Add(time.Hour))
		if requests.Load() != 1 {
			t.Errorf("requests = %d after a second check within a day, want 1", requests.Load())
		}
		if (&UpdateAPI{app: a}).Pending() == nil {
			t.Error("the remembered release is not offered again")
		}
		a.checkForUpdate(ctx, "v1.0.0", now.Add(update.Interval))
		if requests.Load() != 2 {
			t.Errorf("requests = %d a day later, want 2", requests.Load())
		}
	})

	t.Run("up to date", func(t *testing.T) {
		a, _ := newUpdateTestApp(t, "v1.0.0")
		a.checkForUpdate(ctx, "v1.0.0", now)
		if p := (&UpdateAPI{app: a}).Pending(); p != nil {
			t.Errorf("Pending() = %+v, want nil", p)
		}
	})

	t.Run("turned off: no request", func(t *testing.T) {
		a, requests := newUpdateTestApp(t, "v1.1.0")
		a.settings.CheckForUpdates = false
		a.checkForUpdate(ctx, "v1.0.0", now)
		if requests.Load() != 0 || (&UpdateAPI{app: a}).Pending() != nil {
			t.Errorf("requests = %d, pending %v; want nothing", requests.Load(), (&UpdateAPI{app: a}).Pending())
		}
	})

	t.Run("development build: no request", func(t *testing.T) {
		a, requests := newUpdateTestApp(t, "v1.1.0")
		for _, dev := range []string{"dev", "dev-0123456789ab+dirty"} {
			a.checkForUpdate(ctx, dev, now)
		}
		if requests.Load() != 0 {
			t.Errorf("requests = %d, want 0", requests.Load())
		}
	})

	t.Run("skipped release stays quiet, the next one notifies", func(t *testing.T) {
		a, _ := newUpdateTestApp(t, "v1.1.0")
		api := &UpdateAPI{app: a}
		if err := api.Skip("v1.1.0"); err != nil {
			t.Fatal(err)
		}
		a.checkForUpdate(ctx, "v1.0.0", now)
		if api.Pending() != nil {
			t.Error("a skipped release is offered")
		}
		if err := a.saveUpdateState(func(s *update.State) { s.Latest = &update.Release{Tag: "v1.2.0"} }); err != nil {
			t.Fatal(err)
		}
		a.checkForUpdate(ctx, "v1.0.0", now.Add(time.Minute))
		if p := api.Pending(); p == nil || p.Latest.Tag != "v1.2.0" {
			t.Errorf("Pending() = %+v, want v1.2.0", p)
		}
		if err := api.Skip("nightly"); err == nil {
			t.Error("Skip accepted a non-release tag")
		}
	})

	t.Run("failure is remembered, so GitHub is asked once a day", func(t *testing.T) {
		a, requests := newUpdateTestApp(t, "garbage")
		a.checkForUpdate(ctx, "v1.0.0", now)
		a.checkForUpdate(ctx, "v1.0.0", now.Add(time.Hour))
		if requests.Load() != 1 || (&UpdateAPI{app: a}).Pending() != nil {
			t.Errorf("requests = %d, pending %v; want 1 and nothing", requests.Load(), (&UpdateAPI{app: a}).Pending())
		}
	})

	t.Run("state survives a restart", func(t *testing.T) {
		a, _ := newUpdateTestApp(t, "v1.1.0")
		a.checkForUpdate(ctx, "v1.0.0", now)
		restarted := &App{configDir: a.configDir}
		if err := config.LoadJSON(restarted.statePath(), &restarted.state); err != nil {
			t.Fatal(err)
		}
		st := restarted.updateState()
		if !st.CheckedAt.Equal(now) || st.Latest == nil || st.Latest.Tag != "v1.1.0" {
			t.Errorf("state after restart = %+v", st)
		}
	})
}
