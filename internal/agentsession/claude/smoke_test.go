package claude

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"flint/internal/agentsession"
)

// TestRealClaudeSmoke drives the installed `claude` binary (costs a couple of
// cheap Haiku turns). Opt in with FLINT_REAL_CLAUDE=1; FLINT_CLAUDE_BIN
// overrides the binary.
func TestRealClaudeSmoke(t *testing.T) {
	if os.Getenv("FLINT_REAL_CLAUDE") != "1" {
		t.Skip("set FLINT_REAL_CLAUDE=1 to run against the real Claude Code CLI")
	}
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "notes.txt"), []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := New(Options{Binary: os.Getenv("FLINT_CLAUDE_BIN"), Model: "haiku"})
	defer d.Close()
	rec := &recorder{}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	info, err := d.Start(ctx, cwd, rec.sink)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Logf("session %s model %s", info.ID, info.Model)

	stop, err := d.Prompt(ctx, info.ID, "Reply with exactly: pong. Do not use tools.", nil)
	evs := rec.reset()
	if err != nil || stop != agentsession.StopEndTurn || !strings.Contains(text(evs, agentsession.KindMessageChunk), "pong") {
		t.Fatalf("turn 1: %q, %v, events %v", stop, err, kinds(evs))
	}
	if len(ofKind(evs, agentsession.KindUsage)) != 1 {
		t.Errorf("no usage: %v", kinds(evs))
	}

	rec.setHook(func(ev agentsession.Event) {
		if ev.Kind == agentsession.KindPermission {
			if err := d.Respond(info.ID, ev.Permission.ID, agentsession.RejectOnce); err != nil {
				t.Errorf("Respond: %v", err)
			}
		}
	})
	stop, err = d.Prompt(ctx, info.ID, "Run this exact command with the Bash tool: touch smoke.txt. If it is refused, reply: refused.", nil)
	evs = rec.reset()
	if err != nil || stop != agentsession.StopEndTurn {
		t.Fatalf("turn 2: %q, %v", stop, err)
	}
	perms := ofKind(evs, agentsession.KindPermission)
	if len(perms) == 0 || perms[0].Permission.ToolCall.Kind != agentsession.ToolExecute {
		t.Fatalf("no Bash permission request: %v", kinds(evs))
	}
	if _, err := os.Stat(filepath.Join(cwd, "smoke.txt")); err == nil {
		t.Fatal("rejected command ran")
	}
	if err := d.Stop(info.ID); err != nil {
		t.Fatal(err)
	}
}
