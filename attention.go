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
// follows whether Flint is in front (followForeground) and tells the UI
// ("attention:front"), which reads no focus of its own: one answer for Go
// and the web view, in which a dialog of Flint's counts as Flint.

const (
	// eventQuestion tells the UI that a native question opened or closed
	// (data: QuestionEvent).
	eventQuestion = "attention:question"
	// eventFront tells the UI that Flint came to the front or left it
	// (data: FrontEvent).
	eventFront = "attention:front"
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

// FrontEvent is the payload of "attention:front". Seq orders the events:
// the UI ignores one older than the last it applied (events may arrive out
// of order).
type FrontEvent struct {
	Front bool   `json:"front"`
	Seq   uint64 `json:"seq"`
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
	// settings let a question notify; emit sends a UI event. All are set
	// once, before use.
	notify  func(title, body string) error
	allowed func() bool
	emit    func(name string, data ...any)
	// after schedules the reminder (time.AfterFunc; tests replace it).
	after func(d time.Duration, f func()) (stop func() bool)

	mu sync.Mutex
	// focused: the main window has the keyboard focus; active: the app is
	// the active one (macOS reports it; elsewhere it stays true); dialogs:
	// native questions on screen that appeared while Flint was in front, so
	// they took the window's focus without the user leaving Flint (one that
	// appears while the user is elsewhere does not bring Flint to the front).
	focused  bool
	active   bool
	dialogs  int
	seq      uint64
	frontSeq uint64
	open     []*question // on screen, oldest first
}

type question struct {
	id        string
	title     string
	announced bool
	holds     bool // counted in dialogs
	stop      func() bool
}

func newAttention(notify func(title, body string) error, allowed func() bool, emit func(string, ...any)) *attention {
	return &attention{
		notify: notify, allowed: allowed, emit: emit,
		after: func(d time.Duration, f func()) func() bool { return time.AfterFunc(d, f).Stop },
		// In front until the window says otherwise (followForeground asks it
		// once it is shown); the server build's window never does.
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

// Front is where Flint is now, numbered like the "attention:front" events.
func (t *attention) Front() FrontEvent {
	if t == nil {
		return FrontEvent{Front: true}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return FrontEvent{Front: t.inFrontLocked(), Seq: t.frontSeq}
}

// setFocused follows the main window's focus.
func (t *attention) setFocused(on bool) { t.change(func() { t.focused = on }) }

// setActive follows the app's activation (macOS).
func (t *attention) setActive(on bool) { t.change(func() { t.active = on }) }

// change applies fn; when Flint went to the background, the open questions
// not announced yet are announced, and the UI hears of either move.
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
	var ev *FrontEvent
	if before != after {
		t.frontSeq++
		ev = &FrontEvent{Front: after, Seq: t.frontSeq}
	}
	t.mu.Unlock()
	t.announce(due)
	if ev != nil && t.emit != nil {
		t.emit(eventFront, *ev)
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
// returned func runs (the user answered it, or Flint quits). A question
// asked behind another one calls it when it gets its turn, so each is
// announced when it becomes visible: at once when Flint is in the
// background, else when Flint goes there (or after questionRemind) while it
// is still open.
func (t *attention) shown(title string) (done func()) {
	if t == nil {
		return func() {}
	}
	t.mu.Lock()
	t.seq++
	q := &question{id: "q" + strconv.FormatUint(t.seq, 10), title: title}
	background := !t.inFrontLocked()
	q.announced, q.holds = background, !background
	if q.holds {
		t.dialogs++
	}
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
			if q.holds {
				t.dialogs--
			}
			// Only an answer closes a dialog (or Flint quitting), and the
			// user gives it in Flint, whose window gets the focus back: the
			// focus-in that follows is not the user coming back from
			// elsewhere (Linux reports the dialog's focus as the window
			// losing it, and gives it back after).
			t.focused, t.active = true, true
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
