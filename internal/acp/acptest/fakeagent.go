// Package acptest is a fake ACP agent for tests. A test binary doubles as the
// agent: its TestMain calls RunIfAgent, and a test starts the binary itself
// (os.Executable) with Env(mode). The agent is written against raw JSON (not
// package acp's types) so that wire-shape mistakes in the client are not
// mirrored on the agent side.
package acptest

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Environment of the re-executed test binary.
const (
	agentEnv   = "FLINT_ACP_FAKE_AGENT"
	modeEnv    = "FLINT_ACP_FAKE_MODE"
	clientEnv  = "FLINT_ACP_FAKE_CLIENT"  // expected clientInfo.name; empty accepts any
	pidFileEnv = "FLINT_ACP_FAKE_PIDFILE" // the agent and a child it spawns write their PIDs here
	childEnv   = "FLINT_ACP_FAKE_CHILD"   // set in the relaunched child
)

// Modes. Without one of these the first prompt text names the scenario
// (stream, full, cancel, cancelreq, unknown, big, crash, hang).
const (
	// ModeHeadless answers every prompt the way a one-shot run sees it:
	// chatter, a tool call, a permission request and a file write, then the
	// final text "perm:<outcome>:<option> write:<error code>".
	ModeHeadless = "headless"
	// ModeEcho answers every prompt with "echo:" and the prompt's text.
	ModeEcho = "echo"
	// ModeRelaunch behaves like gemini-cli stuck before its handshake: the
	// agent relaunches itself as a child that shares its stdio, both ignore
	// SIGTERM, and the child never answers `initialize` and keeps running
	// after stdin ends. With PIDFileEnv both PIDs are written.
	ModeRelaunch = "relaunch"
	// ModeRelaunchEcho is ModeRelaunch with a child that serves ModeEcho
	// (and still outlives stdin).
	ModeRelaunchEcho = "relaunch-echo"
)

// orphanLimit ends a relaunched child that nothing killed, so a failing
// test doesn't leave it running.
const orphanLimit = time.Minute

// Env returns the environment that turns the test binary into the agent.
func Env(mode string) []string {
	return []string{agentEnv + "=1", modeEnv + "=" + mode}
}

// ClientNameEnv makes the agent insist on this clientInfo.name.
func ClientNameEnv(name string) string { return clientEnv + "=" + name }

// PIDFileEnv makes the agent spawn a child in its process group and write
// both PIDs to path (one per line), so tests can check the group is killed.
func PIDFileEnv(path string) string { return pidFileEnv + "=" + path }

// RunIfAgent serves the protocol on stdio and exits when the process was
// started as the fake agent; otherwise it returns at once. Call it first in
// TestMain.
func RunIfAgent() {
	if os.Getenv(agentEnv) != "1" {
		return
	}
	if mode := os.Getenv(modeEnv); mode == ModeRelaunch || mode == ModeRelaunchEcho {
		relaunch(mode)
	}
	if path := os.Getenv(pidFileEnv); path != "" {
		writePIDs(path)
	}
	Serve(os.Getenv(modeEnv), os.Stdin, os.Stdout)
	os.Exit(0)
}

func writePIDs(path string) {
	child := exec.Command("sleep", "60")
	if err := child.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "fake agent: start child:", err)
		os.Exit(8)
	}
	data := fmt.Sprintf("%d\n%d\n", os.Getpid(), child.Process.Pid)
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "fake agent: write pids:", err)
		os.Exit(8)
	}
}

// relaunch runs the agent the way gemini-cli does: a parent that waits for
// a child sharing its stdin, stdout and stderr. Neither returns.
func relaunch(mode string) {
	signal.Ignore(syscall.SIGTERM)
	if os.Getenv(childEnv) == "1" {
		go func() {
			time.Sleep(orphanLimit)
			os.Exit(7)
		}()
		fmt.Fprintln(os.Stderr, "fake agent: relaunched, waiting for sign-in")
		if mode == ModeRelaunchEcho {
			Serve(ModeEcho, os.Stdin, os.Stdout)
		} else {
			_, _ = io.Copy(io.Discard, os.Stdin)
		}
		select {} // stdin ended; keep running like a hung agent
	}
	child := exec.Command(os.Args[0], os.Args[1:]...)
	child.Stdin, child.Stdout, child.Stderr = os.Stdin, os.Stdout, os.Stderr
	child.Env = append(os.Environ(), childEnv+"=1")
	if err := child.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "fake agent: relaunch:", err)
		os.Exit(8)
	}
	if path := os.Getenv(pidFileEnv); path != "" {
		data := fmt.Sprintf("%d\n%d\n", os.Getpid(), child.Process.Pid)
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			fmt.Fprintln(os.Stderr, "fake agent: write pids:", err)
			os.Exit(8)
		}
	}
	_ = child.Wait()
	os.Exit(0)
}

