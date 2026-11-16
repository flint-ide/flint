package main

import (
	"path/filepath"
	"time"

	as "flint/internal/agentsession"
	"flint/internal/diag"
	"flint/internal/mcpserver"
	"flint/internal/mcptools"
	"flint/internal/settings"
)

// Flint's MCP server gives the agents Flint starts read-only access to what
// Flint knows (diagnostics, editors, terminal output, test results). The stores in a.mcpSources are fed by the services as things
// happen; the server starts with the first agent session that gets it. Tools
// that act are off unless switched on in Settings, and ask the user before
// every call (api_mcp_actions.go).
//
// Restricted Mode: untrusted folders get no server (no token, and a token
// stops working if the folder loses its trust), like custom agents and the
// project's own agent settings.

// newMCP builds the (not yet listening) server and its empty stores.
func newMCP(a *App) (*mcpserver.Server, *mcptools.Sources) {
	sources := mcptools.NewSources()
	sources.Actor = mcpActor{a}
	server := mcpserver.New(mcpserver.Options{
		Tools: func() []mcpserver.Tool {
			return append(sources.Tools(mcpToolConfig(a.currentSettings().MCPServer)), a.features.AgentTools()...)
		},
		Allow:        a.allowMCP,
		Approve:      a.approveAction,
		Version:      diag.Version(version),
		Instructions: mcptools.Instructions,
	})
	return server, sources
}

func mcpToolConfig(m settings.MCPServer) mcptools.Config {
	return mcptools.Config{
		Diagnostics: m.Tools.Diagnostics, Editor: m.Tools.Editor, Terminals: m.Tools.Terminals,
		Tests: m.Tools.Tests, TerminalLines: m.TerminalLines,
		RunTests: m.Actions.Tests, RunConfigs: m.Actions.Run, HTTP: m.Actions.HTTP,
		TestWait: time.Duration(m.TestWaitSeconds) * time.Second,
	}
}

// allowMCP: the server is on and root is trusted (checked on every request).
func (a *App) allowMCP(root string) bool {
	return a.currentSettings().MCPServer.Enabled && a.isTrusted(root)
}

// offerMCP is the agents' MCPProvider: the server's URL and a token for one
// session, which the driver revokes when the session's process ends.
func (a *App) offerMCP(cwd string) (as.MCPServer, bool) {
	if !filepath.IsAbs(cwd) || !a.allowMCP(cwd) {
		return as.MCPServer{}, false
	}
	url, token, revoke, err := a.mcp.Endpoint(filepath.Clean(cwd))
	if err != nil {
		a.logf("mcp server: %v", err)
		return as.MCPServer{}, false
	}
	return as.MCPServer{URL: url, Token: token, Release: revoke}, true
}

// forgetMCPRoots revokes the tokens of closed project folders and drops what
// the stores kept about them.
func (a *App) forgetMCPRoots(roots []string) {
	if a.mcp == nil {
		return
	}
	for _, root := range roots {
		a.mcp.RevokeRoot(root)
		a.mcpSources.Diagnostics.ClearRoot(root)
	}
}

// mcpSettingsChanged closes the listener when the server was switched off
// (every token ends; the next chat after switching it on opens a new one).
func (a *App) mcpSettingsChanged(prev, next settings.MCPServer) {
	if prev.Enabled && !next.Enabled {
		a.mcp.Stop()
	}
}

// EditorContextAPI receives what the editor shows, for the agents' MCP tools.
type EditorContextAPI struct {
	app *App
}

// Update replaces the editor snapshot (sent by the frontend, debounced, when
// the open files, the active file or the selection change). The WebView is
// not trusted: the snapshot is validated and bounded.
func (e *EditorContextAPI) Update(snapshot mcptools.EditorSnapshot) error {
	return e.app.mcpSources.Editor.Set(snapshot)
}
