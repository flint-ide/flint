package main

import (
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"flint/internal/lsp"
	"flint/internal/mcptools"
	"flint/internal/settings"
	"flint/internal/terminal"
)

func mcpPost(t *testing.T, url, token, body string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(data)
}

func (a *App) setSettingsForTest(s settings.Settings) {
	a.settingsMu.Lock()
	a.settings = s
	a.settingsMu.Unlock()
}

// The server is offered only for trusted folders while it is on, and a token
// stops working when either changes.
func TestMCPServerFollowsTrustAndSettings(t *testing.T) {
	a := &App{configDir: t.TempDir(), settings: settings.Default()}
	a.mcp, a.mcpSources = newMCP(a)
	t.Cleanup(func() { _ = a.mcp.Close() })
	project := t.TempDir()

	if _, ok := a.offerMCP(project); ok {
		t.Fatal("offered in Restricted Mode")
	}
	if _, ok := a.offerMCP("relative"); ok {
		t.Fatal("offered for a relative folder")
	}
	if err := a.setTrust(project, true); err != nil {
		t.Fatal(err)
	}
	srv, ok := a.offerMCP(project)
	if !ok || !strings.HasPrefix(srv.URL, "http://127.0.0.1:") || srv.Token == "" {
		t.Fatalf("offer = %+v, %v", srv, ok)
	}
	a.mcpSources.Diagnostics.Set(project+"/main.go", []lsp.Diagnostic{{Severity: 1, Message: "undefined: x"}})
	status, body := mcpPost(t, srv.URL, srv.Token, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_diagnostics"}}`)
	if status != http.StatusOK || !strings.Contains(body, "main.go:1:1: error: undefined: x") {
		t.Fatalf("diagnostics: %d %s", status, body)
	}

	_, body = mcpPost(t, srv.URL, srv.Token, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	if strings.Contains(body, "get_kubernetes_status") || strings.Contains(body, "get_terminal_output") || !strings.Contains(body, "get_editor_state") {
		t.Fatalf("terminal output and cluster status must be opt-in: %s", body)
	}
	on := settings.Default()
	on.MCPServer.Tools.Terminals = true
	a.setSettingsForTest(on)
	if _, body = mcpPost(t, srv.URL, srv.Token, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`); !strings.Contains(body, "get_terminal_output") {
		t.Fatalf("tool switch ignored: %s", body)
	}

	off := settings.Default()
	off.MCPServer.Enabled = false
	a.setSettingsForTest(off)
	if status, _ := mcpPost(t, srv.URL, srv.Token, `{"jsonrpc":"2.0","id":3,"method":"ping"}`); status != http.StatusForbidden {
		t.Fatalf("server off: %d", status)
	}
	if _, ok := a.offerMCP(project); ok {
		t.Fatal("offered while off")
	}
	// Saving the setting closes the listener.
	a.mcpSettingsChanged(settings.Default().MCPServer, off.MCPServer)
	if a.mcp.Listening() {
		t.Fatal("listener open while the server is off")
	}

	a.setSettingsForTest(settings.Default())
	srv, ok = a.offerMCP(project)
	if !ok || !a.mcp.Listening() {
		t.Fatal("not offered again after switching on")
	}
	if err := a.setTrust(project, false); err != nil {
		t.Fatal(err)
	}
	if status, _ := mcpPost(t, srv.URL, srv.Token, `{"jsonrpc":"2.0","id":4,"method":"ping"}`); status != http.StatusForbidden {
		t.Fatalf("trust revoked: %d", status)
	}
}

// Each offer is a new token; the session's release and closing the project
// revoke it; closing the project also forgets its diagnostics.
func TestMCPTokensFollowSessionsAndProjects(t *testing.T) {
	a := &App{configDir: t.TempDir(), settings: settings.Default()}
	a.mcp, a.mcpSources = newMCP(a)
	t.Cleanup(func() { _ = a.mcp.Close() })
	project := t.TempDir()
	if err := a.setTrust(project, true); err != nil {
		t.Fatal(err)
	}
	one, _ := a.offerMCP(project)
	two, _ := a.offerMCP(project)
	if one.Token == two.Token || a.mcp.Live() != 2 {
		t.Fatal("sessions share a token")
	}
	one.Revoke()
	if status, _ := mcpPost(t, one.URL, one.Token, `{"jsonrpc":"2.0","id":1,"method":"ping"}`); status != http.StatusUnauthorized {
		t.Fatalf("released token: %d", status)
	}
	a.mcpSources.Diagnostics.Set(project+"/a.go", []lsp.Diagnostic{{Severity: 1, Message: "x"}})
	a.forgetMCPRoots([]string{project})
	if status, _ := mcpPost(t, two.URL, two.Token, `{"jsonrpc":"2.0","id":1,"method":"ping"}`); status != http.StatusUnauthorized {
		t.Fatalf("closed project's token: %d", status)
	}
	if len(a.mcpSources.Diagnostics.In(project)) != 0 {
		t.Fatal("closed project's diagnostics kept")
	}
}

// A terminal that fails to start leaves no console behind; one that starts is
// kept from its first output.
func TestTerminalConsoleFollowsStart(t *testing.T) {
	a := &App{settings: settings.Default()}
	a.mcp, a.mcpSources = newMCP(a)
	a.terminals = terminal.NewManager(newTerminalEvents(a))
	t.Cleanup(a.terminals.CloseAll)
	api := &TerminalAPI{app: a}
	missing := filepath.Join(t.TempDir(), "missing")
	if err := api.Start("t1", missing, 80, 24, nil); err == nil {
		t.Fatal("started in a missing folder")
	}
	if l := a.mcpSources.Terminals.List(missing); len(l) != 0 {
		t.Fatalf("failed start left %+v", l)
	}
}

func TestEditorContextAPIValidates(t *testing.T) {
	a := &App{settings: settings.Default()}
	a.mcp, a.mcpSources = newMCP(a)
	api := &EditorContextAPI{app: a}
	if err := api.Update(mcptools.EditorSnapshot{Active: "relative.go"}); err == nil {
		t.Fatal("relative path accepted")
	}
	if err := api.Update(mcptools.EditorSnapshot{Files: []mcptools.OpenFile{{Path: "/p/a.go"}}, Active: "/p/a.go"}); err != nil {
		t.Fatal(err)
	}
	if got := a.mcpSources.Editor.In("/p"); got.Active != "/p/a.go" {
		t.Fatalf("snapshot not stored: %+v", got)
	}
}