type fakeMsg struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (m fakeMsg) code() int {
	if m.Error == nil {
		return 0
	}
	return m.Error.Code
}

type fakeAgent struct {
	mode string
	out  io.Writer
	wmu  sync.Mutex

	mu          sync.Mutex
	nextID      int
	sessions    int
	pending     map[string]chan fakeMsg
	cancels     map[string]chan struct{}
	parseErrors chan int
}

// Serve speaks the agent side of ACP on in/out until in ends.
func Serve(mode string, in io.Reader, out io.Writer) {
	a := &fakeAgent{
		mode:        mode,
		out:         out,
		pending:     map[string]chan fakeMsg{},
		cancels:     map[string]chan struct{}{},
		parseErrors: make(chan int, 4),
	}
	r := bufio.NewReader(in)
	for {
		line, err := r.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			a.handle(line)
		}
		if err != nil {
			return
		}
	}
}

func (a *fakeAgent) handle(line []byte) {
	var m fakeMsg
	if err := json.Unmarshal(line, &m); err != nil {
		fmt.Fprintln(os.Stderr, "fake agent: bad json from client:", err)
		os.Exit(9)
	}
	switch {
	case m.Method == "" && string(m.ID) == "null":
		a.parseErrors <- m.code()
	case m.Method == "":
		a.mu.Lock()
		ch := a.pending[string(m.ID)]
		delete(a.pending, string(m.ID))
		a.mu.Unlock()
		if ch != nil {
			ch <- m
		}
	case len(m.ID) == 0:
		a.notification(m)
	default:
		go a.request(m)
	}
}

func (a *fakeAgent) notification(m fakeMsg) {
	if m.Method != "session/cancel" {
		return
	}
	var p struct {
		SessionID string `json:"sessionId"`
	}
	_ = json.Unmarshal(m.Params, &p)
	ch := a.cancelChan(p.SessionID)
	a.mu.Lock()
	defer a.mu.Unlock()
	select {
	case <-ch:
	default:
		close(ch)
	}
}

func (a *fakeAgent) cancelChan(sessionID string) chan struct{} {
	a.mu.Lock()
	defer a.mu.Unlock()
	ch := a.cancels[sessionID]
	if ch == nil {
		ch = make(chan struct{})
		a.cancels[sessionID] = ch
	}
	return ch
}

func (a *fakeAgent) send(v any) {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	a.wmu.Lock()
	defer a.wmu.Unlock()
	_, _ = a.out.Write(append(data, '\n'))
}

func (a *fakeAgent) writeRaw(s string) {
	a.wmu.Lock()
	defer a.wmu.Unlock()
	_, _ = io.WriteString(a.out, s)
}

