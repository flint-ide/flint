package workflows

import (
	"strings"
	"testing"
	"time"
	_ "time/tzdata" // the DST cases need the zones on every system
)

func TestNextDue(t *testing.T) {
	warsaw, err := time.LoadLocation("Europe/Warsaw")
	if err != nil {
		t.Fatal(err)
	}
	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	at := func(loc *time.Location, y int, m time.Month, d, h, min int) time.Time {
		return time.Date(y, m, d, h, min, 0, 0, loc)
	}
	utc := func(y int, m time.Month, d, h, min int) time.Time { return time.Date(y, m, d, h, min, 0, 0, time.UTC) }
	cases := []struct {
		name  string
		at    string
		days  []string
		after time.Time
		loc   *time.Location
		want  time.Time
	}{
		{"later today", "02:30", nil, at(warsaw, 2026, 11, 22, 1, 0), warsaw, at(warsaw, 2026, 11, 22, 2, 30)},
		{"passed today: tomorrow", "02:30", nil, at(warsaw, 2026, 11, 22, 10, 0), warsaw, at(warsaw, 2026, 11, 23, 2, 30)},
		{"exactly now: the next one", "02:30", nil, at(warsaw, 2026, 11, 22, 2, 30), warsaw, at(warsaw, 2026, 11, 23, 2, 30)},
		{"midnight", "00:00", nil, at(warsaw, 2026, 11, 22, 23, 59), warsaw, at(warsaw, 2026, 11, 23, 0, 0)},
		{"late evening", "23:59", nil, at(warsaw, 2026, 11, 22, 23, 59), warsaw, at(warsaw, 2026, 11, 23, 23, 59)},
		// 2026-11-22 is a Sunday.
		{"weekdays from a Sunday", "02:30", []string{"mon", "tue", "wed", "thu", "fri"}, at(warsaw, 2026, 11, 22, 10, 0), warsaw, at(warsaw, 2026, 11, 23, 2, 30)},
		{"one day a week", "09:00", []string{"sat"}, at(warsaw, 2026, 11, 22, 10, 0), warsaw, at(warsaw, 2026, 11, 28, 9, 0)},
		{"today's day, later", "11:00", []string{"sun"}, at(warsaw, 2026, 11, 22, 10, 0), warsaw, at(warsaw, 2026, 11, 22, 11, 0)},
		{"today's day, passed: next week", "09:00", []string{"sun"}, at(warsaw, 2026, 11, 22, 10, 0), warsaw, at(warsaw, 2026, 11, 29, 9, 0)},
		{"another zone", "02:30", nil, utc(2026, 11, 22, 3, 0), newYork, at(newYork, 2026, 11, 22, 2, 30)},
		// Warsaw, 2026-03-29: 02:00 jumps to 03:00, so 02:30 does not exist.
		{"spring gap skipped to the next day", "02:30", nil, at(warsaw, 2026, 3, 28, 12, 0), warsaw, at(warsaw, 2026, 3, 30, 2, 30)},
		{"spring gap on the only day: a week later", "02:30", []string{"sun"}, at(warsaw, 2026, 3, 28, 12, 0), warsaw, at(warsaw, 2026, 4, 5, 2, 30)},
		{"spring: a time after the gap", "03:30", nil, at(warsaw, 2026, 3, 28, 12, 0), warsaw, utc(2026, 3, 29, 1, 30)},
		// Warsaw, 2026-10-25: 03:00 goes back to 02:00, so 02:30 comes twice:
		// 00:30 UTC (summer time) and 01:30 UTC. It is due at the first only.
		{"autumn: the first of the two", "02:30", nil, at(warsaw, 2026, 10, 24, 12, 0), warsaw, utc(2026, 10, 25, 0, 30)},
		{"autumn: once, not again an hour later", "02:30", nil, utc(2026, 10, 25, 0, 30), warsaw, utc(2026, 10, 26, 1, 30)},
		{"autumn: between the two", "02:30", nil, utc(2026, 10, 25, 1, 0), warsaw, utc(2026, 10, 26, 1, 30)},
		// New York, 2026-11-01: 02:00 goes back to 01:00.
		{"autumn in New York", "01:30", nil, at(newYork, 2026, 10, 31, 12, 0), newYork, utc(2026, 11, 1, 5, 30)},
		{"autumn in New York: once", "01:30", nil, utc(2026, 11, 1, 5, 30), newYork, utc(2026, 11, 2, 6, 30)},
		{"not a time", "24:00", nil, at(warsaw, 2026, 11, 22, 1, 0), warsaw, time.Time{}},
		{"not a time either", "2:30", nil, at(warsaw, 2026, 11, 22, 1, 0), warsaw, time.Time{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := NextDue(c.at, c.days, c.after, c.loc); !got.Equal(c.want) {
				t.Errorf("NextDue(%s, %v, %s) = %s, want %s", c.at, c.days, c.after, got.UTC(), c.want.UTC())
			}
		})
	}
}

