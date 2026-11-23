package workflows

import (
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// schedFixture is the fixture with its scheduler on a clock the test sets
// (UTC) and timers the test fires.
type schedFixture struct {
	*fixture
	s      *scheduler
	now    *atomic.Int64 // unix nanoseconds
	timers *fakeTimers
}

func newSchedFixture(t *testing.T, list ...Schedule) *schedFixture {
	t.Helper()
	f := &schedFixture{fixture: newFixture(t), now: &atomic.Int64{}, timers: &fakeTimers{}}
	f.setNow(2026, 11, 22, 1, 0)
	f.s = f.api.sched
	f.s.now = func() time.Time { return time.Unix(0, f.now.Load()).UTC() }
	f.s.loc = func() *time.Location { return time.UTC }
	f.s.afterFunc = f.timers.after
	f.schedules(list...)
	f.s.arm()
	t.Cleanup(f.s.close)
	return f
}

func (f *schedFixture) setNow(y int, m time.Month, d, h, min int) {
	f.now.Store(time.Date(y, m, d, h, min, 0, 0, time.UTC).UnixNano())
}

func (f *schedFixture) schedules(list ...Schedule) {
	f.settings(base(func(s *Settings) { s.Schedules = list }))
}

// armed is the one live timer of the scheduler.
func (f *schedFixture) armed() *fakeTimer {
	f.t.Helper()
	live := f.timers.live()
	if len(live) != 1 {
		f.t.Fatalf("%d timers armed, want 1", len(live))
	}
	return live[0]
}

// fire fires a timer the way time.AfterFunc does: it is no longer armed.
func (f *schedFixture) fire(timer *fakeTimer) {
	f.timers.mu.Lock()
	timer.stopped = true
	f.timers.mu.Unlock()
	timer.f()
}

// armedFor reports whether timer is armed for the schedule due in until
// from now: for that, capped at maxArm.
func (f *schedFixture) armedFor(timer *fakeTimer, until time.Duration) bool {
	now := time.Unix(0, f.now.Load()).UTC()
	return timer.d == min(until, maxArm) && f.s.due.Sub(now) == until
}

// view is the schedule's view called name.
func (f *schedFixture) view(name string) ScheduleView {
	f.t.Helper()
	for _, v := range f.api.Schedules() {
		if v.Name == name {
			return v
		}
	}
	f.t.Fatalf("no schedule %s", name)
	return ScheduleView{}
}

func nightly(root string) Schedule {
	return Schedule{Name: "nightly", Workflow: "feature", Folder: root, Task: "Update the dependencies.", At: "02:30"}
}

func TestScheduleStartsARun(t *testing.T) {
	f := newSchedFixture(t, nightly(""))
	f.schedules(nightly(f.root))
	f.s.arm()
	timer := f.armed()
	if !f.armedFor(timer, 90*time.Minute) {
		t.Fatalf("armed for %s due %s, want 1h30m", timer.d, f.s.due)
	}
	if v := f.view("nightly"); v.Next != time.Date(2026, 11, 22, 2, 30, 0, 0, time.UTC).Unix() || v.LastRun != "" || v.LastAt != 0 {
		t.Errorf("view before %+v", v)
	}
	f.setNow(2026, 11, 22, 2, 30)
	f.fire(timer)
	reqs := f.h.Requested()
	if len(reqs) != 1 || reqs[0][0].Dir != f.root || reqs[0][0].Description != "Update the dependencies." || reqs[0][0].Agent != "claude" ||
		!strings.HasPrefix(reqs[0][0].Origin, originPrefix) {
		t.Fatalf("requested %+v", reqs)
	}
	runs := f.api.Runs()
	if len(runs) != 1 || runs[0].Origin != "schedule:nightly" || runs[0].Workflow != "feature" || runs[0].Log[0].Text != "Started by the schedule nightly" {
		t.Fatalf("runs %+v", runs)
	}
	v := f.view("nightly")
	if v.LastRun != runs[0].ID || v.LastSkipped != "" || v.LastAt != time.Date(2026, 11, 22, 2, 30, 0, 0, time.UTC).Unix() ||
		v.Next != time.Date(2026, 11, 23, 2, 30, 0, 0, time.UTC).Unix() {
		t.Errorf("view after %+v", v)
	}
	if next := f.armed(); next == timer || !f.armedFor(next, 24*time.Hour) {
		t.Errorf("re-armed for %s", next.d)
	}
	if asked := f.h.Asked(); len(asked) != 0 {
		t.Errorf("a schedule asked the user: %q", asked)
	}
	emitted := false
	for _, e := range f.h.Events() {
		emitted = emitted || e.Name == EventChanged && len(e.Data) == 1 && e.Data[0] == ""
	}
	if !emitted {
		t.Error("the outcome was not told to the UI")
	}
}

func TestScheduleSkips(t *testing.T) {
	cases := []struct {
		name   string
		change func(f *schedFixture, sc *Schedule)
		reason string
	}{
		{"folder not open", func(f *schedFixture, sc *Schedule) { sc.Folder = filepath.Join(f.root, "sub") }, "/sub is not open in Flint"},
		{"folder untrusted", func(f *schedFixture, _ *Schedule) { f.h.SetUntrusted(f.root) }, "is in Restricted Mode"},
		{"workflow missing", func(_ *schedFixture, sc *Schedule) { sc.Workflow = "nope" }, `there is no workflow named "nope"`},
		{"agent missing", func(_ *schedFixture, sc *Schedule) { sc.Agent = "gemini" }, `the agent "gemini" is not available`},
		{"budget", func(f *schedFixture, _ *Schedule) {
			f.h.SetBudgetPause(errors.New("paused: today's budget of $5 is spent"))
		}, "paused: today's budget"},
		{"start refused", func(f *schedFixture, _ *Schedule) { f.h.SetCreateError(errors.New("git worktree add failed")) }, "git worktree add failed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newSchedFixture(t)
			sc := nightly(f.root)
			c.change(f, &sc)
			f.schedules(sc)
			f.s.arm()
			f.setNow(2026, 11, 22, 2, 30)
			f.fire(f.armed())
			if n := len(f.api.Runs()); n != 0 {
				t.Errorf("%d runs started", n)
			}
			v := f.view("nightly")
			if !strings.Contains(v.LastSkipped, c.reason) || v.LastRun != "" || v.LastAt == 0 {
				t.Errorf("view %+v, want the reason %q", v, c.reason)
			}
			if len(f.h.Asked()) != 0 {
				t.Error("a schedule asked the user")
			}
			f.armed() // armed again for tomorrow
		})
	}
}

