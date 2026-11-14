package lsp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

const initTimeout = 60 * time.Second

// server is one running language server process for a workspace root.
type server struct {
	language string
	root     string
	cmd      *exec.Cmd
	conn     *conn
	out      *outbox // ordered, non-blocking document notifications
	onDiag   func(path string, diags []Diagnostic)
	// initOptions go into initialize (see initializationOptions).
	initOptions map[string]any
	// onApplyEdit receives a workspace/applyEdit the server sends outside a
	// code action (see collectEdits); nil refuses it.
	onApplyEdit func(raw json.RawMessage) error

	// caps are the server's capabilities from initialize, by name.
	caps map[string]json.RawMessage

	stateMu sync.Mutex
	// rawDiags are the published diagnostics as the server sent them, by
	// path: code actions must get them back unchanged (code, data).
	rawDiags map[string][]rawDiagnostic
	// collecting, while a code action's command runs, receives the edits the
	// server asks Flint to apply.
	collecting *[]json.RawMessage

	stderrMu sync.Mutex
	stderr   bytes.Buffer // last bytes of stderr, for error messages
}

// startServer spawns argv in root and performs the initialize handshake.
func startServer(ctx context.Context, language, root string, argv []string, onDiag func(string, []Diagnostic), onApplyEdit func(json.RawMessage) error) (*server, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = root
	cmd.Env = os.Environ()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // kill helper processes with it
	cmd.WaitDelay = time.Second                           // a helper holding stderr must not keep Wait (and the pipes) open
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	initOptions, err := initializationOptions(argv[0])
	if err != nil {
		return nil, err
	}
	s := &server{language: language, root: root, cmd: cmd, onDiag: onDiag, onApplyEdit: onApplyEdit, initOptions: initOptions, rawDiags: map[string][]rawDiagnostic{}}
	cmd.Stderr = (*tailWriter)(s)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("couldn't start the language server %s (%w). Check that it is installed and runs from a terminal, then reopen the file", argv[0], err)
	}
	s.conn = newConn(stdout, stdin, s.handle)
	go func() { _ = cmd.Wait() }()

	ctx, cancel := context.WithTimeout(ctx, initTimeout)
	defer cancel()
	if err := s.initialize(ctx); err != nil {
		s.kill()
		return nil, fmt.Errorf("the language server %s started but didn't finish setting up (%w). Update it, or check its output below, then reopen the file%s", argv[0], err, s.stderrTail())
	}
	s.out = newOutbox(s.conn)
	return s, nil
}

func (s *server) initialize(ctx context.Context) error {
	rootURI := PathToURI(s.root)
	params := map[string]any{
		"processId":  os.Getpid(),
		"clientInfo": map[string]any{"name": "flint"},
		"rootUri":    rootURI,
		"workspaceFolders": []map[string]any{
			{"uri": rootURI, "name": filepath.Base(s.root)},
		},
		"initializationOptions": s.initOptions,
		"capabilities": map[string]any{
			"general": map[string]any{"positionEncodings": []string{"utf-16"}},
			"textDocument": map[string]any{
				"synchronization": map[string]any{"didSave": true},
				"completion": map[string]any{
					"completionItem": map[string]any{
						"snippetSupport":          false,
						"documentationFormat":     []string{"markdown", "plaintext"},
						"insertReplaceSupport":    true,
						"labelDetailsSupport":     true,
						"deprecatedSupport":       true,
						"commitCharactersSupport": false,
					},
					"contextSupport": true,
				},
				"hover":              map[string]any{"contentFormat": []string{"markdown", "plaintext"}},
				"definition":         map[string]any{"linkSupport": true},
				"typeDefinition":     map[string]any{"linkSupport": true},
				"implementation":     map[string]any{"linkSupport": true},
				"references":         map[string]any{},
				"publishDiagnostics": map[string]any{"relatedInformation": false},
				"rename":             map[string]any{"prepareSupport": true},
				"formatting":         map[string]any{},
				"documentSymbol":     map[string]any{"hierarchicalDocumentSymbolSupport": true},
				"signatureHelp": map[string]any{
					"signatureInformation": map[string]any{
						"documentationFormat":    []string{"markdown", "plaintext"},
						"parameterInformation":   map[string]any{"labelOffsetSupport": true},
						"activeParameterSupport": true,
					},
				},
				"codeAction": map[string]any{
					"codeActionLiteralSupport": map[string]any{
						"codeActionKind": map[string]any{"valueSet": []string{
							"", "quickfix", "refactor", "refactor.extract", "refactor.inline", "refactor.rewrite",
							"source", "source.organizeImports", "source.fixAll",
						}},
					},
					"isPreferredSupport": true,
					"disabledSupport":    true,
					"dataSupport":        true,
					"resolveSupport":     map[string]any{"properties": []string{"edit", "command"}},
				},
			},
			"workspace": map[string]any{
				"workspaceFolders": true,
				"configuration":    true,
				"applyEdit":        true,
				// Text edits only: no resourceOperations, so servers don't
				// send file creations, renames or deletions.
				"workspaceEdit":  map[string]any{"documentChanges": true},
				"symbol":         map[string]any{},
				"executeCommand": map[string]any{},
			},
			"window": map[string]any{"workDoneProgress": false},
		},
	}
	var result struct {
		Capabilities map[string]json.RawMessage `json:"capabilities"`
	}
	if err := s.conn.Call(ctx, "initialize", params, &result); err != nil {
		return err
	}
	s.caps = result.Capabilities
	return s.conn.Notify("initialized", map[string]any{})
}

