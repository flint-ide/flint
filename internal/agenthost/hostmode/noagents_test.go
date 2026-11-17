package hostmode

import (
	"context"
	"strings"
	"testing"
	"time"

	"flint/internal/agenthost/client"
	"flint/internal/terminal"
)

// The headless build has no agent drivers: its host answers agent calls with a
// clear error, and terminals still work.
func TestNoAgentsHostRefusesAgentCallsClearly(t *testing.T) {
	cfg := shortDir(t)
	startHostWith(t, runConfig{ConfigDir: cfg, Version: "test", Idle: time.Hour, NoAgents: true})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := Dial(ctx, cfg, client.Config{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_, err = c.Agents().Start(ctx, "claude", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "agents are not available on this host") {
		t.Fatalf("agent start = %v", err)
	}
	if err := c.Terminals().Start("t1", terminal.Options{Cols: 80, Rows: 24}); err != nil {
		t.Fatalf("terminals must still work: %v", err)
	}
	c.Terminals().Close("t1")
}
