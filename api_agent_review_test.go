package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"flint/internal/settings"
)

// reviewRepo is a repository with one commit, a modified file, an untracked
// one and a .env file, opened as a (trusted or not) project.
func reviewRepo(t *testing.T, claude string, trusted bool) (*AgentAPI, string) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	api, root := inlineEditApp(t, claude, trusted)
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(rel, content string) {
		if err := os.WriteFile(filepath.Join(root, rel), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "-q", "-b", "main")
	run("config", "commit.gpgsign", "false")
	write("main.go", "package main\n\nfunc main() {\n}\n")
	run("add", "-A")
	run("commit", "-q", "-m", "init")
	write("main.go", "package main\n\nfunc main() {\n\tx := 1\n}\n")
	write("new.go", "package main\n\nvar token = \"ghp_0123456789abcdefghijklmnopqrstuvwxyzAB\"\n")
	write(".env", "DB_PASSWORD=hunter2\n")
	return api, root
}

var reviewFiles = []ReviewFile{
	{Path: "main.go", Kind: "modified"},
	{Path: "new.go", Kind: "untracked"},
	{Path: ".env", Kind: "untracked"},
}

func TestReviewChanges(t *testing.T) {
	claude, out := fakeClaude(t, `cat <<'EOF'
Sure! Here is the review:
`+"```json"+`
{"findings": [
  {"file": "main.go", "line": 4, "severity": "error", "title": "x is unused", "explanation": "Go refuses unused variables.", "suggestion": "\t_ = 1"},
  {"file": ".env", "line": 1, "title": "never sent"}
]}
`+"```"+`
EOF`)
	api, root := reviewRepo(t, claude, true)
	res, err := api.ReviewChanges(context.Background(), ReviewRequest{Root: root, Files: reviewFiles})
	if err != nil {
		t.Fatal(err)
	}
	if res.Agent != "claude" || strings.Join(res.Files, ",") != "main.go,new.go" {
		t.Errorf("result = %+v", res)
	}
	if len(res.Findings) != 1 {
		t.Fatalf("findings = %+v", res.Findings)
	}
	f := res.Findings[0]
	if f.Path != "main.go" || f.Line != 4 || f.Severity != "error" || f.LineText != "\tx := 1" || f.Suggestion != "\t_ = 1" {
		t.Errorf("finding = %+v", f)
	}
	notes := strings.Join(res.Notes, "\n")
	if !strings.Contains(notes, ".env") || !strings.Contains(notes, "1 finding(s) left out") {
		t.Errorf("notes = %q", notes)
	}

	stdin, _ := os.ReadFile(filepath.Join(out, "stdin"))
	for _, want := range []string{"## main.go (modified)", "    4 +\tx := 1", "## new.go (new file)"} {
		if !strings.Contains(string(stdin), want) {
			t.Errorf("input lacks %q:\n%s", want, stdin)
		}
	}
	if strings.Contains(string(stdin), "hunter2") || strings.Contains(string(stdin), "ghp_0123") {
		t.Errorf("secret sent:\n%s", stdin)
	}
	args, _ := os.ReadFile(filepath.Join(out, "args"))
	if !strings.Contains(string(args), "--tools\nRead,Grep,Glob") || !strings.Contains(string(args), "--strict-mcp-config") {
		t.Errorf("agent ran without read-only tools:\n%s", args)
	}
}

