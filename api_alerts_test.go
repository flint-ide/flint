//go:build !server

package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

func TestNotificationTextMasksSecrets(t *testing.T) {
	tests := []struct {
		name, in string
		hidden   string
	}{
		{"github token", "app: Run curl -H 'Authorization: token ghp_abcdefghijklmnopqrstuvwxyz0123456789'", "ghp_abcdefghijklmnopqrstuvwxyz0123456789"},
		{"assignment", `app: Run API_KEY="s3cr3tvalue123" ./deploy`, "s3cr3tvalue123"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := notificationText(tt.in)
			if strings.Contains(got, tt.hidden) {
				t.Errorf("notificationText(%q) = %q, still holds the secret", tt.in, got)
			}
		})
	}
	if got := notificationText("app: Run npm test"); got != "app: Run npm test" {
		t.Errorf("plain text changed: %q", got)
	}
}

// fakeOS is the operating system's notification service: a decision the
// user has made (or not), and what was sent.
type fakeOS struct {
	allowed, decided bool // the user's choice, and whether they made one
	asked            int
	sent             []notifications.NotificationOptions
	sendErr          error
}

func (f *fakeOS) CheckNotificationAuthorization() (bool, error) { return f.decided && f.allowed, nil }

func (f *fakeOS) RequestNotificationAuthorization() (bool, error) {
	f.asked++
	f.decided = true
	return f.allowed, nil
}

func (f *fakeOS) SendNotification(o notifications.NotificationOptions) error {
	f.sent = append(f.sent, o)
	return f.sendErr
}

func newTestAlerts(os *fakeOS) *AlertsAPI {
	app := &App{}
	s := &AlertsAPI{app: app, os: os}
	s.status.logf = func(string, ...any) {}
	return s
}

// Notify asks for the permission when the user has not decided, checks it on
// every notification (never cached), masks secrets, numbers each one and
// carries the tab a click shows; Status follows what happened.
func TestNotifyAuthorizesAndSends(t *testing.T) {
	os := &fakeOS{allowed: true}
	s := newTestAlerts(os)
	if err := s.Notify("claude is waiting", "app: Run TOKEN=s3cr3tvalue123 ./x", "chat:c1"); err != nil {
		t.Fatal(err)
	}
	if os.asked != 1 || len(os.sent) != 1 {
		t.Fatalf("asked %d, sent %d", os.asked, len(os.sent))
	}
	got := os.sent[0]
	if got.Title != "claude is waiting" || strings.Contains(got.Body, "s3cr3tvalue123") || got.Data["tab"] != "chat:c1" || got.ID == "" {
		t.Errorf("sent %+v", got)
	}
	_ = s.Notify("t", "b", "")
	if os.asked != 1 || len(os.sent) != 2 || os.sent[1].ID == got.ID {
		t.Errorf("second: asked %d, sent %+v", os.asked, os.sent)
	}
	if v := s.Status(); v.State != notifyAllowed {
		t.Errorf("status %+v", v)
	}
	// The user turns them off in the system: nothing is sent, and Settings says so.
	os.allowed = false
	if err := s.Notify("t", "b", ""); err != errNotAllowed || len(os.sent) != 2 {
		t.Errorf("denied: %v, sent %d", err, len(os.sent))
	}
	if v := s.Status(); v.State != notifyDenied {
		t.Errorf("status %+v", v)
	}
	// Allowed again: it counts from the next one.
	os.allowed = true
	os.sendErr = errors.New("dbus: no reply")
	if err := s.Notify("t", "b", ""); err == nil {
		t.Fatal("a failed delivery was not reported")
	}
	if v := s.Status(); v.State != notifyFailed || !strings.Contains(v.Detail, "dbus: no reply") {
		t.Errorf("status %+v", v)
	}
}
