package codex

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

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

// Only a codex whose `mcp add --help` lists --url and --bearer-token-env-var
// gets Flint's server; the answer is cached per binary.
func TestHTTPMCPSupport(t *testing.T) {
	current := "Usage: codex mcp add [OPTIONS] <NAME> (--url <URL> | -- <COMMAND>...)\n      --url <URL>\n      --bearer-token-env-var <ENV_VAR>\n"
	old := "Usage: codex mcp add [OPTIONS] <NAME> -- <COMMAND>...\n      --env <KEY=VALUE>\n"
	urlOnly := "      --url <URL>\n"
	if !helpSupportsHTTPMCP(current) || helpSupportsHTTPMCP(old) || helpSupportsHTTPMCP(urlOnly) || helpSupportsHTTPMCP("") {
		t.Fatal("wrong decision")
	}

	dir := t.TempDir()
	count := filepath.Join(dir, "count")
	script := func(name, help string) string {
		p := filepath.Join(dir, name)
		body := "#!/bin/sh\necho x >> " + count + "\n[ \"$1 $2 $3\" = \"mcp add --help\" ] || exit 2\nprintf '%s' '" + help + "'\n"
		if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	newer, older := script("codex-new", current), script("codex-old", old)
	if !probeHTTPMCP(newer, os.Environ()) || probeHTTPMCP(older, os.Environ()) {
		t.Fatal("probe decided wrongly")
	}
	if !probeHTTPMCP(newer, os.Environ()) {
		t.Fatal("cached answer changed")
	}
	if data, _ := os.ReadFile(count); strings.Count(string(data), "x") != 2 {
		t.Fatalf("binaries asked %d times, want once each", strings.Count(string(data), "x"))
	}
	if probeHTTPMCP(filepath.Join(dir, "missing"), os.Environ()) {
		t.Fatal("missing binary supported")
	}
}

// A codex without HTTP MCP support is never offered the server (and no
// token is issued).
func TestNoMCPOfferWithoutSupport(t *testing.T) {
	prev := supportsHTTPMCP
	supportsHTTPMCP = func(string, []string) bool { return false }
	t.Cleanup(func() { supportsHTTPMCP = prev })
	offered := false
	d := New(Options{Binary: "/bin/sh", MCP: func(string) (agentsession.MCPServer, bool) {
		offered = true
		return agentsession.MCPServer{URL: "u", Token: "t"}, true
	}})
	defer d.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _ = d.Start(ctx, t.TempDir(), func(agentsession.Event) {}) // /bin/sh is no app-server
	if offered {
		t.Fatal("server offered to a codex without HTTP MCP support")
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
