package settings

import (
	"math"
	"path/filepath"
	"strings"
	"testing"

	"flint/internal/usage"
)

func TestUsageDefaults(t *testing.T) {
	u := Default().Usage
	if u.RetentionDays != usage.DefaultRetentionDays || u.Prices == nil || u.PauseOverBudget || u.Limits() != (usage.Limits{}) {
		t.Errorf("defaults = %+v", u)
	}
	if err := Validate(Default()); err != nil {
		t.Error(err)
	}
}

func TestUsageValidation(t *testing.T) {
	ok := func(change func(*Usage)) Usage {
		u := defaultUsage()
		change(&u)
		return u
	}
	bad := map[string]Usage{
		"retention too short": ok(func(u *Usage) { u.RetentionDays = 1 }),
		"retention too long":  ok(func(u *Usage) { u.RetentionDays = 100000 }),
		"negative budget":     ok(func(u *Usage) { u.DailyBudgetUSD = -1 }),
		"NaN budget":          ok(func(u *Usage) { u.TaskBudgetUSD = math.NaN() }),
		"negative tokens":     ok(func(u *Usage) { u.TaskBudgetTokens = -5 }),
		"empty model":         ok(func(u *Usage) { u.Prices = map[string]usage.Price{"": {In: 1}} }),
		"spaced model":        ok(func(u *Usage) { u.Prices = map[string]usage.Price{" gpt": {In: 1}} }),
		"control in model":    ok(func(u *Usage) { u.Prices = map[string]usage.Price{"a\nb": {In: 1}} }),
		"huge price":          ok(func(u *Usage) { u.Prices = map[string]usage.Price{"gpt": {Out: 1e9}} }),
	}
	for name, u := range bad {
		if CheckUsage(u) == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	good := ok(func(u *Usage) {
		u.DailyBudgetUSD, u.DailyBudgetTokens, u.TaskBudgetUSD = 5, 2_000_000, 1.5
		u.Prices = map[string]usage.Price{"gpt-5": {In: 1.25, Out: 10}}
		u.PauseOverBudget = true
	})
	if err := CheckUsage(good); err != nil {
		t.Error(err)
	}
}

func TestUsageLoadIsForgivingAndSaveRoundTrips(t *testing.T) {
	s, err := decode([]byte(`{"usage":{"retentionDays":2,"dailyBudgetUsd":-3,"taskBudgetTokens":1000,
		"prices":{"gpt-5":{"in":1,"out":2}," bad":{"in":1}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	u := s.Usage
	if u.RetentionDays != usage.DefaultRetentionDays || u.DailyBudgetUSD != 0 || u.TaskBudgetTokens != 1000 {
		t.Errorf("normalized = %+v", u)
	}
	if len(u.Prices) != 1 || u.Prices["gpt-5"].Out != 2 {
		t.Errorf("prices = %+v", u.Prices)
	}
	path := filepath.Join(t.TempDir(), FileName)
	saved, err := Save(path, s)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil || loaded.Usage.Prices["gpt-5"] != saved.Usage.Prices["gpt-5"] {
		t.Errorf("round trip: %+v %v", loaded.Usage, err)
	}
	clone := s.Clone()
	clone.Usage.Prices["x"] = usage.Price{}
	if _, leaked := s.Usage.Prices["x"]; leaked {
		t.Error("Clone shares the prices map")
	}
	s.Usage.RetentionDays = 0
	if _, err := Save(path, s); err == nil || !strings.Contains(err.Error(), "retention") {
		t.Errorf("Save must refuse an invalid retention: %v", err)
	}
}
