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

// openAt opens path with writes on every change (no debounce) unless the
// test changes flushDelay.
func openAt(t *testing.T, path string, c *clock, retention int) *Store {
	t.Helper()
	s, err := Open(path, retention)
	if err != nil {
		t.Fatal(err)
	}
	s.now, s.flushDelay = c.now, 0
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
	unpriced := FromEvent(&as.Usage{InputTokens: 10, OutputTokens: 2, CacheReadTokens: 4})
	if unpriced.UnpricedInputTokens != 10 || unpriced.UnpricedOutputTokens != 2 || unpriced.UnpricedCacheReadTokens != 4 {
		t.Errorf("tokens without a reported cost must be kept for estimates: %+v", unpriced)
	}
	if FromEvent(&as.Usage{InputTokens: -5}).InputTokens != 0 {
		t.Error("negative counts must be dropped")
	}
}

func TestPartsSplitATurnByModel(t *testing.T) {
	parts := Parts(&as.Usage{InputTokens: 30, CostUSD: 0.6, Model: "opus", Models: []as.ModelUsage{
		{Model: "opus", InputTokens: 20, OutputTokens: 5, CostUSD: 0.5},
		{Model: "haiku", InputTokens: 10, CacheReadTokens: 8, OutputTokens: 1, CostUSD: 0.1},
	}})
	if len(parts) != 2 || parts[0].Model != "opus" || parts[0].Usage.Turns != 1 || parts[1].Usage.Turns != 0 {
		t.Fatalf("parts = %+v", parts)
	}
	if parts[1].Usage.CacheReadTokens != 8 || parts[1].Usage.CostUSD != 0.1 {
		t.Errorf("haiku part = %+v", parts[1].Usage)
	}
	if one := Parts(&as.Usage{InputTokens: 3, Model: "gpt"}); len(one) != 1 || one[0].Model != "gpt" || one[0].Usage.Turns != 1 {
		t.Errorf("unsplit turn = %+v", one)
	}
}

func TestAddAggregatesPerDayProjectAgentModelTaskChat(t *testing.T) {
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
	if r.Day != "2026-11-01" || r.Agent != "claude" || r.Chat != "c1" || r.InputTokens != 150 || r.Turns != 2 || !near(r.CostUSD, 0.15) {
		t.Errorf("first row = %+v", r)
	}
	chats := s.chats()
	if len(chats) != 2 || chats[0].ID != "c1" || chats[0].LastDay != "2026-11-02" || chats[0].Label != "fix the bug" {
		t.Errorf("chats = %+v", chats)
	}
}

func TestStoreSurvivesRestart(t *testing.T) {
	s, c, path := newStore(t, "2026-11-01 10:00")
	must(t, s.Add(entry("c1", "/p", "claude", "opus", "t1", turn(100, 10, 0.1))))
	reopened := openAt(t, path, c, 90)
	sum := reopened.Summary(Query{}, nil)
	if sum.Today.InputTokens != 100 || len(sum.Chats) != 1 || sum.Problem != "" {
		t.Errorf("after reopening: %+v", sum)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("usage file must be owner-only: %v %v", info.Mode(), err)
	}
}

// Writes are batched: a turn is in memory at once and on disk after the
// delay, or when Flint quits (Close), never lost on a normal quit.
func TestWritesAreDebouncedAndFlushedOnClose(t *testing.T) {
	s, c, path := newStore(t, "2026-11-01 10:00")
	s.flushDelay = time.Hour
	for i := range 200 {
		must(t, s.Add(entry("c", "/p", "claude", "m", "", turn(int64(i), 1, 0))))
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("nothing should be written before the delay")
	}
	if got := s.Summary(Query{}, nil).Today; got.Turns != 200 || got.InputTokens != 199*200/2 {
		t.Errorf("in memory: %+v", got)
	}
	must(t, s.Close())
	if got := openAt(t, path, c, 90).Summary(Query{}, nil).Today; got.Turns != 200 {
		t.Errorf("after Close and reopening: %+v", got)
	}
	must(t, s.Add(entry("c", "/p", "claude", "m", "", turn(1, 1, 0))))
	if got := openAt(t, path, c, 90).Summary(Query{}, nil).Today; got.Turns != 201 {
		t.Errorf("a change after Close is written at once: %+v", got)
	}
}

