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

// Chat is one chat's use within the range, for Top chats.
type Chat struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Project string `json:"project"`
	Agent   string `json:"agent"`
	Model   string `json:"model,omitempty"` // the model it spent most on
	Task    string `json:"task,omitempty"`
	LastDay string `json:"lastDay"`
	Totals  Totals `json:"totals"`
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
	// Chats are the chats that spent the most within the range.
	Chats []Chat `json:"chats"`
	// Estimated: some cost is an estimate from settings prices.
	Estimated bool `json:"estimated"`
	// Unpriced: some tokens have neither a reported cost nor a price.
	Unpriced bool `json:"unpriced"`
	// CacheLeftOut: cached input of a priced model without a cache-read
	// price is not in the estimates.
	CacheLeftOut  bool `json:"cacheLeftOut"`
	RetentionDays int  `json:"retentionDays"`
	// Problem says why usage is not being saved ("" when it is).
	Problem string `json:"problem,omitempty"`
}

// pricedRow is a row's totals with the estimate filled in.
type pricedRow struct {
	t                            Totals
	estimated, unpriced, leftOut bool
}

func priced(r Row, prices Prices) pricedRow {
	t := r.Totals
	est, ok, leftOut := prices.estimate(r.Model, t)
	t.EstimatedUSD = est
	return pricedRow{t: t, estimated: ok, unpriced: !ok && t.UnpricedInputTokens+t.UnpricedOutputTokens > 0, leftOut: leftOut}
}

// Summary answers the Usage panel for q.
func (s *Store) Summary(q Query, prices Prices) Summary {
	s.mu.Lock()
	f, today := s.snapshot()
	out := Summary{RetentionDays: s.retention}
	if s.readOnly != nil {
		out.Problem = s.readOnly.Error()
	}
	s.mu.Unlock()

	days := q.Days
	if days <= 0 {
		days = monthDays
	}
	days = min(days, maxQueryDays)
	start := dayBefore(today, days-1)
	weekStart, monthStart := dayBefore(today, weekDays-1), dayBefore(today, monthDays-1)

	perDay := map[string]Totals{}
	groups := [4]map[string]Totals{{}, {}, {}, {}}
	chats := map[string]*Chat{}
	chatModels := map[string]map[string]Totals{}
	for _, r := range f.Rows {
		if !q.match(r.Project, r.Task) {
			continue
		}
		p := priced(r, prices)
		if r.Day == today {
			out.Today = out.Today.add(p.t)
		}
		if r.Day >= weekStart {
			out.Week = out.Week.add(p.t)
		}
		if r.Day >= monthStart {
			out.Month = out.Month.add(p.t)
		}
		if r.Day < start {
			continue
		}
		out.Estimated = out.Estimated || p.estimated
		out.Unpriced = out.Unpriced || p.unpriced
		out.CacheLeftOut = out.CacheLeftOut || p.leftOut
		perDay[r.Day] = perDay[r.Day].add(p.t)
		for i, k := range [4]string{r.Project, r.Agent, r.Model, r.Task} {
			if k != "" || i == 2 {
				groups[i][k] = groups[i][k].add(p.t)
			}
		}
		addToChat(chats, chatModels, r, p.t)
	}
	for d := range days {
		day := dayBefore(today, days-1-d)
		out.Days = append(out.Days, DayTotals{Day: day, Totals: perDay[day]})
	}
	out.Projects, out.Agents, out.Models, out.Tasks = ranked(groups[0]), ranked(groups[1]), ranked(groups[2]), ranked(groups[3])
	out.Chats = topChatsOf(chats, chatModels, f.Chats)
	return out
}

// TaskTotals sums everything recorded for each of the given tasks (all
// days that are kept), with estimates from prices: what a task has cost.
func (s *Store) TaskTotals(tasks []string, prices Prices) map[string]Totals {
	out := map[string]Totals{}
	if s == nil || len(tasks) == 0 {
		return out
	}
	want := map[string]bool{}
	for _, t := range tasks {
		want[t] = true
	}
	s.mu.Lock()
	f, _ := s.snapshot()
	s.mu.Unlock()
	for _, r := range f.Rows {
		if r.Task != "" && want[r.Task] {
			out[r.Task] = out[r.Task].add(priced(r, prices).t)
		}
	}
	return out
}

func addToChat(chats map[string]*Chat, models map[string]map[string]Totals, r Row, t Totals) {
	if r.Chat == "" {
		return
	}
	c := chats[r.Chat]
	if c == nil {
		c = &Chat{ID: r.Chat, Project: r.Project, Agent: r.Agent, Task: r.Task}
		chats[r.Chat], models[r.Chat] = c, map[string]Totals{}
	}
	c.Totals = c.Totals.add(t)
	c.LastDay = max(c.LastDay, r.Day)
	models[r.Chat][r.Model] = models[r.Chat][r.Model].add(t)
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

func topChatsOf(chats map[string]*Chat, models map[string]map[string]Totals, infos []chatInfo) []Chat {
	out := make([]Chat, 0, len(chats))
	for id, c := range chats {
		if i := slices.IndexFunc(infos, func(ci chatInfo) bool { return ci.ID == id }); i >= 0 {
			c.Label = infos[i].Label
		}
		c.Model = ranked(models[id])[0].Key
		out = append(out, *c)
	}
	slices.SortFunc(out, func(a, b Chat) int { return cmp.Or(bigger(a.Totals, b.Totals), cmp.Compare(a.ID, b.ID)) })
	return out[:min(len(out), topChats)]
}

// dayBefore is the day n days before day (both YYYY-MM-DD).
func dayBefore(day string, n int) string {
	t, _ := time.ParseInLocation(dayLayout, day, time.Local)
	return t.AddDate(0, 0, -n).Format(dayLayout)
}
