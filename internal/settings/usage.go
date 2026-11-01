package settings

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"strings"
	"unicode"

	"flint/internal/usage"
)

// Usage configures the usage dashboard: how long daily aggregates are kept,
// the budgets and the prices used to estimate costs agents don't report.
type Usage struct {
	// RetentionDays is how many days of daily aggregates usage.json keeps.
	RetentionDays int `json:"retentionDays"`
	// Prices are per-model prices in USD per million tokens, used only for
	// turns whose agent reported no cost; the result is labelled estimated.
	Prices map[string]usage.Price `json:"prices"`
	// Budgets; zero turns one off. Daily covers all chats of the day, task
	// one agent task's chats.
	DailyBudgetUSD    float64 `json:"dailyBudgetUsd"`
	DailyBudgetTokens int64   `json:"dailyBudgetTokens"`
	TaskBudgetUSD     float64 `json:"taskBudgetUsd"`
	TaskBudgetTokens  int64   `json:"taskBudgetTokens"`
	// PauseOverBudget refuses new turns while a budget is spent, until the
	// user continues from the warning.
	PauseOverBudget bool `json:"pauseOverBudget"`
	// CountCacheReads makes token budgets count cached input too (off: a
	// long Claude conversation re-reads its cache on every call).
	CountCacheReads bool `json:"countCacheReads"`
}

// Limits for the usage settings.
const (
	maxBudgetUSD    = 1_000_000
	maxBudgetTokens = 1_000_000_000_000
	maxPrice        = 10_000 // USD per million tokens
	maxPrices       = 200
	maxModelNameLen = 200
)

func defaultUsage() Usage {
	return Usage{RetentionDays: usage.DefaultRetentionDays, Prices: map[string]usage.Price{}}
}

// Limits are the budgets as the usage store takes them.
func (u Usage) Limits() usage.Limits {
	return usage.Limits{
		DailyUSD: u.DailyBudgetUSD, DailyTokens: u.DailyBudgetTokens,
		TaskUSD: u.TaskBudgetUSD, TaskTokens: u.TaskBudgetTokens, CountCacheReads: u.CountCacheReads,
	}
}

func (u Usage) clone() Usage {
	u.Prices = maps.Clone(u.Prices)
	if u.Prices == nil {
		u.Prices = map[string]usage.Price{}
	}
	return u
}

// CheckUsage reports the first invalid value.
func CheckUsage(u Usage) error {
	if u.RetentionDays < usage.MinRetentionDays || u.RetentionDays > usage.MaxRetentionDays {
		return fmt.Errorf("usage retention %d out of range %d–%d days", u.RetentionDays, usage.MinRetentionDays, usage.MaxRetentionDays)
	}
	for _, b := range []struct {
		name string
		v    float64
	}{{"daily budget", u.DailyBudgetUSD}, {"task budget", u.TaskBudgetUSD}} {
		if !validAmount(b.v, maxBudgetUSD) {
			return fmt.Errorf("%s $%g out of range 0–%d", b.name, b.v, maxBudgetUSD)
		}
	}
	for _, b := range []struct {
		name string
		v    int64
	}{{"daily token budget", u.DailyBudgetTokens}, {"task token budget", u.TaskBudgetTokens}} {
		if b.v < 0 || b.v > maxBudgetTokens {
			return fmt.Errorf("%s %d out of range 0–%d", b.name, b.v, int64(maxBudgetTokens))
		}
	}
	if len(u.Prices) > maxPrices {
		return fmt.Errorf("too many model prices (max %d)", maxPrices)
	}
	for model, p := range u.Prices {
		if err := checkPrice(model, p); err != nil {
			return err
		}
	}
	return nil
}

func checkPrice(model string, p usage.Price) error {
	switch {
	case model == "" || model != strings.TrimSpace(model):
		return errors.New("a model price needs a model name without surrounding spaces")
	case len(model) > maxModelNameLen || strings.ContainsFunc(model, unicode.IsControl):
		return fmt.Errorf("invalid model name %q", model)
	case !validAmount(p.In, maxPrice) || !validAmount(p.Out, maxPrice) || !validAmount(p.CacheRead, maxPrice):
		return fmt.Errorf("price of %s out of range 0–%d per million tokens", model, maxPrice)
	}
	return nil
}

func validAmount(v, limit float64) bool { return !math.IsNaN(v) && v >= 0 && v <= limit }

// normalizeUsage keeps the valid values and defaults the rest.
func normalizeUsage(u Usage) Usage {
	def := defaultUsage()
	out := u.clone()
	if CheckUsage(Usage{RetentionDays: u.RetentionDays}) != nil {
		out.RetentionDays = def.RetentionDays
	}
	if !validAmount(u.DailyBudgetUSD, maxBudgetUSD) {
		out.DailyBudgetUSD = 0
	}
	if !validAmount(u.TaskBudgetUSD, maxBudgetUSD) {
		out.TaskBudgetUSD = 0
	}
	if u.DailyBudgetTokens < 0 || u.DailyBudgetTokens > maxBudgetTokens {
		out.DailyBudgetTokens = 0
	}
	if u.TaskBudgetTokens < 0 || u.TaskBudgetTokens > maxBudgetTokens {
		out.TaskBudgetTokens = 0
	}
	prices := map[string]usage.Price{}
	for model, p := range u.Prices {
		if len(prices) < maxPrices && checkPrice(model, p) == nil {
			prices[model] = p
		}
	}
	out.Prices = prices
	return out
}
