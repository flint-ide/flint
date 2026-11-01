package usage

import (
	"slices"
	"strconv"
	"strings"
)

// Limits are the budgets from settings; zero turns one off. The daily budget
// covers every chat of the day; the task budget one agent task's chats over
// the retention. Money counts reported costs plus estimates; tokens are
// input plus output, without cache reads unless CountCacheReads.
type Limits struct {
	DailyUSD        float64 `json:"dailyUsd"`
	DailyTokens     int64   `json:"dailyTokens"`
	TaskUSD         float64 `json:"taskUsd"`
	TaskTokens      int64   `json:"taskTokens"`
	CountCacheReads bool    `json:"countCacheReads"`
}

// Budget scopes and kinds.
const (
	ScopeDay   = "day"
	ScopeTask  = "task"
	KindUSD    = "usd"
	KindTokens = "tokens"
)

// Alert is a budget that is spent.
type Alert struct {
	Scope string  `json:"scope"` // ScopeDay or ScopeTask
	Key   string  `json:"key"`   // the day (YYYY-MM-DD) or the task id
	Kind  string  `json:"kind"`  // KindUSD or KindTokens
	Limit float64 `json:"limit"`
	Spent float64 `json:"spent"`
	// Estimated: Spent includes costs estimated from settings prices.
	Estimated bool `json:"estimated"`
	// Overridden: the user chose to continue anyway (new turns are not paused).
	Overridden bool `json:"overridden"`
}

// mark identifies the alert per threshold: raising the limit arms it again.
func (a Alert) mark() string {
	return a.Scope + "/" + a.Key + "/" + a.Kind + "/" + strconv.FormatFloat(a.Limit, 'g', -1, 64)
}

// alertDay is the day an alert mark is about, for pruning; ok is false for tasks.
func alertDay(m string) (string, bool) {
	parts := strings.Split(m, "/")
	if len(parts) >= 2 && parts[0] == ScopeDay {
		return parts[1], true
	}
	return "", false
}

// overridden: a "Continue anyway" for this very threshold still has room.
func (a Alert) overriddenBy(overrides []Override) bool {
	return slices.ContainsFunc(overrides, func(o Override) bool {
		return o.Scope == a.Scope && o.Key == a.Key && o.Kind == a.Kind && o.Limit == a.Limit && a.Spent < o.Until
	})
}

// Over lists the budgets that today and task (when not "") have reached.
func (s *Store) Over(l Limits, task string, prices Prices) []Alert {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, today := s.snapshot()
	return over(f, today, l, task, prices)
}

// Blocked lists the budgets reached that the user did not override: with
// "pause new turns" on, a chat may not start a turn while any is listed.
func (s *Store) Blocked(l Limits, task string, prices Prices) []Alert {
	return slices.DeleteFunc(s.Over(l, task, prices), func(a Alert) bool { return a.Overridden })
}

// Fire returns the budgets reached that were not reported before, and
// remembers them so each threshold is reported once per day or task.
func (s *Store) Fire(l Limits, task string, prices Prices) ([]Alert, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, today := s.snapshot()
	var fresh []Alert
	for _, a := range over(f, today, l, task, prices) {
		if !slices.Contains(s.f.Alerts, a.mark()) {
			fresh = append(fresh, a)
			s.f.Alerts = append(s.f.Alerts, a.mark())
		}
	}
	if len(fresh) == 0 {
		return nil, nil
	}
	return fresh, s.changed()
}

// Override lets new turns run although today's budget, and task's (when not
// ""), are spent: the user chose to continue from the warning. Each lasts
// for another budget's worth of spending at the current limit.
func (s *Store) Override(l Limits, task string, prices Prices) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, today := s.snapshot()
	changed := false
	for _, a := range over(f, today, l, task, prices) {
		if !a.Overridden {
			s.f.Overrides = append(s.f.Overrides, Override{Scope: a.Scope, Key: a.Key, Kind: a.Kind, Limit: a.Limit, Until: a.Spent + a.Limit})
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return s.changed()
}

func over(f file, today string, l Limits, task string, prices Prices) []Alert {
	var day, tsk Totals
	var dayEst, taskEst bool
	for _, r := range f.Rows {
		inTask := task != "" && r.Task == task
		if r.Day != today && !inTask {
			continue
		}
		p := priced(r, prices)
		if r.Day == today {
			day, dayEst = day.add(p.t), dayEst || p.estimated
		}
		if inTask {
			tsk, taskEst = tsk.add(p.t), taskEst || p.estimated
		}
	}
	tokens := func(t Totals) float64 {
		if l.CountCacheReads {
			return float64(t.Tokens())
		}
		return float64(t.Tokens() - t.CacheReadTokens)
	}
	var out []Alert
	check := func(scope, key string, t Totals, est bool, usd float64, limit int64) {
		if usd > 0 && t.Spent() >= usd {
			out = append(out, Alert{Scope: scope, Key: key, Kind: KindUSD, Limit: usd, Spent: t.Spent(), Estimated: est})
		}
		if limit > 0 && tokens(t) >= float64(limit) {
			out = append(out, Alert{Scope: scope, Key: key, Kind: KindTokens, Limit: float64(limit), Spent: tokens(t)})
		}
	}
	check(ScopeDay, today, day, dayEst, l.DailyUSD, l.DailyTokens)
	if task != "" {
		check(ScopeTask, task, tsk, taskEst, l.TaskUSD, l.TaskTokens)
	}
	for i := range out {
		out[i].Overridden = out[i].overriddenBy(f.Overrides)
	}
	return out
}
