//go:build !server

package main

import (
	"context"
	"fmt"
	"strconv"
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/dock"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

// eventNotificationOpen asks the frontend to show the tab a clicked
// notification was about.
const eventNotificationOpen = "notification:open"

// AlertsAPI tells about work that finished while Flint was in the background:
// a macOS notification and a count on the Dock icon. The frontend decides when.
type AlertsAPI struct {
	app      *App
	notifier *notifications.NotificationService
	dock     *dock.DockService
	// notifyErr is why notifications can't be shown (no app bundle in
	// development builds, or the user declined); nil when they can.
	notifyErr atomic.Pointer[error]
	seq       atomic.Uint64
}

func newAlertsAPI(app *App) *AlertsAPI {
	return &AlertsAPI{app: app, notifier: notifications.New(), dock: dock.New()}
}

// ServiceStartup starts the Wails services; one that can't start only turns its
// feature off (a development binary has no bundle, which notifications need).
func (s *AlertsAPI) ServiceStartup(ctx context.Context, opts application.ServiceOptions) error {
	if err := s.notifier.ServiceStartup(ctx, opts); err != nil {
		s.notifyErr.Store(&err)
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
// editor tab id, may be ""). macOS asks the user for permission the first time.
func (s *AlertsAPI) Notify(title, body, tab string) error {
	if err := s.notifyErr.Load(); err != nil {
		return *err
	}
	ok, err := s.notifier.CheckNotificationAuthorization()
	if err == nil && !ok {
		ok, err = s.notifier.RequestNotificationAuthorization()
	}
	if err != nil || !ok {
		denied := fmt.Errorf("notifications are not allowed for Flint")
		if err != nil {
			denied = err
		}
		s.notifyErr.Store(&denied)
		return denied
	}
	return s.notifier.SendNotification(notifications.NotificationOptions{
		ID:    "flint-" + strconv.FormatUint(s.seq.Add(1), 10),
		Title: title,
		Body:  body,
		Data:  map[string]any{"tab": tab},
	})
}

// SetBadge shows count on the Dock icon (0 removes it).
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
