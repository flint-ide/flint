package usage

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	as "flint/internal/agentsession"
)

// clock is a settable time source for a store.
type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func day(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", s, time.Local)
	if err != nil {
		panic(err)
	}
	return t
}

func openAt(t *testing.T, path string, c *clock, retention int) *Store {
	t.Helper()
	s, err := Open(path, retention)
	if err != nil {
		t.Fatal(err)
	}
	s.now = c.now
	return s
}

func newStore(t *testing.T, at string) (*Store, *clock, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "usage.json")
	c := &clock{day(at)}
	return openAt(t, path, c, 90), c, path
}

func turn(in, out int64, cost float64) Totals {
	return FromEvent(&as.Usage{InputTokens: int(in), OutputTokens: int(out), CostUSD: cost})
}

func entry(chat, project, agent, model, task string, t Totals) Entry {
	return Entry{Chat: chat, Label: "fix the bug", Project: project, Agent: agent, Model: model, Task: task, Usage: t}
}

func TestFromEvent(t *testing.T) {
	got := FromEvent(&as.Usage{InputTokens: 100, OutputTokens: 20, CacheReadTokens: 70, CacheWriteTokens: 5, CostUSD: 0.5})
	want := Totals{InputTokens: 100, OutputTokens: 20, CacheReadTokens: 70, CacheWriteTokens: 5, CostUSD: 0.5, Turns: 1}
	if got != want {
		t.Errorf("FromEvent = %+v, want %+v", got, want)
	}
	unpriced := FromEvent(&as.Usage{InputTokens: 10, OutputTokens: 2})
	if unpriced.UnpricedInputTokens != 10 || unpriced.UnpricedOutputTokens != 2 {
		t.Errorf("tokens without a reported cost must be kept for estimates: %+v", unpriced)
	}
	if FromEvent(&as.Usage{InputTokens: -5}).InputTokens != 0 {
		t.Error("negative counts must be dropped")
	}
}

func TestAddAggregatesPerDayProjectAgentModelTask(t *testing.T) {
	s, c, _ := newStore(t, "2026-11-01 10:00")
	must(t, s.Add(entry("c1", "/p", "claude", "opus", "", turn(100, 10, 0.1))))
	must(t, s.Add(entry("c1", "/p", "claude", "opus", "", turn(50, 5, 0.05))))
	must(t, s.Add(entry("c2", "/p", "codex", "gpt", "t1", turn(30, 3, 0))))
	c.t = day("2026-11-02 09:00")
	must(t, s.Add(entry("c1", "/p", "claude", "opus", "", turn(1, 1, 0.01))))

	rows := s.rows()
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3 (two keys on day 1, one on day 2): %+v", len(rows), rows)
	}
	r := rows[0]
	if r.Day != "2026-11-01" || r.Agent != "claude" || r.InputTokens != 150 || r.Turns != 2 || !near(r.CostUSD, 0.15) {
		t.Errorf("first row = %+v", r)
	}
	chats := s.chats()
	if len(chats) != 2 || chats[0].ID != "c1" || chats[0].Totals.Turns != 3 || chats[0].LastDay != "2026-11-02" {
		t.Errorf("chats = %+v", chats)
	}
}

func TestStoreSurvivesRestart(t *testing.T) {
	s, c, path := newStore(t, "2026-11-01 10:00")
	must(t, s.Add(entry("c1", "/p", "claude", "opus", "t1", turn(100, 10, 0.1))))
	reopened := openAt(t, path, c, 90)
	sum := reopened.Summary(Query{}, nil)
	if sum.Today.InputTokens != 100 || len(sum.Chats) != 1 {
		t.Errorf("after reopening: %+v", sum)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("usage file must be owner-only: %v %v", info.Mode(), err)
	}
}

func TestCorruptFileIsSetAsideNotFatal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.json")
	must(t, os.WriteFile(path, []byte("{not json"), 0o600))
	s, err := Open(path, 90)
	if err == nil || s == nil {
		t.Fatalf("want a usable store and an error to log, got %v %v", s, err)
	}
	if _, err := os.Stat(path + ".bad"); err != nil {
		t.Errorf("the broken file should be kept aside: %v", err)
	}
	must(t, s.Add(entry("c", "/p", "a", "m", "", turn(1, 1, 0))))
}

func TestRetentionPrunesOldDays(t *testing.T) {
	s, c, path := newStore(t, "2026-10-01 10:00")
	must(t, s.Add(entry("old", "/p", "claude", "m", "", turn(1, 1, 0))))
	c.t = day("2026-11-01 10:00")
	s.SetRetention(7)
	must(t, s.Add(entry("new", "/p", "claude", "m", "", turn(2, 2, 0))))
	if rows := s.rows(); len(rows) != 1 || rows[0].Day != "2026-11-01" {
		t.Errorf("rows older than the retention must go: %+v", rows)
	}
	if chats := s.chats(); len(chats) != 1 || chats[0].ID != "new" {
		t.Errorf("chats idle longer than the retention must go: %+v", chats)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "2026-10-01") {
		t.Error("the pruned day is still in the file")
	}
}

func TestBoundsCapRowsAndChats(t *testing.T) {
	s, _, _ := newStore(t, "2026-11-01 10:00")
	s.maxRows, s.maxChats = 3, 2
	for i := range 5 {
		must(t, s.Add(entry(string(rune('a'+i)), "/p", "claude", string(rune('a'+i)), "", turn(1, 1, 0))))
	}
	if len(s.rows()) != 3 || len(s.chats()) != 2 {
		t.Errorf("rows %d chats %d, want 3 and 2", len(s.rows()), len(s.chats()))
	}
}

