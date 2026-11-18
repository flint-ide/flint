package main

import (
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"flint/internal/settings"
)

// fakeNotifier records the notifications Flint sends instead of showing them.
type fakeNotifier struct {
	mu   sync.Mutex
	sent []string // "title|body|tab"
	err  error
}

func (f *fakeNotifier) Notify(title, body, tab string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, title+"|"+body+"|"+tab)
	return f.err
}

func (f *fakeNotifier) list() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.sent)
}

// questionApp is an App whose questions are answered by answer (a.ask), with
// a fake notifier, recorded events and a reminder the test fires by hand.
type questionApp struct {
	app      *App
	notes    *fakeNotifier
	mu       sync.Mutex
	events   []QuestionEvent
	reminder []func()
	backs    int
}

func newQuestionApp(t *testing.T, answer func(title string) bool) *questionApp {
	t.Helper()
	q := &questionApp{notes: &fakeNotifier{}}
	a := &App{configDir: t.TempDir(), settings: settings.Default()}
	a.ask = func(title, _, _ string) bool { return answer(title) }
	a.alerts = q.notes
	a.emitHook = func(name string, data ...any) {
		if name == eventQuestion {
			q.mu.Lock()
			q.events = append(q.events, data[0].(QuestionEvent))
			q.mu.Unlock()
		}
	}
	a.attn = a.newAttention()
	a.attn.back = func() { q.mu.Lock(); q.backs++; q.mu.Unlock() }
	a.attn.after = func(_ time.Duration, f func()) func() bool {
		q.mu.Lock()
		q.reminder = append(q.reminder, f)
		q.mu.Unlock()
		return func() bool { return true }
	}
	q.app = a
	return q
}

func (q *questionApp) questionEvents() []QuestionEvent {
	q.mu.Lock()
	defer q.mu.Unlock()
	return slices.Clone(q.events)
}

func TestQuestionInFrontIsNotAnnounced(t *testing.T) {
	q := newQuestionApp(t, func(string) bool { return true })
	if !q.app.askUser("Allow this agent action?", "msg", "Allow") {
		t.Fatal("the answer was lost")
	}
	if got := q.notes.list(); len(got) != 0 {
		t.Errorf("notified while in front: %v", got)
	}
	// The UI still hears it open and close (the badge counts it meanwhile).
	ev := q.questionEvents()
	if len(ev) != 2 || !ev[0].Open || ev[1].Open || ev[0].ID != ev[1].ID || ev[0].Title != "Allow this agent action?" {
		t.Errorf("events = %+v", ev)
	}
}

func TestQuestionInBackgroundIsAnnounced(t *testing.T) {
	q := newQuestionApp(t, func(string) bool { return false })
	q.app.attn.setFocused(false)
	q.app.askUser("Let this agent direct other agents?", "msg", "Allow")
	want := []string{"Flint needs your answer|Let this agent direct other agents?|"}
	if got := q.notes.list(); !slices.Equal(got, want) {
		t.Errorf("notifications = %v, want %v", got, want)
	}
	// macOS: the app resigned while the window kept its focus.
	r := newQuestionApp(t, func(string) bool { return true })
	r.app.attn.setActive(false)
	r.app.chooseUser("Clone a repository?", "msg", []string{"Clone"})
	if got := r.notes.list(); len(got) != 1 {
		t.Errorf("chooseUser in the background: %v", got)
	}
}

func TestQuestionAnnouncedWhenFlintGoesToTheBackground(t *testing.T) {
	var q *questionApp
	q = newQuestionApp(t, func(string) bool {
		// While the dialog is open it takes the window's focus (Linux): that
		// is not leaving Flint.
		q.app.attn.setFocused(false)
		if n := len(q.notes.list()); n != 0 {
			t.Errorf("the dialog's own focus announced it (%d)", n)
		}
		// On macOS the user switches to another app: now it is announced, once.
		q.app.attn.setActive(false)
		q.app.attn.setActive(true)
		q.app.attn.setActive(false)
		return true
	})
	q.app.askUser("Re-run failed jobs?", "msg", "Re-run")
	if got := q.notes.list(); len(got) != 1 || got[0] != "Flint needs your answer|Re-run failed jobs?|" {
		t.Errorf("notifications = %v", got)
	}
}

