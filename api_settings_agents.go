package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"flint/internal/agentsession/acpdriver"
)

// probeTimeout bounds the "Check" of a custom agent: a node-based CLI can
// take several seconds to start.
const probeTimeout = 30 * time.Second

// ProbeCustomAgent starts a saved custom agent, runs the ACP `initialize`
// handshake and stops it, so Settings can show whether the command speaks
// ACP (and the agent's name and version). Only agents already in settings
// can be checked, so this binding runs nothing the settings file doesn't
// name. The agent starts in an empty temporary folder, not in a project, so
// no project configuration is read; no session is opened.
func (s *SettingsAPI) ProbeCustomAgent(id string) (acpdriver.AgentInfo, error) {
	c, ok := s.app.currentSettings().CustomAgent(id)
	if !ok {
		return acpdriver.AgentInfo{}, fmt.Errorf("no custom agent %q in settings (save it first)", id)
	}
	dir, err := os.MkdirTemp("", "flint-acp-check-")
	if err != nil {
		return acpdriver.AgentInfo{}, err
	}
	defer os.RemoveAll(dir)
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	return acpdriver.Probe(ctx, customAgentOptions(c, nil), dir)
}
