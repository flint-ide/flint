package settings

import (
	"strings"
	"testing"
)

func TestMCPServerDefaultsAndPartialFiles(t *testing.T) {
	def := Default().MCPServer
	// Terminal output is opt-in (see MCPTools).
	if !def.Enabled || !def.Tools.Diagnostics || !def.Tools.Editor || def.Tools.Terminals || !def.Tools.Tests || def.TerminalLines != 200 {
		t.Fatalf("defaults %+v", def)
	}
	got, err := Load(write(t, `{"mcpServer":{"tools":{"terminals":true,"editor":false},"terminalLines":5000}}`))
	if err != nil {
		t.Fatal(err)
	}
	m := got.MCPServer
	if !m.Enabled || !m.Tools.Terminals || m.Tools.Editor || !m.Tools.Diagnostics || m.TerminalLines != MaxTerminalLines {
		t.Fatalf("partial file gave %+v", m)
	}
	got, _ = Load(write(t, `{"mcpServer":{"enabled":false,"terminalLines":"many"}}`))
	if got.MCPServer.Enabled || got.MCPServer.TerminalLines != 200 {
		t.Fatalf("off with a bad value gave %+v", got.MCPServer)
	}
}

// The tools that act are off unless the file switches a group on.
func TestMCPServerActionsAreOffByDefault(t *testing.T) {
	def := Default().MCPServer
	if def.Actions != (MCPActions{}) || def.TestWaitSeconds != 120 {
		t.Fatalf("defaults = %+v", def)
	}
	// A value that isn't a boolean never switches a group on.
	got, _ := Load(write(t, `{"mcpServer":{"actions":{"sql":"yes","run":1}}}`))
	if got.MCPServer.Actions.SQL || got.MCPServer.Actions.Run {
		t.Fatalf("a non-boolean switch loaded: %+v", got.MCPServer.Actions)
	}
	got, err := Load(write(t, `{"mcpServer":{"actions":{"tests":true,"http":true},"testWaitSeconds":5}}`))
	if err != nil {
		t.Fatal(err)
	}
	m := got.MCPServer
	if m.Actions != (MCPActions{Tests: true, HTTP: true}) || m.TestWaitSeconds != MinTestWaitSeconds || !m.Tools.Diagnostics {
		t.Fatalf("loaded %+v", m)
	}
	s := Default()
	s.MCPServer.TestWaitSeconds = MaxTestWaitSeconds + 1
	if err := Validate(s); err == nil {
		t.Fatal("a test wait over the range was accepted")
	}
}

func TestMCPServerValidation(t *testing.T) {
	s := Default()
	s.MCPServer.TerminalLines = 1
	if err := Validate(s); err == nil || !strings.Contains(err.Error(), "terminal lines") {
		t.Fatalf("Validate = %v", err)
	}
	s.MCPServer.TerminalLines = MaxTerminalLines
	if err := Validate(s); err != nil {
		t.Fatal(err)
	}
}
