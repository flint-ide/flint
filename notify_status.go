package main

import (
	"errors"
	"sync"
)

// Whether desktop notifications reach the user, for Settings ▸ Agents
// (AlertsAPI.Status). A notification that fails is otherwise silent: the
// failure is logged once and shown there until one is delivered again.

// NotificationStatus.State values.
const (
	notifyAllowed     = "allowed"     // the last notification (if any) was delivered
	notifyDenied      = "denied"      // the OS does not allow Flint's notifications
	notifyUnavailable = "unavailable" // nothing can show them (no bundle, no service, server build)
	notifyFailed      = "failed"      // allowed, but the last one could not be delivered
)

// NotificationStatus says whether notifications can be delivered, and
// Detail what to do when they can't.
type NotificationStatus struct {
	State  string `json:"state"`
	Detail string `json:"detail"`
}

// errNotAllowed is a denial by the OS.
var errNotAllowed = errors.New("notifications are not allowed for Flint")

// testNotificationBody is what Send Test Notification shows.
const testNotificationBody = "This is how Flint tells you that an agent needs you."

// notifyStatus remembers the last delivery failure and logs each distinct
// one once.
type notifyStatus struct {
	logf   func(format string, args ...any)
	mu     sync.Mutex
	last   string          // the last failure, "" after a delivery
	logged map[string]bool // failures already logged
}

// record notes how a delivery went and returns err.
func (s *notifyStatus) record(err error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil {
		s.last = ""
		return nil
	}
	s.last = err.Error()
	if !s.logged[s.last] {
		if s.logged == nil {
			s.logged = map[string]bool{}
		}
		if len(s.logged) < 32 { // bounded: a flood of distinct errors logs the first ones
			s.logged[s.last] = true
			if s.logf != nil {
				s.logf("notification not delivered: %s", s.last)
			}
		}
	}
	return err
}

// failed records err without returning it.
func (s *notifyStatus) failed(err error) { _ = s.record(err) }

// view is the status from the deliveries alone (the OS allows them).
func (s *notifyStatus) view() NotificationStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.last != "" {
		return NotificationStatus{State: notifyFailed, Detail: "The last notification could not be delivered: " + s.last}
	}
	return NotificationStatus{State: notifyAllowed}
}
