package claude

import (
	"encoding/json"
	"testing"
)

func TestUsageContextFromLastIteration(t *testing.T) {
	line := `{"type":"result","subtype":"success","total_cost_usd":0.1,
	 "usage":{"input_tokens":30,"cache_creation_input_tokens":100,"cache_read_input_tokens":900,"output_tokens":50,
	  "iterations":[{"input_tokens":10,"cache_creation_input_tokens":100,"cache_read_input_tokens":0},
	                {"input_tokens":20,"cache_creation_input_tokens":0,"cache_read_input_tokens":900}]},
	 "modelUsage":{"main":{"costUSD":0.1,"contextWindow":200000},"sub":{"contextWindow":100000}}}`
	var r resultMsg
	if err := json.Unmarshal([]byte(line), &r); err != nil {
		t.Fatal(err)
	}
	u := (&translator{}).usage(&r)
	if u == nil {
		t.Fatal("no usage")
	}
	if u.InputTokens != 1030 || u.OutputTokens != 50 {
		t.Errorf("tokens = %+v", u)
	}
	if u.CacheReadTokens != 900 || u.CacheWriteTokens != 100 {
		t.Errorf("cache tokens = %+v", u)
	}
	if u.ContextTokens != 920 || u.ContextWindow != 200000 {
		t.Errorf("context = %+v (want last iteration 920 of 200000)", u)
	}
}

func TestUsageContextFallsBackToTurnInput(t *testing.T) {
	var r resultMsg
	line := `{"type":"result","usage":{"input_tokens":5,"cache_read_input_tokens":95,"output_tokens":1}}`
	if err := json.Unmarshal([]byte(line), &r); err != nil {
		t.Fatal(err)
	}
	u := (&translator{}).usage(&r)
	if u.ContextTokens != 100 || u.ContextWindow != 0 {
		t.Errorf("context = %+v", u)
	}
}

func result(t *testing.T, line string) *resultMsg {
	t.Helper()
	var r resultMsg
	if err := json.Unmarshal([]byte(line), &r); err != nil {
		t.Fatal(err)
	}
	return &r
}

// After --resume the first total_cost_usd may include earlier runs: without a
// per-turn figure that turn's cost is unknown (tokens only), never the total.
func TestResumedFirstTotalIsNotTheTurnCost(t *testing.T) {
	tr := newTranslator(true)
	u := tr.usage(result(t, `{"total_cost_usd":12.5,"usage":{"input_tokens":10,"output_tokens":5}}`))
	if u.CostUSD != 0 || u.InputTokens != 10 {
		t.Errorf("first result after resume: %+v", u)
	}
	u = tr.usage(result(t, `{"total_cost_usd":12.75,"usage":{"input_tokens":10,"output_tokens":5}}`))
	if u.CostUSD < 0.2499 || u.CostUSD > 0.2501 {
		t.Errorf("second result: cost %v, want the difference 0.25", u.CostUSD)
	}
	fresh := newTranslator(false)
	if u := fresh.usage(result(t, `{"total_cost_usd":0.3,"usage":{"input_tokens":1}}`)); u.CostUSD != 0.3 {
		t.Errorf("a new session's first total is its first turn: %+v", u)
	}
	since := newTranslator(true)
	u = since.usage(result(t, `{"total_cost_usd":9,"usage":{"input_tokens":1},"model_usage_since_last_result":{"m":{"costUSD":0.1}}}`))
	if u.CostUSD != 0.1 {
		t.Errorf("a per-turn figure is exact even after resume: %+v", u)
	}
}

func TestTurnSplitByModel(t *testing.T) {
	r := result(t, `{"total_cost_usd":1,"usage":{"input_tokens":30,"output_tokens":20},"model_usage_since_last_result":{
		"claude-haiku":{"inputTokens":5,"outputTokens":5,"cacheReadInputTokens":100,"cacheCreationInputTokens":0,"costUSD":0.01},
		"claude-opus":{"inputTokens":10,"outputTokens":15,"cacheReadInputTokens":1000,"cacheCreationInputTokens":50,"costUSD":0.5}}}`)
	tr := newTranslator(false)
	u := tr.usage(r)
	if len(u.Models) != 2 || u.Models[0].Model != "claude-opus" {
		t.Fatalf("models = %+v", u.Models)
	}
	m := u.Models[0]
	if m.InputTokens != 1060 || m.CacheReadTokens != 1000 || m.CacheWriteTokens != 50 || m.OutputTokens != 15 || m.CostUSD != 0.5 {
		t.Errorf("opus part = %+v", m)
	}
	plain := newTranslator(false)
	if one := plain.usage(result(t, `{"total_cost_usd":1,"usage":{"input_tokens":3}}`)); one.Models != nil {
		t.Errorf("no per-turn split without model_usage_since_last_result: %+v", one.Models)
	}
}