// handle answers server→client traffic with the minimum servers expect.
func (s *server) handle(method string, params json.RawMessage) (any, error) {
	switch method {
	case "textDocument/publishDiagnostics":
		var p struct {
			URI         string       `json:"uri"`
			Diagnostics []Diagnostic `json:"diagnostics"`
		}
		if json.Unmarshal(params, &p) == nil {
			if path := URIToPath(p.URI); path != "" {
				s.keepDiagnostics(path, params)
				if p.Diagnostics == nil {
					p.Diagnostics = []Diagnostic{}
				}
				if s.onDiag != nil {
					s.onDiag(path, p.Diagnostics)
				}
			}
		}
		return nil, nil
	case "workspace/applyEdit":
		var p struct {
			Edit json.RawMessage `json:"edit"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return map[string]any{"applied": false, "failureReason": err.Error()}, nil
		}
		s.stateMu.Lock()
		if s.collecting != nil {
			*s.collecting = append(*s.collecting, p.Edit)
			s.stateMu.Unlock()
			return map[string]any{"applied": true}, nil
		}
		s.stateMu.Unlock()
		if s.onApplyEdit == nil {
			return map[string]any{"applied": false, "failureReason": "not supported"}, nil
		}
		if err := s.onApplyEdit(p.Edit); err != nil {
			return map[string]any{"applied": false, "failureReason": err.Error()}, nil
		}
		return map[string]any{"applied": true}, nil
	case "workspace/configuration":
		// One null per requested item: "use your defaults".
		var p struct {
			Items []json.RawMessage `json:"items"`
		}
		_ = json.Unmarshal(params, &p)
		return make([]any, len(p.Items)), nil
	case "workspace/workspaceFolders":
		return []map[string]any{{"uri": PathToURI(s.root), "name": filepath.Base(s.root)}}, nil
	case "client/registerCapability", "client/unregisterCapability",
		"window/workDoneProgress/create", "window/showMessageRequest":
		return nil, nil
	case "window/logMessage", "window/showMessage", "$/progress", "telemetry/event", "$/logTrace":
		return nil, nil
	}
	return nil, fmt.Errorf("method not supported: %s", method)
}

// rawDiagnostic is a published diagnostic kept as sent, with its range.
type rawDiagnostic struct {
	rng Range
	raw json.RawMessage
}

// maxKeptDiagnostics bounds the diagnostics kept per file for code actions.
const maxKeptDiagnostics = 2000

func (s *server) keepDiagnostics(path string, params json.RawMessage) {
	var p struct {
		Diagnostics []json.RawMessage `json:"diagnostics"`
	}
	_ = json.Unmarshal(params, &p)
	if len(p.Diagnostics) > maxKeptDiagnostics {
		p.Diagnostics = p.Diagnostics[:maxKeptDiagnostics]
	}
	kept := make([]rawDiagnostic, 0, len(p.Diagnostics))
	for _, raw := range p.Diagnostics {
		var d struct {
			Range Range `json:"range"`
		}
		if json.Unmarshal(raw, &d) == nil {
			kept = append(kept, rawDiagnostic{rng: d.Range, raw: raw})
		}
	}
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if len(kept) == 0 {
		delete(s.rawDiags, path)
	} else {
		s.rawDiags[path] = kept
	}
}

// diagnosticsIn returns the server's own diagnostics of path that touch rng.
func (s *server) diagnosticsIn(path string, rng Range) []json.RawMessage {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	out := []json.RawMessage{}
	for _, d := range s.rawDiags[path] {
		if !before(d.rng.End, rng.Start) && !before(rng.End, d.rng.Start) {
			out = append(out, d.raw)
		}
	}
	return out
}

func before(a, b Position) bool {
	return a.Line < b.Line || (a.Line == b.Line && a.Character < b.Character)
}

// collectEdits starts collecting the edits the server asks Flint to apply;
// the returned function stops and returns them.
func (s *server) collectEdits() func() []json.RawMessage {
	var edits []json.RawMessage
	s.stateMu.Lock()
	s.collecting = &edits
	s.stateMu.Unlock()
	return func() []json.RawMessage {
		s.stateMu.Lock()
		defer s.stateMu.Unlock()
		s.collecting = nil
		return edits
	}
}

// capability returns the server's capability by name (nil when absent).
func (s *server) capability(name string) json.RawMessage { return s.caps[name] }

// can reports whether the server announced a capability (true or options).
func (s *server) can(name string) bool {
	raw := s.caps[name]
	return len(raw) > 0 && string(raw) != "null" && string(raw) != "false"
}

// canSub reports whether a capability's options turn on a boolean field.
func (s *server) canSub(name, field string) bool {
	var opts map[string]json.RawMessage
	if json.Unmarshal(s.caps[name], &opts) != nil {
		return false
	}
	return string(opts[field]) == "true"
}

func (s *server) alive() bool {
	select {
	case <-s.conn.done:
		return false
	default:
		return true
	}
}

// shutdown asks the server to exit politely, then kills it.
func (s *server) shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if s.alive() {
		_ = s.conn.Call(ctx, "shutdown", nil, nil)
		_ = s.conn.Notify("exit", nil)
	}
	s.kill()
}

func (s *server) kill() {
	if s.cmd.Process != nil {
		_ = syscall.Kill(-s.cmd.Process.Pid, syscall.SIGKILL)
	}
}

// tailWriter keeps the last few KB of the server's stderr.
type tailWriter server

func (t *tailWriter) Write(p []byte) (int, error) {
	t.stderrMu.Lock()
	defer t.stderrMu.Unlock()
	t.stderr.Write(p)
	if over := t.stderr.Len() - 4096; over > 0 {
		t.stderr.Next(over)
	}
	return len(p), nil
}

func (s *server) stderrTail() string {
	s.stderrMu.Lock()
	defer s.stderrMu.Unlock()
	if s.stderr.Len() == 0 {
		return ""
	}
	return ": " + s.stderr.String()
}