func TestBackgroundFlushWrites(t *testing.T) {
	s, _, path := newStore(t, "2026-11-01 10:00")
	s.flushDelay = 20 * time.Millisecond
	must(t, s.Add(entry("c", "/p", "claude", "m", "", turn(1, 1, 0))))
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the delayed write never happened")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestCorruptFileIsSetAsideNeverOverwritingAnEarlierOne(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "usage.json")
	stamp := time.Now().Format("20060102-150405")
	must(t, os.WriteFile(path+".bad-"+stamp, []byte("older broken file"), 0o600))
	must(t, os.WriteFile(path, []byte("{not json"), 0o600))
	s, err := Open(path, 90)
	if err == nil || s == nil {
		t.Fatalf("want a usable store and an error to log, got %v %v", s, err)
	}
	older, _ := os.ReadFile(path + ".bad-" + stamp)
	if string(older) != "older broken file" {
		t.Errorf("an earlier .bad file was overwritten: %q", older)
	}
	moved, _ := filepath.Glob(path + ".bad-*")
	if len(moved) != 2 {
		t.Errorf("bad files = %v", moved)
	}
	s.flushDelay = 0
	must(t, s.Add(entry("c", "/p", "a", "m", "", turn(1, 1, 0))))
	if s.Problem() != "" {
		t.Errorf("after moving the file aside, usage is saved again: %s", s.Problem())
	}
}

func TestUnreadableFileIsNeverOverwritten(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads anything")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "usage.json")
	must(t, os.WriteFile(path, []byte(`{"version":1,"rows":[]}`), 0o000))
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	s, err := Open(path, 90)
	if err == nil || s.Problem() == "" {
		t.Fatalf("an unreadable file must be reported: %v", err)
	}
	s.flushDelay = 0
	must(t, s.Add(entry("c", "/p", "a", "m", "", turn(1, 1, 0))))
	must(t, s.Close())
	must(t, os.Chmod(path, 0o600))
	data, _ := os.ReadFile(path)
	if string(data) != `{"version":1,"rows":[]}` {
		t.Errorf("the file was replaced: %s", data)
	}
	if sum := s.Summary(Query{}, nil); sum.Today.Turns != 1 || sum.Problem == "" {
		t.Errorf("usage still counts in memory and the panel says why: %+v", sum)
	}
}

func TestNewerFormatIsReadOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "usage.json")
	body := `{"version":99,"rows":[{"day":"2026-11-01","project":"/p","agent":"claude","inputTokens":7,"outputTokens":1,"costUsd":0,"turns":1}]}`
	must(t, os.WriteFile(path, []byte(body), 0o600))
	s, err := Open(path, 90)
	if err == nil {
		t.Fatal("a newer format must be reported")
	}
	s.now, s.flushDelay = (&clock{day("2026-11-01 12:00")}).now, 0
	if sum := s.Summary(Query{}, nil); sum.Today.InputTokens != 7 || !strings.Contains(sum.Problem, "newer Flint") {
		t.Errorf("shown read-only: %+v", sum)
	}
	must(t, s.Add(entry("c", "/p", "a", "m", "", turn(1, 1, 0))))
	must(t, s.Close())
	if data, _ := os.ReadFile(path); string(data) != body {
		t.Errorf("a newer file was rewritten: %s", data)
	}
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
	must(t, s.Add(entry("new", "/p", "claude", "m", "", turn(2, 2, 0))))
	if rows := s.rows(); len(rows) != 1 || rows[0].Turns != 2 {
		t.Errorf("the index must follow pruning: %+v", rows)
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
	if len(sum.Chats) != 3 || sum.Chats[0].ID != "c1" || sum.Chats[0].Label != "fix the bug" {
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

// Top chats count only what the chat used inside the range, and estimate
// each model's share with its own price.
func TestTopChatsFollowTheRangeAndPriceEachModel(t *testing.T) {
	s, c, _ := newStore(t, "2026-10-01 10:00")
	must(t, s.Add(entry("long", "/a", "codex", "gpt-5", "", turn(9_000_000, 0, 0))))
	c.t = day("2026-11-01 10:00")
	must(t, s.Add(entry("long", "/a", "codex", "gpt-5", "", turn(1_000_000, 0, 0))))
	must(t, s.Add(entry("long", "/a", "codex", "gpt-5-mini", "", turn(1_000_000, 0, 0))))
	prices := Prices{"gpt-5": {In: 2}, "gpt-5-mini": {In: 0.5}}
	sum := s.Summary(Query{Days: 7}, prices)
	if len(sum.Chats) != 1 {
		t.Fatalf("chats = %+v", sum.Chats)
	}
	ch := sum.Chats[0]
	if ch.Totals.InputTokens != 2_000_000 {
		t.Errorf("the chat's use before the range must not count: %+v", ch.Totals)
	}
	if !near(ch.Totals.EstimatedUSD, 2.5) || ch.Model != "gpt-5" {
		t.Errorf("each model priced on its own (2 + 0.5), main model gpt-5: %+v", ch)
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

// Cached input is priced with the cache-read price, or left out (and said
// so) when the model has none: never at the full input price.
func TestCachedInputIsNotPricedAsFreshInput(t *testing.T) {
	s, _, _ := newStore(t, "2026-11-01 10:00")
	cached := FromEvent(&as.Usage{InputTokens: 1_000_000, CacheReadTokens: 800_000})
	must(t, s.Add(entry("c", "/a", "codex", "gpt-5", "", cached)))
	without := s.Summary(Query{}, Prices{"gpt-5": {In: 1}})
	if !near(without.Today.EstimatedUSD, 0.2) || !without.CacheLeftOut {
		t.Errorf("without a cache price: %v (left out %v), want 0.2 and left out", without.Today.EstimatedUSD, without.CacheLeftOut)
	}
	with := s.Summary(Query{}, Prices{"gpt-5": {In: 1, CacheRead: 0.1}})
	if !near(with.Today.EstimatedUSD, 0.2+0.08) || with.CacheLeftOut {
		t.Errorf("with a cache price: %v (left out %v), want 0.28", with.Today.EstimatedUSD, with.CacheLeftOut)
	}
}

func TestCSVExport(t *testing.T) {
	s, _, _ := newStore(t, "2026-11-01 10:00")
	must(t, s.Add(entry("c1", "/a", "codex", "=cmd()", "t1", turn(2_000_000, 0, 0))))
	must(t, s.Add(entry("c9", "/a", "codex", "=cmd()", "t1", turn(1_000_000, 0, 0))))
	must(t, s.Add(entry("c2", "/a", "claude", "opus", "", turn(10, 1, 0.25))))
	data, err := s.CSV(Query{}, Prices{"=cmd()": {In: 1}})
	must(t, err)
	recs, err := csv.NewReader(strings.NewReader(string(data))).ReadAll()
	must(t, err)
	if len(recs) != 3 || recs[0][0] != "day" {
		t.Fatalf("csv (chats summed per key) = %q", data)
	}
	var codex []string
	for _, r := range recs[1:] {
		if r[2] == "codex" {
			codex = r
		}
	}
	if codex == nil || codex[3] != "'=cmd()" || codex[5] != "2" {
		t.Errorf("formula-like cells must be neutralised, chats summed: %v", codex)
	}
	if codex[len(codex)-1] != "3.000000" {
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
