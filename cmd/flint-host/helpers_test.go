package main

import (
	"context"
	"strings"
	"sync"
	"syscall"
	"time"

	"flint/internal/agenthost/client"
	"flint/internal/agenthost/hostmode"
	"flint/internal/terminal"
)

// sink collects terminal output.
type sink struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (s *sink) Output(_ string, b []byte) {
	s.mu.Lock()
	s.buf.Write(b)
	s.mu.Unlock()
}
func (s *sink) Exited(string, int)          {}
func (s *sink) Title(string, terminal.Info) {}
func (s *sink) AgentState(string, string)   {}

func (s *sink) text() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// stopLeftoverHost makes sure no host of this config directory runs on.
func stopLeftoverHost(cfg string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := hostmode.Dial(ctx, cfg, client.Config{Version: "cleanup"})
	if err != nil {
		return
	}
	pid := c.Hello().Pid
	_ = c.Shutdown(ctx, true)
	_ = c.Close()
	deadline := time.Now().Add(5 * time.Second)
	for syscall.Kill(pid, 0) == nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if syscall.Kill(pid, 0) == nil {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}
