package usage

import (
	"cmp"
	"slices"
	"time"
)

// Ranges of the Usage panel.
const (
	weekDays     = 7
	monthDays    = 30
	topChats     = 10
	maxQueryDays = 366
)

// Query narrows a summary or an export. Empty fields match everything.
type Query struct {
	Project string `json:"project"`
	Task    string `json:"task"`
	// Days is the range of the chart, the breakdowns and Top chats (default 30).
	Days int `json:"days"`
}

func (q Query) match(project, task string) bool {
	return (q.Project == "" || q.Project == project) && (q.Task == "" || q.Task == task)
}

// Group is the totals of one project, agent, model or task.
type Group struct {
	Key    string `json:"key"`
	Totals Totals `json:"totals"`
}

// DayTotals is one day of the chart.
type DayTotals struct {
	Day    string `json:"day"`
	Totals Totals `json:"totals"`
}

// Summary is what the Usage panel shows.
type Summary struct {
	Today Totals `json:"today"`
	Week  Totals `json:"week"`  // the last 7 days, today included
	Month Totals `json:"month"` // the last 30 days
	// Days has every day of the range, oldest first (zero when idle).
	Days     []DayTotals `json:"days"`
	Projects []Group     `json:"projects"`
	Agents   []Group     `json:"agents"`
	Models   []Group     `json:"models"`
	Tasks    []Group     `json:"tasks"` // chats in agent tasks only
	// Chats are the chats active in the range that spent the most.
	Chats []Chat `json:"chats"`
	// Estimated: some cost is an estimate from settings prices.
	Estimated bool `json:"estimated"`
	// Unpriced: some tokens have neither a reported cost nor a price.
	Unpriced      bool `json:"unpriced"`
	RetentionDays int  `json:"retentionDays"`
}

// priced is a row's totals with the estimate filled in.
func priced(r Row, prices Prices) (t Totals, estimated, unpriced bool) {
	t = r.Totals
	est, ok := prices.estimate(r.Model, t)
	t.EstimatedUSD = est
	return t, ok, !ok && t.UnpricedInputTokens+t.UnpricedOutputTokens > 0
}

// Summary answers the Usage panel for q.
func (s *Store) Summary(q Query, prices Prices) Summary {
	s.mu.Lock()
	f, today := s.snapshot()
	retention := s.retention
	s.mu.Unlock()

	days := q.Days
	if days <= 0 {
		days = monthDays
	}
	days = min(days, maxQueryDays)
	start := dayBefore(today, days-1)
	weekStart, monthStart := dayBefore(today, weekDays-1), dayBefore(today, monthDays-1)

	out := Summary{RetentionDays: retention}
	perDay := map[string]Totals{}
	groups := [4]map[string]Totals{{}, {}, {}, {}}
	for _, r := range f.Rows {
		if !q.match(r.Project, r.Task) {
			continue
		}
		t, est, unp := priced(r, prices)
		if r.Day == today {
			out.Today = out.Today.add(t)
		}
		if r.Day >= weekStart {
			out.Week = out.Week.add(t)
		}
		if r.Day >= monthStart {
			out.Month = out.Month.add(t)
		}
		if r.Day < start {
			continue
		}
		out.Estimated = out.Estimated || est
		out.Unpriced = out.Unpriced || unp
		perDay[r.Day] = perDay[r.Day].add(t)
		for i, k := range [4]string{r.Project, r.Agent, r.Model, r.Task} {
			if k != "" || i == 2 {
				groups[i][k] = groups[i][k].add(t)
			}
		}
	}
	for d := range days {
		day := dayBefore(today, days-1-d)
		out.Days = append(out.Days, DayTotals{Day: day, Totals: perDay[day]})
	}
	out.Projects, out.Agents, out.Models, out.Tasks = ranked(groups[0]), ranked(groups[1]), ranked(groups[2]), ranked(groups[3])
	out.Chats = topChatsOf(f.Chats, q, start, prices)
	return out
}

// bigger orders by spent money, then tokens: biggest first.
func bigger(a, b Totals) int {
	return cmp.Or(cmp.Compare(b.Spent(), a.Spent()), cmp.Compare(b.Tokens(), a.Tokens()))
}

func ranked(m map[string]Totals) []Group {
	out := make([]Group, 0, len(m))
	for k, t := range m {
		out = append(out, Group{Key: k, Totals: t})
	}
	slices.SortFunc(out, func(a, b Group) int { return cmp.Or(bigger(a.Totals, b.Totals), cmp.Compare(a.Key, b.Key)) })
	return out
}

func topChatsOf(chats []Chat, q Query, start string, prices Prices) []Chat {
	out := []Chat{}
	for _, c := range chats {
		if c.LastDay < start || !q.match(c.Project, c.Task) {
			continue
		}
		c.Totals.EstimatedUSD, _ = prices.estimate(c.Model, c.Totals)
		out = append(out, c)
	}
	slices.SortFunc(out, func(a, b Chat) int { return cmp.Or(bigger(a.Totals, b.Totals), cmp.Compare(a.ID, b.ID)) })
	return out[:min(len(out), topChats)]
}

// dayBefore is the day n days before day (both YYYY-MM-DD).
func dayBefore(day string, n int) string {
	t, _ := time.ParseInLocation(dayLayout, day, time.Local)
	return t.AddDate(0, 0, -n).Format(dayLayout)
}
