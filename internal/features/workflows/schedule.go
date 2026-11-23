package workflows

import (
	"errors"
	"flint/internal/agentdefs"
	"flint/internal/textsafe"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Schedule starts a run of Workflow in Folder at a time of day, written by
// the user into their own settings (workflows.schedules), never read from a
// repository. The scheduler is in schedule_timer.go.
type Schedule struct {
	// Name tells the schedules apart (the workflow-name pattern); the runs
	// it starts have Origin "schedule:<name>".
	Name string `json:"name"`
	// Workflow is the workflow's name, Folder the open, trusted workspace
	// folder the run's task branches from (absolute).
	Workflow string `json:"workflow"`
	Folder   string `json:"folder"`
	// Task is what the run should deliver; Agent takes the steps that name
	// none ("" = the default agent).
	Task  string `json:"task"`
	Agent string `json:"agent"`
	// At is the local time of day, "HH:MM" (24 hours); Days the days of the
	// week it runs on ("mon" … "sun"), empty for every day.
	At   string   `json:"at"`
	Days []string `json:"days"`
}

// Bounds of the schedules.
const (
	maxSchedules = 20
	// maxScheduleTask bounds a schedule's task, in bytes.
	maxScheduleTask = 8000
	// OriginSchedule starts the Origin of a run a schedule started.
	OriginSchedule = "schedule:"
)

// weekdays are the names of Schedule.Days, in week order.
var weekdays = []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}

// clockTime is Schedule.At.
var clockTime = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

// normalizeSchedules keeps the valid schedules, the first one of each name,
// at most maxSchedules, with their days in week order and each once.
func normalizeSchedules(list []Schedule) []Schedule {
	out := []Schedule{}
	for _, s := range list {
		if len(out) == maxSchedules {
			break
		}
		if checkSchedule(s) != nil || slices.ContainsFunc(out, func(o Schedule) bool { return o.Name == s.Name }) {
			continue
		}
		s.Days = weekOrder(s.Days)
		out = append(out, s)
	}
	return out
}

// weekOrder is days in week order, each once.
func weekOrder(days []string) []string {
	out := []string{}
	for _, d := range weekdays {
		if slices.Contains(days, d) {
			out = append(out, d)
		}
	}
	return out
}

// checkSchedules refuses more than maxSchedules, a name used twice and an
// invalid schedule, naming the schedule and the field.
func checkSchedules(list []Schedule) error {
	if len(list) > maxSchedules {
		return fmt.Errorf("workflows.schedules lists at most %d schedules", maxSchedules)
	}
	for i, s := range list {
		if err := checkSchedule(s); err != nil {
			who := fmt.Sprintf("the schedule %q", s.Name)
			if !ValidName(s.Name) {
				who = fmt.Sprintf("schedule %d", i+1)
			}
			return fmt.Errorf("workflows.schedules: %s: %w", who, err)
		}
		if slices.ContainsFunc(list[:i], func(o Schedule) bool { return o.Name == s.Name }) {
			return fmt.Errorf("workflows.schedules: two schedules are named %q", s.Name)
		}
	}
	return nil
}

// checkSchedule checks one schedule's fields.
func checkSchedule(s Schedule) error {
	switch {
	case !ValidName(s.Name):
		return errors.New("name must be lowercase letters, digits and dashes")
	case !ValidName(s.Workflow):
		return errors.New("workflow must be a workflow's name (lowercase letters, digits and dashes)")
	case s.Folder == "" || len(s.Folder) > maxPath || !filepath.IsAbs(s.Folder) || filepath.Clean(s.Folder) != s.Folder ||
		strings.IndexFunc(s.Folder, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0:
		return errors.New("folder must be an absolute, clean path")
	case strings.TrimSpace(s.Task) == "":
		return errors.New("task must say what the run should deliver")
	case len(s.Task) > maxScheduleTask:
		return fmt.Errorf("task is longer than %d bytes", maxScheduleTask)
	case s.Agent != "" && !agentdefs.ValidName(s.Agent):
		return errors.New("agent must be empty (the default agent) or an agent's name")
	case !clockTime.MatchString(s.At):
		return errors.New("at must be a time of day as HH:MM (24 hours, local time)")
	}
	if r, bad := textsafe.HiddenRune(s.Task); bad {
		return fmt.Errorf("task contains %s", textsafe.RuneKind(r))
	}
	for _, d := range s.Days {
		if !slices.Contains(weekdays, d) {
			return fmt.Errorf("days may list only mon, tue, wed, thu, fri, sat and sun, not %q", d)
		}
	}
	return nil
}

// NextDue is the first time after t at which a schedule at "HH:MM" on days
// (empty: every day) is due in loc; the zero time when at is not a time of
// day. A day on which the time does not exist (the clock jumps over it) is
// skipped; on a day it happens twice (the clock goes back) the schedule is
// due at the first, once.
func NextDue(at string, days []string, t time.Time, loc *time.Location) time.Time {
	if !clockTime.MatchString(at) {
		return time.Time{}
	}
	hour, minute := int(at[0]-'0')*10+int(at[1]-'0'), int(at[3]-'0')*10+int(at[4]-'0')
	local := t.In(loc)
	// Fifteen days cover a week of days and a skipped one.
	for d := 0; d < 15; d++ {
		day := time.Date(local.Year(), local.Month(), local.Day()+d, 12, 0, 0, 0, loc)
		if len(days) > 0 && !slices.Contains(days, weekdays[(int(day.Weekday())+6)%7]) {
			continue
		}
		due, ok := earliestAt(day, hour, minute, loc)
		if ok && due.After(t) {
			return due
		}
	}
	return time.Time{}
}

// earliestAt is the first instant on day's date whose wall clock in loc reads
// hour:minute; false when the clock skips it that day.
func earliestAt(day time.Time, hour, minute int, loc *time.Location) (time.Time, bool) {
	y, m, d := day.Date()
	at := time.Date(y, m, d, hour, minute, 0, 0, loc)
	same := func(c time.Time) bool {
		cy, cm, cd := c.Date()
		return cy == y && cm == m && cd == d && c.Hour() == hour && c.Minute() == minute
	}
	if !same(at) {
		return time.Time{}, false // normalised out of a gap
	}
	// When the clock went back before at, the same wall time may have come
	// an offset's difference earlier, in the zone before the change.
	if start, _ := at.ZoneBounds(); !start.IsZero() {
		_, before := start.Add(-time.Second).Zone()
		_, now := at.Zone()
		if shift := time.Duration(before-now) * time.Second; shift > 0 {
			if earlier := at.Add(-shift); earlier.Before(start) && same(earlier) {
				return earlier, true
			}
		}
	}
	return at, true
}
