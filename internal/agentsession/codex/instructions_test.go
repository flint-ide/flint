package codex

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"flint/internal/agent"
)

// A defined agent's instructions and model travel as developerInstructions
// and model of thread/start and thread/resume.
func TestInstructionsAndModel(t *testing.T) {
	const instructions = "--yolo\nYou review; you never edit. \"Quotes\" stay text."
	for _, tt := range []struct {
		fixture, method string
		resume          bool
	}{
		{"text_two_turns.jsonl", methodThreadStart, false},
		{"resume.jsonl", methodThreadResume, true},
	} {
		t.Run(tt.method, func(t *testing.T) {
			h := newHarness(t, tt.fixture, nil)
			h.d.opts.Instructions = instructions
			h.d.opts.Settings = func() agent.Config {
				return agent.Config{Path: h.d.opts.Binary, Model: "gpt-defined"}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			var err error
			if tt.resume {
				_, err = h.d.Resume(ctx, h.cwd, "thr-old", h.rec.sink)
			} else {
				_, err = h.d.Start(ctx, h.cwd, h.rec.sink)
			}
			if err != nil {
				t.Fatal(err)
			}
			params := sentParams(t, h.clientLog(t), tt.method)
			if params["developerInstructions"] != instructions || params["model"] != "gpt-defined" {
				t.Fatalf("%s params: %v", tt.method, params)
			}
		})
	}
}

func TestNoInstructionsNoField(t *testing.T) {
	h := newHarness(t, "text_two_turns.jsonl", nil)
	h.start(t)
	if params := sentParams(t, h.clientLog(t), methodThreadStart); params["developerInstructions"] != nil {
		t.Fatalf("params: %v", params)
	}
}

// sentParams returns the params of the first request method in the client log.
func sentParams(t *testing.T, log, method string) map[string]any {
	t.Helper()
	for line := range strings.SplitSeq(log, "\n") {
		var msg struct {
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if json.Unmarshal([]byte(line), &msg) == nil && msg.Method == method {
			return msg.Params
		}
	}
	t.Fatalf("no %s in client log:\n%s", method, log)
	return nil
}
