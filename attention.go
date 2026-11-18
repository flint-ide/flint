package main

import (
	"strconv"
	"sync"
	"time"
)

// Flint's own questions (askUser, chooseUser: native dialogs) are things the
// user must answer, like an agent's approval request, but only Go knows when
// one is on screen. attention tells the user about them: a desktop
// notification when one appears while Flint is in the background (or when
// Flint goes to the background while it is open), and an "attention:question"
// event so the UI counts it in the badge until it is answered. It also
// follows whether Flint is in front, from the window's and the app's own
// events (followForeground), and tells the features when the user is back.

const (
	// eventQuestion tells the UI that a native question opened or closed
	// (data: QuestionEvent).
	eventQuestion = "attention:question"
	// eventAlert asks the UI to tell the user about something Flint noticed
	// on its own (data: AlertEvent): a toast in front, a notification behind.
	eventAlert = "attention:alert"
	// questionRemind announces a question still unanswered after this long
	// although Flint was in front when it appeared: on Linux a dialog takes
	// the window's focus, so leaving Flint while it is open is not visible.
	questionRemind = 2 * time.Minute
	// questionNotice is the notification's title; its body is the question.
	questionNotice = "Flint needs your answer"
)

// QuestionEvent is the payload of "attention:question".
type QuestionEvent struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Open  bool   `json:"open"`
}

// AlertEvent is the payload of "attention:alert". Tab is the editor tab the
// alert is about ("chat:<id>", may be ""); Kind is "info" or "error".
type AlertEvent struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Tab   string `json:"tab"`
	Kind  string `json:"kind"`
}

// alert sends an AlertEvent.
func (a *App) alert(title, body, tab string) {
	a.emit(eventAlert, AlertEvent{Title: title, Body: body, Tab: tab, Kind: "error"})
}

// attention follows the foreground and the open native questions. Its zero
// value is not ready (newAttention); a nil *attention does nothing, so tests
// that build an App by hand need none.
type attention struct {
	// notify sends a desktop notification; allowed reports whether the
	// settings let a question notify; emit sends a UI event; back runs when
	// Flint comes to the front again. All are set once, before use.
	notify  func(title, body string) error
	allowed func() bool
	emit    func(name string, data ...any)
	back    func()
	// after schedules the reminder (time.AfterFunc; tests replace it).
	after func(d time.Duration, f func()) (stop func() bool)

	mu sync.Mutex
	// focused: the main window has the keyboard focus; active: the app is
	// the active one (macOS reports it; elsewhere it stays true); dialogs:
	// native questions on screen, which take the window's focus without the
	// user leaving Flint.
	focused, active bool
	dialogs         int
	seq             uint64
	open            []*question // on screen, oldest first
}

type question struct {
	id        string
	title     string
	announced bool
	stop      func() bool
}

func newAttention(notify func(title, body string) error, allowed func() bool, emit func(string, ...any), back func()) *attention {
	return &attention{
		notify: notify, allowed: allowed, emit: emit, back: back,
		after: func(d time.Duration, f func()) func() bool { return time.AfterFunc(d, f).Stop },
		// Flint starts in front: the window is shown and focused at launch.
		focused: true, active: true,
	}
}

// inFrontLocked: the user can see Flint (callers hold mu).
func (t *attention) inFrontLocked() bool { return t.active && (t.focused || t.dialogs > 0) }

// InFront reports whether Flint is the app in front of the user.
func (t *attention) InFront() bool {
	if t == nil {
		return true
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.inFrontLocked()
}

// setFocused follows the main window's focus.
func (t *attention) setFocused(on bool) { t.change(func() { t.focused = on }) }

// setActive follows the app's activation (macOS).
func (t *attention) setActive(on bool) { t.change(func() { t.active = on }) }

// change applies fn and reacts to Flint going to the background (the open
// questions not announced yet are announced) or coming back (back runs).
func (t *attention) change(fn func()) {
	if t == nil {
		return
	}
	t.mu.Lock()
	before := t.inFrontLocked()
	fn()
	after := t.inFrontLocked()
	var due []*question
	if before && !after {
		due = t.dueLocked()
	}
	t.mu.Unlock()
	t.announce(due)
	if !before && after && t.back != nil {
		t.back()
	}
}

// dueLocked marks the open questions not announced yet as announced and
// returns them (callers hold mu).
func (t *attention) dueLocked() []*question {
	var due []*question
	for _, q := range t.open {
		if !q.announced {
			q.announced = true
			due = append(due, q)
		}
	}
	return due
}

func (t *attention) announce(due []*question) {
	if len(due) == 0 || t.notify == nil || (t.allowed != nil && !t.allowed()) {
		return
	}
	for _, q := range due {
		_ = t.notify(questionNotice, q.title) // a failure is logged and shown by the notifier
	}
}

// shown records that a native question is on screen from now until the
// returned func runs. A question asked behind another one calls it when it
// gets its turn, so each is announced when it becomes visible: at once when
// Flint is in the background, else when Flint goes there (or after
// questionRemind) while it is still open.
func (t *attention) shown(title string) (done func()) {
	if t == nil {
		return func() {}
	}
	t.mu.Lock()
	t.seq++
	q := &question{id: "q" + strconv.FormatUint(t.seq, 10), title: title}
	background := !t.inFrontLocked()
	q.announced = background
	t.dialogs++
	t.open = append(t.open, q)
	t.mu.Unlock()
	t.emitQuestion(QuestionEvent{ID: q.id, Title: title, Open: true})
	if background {
		t.announce([]*question{q})
	}
	stop := t.after(questionRemind, func() {
		t.mu.Lock()
		due := !q.announced && t.isOpenLocked(q)
		q.announced = q.announced || due
		t.mu.Unlock()
		if due {
			t.announce([]*question{q})
		}
	})
	t.mu.Lock()
	q.stop = stop
	t.mu.Unlock()
	return func() {
		t.change(func() {
			t.dialogs--
			for i, x := range t.open {
				if x == q {
					t.open = append(t.open[:i:i], t.open[i+1:]...)
					break
				}
			}
			if q.stop != nil {
				q.stop()
			}
		})
		t.emitQuestion(QuestionEvent{ID: q.id, Title: title, Open: false})
	}
}

func (t *attention) isOpenLocked(q *question) bool {
	for _, x := range t.open {
		if x == q {
			return true
		}
	}
	return false
}

func (t *attention) emitQuestion(e QuestionEvent) {
	if t.emit != nil {
		t.emit(eventQuestion, e)
	}
}