func TestQuestionReminderAfterAWhile(t *testing.T) {
	var q *questionApp
	q = newQuestionApp(t, func(string) bool {
		q.mu.Lock()
		remind := q.reminder[0]
		q.mu.Unlock()
		remind()
		remind() // once
		return true
	})
	q.app.askUser("Create a pull request?", "msg", "Push and Create")
	if got := q.notes.list(); len(got) != 1 {
		t.Errorf("notifications = %v, want one reminder", got)
	}
	// A reminder that fires after the answer says nothing.
	q.mu.Lock()
	late := q.reminder[0]
	q.mu.Unlock()
	late()
	if got := q.notes.list(); len(got) != 1 {
		t.Errorf("a reminder after the answer: %v", got)
	}
}

// Questions wait for each other (confirmMu); each is announced when it
// becomes visible, not when it was asked.
func TestQueuedQuestionsAreAnnouncedWhenShown(t *testing.T) {
	release := make(chan struct{})
	shown := make(chan string, 2)
	q := newQuestionApp(t, func(title string) bool {
		shown <- title
		if title == "first" {
			<-release
		}
		return true
	})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); q.app.askUser("first", "", "OK") }()
	if got := <-shown; got != "first" {
		t.Fatalf("shown %q", got)
	}
	wg.Add(1)
	go func() { defer wg.Done(); q.app.askUser("second", "", "OK") }()
	// The user leaves while the first is open: the first is announced; the
	// second waits behind it and says nothing yet.
	q.app.attn.setFocused(false)
	q.app.attn.setActive(false)
	waitUntil(t, "the first question announced", func() bool { return len(q.notes.list()) == 1 })
	if got := q.notes.list(); got[0] != "Flint needs your answer|first|" {
		t.Fatalf("notifications = %v", got)
	}
	close(release)
	if got := <-shown; got != "second" {
		t.Fatalf("shown %q", got)
	}
	wg.Wait()
	if got := q.notes.list(); len(got) != 2 || got[1] != "Flint needs your answer|second|" {
		t.Errorf("notifications = %v", got)
	}
	ev := q.questionEvents()
	if len(ev) != 4 || ev[0].Title != "first" || ev[0].Open != true || ev[1].Title != "first" || ev[1].Open ||
		ev[2].Title != "second" || !ev[2].Open || ev[3].Open {
		t.Errorf("events = %+v", ev)
	}
}

func TestQuestionNotificationFollowsTheSettings(t *testing.T) {
	for _, s := range []struct{ background, approval bool }{{false, true}, {true, false}} {
		q := newQuestionApp(t, func(string) bool { return true })
		q.app.settings.NotifyInBackground, q.app.settings.NotifyOnApproval = s.background, s.approval
		q.app.attn.setFocused(false)
		q.app.askUser("x", "", "OK")
		if got := q.notes.list(); len(got) != 0 {
			t.Errorf("%+v: notified %v", s, got)
		}
		// The UI still counts it.
		if len(q.questionEvents()) != 2 {
			t.Errorf("%+v: events %+v", s, q.questionEvents())
		}
	}
}

// Coming back runs the features' Foreground hook once per return; a dialog
// closing does not count as coming back.
func TestComingBackRunsTheHook(t *testing.T) {
	q := newQuestionApp(t, func(string) bool { return true })
	q.app.askUser("x", "", "OK")
	q.app.attn.setFocused(false)
	q.app.attn.setFocused(false)
	q.app.attn.setFocused(true)
	q.app.attn.setFocused(true)
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.backs != 1 {
		t.Errorf("back ran %d times, want 1", q.backs)
	}
}

func TestNotifyStatusLogsEachFailureOnce(t *testing.T) {
	var logged []string
	s := notifyStatus{logf: func(format string, args ...any) { logged = append(logged, format) }}
	if v := s.view(); v.State != notifyAllowed {
		t.Fatalf("fresh status %+v", v)
	}
	boom := errors.New("dbus: no reply")
	for range 3 {
		if err := s.record(boom); err != boom {
			t.Fatal(err)
		}
	}
	if v := s.view(); v.State != notifyFailed || v.Detail != "The last notification could not be delivered: dbus: no reply" {
		t.Errorf("after failures %+v", v)
	}
	if len(logged) != 1 {
		t.Errorf("logged %d times", len(logged))
	}
	_ = s.record(nil)
	if v := s.view(); v.State != notifyAllowed {
		t.Errorf("after a delivery %+v", v)
	}
}