func TestReviewChangesRestrictedMode(t *testing.T) {
	claude, out := fakeClaude(t, `echo '{"findings": []}'`)
	api, root := reviewRepo(t, claude, false)
	res, err := api.ReviewChanges(context.Background(), ReviewRequest{Root: root, Files: reviewFiles[:1]})
	if err != nil || len(res.Findings) != 0 {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	args, _ := os.ReadFile(filepath.Join(out, "args"))
	if !strings.Contains(string(args), "--setting-sources\nuser") || strings.Contains(string(args), "--verbose") {
		t.Errorf("untrusted folder: agent arguments\n%s", args)
	}
}

func TestReviewChangesUsesTheReviewAgentSetting(t *testing.T) {
	claude, _ := fakeClaude(t, `echo '{"findings": []}'`)
	api, root := reviewRepo(t, claude, true)
	s := api.app.settings
	s.AgentReview.Agent = "codex"
	s.Agents["codex"] = settings.AgentSettings{Path: filepath.Join(t.TempDir(), "missing-codex")}
	api.app.settings = s
	_, err := api.ReviewChanges(context.Background(), ReviewRequest{Root: root, Files: reviewFiles[:1]})
	if err == nil || !strings.Contains(err.Error(), "codex") {
		t.Errorf("err = %v", err)
	}
}

func TestReviewChangesRejectsBadRequests(t *testing.T) {
	claude, out := fakeClaude(t, `echo '{"findings": []}'`)
	api, root := reviewRepo(t, claude, true)
	for name, req := range map[string]ReviewRequest{
		"relative root":   {Root: "repo", Files: reviewFiles},
		"foreign root":    {Root: t.TempDir(), Files: reviewFiles},
		"no files":        {Root: root},
		"escaping path":   {Root: root, Files: []ReviewFile{{Path: "../x.go", Kind: "modified"}}},
		"absolute path":   {Root: root, Files: []ReviewFile{{Path: "/etc/hosts", Kind: "untracked"}}},
		"escaping rename": {Root: root, Files: []ReviewFile{{Path: "a.go", OrigPath: "../b.go", Kind: "renamed"}}},
		"unknown kind":    {Root: root, Files: []ReviewFile{{Path: "main.go", Kind: "rm -rf"}}},
		"only secrets":    {Root: root, Files: []ReviewFile{{Path: ".env", Kind: "untracked"}}},
	} {
		if _, err := api.ReviewChanges(context.Background(), req); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := os.Stat(filepath.Join(out, "args")); err == nil {
		t.Error("the agent ran for a rejected request")
	}
}

func TestReviewChangesReportsUnreadableAnswers(t *testing.T) {
	claude, _ := fakeClaude(t, `echo 'Looks good to me!'`)
	api, root := reviewRepo(t, claude, true)
	_, err := api.ReviewChanges(context.Background(), ReviewRequest{Root: root, Files: reviewFiles[:1]})
	if err == nil || !strings.Contains(err.Error(), "JSON") {
		t.Errorf("err = %v", err)
	}
}

func TestReviewChangesDoesNotFollowSymlinks(t *testing.T) {
	claude, out := fakeClaude(t, `echo '{"findings": []}'`)
	api, root := reviewRepo(t, claude, true)
	secret := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(secret, []byte("top secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := api.ReviewChanges(context.Background(), ReviewRequest{Root: root, Files: []ReviewFile{{Path: "link.txt", Kind: "untracked"}}}); err != nil {
		t.Fatal(err)
	}
	stdin, _ := os.ReadFile(filepath.Join(out, "stdin"))
	if strings.Contains(string(stdin), "top secret") {
		t.Errorf("symlink followed:\n%s", stdin)
	}
}

func TestReviewChangesCancelStopsTheAgent(t *testing.T) {
	claude, out := fakeClaude(t, "sleep 30 & echo $! > \""+"$(dirname \"$0\")"+"/child\"\nwait")
	api, root := reviewRepo(t, claude, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := api.ReviewChanges(ctx, ReviewRequest{Root: root, Files: reviewFiles[:1]})
		done <- err
	}()
	// Cancel once the agent runs and has started a child of its own.
	pid := ""
	for deadline := time.Now().Add(30 * time.Second); pid == ""; time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the fake agent did not start its child")
		}
		data, _ := os.ReadFile(filepath.Join(out, "child"))
		pid = strings.TrimSpace(string(data))
	}
	start := time.Now()
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("cancelled run took %s", d)
	}
	// The agent's own child (a grandchild of Flint) is gone too.
	for deadline := time.Now().Add(5 * time.Second); exec.Command("kill", "-0", pid).Run() == nil; time.Sleep(50 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("child %s still runs", pid)
		}
	}
}

func TestReviewChangesCancelledBeforeTheAgentStarts(t *testing.T) {
	claude, out := fakeClaude(t, `echo '{"findings": []}'`)
	api, root := reviewRepo(t, claude, true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := api.ReviewChanges(ctx, ReviewRequest{Root: root, Files: reviewFiles[:1]}); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if _, err := os.Stat(filepath.Join(out, "args")); err == nil {
		t.Error("the agent ran after the review was cancelled")
	}
}
