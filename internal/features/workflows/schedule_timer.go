package workflows

import (
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// missedAfter is how late a schedule's timer may fire and still start its
// run. A timer fires later than its time only when the computer slept
// (timers do not count the sleep): that time was missed, and a missed time
// is not caught up.
const missedAfter = 5 * time.Minute

// maxArm bounds one arm of the timer. A timer counts the time the computer
// is awake, not the clock: armed on Friday for Monday 02:30, it would fire
// days late after a weekend asleep. Armed for at most a few minutes, it
// fires within maxArm of waking, finds its time not reached yet (fire's
// "early" path arms again) or just passed, and the local time zone and the
// clock are read again each time.
const maxArm = 3 * time.Minute

// scheduler starts the runs of workflows.schedules at their times. One
// timer is armed for the earliest next due time (no polling): at Start,
// after every change of the settings, and after it fired. Close stops it.
// What happened the last time each schedule was due is kept in memory only.
type scheduler struct {
	a *API
	// now is the clock, loc the time zone the schedules' times are in, and
	// afterFunc arms a timer (time.AfterFunc) and returns its stop. Tests
	// replace them.
	now       func() time.Time
	loc       func() *time.Location
	afterFunc func(d time.Duration, f func()) (stop func() bool)

	mu sync.Mutex
	// gen counts the timers armed: a timer that fires for an older one does
	// nothing. stop stops the armed one; due is its time and names the
	// schedules due then.
	gen   int
	stop  func() bool
	due   time.Time
	names []string
	// closed: Close ran, nothing is armed or started again.
	closed bool
	// last is the latest outcome of each schedule, by name.
	last map[string]scheduleOutcome
	// trustState holds what each schedule was accepted to run
	// (schedule_trust.go).
	trustState
}

// scheduleOutcome is what happened when a schedule was last due: the run it
// started, or why it started none; at is when (unix seconds).
type scheduleOutcome struct {
	run, skipped string
	at           int64
}

// ScheduleView is a schedule as Settings ▸ Workflows and the Workflows panel
// show it.
type ScheduleView struct {
	Name     string   `json:"name"`
	Workflow string   `json:"workflow"`
	Folder   string   `json:"folder"`
	Task     string   `json:"task"`
	Agent    string   `json:"agent"`
	At       string   `json:"at"`
	Days     []string `json:"days"`
	// Next is when it is due next (unix seconds); 0 when it cannot start a
	// run now (its folder is not open or not trusted, or Flint quits).
	Next int64 `json:"next"`
	// LastRun is the run it started the last time it was due, LastSkipped
	// why it started none then ("" when it started one), LastAt when (unix
	// seconds); all empty when it was not due since Flint started.
	LastRun     string `json:"lastRun"`
	LastSkipped string `json:"lastSkipped"`
	LastAt      int64  `json:"lastAt"`
	// Changed: it started nothing the last time it was due because its
	// workflow or a run configuration it uses changed since it was saved;
	// AcceptSchedule accepts them.
	Changed bool `json:"changed"`
}

func newScheduler(a *API) *scheduler {
	return &scheduler{
		a:   a,
		now: time.Now,
		loc: localZone,
		afterFunc: func(d time.Duration, f func()) func() bool {
			return time.AfterFunc(d, f).Stop
		},
		last: map[string]scheduleOutcome{},
	}
}

// arm arms the timer for the schedules in the settings now.
func (s *scheduler) arm() { s.armAfter(s.now()) }

// armAfter stops the armed timer and arms one for the earliest time after
// `after` at which a schedule is due (none when no schedule is, or Close
// ran). The outcomes of schedules no longer in the settings are dropped.
func (s *scheduler) armAfter(after time.Time) {
	list := Section.Get(s.a.host.Settings()).Schedules
	loc := s.loc()
	var due time.Time
	var names []string
	for _, sc := range list {
		next := NextDue(sc.At, sc.Days, after, loc)
		switch {
		case next.IsZero():
		case due.IsZero() || next.Before(due):
			due, names = next, []string{sc.Name}
		case next.Equal(due):
			names = append(names, sc.Name)
		}
	}
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	maps.DeleteFunc(s.last, func(name string, _ scheduleOutcome) bool {
		return !slices.ContainsFunc(list, func(sc Schedule) bool { return sc.Name == name })
	})
	if s.stop != nil {
		s.stop()
		s.stop = nil
	}
	s.gen++
	s.due, s.names = due, names
	if s.closed || due.IsZero() {
		return
	}
	gen := s.gen
	s.stop = s.afterFunc(min(max(due.Sub(now), 0), maxArm), func() { s.fire(gen) })
}

// localZone reads the system's time zone now: time.Local is read once when
// Flint starts, and a laptop changes zone while it runs. $TZ wins, as for
// time.Local; time.Local when nothing can be read.
func localZone() *time.Location {
	if tz, ok := os.LookupEnv("TZ"); ok {
		if tz == "" {
			return time.UTC
		}
		if loc, err := time.LoadLocation(strings.TrimPrefix(tz, ":")); err == nil {
			return loc
		}
		return time.Local
	}
	if data, err := os.ReadFile("/etc/localtime"); err == nil {
		if loc, err := time.LoadLocationFromTZData("Local", data); err == nil {
			return loc
		}
	}
	return time.Local
}

// fire starts the runs of the schedules due at the armed time, then arms
// the timer for the next one. A timer that fires early (the clock was set
// back) arms again; one that fires much later than its time (the computer
// slept) starts nothing.
func (s *scheduler) fire(gen int) {
	s.mu.Lock()
	if s.closed || gen != s.gen {
		s.mu.Unlock()
		return
	}
	due, names := s.due, slices.Clone(s.names)
	s.stop = nil
	s.mu.Unlock()
	now := s.now()
	switch {
	case now.Before(due.Add(-time.Second)):
		// Early: a capped arm (maxArm), or the clock was set back.
		s.armAfter(now)
		return
	case now.Sub(due) > missedAfter:
		for _, name := range names {
			s.record(name, "", fmt.Sprintf("missed at %s: Flint was not running then (the computer slept)", due.In(s.loc()).Format("Mon 15:04")))
		}
	default:
		for _, name := range names {
			s.runSchedule(name)
		}
	}
	// Times missed while the computer slept are not caught up either.
	after := due
	if late := now.Add(-missedAfter); late.After(after) {
		after = late
	}
	s.armAfter(after)
}

// runSchedule starts the run of the schedule called name, as it is in the
// settings now, and keeps the outcome.
func (s *scheduler) runSchedule(name string) {
	list := Section.Get(s.a.host.Settings()).Schedules
	i := slices.IndexFunc(list, func(sc Schedule) bool { return sc.Name == name })
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if i < 0 || closed {
		return
	}
	run, skipped := s.a.startScheduled(list[i])
	if skipped != "" {
		slog.Info("workflows: schedule skipped", "schedule", name, "reason", skipped)
	}
	s.record(name, run, skipped)
}

// record keeps a schedule's outcome and tells the UI.
func (s *scheduler) record(name, run, skipped string) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.last[name] = scheduleOutcome{run: run, skipped: skipped, at: s.now().Unix()}
	s.mu.Unlock()
	s.a.host.Emit(EventChanged, "")
}

