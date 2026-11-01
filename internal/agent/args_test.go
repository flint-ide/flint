package agent

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestCheckArgs(t *testing.T) {
	tests := []struct {
		name string
		kind Kind
		args []string
		want string // error substring; "" means valid
	}{
		{"none", Claude, nil, ""},
		{"claude flags", Claude, []string{"--add-dir", "/tmp/x", "--append-system-prompt", "Be brief.", "--debug=api"}, ""},
		{"codex config", Codex, []string{"-c", "model_reasoning_effort=high", "--enable", "web_search"}, ""},
		{"empty", Claude, []string{""}, "empty"},
		{"newline", Claude, []string{"--append-system-prompt", "a\nb"}, "newline"},
		{"too long", Claude, []string{strings.Repeat("x", MaxArgLen+1)}, "longer"},
		{"too many", Claude, slices.Repeat([]string{"-d"}, MaxArgs+1), "too many"},
		{"double dash", Claude, []string{"--"}, `"--"`},
		{"setting sources", Claude, []string{"--setting-sources", "user,project"}, "--setting-sources"},
		{"setting sources inline", Claude, []string{"--setting-sources=project"}, "--setting-sources"},
		{"settings file", Claude, []string{"--settings", ".claude/settings.json"}, "--settings"},
		{"skip permissions", Claude, []string{"--dangerously-skip-permissions"}, "--dangerously-skip-permissions"},
		{"permission mode", Claude, []string{"--permission-mode", "bypassPermissions"}, "--permission-mode"},
		{"model flag", Claude, []string{"--model", "opus"}, "--model"},
		{"combined short", Claude, []string{"-dp"}, "-p"},
		{"resume short", Claude, []string{"-r"}, "-r"},
		{"codex yolo", Codex, []string{"--yolo"}, "--yolo"},
		{"codex sandbox", Codex, []string{"-s", "danger-full-access"}, "-s"},
		{"codex approval config", Codex, []string{"-c", "approval_policy=never"}, "approval_policy"},
		{"codex sandbox config inline", Codex, []string{"--config=sandbox_mode=danger-full-access"}, "sandbox_mode"},
		{"codex config short inline", Codex, []string{`-cprojects."/x".trust_level=trusted`}, "projects"},
		{"codex own mcp server", Codex, []string{"-c", `mcp_servers.github.url="https://x"`}, ""},
		{"codex flint mcp server", Codex, []string{"-c", `mcp_servers.flint.url="http://evil"`}, "mcp_servers.flint"},
		{"claude own mcp config", Claude, []string{"--mcp-config", "/home/me/mcp.json", "--strict-mcp-config"}, ""},
		{"codex listen", Codex, []string{"--listen", "ws://0.0.0.0:1"}, "--listen"},
		{"codex model short", Codex, []string{"-mo3"}, "-m"},
		{"codex config short with =", Codex, []string{`-c=sandbox_mode="danger-full-access"`}, "sandbox_mode"},
		{"codex profile", Codex, []string{"-p", "x"}, "-p"},
		{"codex profile config", Codex, []string{"-c", `profiles.x.sandbox_mode="danger-full-access"`}, "profiles"},
		{"claude allowed tools", Claude, []string{"--allowedTools", "Bash"}, "--allowedTools"},
		{"claude tools", Claude, []string{"--tools", "Bash,Edit"}, "--tools"},
		{"claude tools inline", Claude, []string{"--tools=default"}, "--tools"},
		{"claude allowed tools kebab", Claude, []string{"--allowed-tools=Bash"}, "--allowed-tools"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckArgs(tt.kind, tt.args)
			if tt.want == "" {
				if err != nil {
					t.Errorf("CheckArgs() error = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("CheckArgs() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestCheckPathAndModel(t *testing.T) {
	for _, p := range []string{"", "claude", "/opt/homebrew/bin/claude", "/Users/me/my tools/codex"} {
		if err := CheckPath(p); err != nil {
			t.Errorf("CheckPath(%q) = %v", p, err)
		}
	}
	for _, p := range []string{"./claude", "node_modules/.bin/claude", "-x", " claude", "/bin/a\nb", strings.Repeat("a", MaxPathLen+1)} {
		if err := CheckPath(p); err == nil {
			t.Errorf("CheckPath(%q) = nil, want error", p)
		}
	}
	for _, m := range []string{"", "opus", "claude-sonnet-4-5[1m]", "us.anthropic.claude-opus-4-1:0", "gpt-5-codex"} {
		if err := CheckModel(m); err != nil {
			t.Errorf("CheckModel(%q) = %v", m, err)
		}
	}
	for _, m := range []string{"--dangerously-skip-permissions", "a b", "x;y", strings.Repeat("m", 200)} {
		if err := CheckModel(m); err == nil {
			t.Errorf("CheckModel(%q) = nil, want error", m)
		}
	}
}

func TestComposeArgsKeepsFlintFlagsLast(t *testing.T) {
	flint := []string{"-p", "hi", "--setting-sources", "user"}
	got, err := ComposeArgs(Claude, []string{"--add-dir", "/tmp/x"}, flint, false)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"--add-dir", "/tmp/x", "-p", "hi", "--setting-sources", "user"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ComposeArgs() = %q, want %q", got, want)
	}
	// Restricted Mode drops the extra arguments entirely.
	got, err = ComposeArgs(Claude, []string{"--mcp-config", ".mcp.json"}, flint, true)
	if err != nil || !reflect.DeepEqual(got, flint) {
		t.Errorf("restricted ComposeArgs() = %q, %v, want only Flint's", got, err)
	}
	// A reserved flag fails instead of reaching the CLI.
	if _, err := ComposeArgs(Claude, []string{"--setting-sources", "project"}, flint, false); err == nil {
		t.Error("ComposeArgs() accepted --setting-sources")
	}
}

// A headless Claude run may only read: the prompt carries file context from
// the repository, which could try to make the agent edit files or run
// commands. It also starts no MCP servers.
func TestHeadlessClaudeIsReadOnly(t *testing.T) {
	got, err := headlessArgs(Claude, Config{}, "p", "", false)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(got, " ")
	if !strings.Contains(joined, "--tools "+headlessTools) || !strings.Contains(joined, "--strict-mcp-config") {
		t.Errorf("headless claude args = %q", got)
	}
	for _, tool := range []string{"Bash", "Edit", "Write", "MultiEdit", "NotebookEdit", "WebFetch", "WebSearch", "Task"} {
		if strings.Contains(","+headlessTools+",", ","+tool+",") {
			t.Errorf("headless tools include %s", tool)
		}
	}
}

func TestHeadlessArgs(t *testing.T) {
	cfg := Config{Model: "haiku", Args: []string{"--append-system-prompt", "Be terse."}}
	got, err := headlessArgs(Claude, cfg, "describe", "diff", true)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"-p", "describe", "--tools", headlessTools, "--strict-mcp-config", "--model", "haiku", "--setting-sources", "user"}; !reflect.DeepEqual(got, want) {
		t.Errorf("untrusted claude args = %q, want %q", got, want)
	}
	got, err = headlessArgs(Codex, Config{Model: "o3", Args: []string{"-c", "model_reasoning_effort=low"}}, "describe", "diff", false)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"-c", "model_reasoning_effort=low", "exec", "--sandbox", "read-only", "--model", "o3", "describe\n\ndiff"}; !reflect.DeepEqual(got, want) {
		t.Errorf("codex args = %q, want %q", got, want)
	}
	if _, err := headlessArgs(Claude, Config{Model: "-x"}, "p", "", false); err == nil {
		t.Error("invalid model accepted")
	}
}
