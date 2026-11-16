// Package mcptools is what Flint's MCP server (internal/mcpserver) tells the
// agents it starts: language-server diagnostics, the open editors and the
// selection, recent terminal and run output and the last test run. The stores here are fed by Flint as things happen (no
// polling); the tools read them for one project at a time.
package mcptools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"flint/internal/mcpserver"
)

// Tool names (agents see them as mcp__flint__<name>).
const (
	ToolDiagnostics = "get_diagnostics"
	ToolEditor      = "get_editor_state"
	ToolTerminals   = "get_terminal_output"
	ToolTests       = "get_test_results"
)

// Names lists every tool Flint can offer.
var Names = []string{ToolDiagnostics, ToolEditor, ToolTerminals, ToolTests}

// Instructions are sent to the agent with initialize.
const Instructions = "You are running inside Flint, the user's IDE. The get_ tools are read-only and show what Flint sees and a terminal doesn't: " +
	"language-server diagnostics, the files open in the editor and the user's selection, recent output of the user's terminals and run consoles, " +
	"and the results of the last test run. Use them to find out what the user is looking at or what is failing before you ask. " +
	"When the user switched them on, there are also tools that act in Flint (run_tests, run_configuration, send_http_request, run_sql_query): " +
	"their results show in Flint's own tabs too, and the user is asked before each call, so call them only when the task needs it."

// Config selects the tools offered (settings.json "mcpServer").
type Config struct {
	Diagnostics, Editor, Terminals, Tests bool
	// TerminalLines is the most lines get_terminal_output and get_run_output
	// return per console.
	TerminalLines int
	// RunTests, RunConfigs and HTTP offer the tools that act (actions.go);
	// they need Sources.Actor.
	RunTests, RunConfigs, HTTP bool
	// TestWait is how long run_tests waits for a run to finish.
	TestWait time.Duration
}

// Sources is what the tools read.
type Sources struct {
	Diagnostics *Diagnostics
	Editor      *Editor
	Terminals   *Terminals
	Tests       *Tests
	// Actor does what the acting tools ask; nil offers none of them.
	Actor Actor
	// Now is the clock (tests); nil is time.Now.
	Now func() time.Time
}

// NewSources returns empty stores.
func NewSources() *Sources {
	return &Sources{Diagnostics: NewDiagnostics(), Editor: NewEditor(), Terminals: NewTerminals(), Tests: NewTests()}
}

func (s *Sources) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Tools returns the enabled tools.
func (s *Sources) Tools(cfg Config) []mcpserver.Tool {
	var out []mcpserver.Tool
	if cfg.Diagnostics {
		out = append(out, mcpserver.Tool{
			Name:  ToolDiagnostics,
			Title: "Diagnostics",
			Description: "Errors and warnings the language servers report for this project (the squiggles in Flint's editor). " +
				"Only files a language server has checked are included: usually the open files and, for Go, their packages. " +
				"Optionally limited to a file or folder and a minimum severity.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{` +
				`"path":{"type":"string","description":"File or folder, relative to the project or absolute inside it. Default: the whole project."},` +
				`"severity":{"type":"string","enum":["error","warning","information","hint"],"description":"Minimum severity. Default: hint (everything)."}` +
				`},"additionalProperties":false}`),
			Call: s.diagnostics,
		})
	}
	if cfg.Editor {
		out = append(out, mcpserver.Tool{
			Name:  ToolEditor,
			Title: "Editor state",
			Description: "The files open in Flint's editor (and which have unsaved changes), the active file, and the user's " +
				"selection in it with its text (or the cursor position). Use it when the user says \"this\", \"here\" or \"the selected code\".",
			InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
			Call:        s.editor,
		})
	}
	if cfg.Terminals {
		lines := max(1, cfg.TerminalLines)
		out = append(out, mcpserver.Tool{
			Name:  ToolTerminals,
			Title: "Terminal output",
			Description: "Recent output of the user's terminals and run consoles (dev servers, builds, test runs) in this project, as plain text. " +
				"Without a terminal id it lists every console with its last lines; pass an id for a longer tail of one.",
			InputSchema: json.RawMessage(fmt.Sprintf(`{"type":"object","properties":{`+
				`"terminal":{"type":"string","description":"Id of one console, as listed by a call without it."},`+
				`"lines":{"type":"integer","minimum":1,"maximum":%d,"description":"Lines per console (default %d for one console, %d for the list)."}`+
				`},"additionalProperties":false}`, lines, lines, min(lines, listLines))),
			Call: func(ctx context.Context, root string, args json.RawMessage) (string, error) {
				return s.terminals(ctx, root, args, lines)
			},
		})
	}
	if cfg.Tests {
		out = append(out, mcpserver.Tool{
			Name:  ToolTests,
			Title: "Last test results",
			Description: "Results of the last test run started in Flint for this project: totals, and each failed test with its location and output. " +
				"Tells you whether the run is still going.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
			Call:        s.tests,
		})
	}
	return append(out, s.actionTools(cfg)...)
}

// decode reads a tool's arguments; unknown fields are ignored.
func decode(args json.RawMessage, v any) error {
	if len(args) == 0 {
		return nil
	}
	if err := json.Unmarshal(args, v); err != nil {
		return fmt.Errorf("invalid arguments: %v", err)
	}
	return nil
}

// scopePath resolves a path argument inside root.
func scopePath(root, p string) (string, error) {
	if p == "" {
		return root, nil
	}
	if len(p) > maxPathLen {
		return "", fmt.Errorf("path too long")
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(root, p)
	}
	p = filepath.Clean(p)
	if !within(p, root) {
		return "", fmt.Errorf("%s is outside the project", p)
	}
	return p, nil
}
