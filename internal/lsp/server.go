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

	stderrMu sync.Mutex
	stderr   bytes.Buffer // last bytes of stderr, for error messages
}

// startServer spawns argv in root and performs the initialize handshake.
func startServer(ctx context.Context, language, root string, argv []string, onDiag func(string, []Diagnostic)) (*server, error) {
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
	s := &server{language: language, root: root, cmd: cmd, onDiag: onDiag, initOptions: initOptions}
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
			},
			"workspace": map[string]any{
				"workspaceFolders": true,
				"configuration":    true,
			},
			"window": map[string]any{"workDoneProgress": false},
		},
	}
	if err := s.conn.Call(ctx, "initialize", params, nil); err != nil {
		return err
	}
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
		if json.Unmarshal(params, &p) == nil && s.onDiag != nil {
			if path := URIToPath(p.URI); path != "" {
				if p.Diagnostics == nil {
					p.Diagnostics = []Diagnostic{}
				}
				s.onDiag(path, p.Diagnostics)
			}
		}
		return nil, nil
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
