//go:build server

package main

import "errors"

// AlertsAPI has nothing to alert with in the browser-only server build.
type AlertsAPI struct{}

func newAlertsAPI(app *App) *AlertsAPI {
	s := &AlertsAPI{}
	app.alerts = s
	return s
}

// errServerBuild: notifications and the badge need the desktop app.
var errServerBuild = errors.New("notifications need the desktop app")

func (*AlertsAPI) Notify(string, string, string) error { return errServerBuild }

func (*AlertsAPI) SetBadge(int) error { return nil }

// Status says that this build cannot show notifications.
func (*AlertsAPI) Status() NotificationStatus {
	return NotificationStatus{
		State:  notifyUnavailable,
		Detail: "Not available in this build: the browser build (server mode) can't show desktop notifications or a badge; the desktop app can.",
	}
}

// Test fails: there is nothing to show the notification with.
func (*AlertsAPI) Test() error { return errServerBuild }
