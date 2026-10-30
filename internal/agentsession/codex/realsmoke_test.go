package codex

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"flint/internal/agentsession"
)

// TestRealCodexSmoke drives the installed codex binary through one short
// turn (one file edit and one command). It uses the real API, so it only
// runs with FLINT_REAL_CODEX=1. FLINT_CODEX_MODEL picks the model.
func TestRealCodexSmoke(t *testing.T) {
	if os.Getenv("FLINT_REAL_CODEX") != "1" {
		t.Skip("set FLINT_REAL_CODEX=1 to run against the real codex app-server")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	d := New(Options{
		Model:  os.Getenv("FLINT_CODEX_MODEL"),
		Config: []string{`model_reasoning_effort="low"`, `notify=[]`},
	})
	defer d.Close()
	rec := newRecorder()
	rec.setReact(func(ev agentsession.Event) {
		if ev.Kind == agentsession.KindPermission { // nothing here should need approval
			_ = d.Respond(ev.SessionID, ev.Permission.ID, decisionDecline)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	info, err := d.Start(ctx, dir, rec.sink)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Logf("session %+v", info)
	stop, err := d.Prompt(ctx, info.ID, "Append a line 'world' to a.txt using apply_patch, then run the shell command `cat a.txt`, then reply 'done'.", nil)
	if err != nil || stop != agentsession.StopEndTurn {
		t.Fatalf("Prompt: stop=%q err=%v events=%v", stop, err, kinds(rec.all()))
	}
	var edit, run bool
	for _, tc := range rec.toolCalls() {
		t.Logf("tool %s %q status=%s diffs=%d", tc.Kind, tc.Title, tc.Status, len(tc.Diffs))
		edit = edit || (tc.Kind == agentsession.ToolEdit && len(tc.Diffs) == 1 && strings.Contains(tc.Diffs[0].NewText, "world"))
		run = run || (tc.Kind == agentsession.ToolExecute && tc.Status == agentsession.StatusCompleted)
	}
	if !edit || !run {
		t.Errorf("expected an edit with a diff and a completed command; events=%v", kinds(rec.all()))
	}
	if n := len(rec.ofKind(agentsession.KindPermission)); n != 0 {
		t.Errorf("unexpected %d permission requests", n)
	}
	if rec.text(agentsession.KindMessageChunk) == "" || len(rec.ofKind(agentsession.KindUsage)) != 1 {
		t.Errorf("missing message or usage; events=%v", kinds(rec.all()))
	}
}
