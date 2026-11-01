package codex

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"flint/internal/agent"
	"flint/internal/agentsession"
)

// The user's extra arguments precede app-server, Flint's overrides follow,
// and Restricted Mode drops the extra arguments.
func TestBuildArgs(t *testing.T) {
	opts := Options{Config: []string{"model_verbosity=low"}, ExtraArgs: []string{"-c", "model_reasoning_effort=high"}}
	got, err := buildArgs(opts, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"-c", "model_reasoning_effort=high", "app-server", "-c", "model_verbosity=low"}; !reflect.DeepEqual(got, want) {
		t.Errorf("buildArgs() = %q, want %q", got, want)
	}
	if got, err := buildArgs(opts, true, nil); err != nil || !reflect.DeepEqual(got, []string{"app-server", "-c", "model_verbosity=low"}) {
		t.Errorf("untrusted buildArgs() = %q, %v", got, err)
	}
	for _, bad := range [][]string{{"--dangerously-bypass-approvals-and-sandbox"}, {"-c", "sandbox_mode=danger-full-access"}, {"--"}, {"-c", "mcp_servers.flint.url=\"http://evil\""}} {
		if _, err := buildArgs(Options{ExtraArgs: bad}, false, nil); err == nil {
			t.Errorf("buildArgs() accepted %q", bad)
		}
	}
}

// Flint's MCP server is configured with -c overrides; the bearer token is
// read from an environment variable, never passed on the command line.
func TestBuildArgsWithFlintMCPServer(t *testing.T) {
	srv := &agentsession.MCPServer{URL: "http://127.0.0.1:4321/mcp", Token: "tok-secret"}
	got, err := buildArgs(Options{ExtraArgs: []string{"-c", "mcp_servers.github.url=\"https://x\""}}, false, srv)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-c", "mcp_servers.github.url=\"https://x\"", "app-server",
		"-c", `mcp_servers.flint.url="http://127.0.0.1:4321/mcp"`, "-c", `mcp_servers.flint.bearer_token_env_var="FLINT_MCP_TOKEN"`}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildArgs() = %q, want %q", got, want)
	}
	t.Setenv("FLINT_MCP_TOKEN", "inherited")
	if env := buildEnv(nil, srv); lastValue(env, "FLINT_MCP_TOKEN") != "tok-secret" {
		t.Errorf("token env %q", lastValue(env, "FLINT_MCP_TOKEN"))
	}
	if env := buildEnv(nil, nil); lastValue(env, "FLINT_MCP_TOKEN") != "" {
		t.Error("an inherited token reaches a session without the server")
	}
}

func lastValue(env []string, key string) string {
	v := ""
	for _, kv := range env {
		if k, val, _ := strings.Cut(kv, "="); k == key {
			v = val
		}
	}
	return v
}

// Settings changes reach new sessions through Options.Settings.
func TestSettingsAreReadOnStart(t *testing.T) {
	cfg := agent.Config{Path: "/nonexistent/codex-from-settings"}
	d := New(Options{Binary: "/bin/sh", Settings: func() agent.Config { return cfg }})
	defer d.Close()
	sink := func(agentsession.Event) {}
	if _, err := d.Start(context.Background(), t.TempDir(), sink); err == nil || !strings.Contains(err.Error(), "codex-from-settings") {
		t.Errorf("Start with configured path: %v", err)
	}
	cfg = agent.Config{Path: "/bin/sh", Args: []string{"--yolo"}}
	if _, err := d.Start(context.Background(), t.TempDir(), sink); err == nil || !strings.Contains(err.Error(), "--yolo") {
		t.Errorf("Start with reserved extra argument: %v", err)
	}
}
