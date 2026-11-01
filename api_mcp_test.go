package main

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"flint/internal/lsp"
	"flint/internal/mcptools"
	"flint/internal/settings"
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

	off := settings.Default()
	off.MCPServer.Tools.Kubernetes = false
	a.setSettingsForTest(off)
	_, body = mcpPost(t, srv.URL, srv.Token, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	if strings.Contains(body, "get_kubernetes_status") || !strings.Contains(body, "get_terminal_output") {
		t.Fatalf("tool switch ignored: %s", body)
	}

	off.MCPServer.Enabled = false
	a.setSettingsForTest(off)
	if status, _ := mcpPost(t, srv.URL, srv.Token, `{"jsonrpc":"2.0","id":3,"method":"ping"}`); status != http.StatusForbidden {
		t.Fatalf("server off: %d", status)
	}
	if _, ok := a.offerMCP(project); ok {
		t.Fatal("offered while off")
	}

	a.setSettingsForTest(settings.Default())
	if err := a.setTrust(project, false); err != nil {
		t.Fatal(err)
	}
	if status, _ := mcpPost(t, srv.URL, srv.Token, `{"jsonrpc":"2.0","id":4,"method":"ping"}`); status != http.StatusForbidden {
		t.Fatalf("trust revoked: %d", status)
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
