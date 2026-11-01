// Package usage records what agent chats consume (tokens, and cost when the
// agent reports it) as daily aggregates per project, agent, model, agent
// task and chat, and answers the Usage panel, the CSV export and the budget
// checks.
//
// Everything stays local: one small JSON file in Flint's config directory,
// written atomically a few seconds after changes and when Flint quits (a
// crash leaves the previous version), bounded by a retention in days and by
// row and chat caps. Costs are only what the agent reported; an estimate
// exists only for models the user gave a price in settings, and is kept
// apart so the UI can label it.
package usage

import (
	"math"
	"strings"
	"unicode"
	"unicode/utf8"

	as "flint/internal/agentsession"
)

// Totals is consumption summed over turns. InputTokens includes the cached
// part (CacheReadTokens, CacheWriteTokens) when the agent reports it.
type Totals struct {
	InputTokens      int64 `json:"inputTokens"`
	OutputTokens     int64 `json:"outputTokens"`
	CacheReadTokens  int64 `json:"cacheReadTokens,omitempty"`
	CacheWriteTokens int64 `json:"cacheWriteTokens,omitempty"`
	// CostUSD is what the agents reported.
	CostUSD float64 `json:"costUsd"`
	// The unpriced tokens are those of turns whose agent reported no cost:
	// the only ones an estimate applies to. The cache-read part is priced
	// separately (or left out when the model has no cache-read price).
	UnpricedInputTokens     int64 `json:"unpricedInputTokens,omitempty"`
	UnpricedOutputTokens    int64 `json:"unpricedOutputTokens,omitempty"`
	UnpricedCacheReadTokens int64 `json:"unpricedCacheReadTokens,omitempty"`
	Turns                   int   `json:"turns"`
	// EstimatedUSD prices the unpriced tokens with the prices from settings.
	// Computed for answers, never stored.
	EstimatedUSD float64 `json:"estimatedUsd,omitempty"`
}

// Tokens is input plus output.
func (t Totals) Tokens() int64 { return t.InputTokens + t.OutputTokens }

// Spent is the reported cost plus the estimate.
func (t Totals) Spent() float64 { return t.CostUSD + t.EstimatedUSD }

func (t Totals) add(o Totals) Totals {
	return Totals{
		InputTokens:             t.InputTokens + o.InputTokens,
		OutputTokens:            t.OutputTokens + o.OutputTokens,
		CacheReadTokens:         t.CacheReadTokens + o.CacheReadTokens,
		CacheWriteTokens:        t.CacheWriteTokens + o.CacheWriteTokens,
		CostUSD:                 t.CostUSD + o.CostUSD,
		UnpricedInputTokens:     t.UnpricedInputTokens + o.UnpricedInputTokens,
		UnpricedOutputTokens:    t.UnpricedOutputTokens + o.UnpricedOutputTokens,
		UnpricedCacheReadTokens: t.UnpricedCacheReadTokens + o.UnpricedCacheReadTokens,
		Turns:                   t.Turns + o.Turns,
		EstimatedUSD:            t.EstimatedUSD + o.EstimatedUSD,
	}
}

func totals(in, out, cacheRead, cacheWrite int, cost float64, turns int) Totals {
	nonNeg := func(n int) int64 { return int64(max(n, 0)) }
	t := Totals{
		InputTokens:      nonNeg(in),
		OutputTokens:     nonNeg(out),
		CacheReadTokens:  min(nonNeg(cacheRead), nonNeg(in)),
		CacheWriteTokens: nonNeg(cacheWrite),
		Turns:            turns,
	}
	if cost > 0 && !math.IsInf(cost, 0) {
		t.CostUSD = cost
	} else {
		t.UnpricedInputTokens, t.UnpricedOutputTokens, t.UnpricedCacheReadTokens = t.InputTokens, t.OutputTokens, t.CacheReadTokens
	}
	return t
}

// FromEvent is one turn's usage event as totals.
func FromEvent(u *as.Usage) Totals {
	if u == nil {
		return Totals{}
	}
	return totals(u.InputTokens, u.OutputTokens, u.CacheReadTokens, u.CacheWriteTokens, u.CostUSD, 1)
}

// Part is the share of a turn one model used.
type Part struct {
	Model string
	Usage Totals
}

// Parts splits a turn by model when the agent reported that (the turn is
// counted once, for the model listed first), else it is one part for
// u.Model ("" when unknown).
func Parts(u *as.Usage) []Part {
	if u == nil {
		return nil
	}
	if len(u.Models) == 0 {
		return []Part{{Model: u.Model, Usage: FromEvent(u)}}
	}
	out := make([]Part, 0, len(u.Models))
	for i, m := range u.Models {
		turns := 0
		if i == 0 {
			turns = 1
		}
		out = append(out, Part{Model: m.Model, Usage: totals(m.InputTokens, m.OutputTokens, m.CacheReadTokens, m.CacheWriteTokens, m.CostUSD, turns)})
	}
	return out
}

// Price is what a model costs, in USD per million tokens. CacheRead prices
// cached input; 0 leaves cached tokens out of estimates (they cost a
// fraction of fresh input, so the input price would overstate them).
type Price struct {
	In        float64 `json:"in"`
	Out       float64 `json:"out"`
	CacheRead float64 `json:"cacheRead,omitempty"`
}

// Prices maps model names (matched exactly, ignoring case) to prices.
type Prices map[string]Price

func (p Prices) lookup(model string) (Price, bool) {
	if pr, ok := p[model]; ok {
		return pr, true
	}
	for name, pr := range p {
		if strings.EqualFold(strings.TrimSpace(name), model) {
			return pr, true
		}
	}
	return Price{}, false
}

// estimate prices t's unpriced tokens for model; ok is false without a
// price, cacheLeftOut when cached tokens had no cache-read price.
func (p Prices) estimate(model string, t Totals) (usd float64, ok, cacheLeftOut bool) {
	if t.UnpricedInputTokens+t.UnpricedOutputTokens == 0 {
		return 0, false, false
	}
	pr, ok := p.lookup(model)
	if !ok {
		return 0, false, false
	}
	cached := min(t.UnpricedCacheReadTokens, t.UnpricedInputTokens)
	usd = float64(t.UnpricedInputTokens-cached)*pr.In + float64(t.UnpricedOutputTokens)*pr.Out
	if cached > 0 {
		if pr.CacheRead > 0 {
			usd += float64(cached) * pr.CacheRead
		} else {
			cacheLeftOut = true
		}
	}
	return usd / 1e6, true, cacheLeftOut
}

// Entry is one model's part of one turn of one chat.
type Entry struct {
	Chat    string // the chat's id
	Label   string // what the chat is about (its first message), shown in Top chats
	Project string // the project folder (a task's main checkout)
	Agent   string
	Model   string
	Task    string // the agent task id, "" for chats outside tasks
	Usage   Totals
}

// Bounds of stored strings.
const (
	maxField = 512 // project paths, model and agent names, ids
	maxLabel = 80  // runes of a chat's label
)

// clean keeps strings printable (control characters become spaces) and
// bounded: they come from agents, prompts and paths.
func clean(s string, limit int) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	if len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// label is the first line of a message, single-spaced and cut to maxLabel runes.
func label(s string) string {
	s = clean(strings.Join(strings.Fields(clean(s, 16*maxLabel)), " "), 4*maxLabel)
	if r := []rune(s); len(r) > maxLabel {
		return string(r[:maxLabel-1]) + "…"
	}
	return s
}
