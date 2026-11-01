package settings

import (
	"strings"
	"testing"
)

func TestMCPServerDefaultsAndPartialFiles(t *testing.T) {
	def := Default().MCPServer
	// Terminal output and cluster status are opt-in (see MCPTools).
	if !def.Enabled || !def.Tools.Diagnostics || !def.Tools.Editor || def.Tools.Terminals || !def.Tools.Tests || def.Tools.Kubernetes || def.TerminalLines != 200 {
		t.Fatalf("defaults %+v", def)
	}
	got, err := Load(write(t, `{"mcpServer":{"tools":{"terminals":true,"editor":false},"terminalLines":5000}}`))
	if err != nil {
		t.Fatal(err)
	}
	m := got.MCPServer
	if !m.Enabled || m.Tools.Kubernetes || !m.Tools.Terminals || m.Tools.Editor || !m.Tools.Diagnostics || m.TerminalLines != MaxTerminalLines {
		t.Fatalf("partial file gave %+v", m)
	}
	got, _ = Load(write(t, `{"mcpServer":{"enabled":false,"terminalLines":"many"}}`))
	if got.MCPServer.Enabled || got.MCPServer.TerminalLines != 200 {
		t.Fatalf("off with a bad value gave %+v", got.MCPServer)
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
