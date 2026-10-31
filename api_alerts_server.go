//go:build server

package main

import "errors"

// AlertsAPI has nothing to alert with in the browser-only server build.
type AlertsAPI struct{}

func newAlertsAPI(*App) *AlertsAPI { return &AlertsAPI{} }

func (*AlertsAPI) Notify(string, string, string) error {
	return errors.New("notifications need the desktop app")
}

func (*AlertsAPI) SetBadge(int) error { return nil }
