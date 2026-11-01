package usage

import (
	"slices"
	"strconv"
	"strings"
)

// Limits are the budgets from settings; zero turns one off. The daily budget
// covers every chat of the day; the task budget one agent task's chats over
// the retention. Money counts reported costs plus estimates; tokens are
// input (cache included) plus output.
type Limits struct {
	DailyUSD    float64 `json:"dailyUsd"`
	DailyTokens int64   `json:"dailyTokens"`
	TaskUSD     float64 `json:"taskUsd"`
	TaskTokens  int64   `json:"taskTokens"`
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

func (a Alert) scopeMark() string { return a.Scope + "/" + a.Key }

// mark identifies the alert per threshold: raising the limit arms it again.
func (a Alert) mark() string {
	return "alert/" + a.scopeMark() + "/" + a.Kind + "/" + strconv.FormatFloat(a.Limit, 'g', -1, 64)
}

// markDay is the day a mark is about, for pruning; ok is false for task marks.
func markDay(m string) (string, bool) {
	parts := strings.Split(m, "/")
	if len(parts) >= 3 && parts[1] == ScopeDay {
		return parts[2], true
	}
	return "", false
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
		if !slices.Contains(f.Marks, a.mark()) {
			fresh = append(fresh, a)
			f.Marks = append(f.Marks, a.mark())
		}
	}
	if len(fresh) == 0 {
		return nil, nil
	}
	return fresh, s.write(f)
}

// Override lets new turns run although today's budget, and task's (when not
// ""), are spent: the user chose to continue from the warning.
func (s *Store) Override(task string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, today := s.snapshot()
	scopes := []string{ScopeDay + "/" + today}
	if task != "" {
		scopes = append(scopes, ScopeTask+"/"+clean(task, maxField))
	}
	changed := false
	for _, sc := range scopes {
		if m := "override/" + sc; !slices.Contains(f.Marks, m) {
			f.Marks, changed = append(f.Marks, m), true
		}
	}
	if !changed {
		return nil
	}
	return s.write(f)
}

func over(f file, today string, l Limits, task string, prices Prices) []Alert {
	var day, tsk Totals
	var dayEst, taskEst bool
	for _, r := range f.Rows {
		if r.Day != today && (task == "" || r.Task != task) {
			continue
		}
		t, est, _ := priced(r, prices)
		if r.Day == today {
			day, dayEst = day.add(t), dayEst || est
		}
		if task != "" && r.Task == task {
			tsk, taskEst = tsk.add(t), taskEst || est
		}
	}
	var out []Alert
	check := func(scope, key string, t Totals, est bool, usd float64, tokens int64) {
		if usd > 0 && t.Spent() >= usd {
			out = append(out, Alert{Scope: scope, Key: key, Kind: KindUSD, Limit: usd, Spent: t.Spent(), Estimated: est})
		}
		if tokens > 0 && t.Tokens() >= tokens {
			out = append(out, Alert{Scope: scope, Key: key, Kind: KindTokens, Limit: float64(tokens), Spent: float64(t.Tokens())})
		}
	}
	check(ScopeDay, today, day, dayEst, l.DailyUSD, l.DailyTokens)
	if task != "" {
		check(ScopeTask, task, tsk, taskEst, l.TaskUSD, l.TaskTokens)
	}
	for i := range out {
		out[i].Overridden = slices.Contains(f.Marks, "override/"+out[i].scopeMark())
	}
	return out
}