func TestLabelAndFieldsAreBounded(t *testing.T) {
	s, _, _ := newStore(t, "2026-11-01 10:00")
	e := entry("c", strings.Repeat("p", 5000), "claude", "m", "", turn(1, 1, 0))
	e.Label = strings.Repeat("é", 500) + "\x1b[31m"
	must(t, s.Add(e))
	ch := s.chats()[0]
	if len([]rune(ch.Label)) > maxLabel || strings.ContainsRune(ch.Label, '\x1b') {
		t.Errorf("label not cleaned: %q", ch.Label)
	}
	if len(s.rows()[0].Project) > maxField {
		t.Error("project not cut")
	}
}

func TestSummaryRangesFiltersAndBreakdowns(t *testing.T) {
	s, c, _ := newStore(t, "2026-10-01 10:00")
	must(t, s.Add(entry("c0", "/a", "claude", "opus", "", turn(1000, 0, 1))))
	c.t = day("2026-10-28 10:00")
	must(t, s.Add(entry("c1", "/a", "claude", "opus", "", turn(100, 10, 0.5))))
	c.t = day("2026-11-01 10:00")
	must(t, s.Add(entry("c2", "/a", "codex", "gpt-5", "t1", turn(40, 4, 0))))
	must(t, s.Add(entry("c3", "/b", "claude", "sonnet", "", turn(20, 2, 0.2))))

	sum := s.Summary(Query{}, nil)
	if sum.Today.InputTokens != 60 || sum.Week.InputTokens != 160 || sum.Month.InputTokens != 160 {
		t.Errorf("today/7d/30d = %d/%d/%d", sum.Today.InputTokens, sum.Week.InputTokens, sum.Month.InputTokens)
	}
	if len(sum.Days) != 30 || sum.Days[29].Day != "2026-11-01" || sum.Days[29].Totals.Turns != 2 || sum.Days[0].Day != "2026-10-03" {
		t.Errorf("days: %d, last %+v, first %+v", len(sum.Days), sum.Days[len(sum.Days)-1], sum.Days[0])
	}
	if len(sum.Projects) != 2 || sum.Projects[0].Key != "/a" {
		t.Errorf("projects (biggest cost first): %+v", sum.Projects)
	}
	if len(sum.Tasks) != 1 || sum.Tasks[0].Key != "t1" {
		t.Errorf("tasks (chats outside tasks are not a task): %+v", sum.Tasks)
	}
	if len(sum.Agents) != 2 || len(sum.Models) != 3 {
		t.Errorf("agents %+v models %+v", sum.Agents, sum.Models)
	}
	if !sum.Unpriced || sum.Estimated {
		t.Errorf("codex tokens have no cost and no price: unpriced=%v estimated=%v", sum.Unpriced, sum.Estimated)
	}
	if len(sum.Chats) != 3 || sum.Chats[0].ID != "c1" {
		t.Errorf("top chats in range, by cost: %+v", sum.Chats)
	}

	byTask := s.Summary(Query{Task: "t1"}, nil)
	if byTask.Today.InputTokens != 40 || len(byTask.Chats) != 1 || len(byTask.Projects) != 1 {
		t.Errorf("task filter: %+v", byTask)
	}
	byProject := s.Summary(Query{Project: "/b", Days: 7}, nil)
	if byProject.Week.InputTokens != 20 || len(byProject.Days) != 7 {
		t.Errorf("project filter: week %d days %d", byProject.Week.InputTokens, len(byProject.Days))
	}
}

func TestEstimatesUseOnlyConfiguredPrices(t *testing.T) {
	s, _, _ := newStore(t, "2026-11-01 10:00")
	must(t, s.Add(entry("c1", "/a", "codex", "gpt-5", "", turn(2_000_000, 1_000_000, 0))))
	must(t, s.Add(entry("c2", "/a", "claude", "opus", "", turn(1_000_000, 0, 3))))
	sum := s.Summary(Query{}, Prices{"GPT-5": {In: 1.25, Out: 10}, "opus": {In: 100, Out: 100}})
	if !near(sum.Today.EstimatedUSD, 2*1.25+10) {
		t.Errorf("estimate = %v, want 12.5", sum.Today.EstimatedUSD)
	}
	if !near(sum.Today.CostUSD, 3) {
		t.Errorf("a reported cost is never re-priced: %v", sum.Today.CostUSD)
	}
	if !sum.Estimated || sum.Unpriced {
		t.Errorf("estimated=%v unpriced=%v", sum.Estimated, sum.Unpriced)
	}
}

func TestCSVExport(t *testing.T) {
	s, _, _ := newStore(t, "2026-11-01 10:00")
	must(t, s.Add(entry("c1", "/a", "codex", "=cmd()", "t1", turn(2_000_000, 0, 0))))
	must(t, s.Add(entry("c2", "/a", "claude", "opus", "", turn(10, 1, 0.25))))
	data, err := s.CSV(Query{}, Prices{"=cmd()": {In: 1}})
	must(t, err)
	recs, err := csv.NewReader(strings.NewReader(string(data))).ReadAll()
	must(t, err)
	if len(recs) != 3 || recs[0][0] != "day" {
		t.Fatalf("csv = %q", data)
	}
	var codex []string
	for _, r := range recs[1:] {
		if r[2] == "codex" {
			codex = r
		}
	}
	if codex == nil || codex[3] != "'=cmd()" {
		t.Errorf("formula-like cells must be neutralised: %v", codex)
	}
	if codex[len(codex)-1] != "2.000000" {
		t.Errorf("estimated cost column = %q", codex[len(codex)-1])
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func near(a, b float64) bool { d := a - b; return d < 1e-9 && d > -1e-9 }
