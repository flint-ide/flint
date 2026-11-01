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