// close stops the timer for good.
func (s *scheduler) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.gen++
	if s.stop != nil {
		s.stop()
		s.stop = nil
	}
}

// startScheduled starts the run of a due schedule through startRun, the
// path of Run Workflow…, with the schedule as its origin; or says why it
// starts none: the folder is not an open workspace folder or not trusted,
// the schedule's last run is not over, the usage budget pauses new turns,
// or the start itself refused (the workflow is missing or cannot run, an
// agent is not available…). Nothing on this path asks the user.
func (a *API) startScheduled(sc Schedule) (run, skipped string) {
	folder := filepath.Clean(sc.Folder)
	switch {
	case !isRoot(a.host.Roots(), folder):
		return "", folder + " is not open in Flint"
	case !a.host.Trusted(folder):
		return "", folder + " is in Restricted Mode"
	case a.engine.originBusy(OriginSchedule + sc.Name):
		return "", "the last run is still going"
	}
	if err := a.host.BudgetPause(); err != nil {
		return "", err.Error()
	}
	a.sched.syncing.Wait()
	if err := a.sched.checkTrust(sc); err != nil {
		return "", err.Error()
	}
	v, err := a.startRun(runRequest{
		dir: folder, workflow: sc.Workflow, description: sc.Task, agent: sc.Agent,
		origin: OriginSchedule + sc.Name, by: "Started by the schedule " + sc.Name,
		consent: consentSchedule,
	})
	if err != nil {
		return "", err.Error()
	}
	return v.ID, ""
}

// isRoot reports whether folder (clean) is one of the open workspace folders.
func isRoot(roots []string, folder string) bool {
	return slices.ContainsFunc(roots, func(r string) bool { return filepath.Clean(r) == folder })
}

// originBusy reports whether a run with this origin is not over yet (one
// being made counts).
func (e *engine) originBusy(origin string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.ContainsFunc(e.runs, func(r Run) bool { return r.Origin == origin && !r.Over() })
}

// Schedules lists workflows.schedules with when each is due next and what
// happened the last time it was due.
func (a *API) Schedules() []ScheduleView {
	list := Section.Get(a.host.Settings()).Schedules
	s := a.sched
	now, loc, roots := s.now(), s.loc(), a.host.Roots()
	s.mu.Lock()
	closed, last := s.closed, maps.Clone(s.last)
	s.mu.Unlock()
	out := make([]ScheduleView, 0, len(list))
	for _, sc := range list {
		v := ScheduleView{
			Name: sc.Name, Workflow: sc.Workflow, Folder: sc.Folder, Task: sc.Task, Agent: sc.Agent, At: sc.At,
			Days: append([]string{}, sc.Days...),
		}
		folder := filepath.Clean(sc.Folder)
		if !closed && isRoot(roots, folder) && a.host.Trusted(folder) {
			if next := NextDue(sc.At, sc.Days, now, loc); !next.IsZero() {
				v.Next = next.Unix()
			}
		}
		if o, ok := last[sc.Name]; ok {
			v.LastRun, v.LastSkipped, v.LastAt = o.run, o.skipped, o.at
			v.Changed = o.skipped == errScheduleChanged.Error()
		}
		out = append(out, v)
	}
	return out
}
