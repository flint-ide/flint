package update

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"flint/internal/feature/featuretest"
	"flint/internal/settings"
	"flint/internal/update"
)

// newNotesAPI is the feature running version, its client talking to a local
// server that knows the releases in bodies (tag → release notes); requests
// counts the calls.
func newNotesAPI(t *testing.T, version string, bodies map[string]string) (*API, *featuretest.Host, *atomic.Int32) {
	t.Helper()
	settings.Register(Enabled)
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		tag := strings.TrimPrefix(r.URL.Path, "/repos/flint-ide/flint/releases/tags/")
		if strings.HasSuffix(r.URL.Path, "/releases/latest") {
			tag = "v1.1.0"
		}
		body, ok := bodies[tag]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"tag_name": "` + tag + `", "html_url": "https://github.com/flint-ide/flint/releases/tag/` + tag + `", "body": "` + body + `"}`))
	}))
	t.Cleanup(srv.Close)
	h := featuretest.New(t.Context())
	h.VersionString = version
	api := newAPI(h)
	api.updates.Base = srv.URL
	return api, h, &requests
}

func TestWhatsNew(t *testing.T) {
	bodies := map[string]string{"v1.1.0": `* feat: release notes page\n* fix: toast position`, "v1.2.0": ""}
	ran := func(t *testing.T, a *API, version string) {
		t.Helper()
		if err := a.saveState(func(s *update.State) { s.LastSeen = version }); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("shown once after an update", func(t *testing.T) {
		a, _, requests := newNotesAPI(t, "v1.1.0", bodies)
		ran(t, a, "v1.0.0")
		notes, err := a.WhatsNew()
		if err != nil || notes == nil {
			t.Fatalf("WhatsNew() = %+v, %v", notes, err)
		}
		if notes.Tag != "v1.1.0" || len(notes.Features) != 1 || len(notes.Fixes) != 1 {
			t.Errorf("notes = %+v", notes)
		}
		if again, err := a.WhatsNew(); err != nil || again != nil {
			t.Errorf("second launch: WhatsNew() = %+v, %v; want nothing", again, err)
		}
		if requests.Load() != 1 {
			t.Errorf("requests = %d, want 1", requests.Load())
		}
	})

	t.Run("an installation older than the record", func(t *testing.T) {
		a, _, _ := newNotesAPI(t, "v1.1.0", bodies)
		if err := a.saveState(func(s *update.State) { s.CheckedAt = time.Now() }); err != nil {
			t.Fatal(err)
		}
		if notes, err := a.WhatsNew(); err != nil || notes == nil {
			t.Errorf("WhatsNew() = %+v, %v; want the notes", notes, err)
		}
	})

	t.Run("a fresh install whose first check already ran", func(t *testing.T) {
		a, _, requests := newNotesAPI(t, "v1.1.0", bodies)
		a.rememberLaunch()
		// The background check stamps the state before the UI asks.
		if err := a.saveState(func(s *update.State) { s.CheckedAt = time.Now() }); err != nil {
			t.Fatal(err)
		}
		if notes, err := a.WhatsNew(); err != nil || notes != nil {
			t.Errorf("WhatsNew() = %+v, %v; want nothing", notes, err)
		}
		if requests.Load() != 0 {
			t.Errorf("requests = %d, want 0", requests.Load())
		}
	})

	quiet := func(name, version, lastSeen string, prepare func(*featuretest.Host)) {
		t.Run(name, func(t *testing.T) {
			a, h, requests := newNotesAPI(t, version, bodies)
			if lastSeen != "" {
				ran(t, a, lastSeen)
			}
			if prepare != nil {
				prepare(h)
			}
			if notes, err := a.WhatsNew(); err != nil || notes != nil {
				t.Errorf("WhatsNew() = %+v, %v; want nothing", notes, err)
			}
			if requests.Load() != 0 {
				t.Errorf("requests = %d, want 0", requests.Load())
			}
			if update.IsRelease(version) && a.state().LastSeen != version {
				t.Errorf("LastSeen = %q, want %q", a.state().LastSeen, version)
			}
		})
	}
	quiet("fresh install", "v1.1.0", "", nil)
	quiet("same version", "v1.1.0", "v1.1.0", nil)
	quiet("downgrade", "v1.1.0", "v1.2.0", nil)
	quiet("development build", "dev-0123456789ab", "v1.0.0", nil)
	quiet("update checks turned off", "v1.1.0", "v1.0.0", func(h *featuretest.Host) {
		h.SetSettings(Enabled.With(settings.Default(), false))
	})

	t.Run("a release without notes, or unknown to GitHub, is remembered", func(t *testing.T) {
		for _, version := range []string{"v1.2.0", "v1.3.0"} {
			a, _, requests := newNotesAPI(t, version, bodies)
			ran(t, a, "v1.0.0")
			for range 2 {
				if notes, err := a.WhatsNew(); err != nil || notes != nil {
					t.Errorf("%s: WhatsNew() = %+v, %v; want nothing", version, notes, err)
				}
			}
			if requests.Load() != 1 {
				t.Errorf("%s: requests = %d, want 1", version, requests.Load())
			}
		}
	})

	t.Run("a failure is tried again at the next launch", func(t *testing.T) {
		a, _, _ := newNotesAPI(t, "v1.1.0", bodies)
		ran(t, a, "v1.0.0")
		good := a.updates.Base
		a.updates.Base = "http://127.0.0.1:1"
		if notes, err := a.WhatsNew(); err == nil || notes != nil {
			t.Fatalf("WhatsNew() = %+v, %v; want an error", notes, err)
		}
		a.updates.Base = good
		if notes, err := a.WhatsNew(); err != nil || notes == nil {
			t.Errorf("WhatsNew() = %+v, %v; want the notes", notes, err)
		}
	})
}

func TestReleaseNotes(t *testing.T) {
	bodies := map[string]string{"v1.1.0": `* feat: release notes page`}
	for _, version := range []string{"v1.1.0", "dev-0123456789ab"} {
		a, _, _ := newNotesAPI(t, version, bodies)
		notes, err := a.ReleaseNotes()
		if err != nil || notes.Tag != "v1.1.0" || len(notes.Features) != 1 {
			t.Errorf("%s: ReleaseNotes() = %+v, %v", version, notes, err)
		}
	}
}

func TestOpenLink(t *testing.T) {
	a, h, _ := newNotesAPI(t, "v1.1.0", nil)
	for _, link := range []string{"https://github.com/flint-ide/flint/pull/7", "https://evil.example/pull/7"} {
		if err := a.OpenLink(link); err != nil {
			t.Fatal(err)
		}
	}
	if opened := h.Opened(); len(opened) != 2 || opened[0] != "https://github.com/flint-ide/flint/pull/7" || opened[1] != update.ReleasesURL {
		t.Errorf("opened %q", opened)
	}
}