func TestScheduleNextIsZeroWhenItCannotRun(t *testing.T) {
	f := newSchedFixture(t)
	f.schedules(nightly(f.root), Schedule{Name: "elsewhere", Workflow: "feature", Folder: "/not/open", Task: "x", At: "03:00"})
	if v := f.view("elsewhere"); v.Next != 0 {
		t.Errorf("a folder that is not open is due at %d", v.Next)
	}
	f.h.SetUntrusted(f.root)
	if v := f.view("nightly"); v.Next != 0 {
		t.Errorf("an untrusted folder is due at %d", v.Next)
	}
}

func TestScheduleDoesNotOverlap(t *testing.T) {
	f := newSchedFixture(t)
	f.schedules(nightly(f.root))
	f.s.arm()
	f.setNow(2026, 11, 22, 2, 30)
	f.fire(f.armed())
	first := f.view("nightly").LastRun
	if first == "" {
		t.Fatal("the first time started nothing")
	}
	// The next day the first run still waits at its first step.
	f.setNow(2026, 11, 23, 2, 30)
	f.fire(f.armed())
	if v := f.view("nightly"); v.LastSkipped != "the last run is still going" || v.LastRun != "" {
		t.Errorf("view %+v", v)
	}
	if n := len(f.h.Requested()); n != 1 {
		t.Errorf("%d starts, want 1", n)
	}
	// Once it is over, the schedule starts again.
	if err := f.api.Stop(first); err != nil {
		t.Fatal(err)
	}
	f.setNow(2026, 11, 24, 2, 30)
	f.fire(f.armed())
	if v := f.view("nightly"); v.LastRun == "" || v.LastRun == first {
		t.Errorf("view %+v", v)
	}
}

func TestScheduleRearmsOnSettingsChange(t *testing.T) {
	f := newSchedFixture(t)
	f.schedules(nightly(f.root))
	f.s.arm()
	old := f.armed()
	early := nightly(f.root)
	early.Name, early.At = "early", "01:30"
	f.schedules(nightly(f.root), early)
	f.s.arm() // SettingsChanged
	if !old.stopped {
		t.Error("the old timer is still armed")
	}
	if timer := f.armed(); !f.armedFor(timer, 30*time.Minute) {
		t.Errorf("armed for %s, want the earlier schedule's 30m", timer.d)
	}
	old.f() // fires anyway (it was already on its way): stale, nothing starts
	if n := len(f.h.Requested()); n != 0 {
		t.Errorf("a stale timer started %d runs", n)
	}
	// Removing every schedule disarms.
	f.schedules()
	f.s.arm()
	if live := f.timers.live(); len(live) != 0 {
		t.Errorf("%d timers armed without schedules", len(live))
	}
}

func TestScheduleSameTimeStartsEach(t *testing.T) {
	f := newSchedFixture(t)
	other := nightly(f.root)
	other.Name, other.Task = "other", "Another task."
	f.schedules(nightly(f.root), other)
	f.s.arm()
	f.setNow(2026, 11, 22, 2, 30)
	f.fire(f.armed())
	if n := len(f.h.Requested()); n != 2 {
		t.Errorf("%d runs started, want 2", n)
	}
}