func TestCheckSchedules(t *testing.T) {
	valid := Schedule{Name: "nightly-deps", Workflow: "feature", Folder: "/abs/repo", Task: "Update the dependencies.", At: "02:30", Days: []string{"fri", "mon"}}
	with := func(change func(s *Schedule)) []Schedule {
		s := valid
		change(&s)
		return []Schedule{s}
	}
	many := make([]Schedule, maxSchedules+1)
	for i := range many {
		many[i] = valid
		many[i].Name = "s" + strings.Repeat("x", i)
	}
	cases := []struct {
		name string
		list []Schedule
		err  string // "" for valid
	}{
		{"valid", []Schedule{valid}, ""},
		{"an agent", with(func(s *Schedule) { s.Agent = "codex" }), ""},
		{"every day", with(func(s *Schedule) { s.Days = nil }), ""},
		{"a task of the most bytes", with(func(s *Schedule) { s.Task = strings.Repeat("a", maxScheduleTask) }), ""},
		{"too many", many, "at most 20"},
		{"same name twice", []Schedule{valid, valid}, `two schedules are named "nightly-deps"`},
		{"bad name", with(func(s *Schedule) { s.Name = "Nightly Deps" }), "schedule 1: name"},
		{"bad workflow", with(func(s *Schedule) { s.Workflow = "../x" }), `"nightly-deps": workflow`},
		{"relative folder", with(func(s *Schedule) { s.Folder = "repo" }), "folder must be"},
		{"unclean folder", with(func(s *Schedule) { s.Folder = "/abs/../repo" }), "folder must be"},
		{"empty task", with(func(s *Schedule) { s.Task = "  " }), "task must say"},
		{"long task", with(func(s *Schedule) { s.Task = strings.Repeat("a", maxScheduleTask+1) }), "longer than 8000 bytes"},
		{"hidden characters", with(func(s *Schedule) { s.Task = "update‮deps" }), "task contains"},
		{"bad agent", with(func(s *Schedule) { s.Agent = "Claude Code" }), "agent must be"},
		{"bad time", with(func(s *Schedule) { s.At = "2:30" }), "at must be"},
		{"no such hour", with(func(s *Schedule) { s.At = "24:00" }), "at must be"},
		{"bad day", with(func(s *Schedule) { s.Days = []string{"monday"} }), `days may list only`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := checkSchedules(c.list)
			switch {
			case c.err == "" && err != nil:
				t.Errorf("refused: %v", err)
			case c.err != "" && (err == nil || !strings.Contains(err.Error(), c.err)):
				t.Errorf("err = %v, want %q", err, c.err)
			}
			if err := checkSettings(base(func(s *Settings) { s.Schedules = c.list })); (err != nil) != (c.err != "") {
				t.Errorf("checkSettings = %v", err)
			}
		})
	}
}

func TestNormalizeSchedules(t *testing.T) {
	valid := Schedule{Name: "a", Workflow: "feature", Folder: "/abs/repo", Task: "Do it.", At: "02:30", Days: []string{"sun", "mon", "sun"}}
	bad := valid
	bad.Name, bad.At = "b", "25:00"
	dup := valid
	dup.Task = "Another"
	got := normalizeSettings(base(func(s *Settings) { s.Schedules = []Schedule{valid, bad, dup} })).Schedules
	if len(got) != 1 || got[0].Task != "Do it." || strings.Join(got[0].Days, ",") != "mon,sun" {
		t.Errorf("normalized %+v", got)
	}
	many := make([]Schedule, maxSchedules+5)
	for i := range many {
		many[i] = valid
		many[i].Name = "s" + strings.Repeat("x", i)
	}
	if got := normalizeSchedules(many); len(got) != maxSchedules {
		t.Errorf("kept %d", len(got))
	}
	if valid.Days[0] != "sun" {
		t.Error("normalize changed its input")
	}
}
