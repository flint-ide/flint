package usage

import (
	"bytes"
	"cmp"
	"encoding/csv"
	"slices"
	"strconv"
	"strings"
)

var csvHeader = []string{
	"day", "project", "agent", "model", "task", "turns",
	"input_tokens", "output_tokens", "cache_read_tokens", "cache_write_tokens",
	"cost_usd_reported", "cost_usd_estimated",
}

// CSV exports the daily aggregates matching q (all retained days, chats
// summed), oldest first. The estimated cost column is empty for rows
// without a price.
func (s *Store) CSV(q Query, prices Prices) ([]byte, error) {
	s.mu.Lock()
	f, _ := s.snapshot()
	s.mu.Unlock()
	sums := map[Key]Totals{}
	for _, r := range f.Rows {
		if q.match(r.Project, r.Task) {
			k := r.Key
			k.Chat = ""
			sums[k] = sums[k].add(r.Totals)
		}
	}
	rows := make([]Row, 0, len(sums))
	for k, t := range sums {
		rows = append(rows, Row{Key: k, Totals: t})
	}
	slices.SortFunc(rows, func(a, b Row) int {
		return cmp.Or(
			strings.Compare(a.Day, b.Day), strings.Compare(a.Project, b.Project), strings.Compare(a.Agent, b.Agent),
			strings.Compare(a.Model, b.Model), strings.Compare(a.Task, b.Task),
		)
	})
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	_ = w.Write(csvHeader)
	for _, r := range rows {
		est := ""
		if v, ok, _ := prices.estimate(r.Model, r.Totals); ok {
			est = strconv.FormatFloat(v, 'f', 6, 64)
		}
		_ = w.Write([]string{
			r.Day, cell(r.Project), cell(r.Agent), cell(r.Model), cell(r.Task), strconv.Itoa(r.Turns),
			itoa(r.InputTokens), itoa(r.OutputTokens), itoa(r.CacheReadTokens), itoa(r.CacheWriteTokens),
			strconv.FormatFloat(r.CostUSD, 'f', 6, 64), est,
		})
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// cell keeps spreadsheets from reading a value as a formula.
func cell(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}