func (a *fakeAgent) reply(id json.RawMessage, result any) {
	a.send(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func (a *fakeAgent) fail(id json.RawMessage, code int, msg string) {
	a.send(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": msg}})
}

// startCall sends a request and returns its id and response channel.
func (a *fakeAgent) startCall(method string, params any) (int, chan fakeMsg) {
	a.mu.Lock()
	a.nextID++
	id := a.nextID
	ch := make(chan fakeMsg, 1)
	a.pending[strconv.Itoa(id)] = ch
	a.mu.Unlock()
	a.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	return id, ch
}

func (a *fakeAgent) call(method string, params any) fakeMsg {
	_, ch := a.startCall(method, params)
	return <-ch
}

func (a *fakeAgent) update(sessionID string, update map[string]any) {
	a.send(map[string]any{"jsonrpc": "2.0", "method": "session/update",
		"params": map[string]any{"sessionId": sessionID, "update": update}})
}

func (a *fakeAgent) message(sessionID, text string) {
	a.update(sessionID, map[string]any{"sessionUpdate": "agent_message_chunk",
		"content": map[string]any{"type": "text", "text": text}})
}

func (a *fakeAgent) request(m fakeMsg) {
	switch m.Method {
	case "initialize":
		a.initialize(m)
	case "authenticate":
		var p struct {
			MethodID string `json:"methodId"`
		}
		_ = json.Unmarshal(m.Params, &p)
		if p.MethodID != "agent-login" {
			a.fail(m.ID, -32602, "unknown auth method")
			return
		}
		a.reply(m.ID, map[string]any{})
	case "session/new":
		a.newSession(m)
	case "session/set_mode":
		var p struct {
			SessionID string `json:"sessionId"`
			ModeID    string `json:"modeId"`
		}
		_ = json.Unmarshal(m.Params, &p)
		if p.ModeID != "default" && p.ModeID != "plan" {
			a.fail(m.ID, -32602, "unknown mode")
			return
		}
		a.update(p.SessionID, map[string]any{"sessionUpdate": "current_mode_update", "currentModeId": p.ModeID})
		a.reply(m.ID, map[string]any{})
	case "session/prompt":
		a.prompt(m)
	default:
		a.fail(m.ID, -32601, "Method not found")
	}
}

func (a *fakeAgent) initialize(m fakeMsg) {
	if a.mode == "noinit" {
		return
	}
	var p struct {
		ProtocolVersion    *int `json:"protocolVersion"`
		ClientCapabilities struct {
			FS struct {
				ReadTextFile  bool `json:"readTextFile"`
				WriteTextFile bool `json:"writeTextFile"`
			} `json:"fs"`
			Terminal *bool `json:"terminal"`
		} `json:"clientCapabilities"`
		ClientInfo struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"clientInfo"`
	}
	_ = json.Unmarshal(m.Params, &p)
	caps := p.ClientCapabilities
	if p.ProtocolVersion == nil || *p.ProtocolVersion != 1 || !caps.FS.ReadTextFile || !caps.FS.WriteTextFile ||
		caps.Terminal == nil || *caps.Terminal || !clientNameOK(p.ClientInfo.Name) || p.ClientInfo.Version == "" {
		a.fail(m.ID, -32602, "unexpected initialize params: "+string(m.Params))
		return
	}
	version := 1
	if a.mode == "badversion" {
		version = 99
	}
	a.reply(m.ID, map[string]any{
		"protocolVersion": version,
		"agentCapabilities": map[string]any{
			"loadSession":        true,
			"promptCapabilities": map[string]any{"image": true, "audio": false, "embeddedContext": true},
			"mcpCapabilities":    map[string]any{"http": true, "sse": false},
		},
		"authMethods": []any{map[string]any{"id": "agent-login", "name": "Log in", "description": "Use your account"}},
		"agentInfo":   map[string]any{"name": "fake-agent", "title": "Fake Agent", "version": "1.2.3"},
	})
}

func (a *fakeAgent) newSession(m fakeMsg) {
	var p struct {
		Cwd        string            `json:"cwd"`
		MCPServers []json.RawMessage `json:"mcpServers"`
	}
	_ = json.Unmarshal(m.Params, &p)
	if !filepath.IsAbs(p.Cwd) || p.MCPServers == nil {
		a.fail(m.ID, -32602, "bad session/new params: "+string(m.Params))
		return
	}
	a.mu.Lock()
	a.sessions++
	id := fmt.Sprintf("sess_%d", a.sessions)
	a.mu.Unlock()
	a.reply(m.ID, map[string]any{
		"sessionId": id,
		"modes": map[string]any{
			"currentModeId": "default",
			"availableModes": []any{
				map[string]any{"id": "default", "name": "Default"},
				map[string]any{"id": "plan", "name": "Plan", "description": "Plan only"},
			},
		},
	})
}

type fakePrompt struct {
	SessionID string           `json:"sessionId"`
	Prompt    []map[string]any `json:"prompt"`
}

func (a *fakeAgent) prompt(m fakeMsg) {
	var p fakePrompt
	if err := json.Unmarshal(m.Params, &p); err != nil || len(p.Prompt) == 0 || p.Prompt[0]["type"] != "text" {
		a.fail(m.ID, -32602, "bad prompt")
		return
	}
	scenarios := map[string]func(json.RawMessage, fakePrompt){
		"stream":    a.scenarioStream,
		"full":      a.scenarioFull,
		"cancel":    a.scenarioCancel,
		"cancelreq": a.scenarioCancelRequest,
		"unknown":   a.scenarioUnknown,
		"big":       a.scenarioBig,
		"crash":     a.scenarioCrash,
		"hang":      a.scenarioHang,
	}
	switch a.mode {
	case ModeHeadless:
		a.scenarioHeadless(m.ID, p)
		return
	case ModeEcho:
		a.message(p.SessionID, "echo:"+fmt.Sprint(p.Prompt[0]["text"]))
		a.endTurn(m.ID, "end_turn")
		return
	}
	run := scenarios[fmt.Sprint(p.Prompt[0]["text"])]
	if run == nil {
		a.fail(m.ID, -32602, "unknown scenario")
		return
	}
	run(m.ID, p)
}

func (a *fakeAgent) endTurn(id json.RawMessage, reason string) {
	a.reply(id, map[string]any{"stopReason": reason})
}

func (a *fakeAgent) scenarioStream(id json.RawMessage, p fakePrompt) {
	a.update(p.SessionID, map[string]any{"sessionUpdate": "agent_thought_chunk",
		"content": map[string]any{"type": "text", "text": "thinking"}})
	a.message(p.SessionID, "Hello")
	a.message(p.SessionID, " world")
	a.update(p.SessionID, map[string]any{"sessionUpdate": "plan", "entries": []any{
		map[string]any{"content": "step 1", "priority": "high", "status": "pending"}}})
	a.endTurn(id, "end_turn")
}

var fakeToolCall = map[string]any{"toolCallId": "call_1", "title": "Edit file", "kind": "edit", "status": "pending"}

func (a *fakeAgent) scenarioFull(id json.RawMessage, p fakePrompt) {
	if len(p.Prompt) < 2 || p.Prompt[1]["type"] != "resource_link" || p.Prompt[1]["name"] == nil {
		a.fail(id, -32602, "full scenario needs a resource_link")
		return
	}
	path := strings.TrimPrefix(fmt.Sprint(p.Prompt[1]["uri"]), "file://")
	sid := p.SessionID
	a.update(sid, map[string]any{"sessionUpdate": "tool_call", "toolCallId": "call_1",
		"title": "Edit file", "kind": "edit", "status": "pending"})

	read := a.call("fs/read_text_file", map[string]any{"sessionId": sid, "path": path, "line": 2, "limit": 1})
	if read.Error != nil {
		a.message(sid, fmt.Sprintf("read-error:%d", read.code()))
	} else {
		var r struct {
			Content string `json:"content"`
		}
		_ = json.Unmarshal(read.Result, &r)
		a.message(sid, "read:"+r.Content)
	}

	perm := a.call("session/request_permission", map[string]any{"sessionId": sid, "toolCall": fakeToolCall,
		"options": []any{
			map[string]any{"optionId": "allow-once", "name": "Allow", "kind": "allow_once"},
			map[string]any{"optionId": "reject-once", "name": "Reject", "kind": "reject_once"},
		}})
	if perm.Error != nil {
		a.message(sid, fmt.Sprintf("perm-error:%d", perm.code()))
		a.endTurn(id, "end_turn")
		return
	}
	var out struct {
		Outcome struct {
			Outcome  string `json:"outcome"`
			OptionID string `json:"optionId"`
		} `json:"outcome"`
	}
	_ = json.Unmarshal(perm.Result, &out)
	if out.Outcome.Outcome == "selected" && out.Outcome.OptionID == "allow-once" {
		w := a.call("fs/write_text_file", map[string]any{"sessionId": sid, "path": path, "content": "patched\n"})
		if w.Error != nil || string(w.Result) != "{}" {
			a.message(sid, fmt.Sprintf("write-error:%d:%s", w.code(), w.Result))
		} else {
			a.update(sid, map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "call_1", "status": "completed"})
			a.message(sid, "wrote")
		}
	} else {
		a.update(sid, map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "call_1", "status": "failed"})
		a.message(sid, "rejected:"+out.Outcome.Outcome+":"+out.Outcome.OptionID)
	}
	a.endTurn(id, "end_turn")
}

func (a *fakeAgent) scenarioCancel(id json.RawMessage, p fakePrompt) {
	sid := p.SessionID
	cancelled := a.cancelChan(sid)
	a.update(sid, map[string]any{"sessionUpdate": "tool_call", "toolCallId": "call_1",
		"title": "Edit file", "kind": "edit", "status": "pending"})
	perm := a.call("session/request_permission", map[string]any{"sessionId": sid, "toolCall": fakeToolCall,
		"options": []any{map[string]any{"optionId": "allow-once", "name": "Allow", "kind": "allow_once"}}})
	var out struct {
		Outcome struct {
			Outcome string `json:"outcome"`
		} `json:"outcome"`
	}
	_ = json.Unmarshal(perm.Result, &out)
	select {
	case <-cancelled:
		a.message(sid, "perm:"+out.Outcome.Outcome)
		// A second permission request after cancel must be auto-cancelled.
		late := a.call("session/request_permission", map[string]any{"sessionId": sid, "toolCall": fakeToolCall,
			"options": []any{map[string]any{"optionId": "allow-once", "name": "Allow", "kind": "allow_once"}}})
		_ = json.Unmarshal(late.Result, &out)
		a.message(sid, " late:"+out.Outcome.Outcome)
		a.endTurn(id, "cancelled")
	case <-time.After(10 * time.Second):
		a.message(sid, "no-cancel")
		a.endTurn(id, "end_turn")
	}
}

func (a *fakeAgent) scenarioCancelRequest(id json.RawMessage, p fakePrompt) {
	reqID, ch := a.startCall("session/request_permission", map[string]any{"sessionId": p.SessionID,
		"toolCall": fakeToolCall,
		"options":  []any{map[string]any{"optionId": "allow-once", "name": "Allow", "kind": "allow_once"}}})
	a.send(map[string]any{"jsonrpc": "2.0", "method": "$/cancel_request", "params": map[string]any{"requestId": reqID}})
	res := <-ch
	a.message(p.SessionID, fmt.Sprintf("cancelreq:%d", res.code()))
	a.endTurn(id, "end_turn")
}

func (a *fakeAgent) scenarioUnknown(id json.RawMessage, p fakePrompt) {
	sid := p.SessionID
	a.writeRaw("this is not json\n")
	parse := 0
	select {
	case parse = <-a.parseErrors:
	case <-time.After(5 * time.Second):
	}
	codes := []int{
		a.call("terminal/create", map[string]any{"sessionId": sid, "command": "ls"}).code(),
		a.call("_vendor/custom", map[string]any{}).code(),
		a.call("fs/read_text_file", "bogus").code(),
		a.call("fs/read_text_file", map[string]any{"sessionId": sid}).code(),
	}
	a.message(sid, fmt.Sprintf("codes:%d,%d,%d,%d parse:%d", codes[0], codes[1], codes[2], codes[3], parse))
	a.endTurn(id, "end_turn")
}

func (a *fakeAgent) scenarioBig(id json.RawMessage, p fakePrompt) {
	for _, block := range p.Prompt {
		if block["type"] != "resource" {
			continue
		}
		res, _ := block["resource"].(map[string]any)
		a.message(p.SessionID, fmt.Sprint(res["text"]))
	}
	a.endTurn(id, "end_turn")
}

func (a *fakeAgent) scenarioCrash(_ json.RawMessage, p fakePrompt) {
	a.message(p.SessionID, "about to crash")
	fmt.Fprintln(os.Stderr, "fatal: kaboom")
	os.Exit(3)
}

func (a *fakeAgent) scenarioHang(_ json.RawMessage, p fakePrompt) {
	a.message(p.SessionID, "hanging")
	select {}
}

func (a *fakeAgent) scenarioHeadless(id json.RawMessage, p fakePrompt) {
	sid := p.SessionID
	a.message(sid, "Let me look at the changes.")
	a.update(sid, map[string]any{"sessionUpdate": "tool_call", "toolCallId": "call_1",
		"title": "Edit notes", "kind": "edit", "status": "pending"})
	perm := a.call("session/request_permission", map[string]any{"sessionId": sid, "toolCall": fakeToolCall,
		"options": []any{
			map[string]any{"optionId": "allow-once", "name": "Allow", "kind": "allow_once"},
			map[string]any{"optionId": "reject-once", "name": "Reject", "kind": "reject_once"},
		}})
	var out struct {
		Outcome struct {
			Outcome  string `json:"outcome"`
			OptionID string `json:"optionId"`
		} `json:"outcome"`
	}
	_ = json.Unmarshal(perm.Result, &out)
	cwd, _ := os.Getwd()
	w := a.call("fs/write_text_file", map[string]any{"sessionId": sid, "path": filepath.Join(cwd, "notes.txt"), "content": "x"})
	a.message(sid, "perm:"+out.Outcome.Outcome+":"+out.Outcome.OptionID)
	a.message(sid, fmt.Sprintf(" write:%d", w.code()))
	a.endTurn(id, "end_turn")
}

func clientNameOK(name string) bool {
	if want := os.Getenv(clientEnv); want != "" {
		return name == want
	}
	return name != ""
}