func TestScheduleMissedAndEarly(t *testing.T) {
	f := newSchedFixture(t)
	f.schedules(nightly(f.root))
	f.s.arm()
	// The timer fires early (the clock was set back): it arms again.
	f.setNow(2026, 11, 22, 2, 0)
	f.fire(f.armed())
	if timer := f.armed(); !f.armedFor(timer, 30*time.Minute) || len(f.h.Requested()) != 0 {
		t.Errorf("early: armed for %s, %d starts", timer.d, len(f.h.Requested()))
	}
	// It fires hours late (the computer slept): missed, not caught up.
	f.setNow(2026, 11, 22, 9, 0)
	f.fire(f.armed())
	if n := len(f.h.Requested()); n != 0 {
		t.Errorf("a missed time started %d runs", n)
	}
	if v := f.view("nightly"); !strings.HasPrefix(v.LastSkipped, "missed at Sun 02:30") {
		t.Errorf("view %+v", v)
	}
	if timer := f.armed(); !f.armedFor(timer, 17*time.Hour+30*time.Minute) {
		t.Errorf("armed for %s, want tomorrow's", timer.d)
	}
}

func TestScheduleNothingAfterClose(t *testing.T) {
	f := newSchedFixture(t)
	f.schedules(nightly(f.root))
	f.s.arm()
	timer := f.armed()
	f.s.close()
	if !timer.stopped {
		t.Error("Close left the timer armed")
	}
	f.setNow(2026, 11, 22, 2, 30)
	timer.f() // already on its way when Close ran
	f.s.arm() // a settings change after Close
	if n := len(f.h.Requested()); n != 0 || len(f.timers.live()) != 0 {
		t.Errorf("after Close: %d starts, %d timers", n, len(f.timers.live()))
	}
	if v := f.view("nightly"); v.Next != 0 || v.LastAt != 0 {
		t.Errorf("view after Close %+v", v)
	}
}

func TestScheduleOutcomesFollowTheSettings(t *testing.T) {
	f := newSchedFixture(t)
	f.schedules(nightly(f.root))
	f.s.arm()
	f.setNow(2026, 11, 22, 2, 30)
	f.fire(f.armed())
	f.schedules()
	f.s.arm()
	f.schedules(nightly(f.root))
	f.s.arm()
	if v := f.view("nightly"); v.LastAt != 0 {
		t.Errorf("a removed schedule's outcome came back: %+v", v)
	}
}

// A computer asleep between the arm and the time: the timer is armed for
// at most maxArm, so after waking it fires within that, and a time not
// reached yet is armed again; a time that passed while asleep (by less
// than missedAfter) starts its run, one far past is missed.
func TestScheduleAfterSleep(t *testing.T) {
	f := newSchedFixture(t)
	mon := nightly(f.root)
	mon.Days = []string{"mon"}
	f.schedules(mon)
	f.setNow(2026, 11, 20, 18, 0) // Friday
	f.s.arm()
	timer := f.armed()
	if timer.d != maxArm {
		t.Fatalf("armed for %s on Friday for Monday", timer.d)
	}
	// Asleep over the weekend; awake on Sunday evening, the capped timer
	// fires: Monday is not there yet, so it arms again.
	f.setNow(2026, 11, 22, 20, 0)
	f.fire(timer)
	if len(f.h.Requested()) != 0 || f.view("nightly").LastSkipped != "" {
		t.Fatal("a schedule started or was missed before its time")
	}
	timer = f.armed()
	if !f.armedFor(timer, 6*time.Hour+30*time.Minute) {
		t.Fatalf("re-armed for %s due %s", timer.d, f.s.due)
	}
	// Asleep again, awake at 02:31 on Monday: the timer fires a minute
	// after the time, which starts the run.
	f.setNow(2026, 11, 23, 2, 31)
	f.fire(timer)
	if n := len(f.h.Requested()); n != 1 {
		t.Fatalf("%d runs started after waking a minute late", n)
	}
}

// The schedules' times are read in the zone of each arm, not the one Flint
// started in.
func TestScheduleZoneIsReadAtEachArm(t *testing.T) {
	f := newSchedFixture(t)
	f.schedules(nightly(f.root))
	zone := time.UTC
	f.s.loc = func() *time.Location { return zone }
	f.s.arm()
	if !f.armedFor(f.armed(), 90*time.Minute) {
		t.Fatalf("UTC: due %s", f.s.due)
	}
	zone = time.FixedZone("UTC+1", 3600) // 02:30 there is 01:30 UTC: 30 minutes from now
	f.s.arm()
	if !f.armedFor(f.armed(), 30*time.Minute) {
		t.Errorf("after the zone changed: due %s", f.s.due)
	}
	t.Setenv("TZ", "Asia/Tokyo")
	if got := localZone(); got.String() != "Asia/Tokyo" {
		t.Errorf("localZone with TZ = %s", got)
	}
}
