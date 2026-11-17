package agent

import (
	"fmt"
	"maps"
	"slices"
)

// Model tiers name a kind of model instead of a model, so that an agent
// definition can ask for one on whichever engine it runs on
// (docs/design/agent-definitions.md): settings map each tier to a model per
// engine ("agents" → "claude" → "tiers"), with built-in defaults where an
// engine has stable aliases.
const (
	TierFast     = "fast"
	TierBalanced = "balanced"
	TierStrong   = "strong"
)

// Tiers lists the tier names, from the cheapest to the most capable.
func Tiers() []string { return []string{TierFast, TierBalanced, TierStrong} }

// IsTier reports whether model is a tier name rather than a model name.
func IsTier(model string) bool { return slices.Contains(Tiers(), model) }

// DefaultTiers is the built-in model of each tier on engine k: Claude Code's
// own stable aliases. Codex has none, because its model names change; an
// unmapped tier runs on the engine's configured model.
func DefaultTiers(k Kind) map[string]string {
	if k == Claude {
		return map[string]string{TierFast: "haiku", TierBalanced: "sonnet", TierStrong: "opus"}
	}
	return map[string]string{}
}

// CheckTiers validates a tier map from settings: known tier names only, each
// a valid model or "" (no model for that tier, not even the default).
func CheckTiers(tiers map[string]string) error {
	for _, tier := range slices.Sorted(maps.Keys(tiers)) {
		if !IsTier(tier) {
			return fmt.Errorf("unknown model tier %q: use fast, balanced or strong", tier)
		}
		if err := CheckModel(tiers[tier]); err != nil {
			return fmt.Errorf("tier %s: %w", tier, err)
		}
	}
	return nil
}

// TierModels is the model each tier runs on for engine k: settings (a tier
// set to "" has none) over the built-in defaults. A tier missing from the
// result has no model of its own.
func TierModels(k Kind, settings map[string]string) map[string]string {
	out := DefaultTiers(k)
	for tier, model := range settings {
		if IsTier(tier) {
			out[tier] = model
		}
	}
	maps.DeleteFunc(out, func(_, model string) bool { return model == "" })
	return out
}
