//go:build !server

package main

import (
	"context"
	"runtime"
	"strconv"
	"sync/atomic"

	"flint/internal/redact"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/dock"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

// eventNotificationOpen asks the frontend to show the tab a clicked
// notification was about.
const eventNotificationOpen = "notification:open"

// AlertsAPI tells the user about what needs them while Flint is in the
// background: a desktop notification, and a count on the Dock icon (macOS;
// Linux has no Dock badge). The frontend decides when for what it sees (chat
// events, runs, CI); Go itself for its own questions (attention.go).
type AlertsAPI struct {
	app      *App
	notifier *notifications.NotificationService
	dock     *dock.DockService
	// startErr is why the notification service could not start (no app
	// bundle in development builds, no D-Bus session); nil when it did.
	// Authorization is asked every time instead: the user may allow Flint in
	// System Settings while it runs.
	startErr atomic.Pointer[error]
	status   notifyStatus
	seq      atomic.Uint64
}

func newAlertsAPI(app *App) *AlertsAPI {
	s := &AlertsAPI{app: app, notifier: notifications.New(), dock: dock.New()}
	s.status.logf = app.logf
	app.alerts = s
	return s
}

// ServiceStartup starts the Wails services; one that can't start only turns its
// feature off (a development binary has no bundle, which notifications need).
func (s *AlertsAPI) ServiceStartup(ctx context.Context, opts application.ServiceOptions) error {
	if err := s.notifier.ServiceStartup(ctx, opts); err != nil {
		s.startErr.Store(&err)
		s.status.failed(err)
	} else {
		s.notifier.OnNotificationResponse(s.opened)
	}
	_ = s.dock.ServiceStartup(ctx, opts)
	return nil
}

func (s *AlertsAPI) ServiceShutdown() error {
	_ = s.dock.ServiceShutdown()
	return s.notifier.ServiceShutdown()
}

// Notify shows a notification; clicking it brings Flint forward on tab (an
// editor tab id, may be ""). macOS asks the user for permission the first
// time. A failure is logged once and shown in Settings ▸ Agents (Status).
func (s *AlertsAPI) Notify(title, body, tab string) error {
	return s.status.record(s.send(title, body, tab))
}

func (s *AlertsAPI) send(title, body, tab string) error {
	if err := s.startErr.Load(); err != nil {
		return *err
	}
	if err := s.authorize(); err != nil {
		return err
	}
	return s.notifier.SendNotification(notifications.NotificationOptions{
		ID:    "flint-" + strconv.FormatUint(s.seq.Add(1), 10),
		Title: notificationText(title),
		Body:  notificationText(body),
		Data:  map[string]any{"tab": tab},
	})
}

// authorize checks the OS permission, asking for it when the user has not
// decided yet. Checked on every notification, never cached: a denial the
// user lifts in System Settings counts from the next one.
func (s *AlertsAPI) authorize() error {
	ok, err := s.notifier.CheckNotificationAuthorization()
	if err == nil && !ok {
		ok, err = s.notifier.RequestNotificationAuthorization()
	}
	if err != nil {
		return err
	}
	if !ok {
		return errNotAllowed
	}
	return nil
}

// Status tells Settings ▸ Agents whether notifications can be delivered.
func (s *AlertsAPI) Status() NotificationStatus {
	if err := s.startErr.Load(); err != nil {
		return NotificationStatus{State: notifyUnavailable, Detail: unavailableDetail(*err)}
	}
	ok, err := s.notifier.CheckNotificationAuthorization()
	switch {
	case err != nil:
		return NotificationStatus{State: notifyUnavailable, Detail: unavailableDetail(err)}
	case !ok:
		return NotificationStatus{State: notifyDenied, Detail: deniedDetail()}
	}
	return s.status.view()
}

// Test sends a test notification, asking for the permission first if the
// user has not decided yet.
func (s *AlertsAPI) Test() error {
	return s.Notify("Flint", testNotificationBody, "")
}

// unavailableDetail says why notifications can't be shown at all.
func unavailableDetail(err error) string {
	if runtime.GOOS == "darwin" {
		return "macOS shows notifications only for an installed app (a build from source run outside its app bundle has none): " + err.Error()
	}
	return "No notification service answered (Flint uses the desktop's org.freedesktop.Notifications over D-Bus): " + err.Error()
}

// deniedDetail says what to do about a denial.
func deniedDetail() string {
	if runtime.GOOS == "darwin" {
		return "Flint is not allowed to show notifications. Allow Flint in System Settings ▸ Notifications, or choose Send Test Notification to be asked."
	}
	return "The desktop does not allow Flint's notifications: allow them in your desktop's notification settings."
}

// notificationText is what reaches the OS: notifications show on the lock
// screen and in other apps' history, and some carry text an agent wrote (what
// it asks to run), so secrets are masked here whatever the caller did.
func notificationText(s string) string { return redact.Secrets(s) }

// SetBadge shows count on the Dock icon (0 removes it); nothing on Linux.
func (s *AlertsAPI) SetBadge(count int) error {
	if count <= 0 {
		return s.dock.RemoveBadge()
	}
	return s.dock.SetBadge(strconv.Itoa(count))
}

func (s *AlertsAPI) opened(result notifications.NotificationResult) {
	if result.Error != nil {
		return
	}
	if w := s.app.window; w != nil {
		w.UnMinimise()
		w.Show()
		w.Focus()
	}
	tab, _ := result.Response.UserInfo["tab"].(string)
	s.app.emit(eventNotificationOpen, tab)
}
