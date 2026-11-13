// testext is an extension for the host's tests: each command exercises one
// part of the protocol, including the ways an extension misbehaves.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
)

type msg struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   json.RawMessage `json:"error,omitempty"`
}

var (
	outMu   sync.Mutex
	nextID  atomic.Int64
	waitMu  sync.Mutex
	waiting = map[string]chan msg{}
)

func send(v any) {
	data, _ := json.Marshal(v)
	outMu.Lock()
	defer outMu.Unlock()
	os.Stdout.Write(append(data, '\n'))
}

func reply(id json.RawMessage, result any) {
	send(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func notify(method string, params any) {
	send(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

// call asks Flint and waits for the answer (result or error, as JSON).
func call(method string, params any) string {
	n := nextID.Add(1)
	ch := make(chan msg, 1)
	waitMu.Lock()
	waiting[fmt.Sprint(n)] = ch
	waitMu.Unlock()
	send(map[string]any{"jsonrpc": "2.0", "id": n, "method": method, "params": params})
	m := <-ch
	if m.Error != nil {
		return "error " + string(m.Error)
	}
	return string(m.Result)
}

func main() {
	fmt.Fprintln(os.Stderr, "testext starting, env:", strings.Join(os.Environ(), " "))
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 1<<20), 8<<20)
	for sc.Scan() {
		var m msg
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			continue
		}
		if m.Method == "" {
			waitMu.Lock()
			ch := waiting[string(m.ID)]
			delete(waiting, string(m.ID))
			waitMu.Unlock()
			if ch != nil {
				ch <- m
			}
			continue
		}
		if m.ID == nil {
			if m.Method == "settings/changed" || m.Method == "workspace/rootsChanged" || m.Method == "view/visible" || strings.HasPrefix(m.Method, "editor/") {
				notify("log", map[string]string{"level": "info", "message": m.Method + " " + string(m.Params)})
			}
			continue
		}
		go handle(m)
	}
}

func handle(m msg) {
	switch m.Method {
	case "initialize":
		notify("log", map[string]string{"level": "info", "message": "initialize " + string(m.Params)})
		if os.Getenv("TESTEXT_CONTRACT") != "" {
			reply(m.ID, map[string]any{"contract": 99})
			return
		}
		reply(m.ID, map[string]any{"contract": 1, "capabilities": map[string]any{}})
	case "shutdown":
		reply(m.ID, nil)
		os.Exit(0)
	case "view/resolve":
		var p struct{ Parent string }
		_ = json.Unmarshal(m.Params, &p)
		if p.Parent != "" {
			reply(m.ID, map[string]any{"kind": "tree", "items": []any{map[string]any{"id": p.Parent + "/child", "label": "child of " + p.Parent}}})
			return
		}
		reply(m.ID, map[string]any{"kind": "tree", "items": []any{
			map[string]any{"id": "a", "label": "Alpha\x1b[31m", "icon": "files", "hasChildren": true,
				"command": map[string]any{"id": "test.ext.status", "args": map[string]int{"n": 1}}},
			map[string]any{"id": "b", "label": "Beta", "icon": "<svg>", "command": map[string]any{"id": "git.push"}},
			map[string]any{"id": "a", "label": "duplicate"},
		}})
	case "agentTool/call":
		var p struct {
			Name      string
			Root      string
			Arguments map[string]any
		}
		_ = json.Unmarshal(m.Params, &p)
		reply(m.ID, map[string]any{"text": fmt.Sprintf("%s in %s with %v", p.Name, p.Root, p.Arguments["q"])})
	case "editor/action":
		var p struct {
			Set, Path, Action, Decoration string
			Version, Line, From, To       int
		}
		_ = json.Unmarshal(m.Params, &p)
		notify("log", map[string]string{"level": "info", "message": "editor/action " + string(m.Params)})
		switch p.Action {
		case "bad":
			reply(m.ID, map[string]any{"edits": []any{map[string]any{"line": 0, "from": 0, "to": 1, "text": "x"}}})
		case "many":
			edits := make([]any, 1001)
			for i := range edits {
				edits[i] = map[string]any{"line": i + 1, "from": 0, "to": 0, "text": "x"}
			}
			reply(m.ID, map[string]any{"edits": edits})
		case "none":
			reply(m.ID, map[string]any{})
		default:
			reply(m.ID, map[string]any{"edits": []any{
				map[string]any{"line": p.Line, "from": p.From, "to": p.To, "text": "fixed " + p.Decoration},
				map[string]any{"line": p.Line + 1, "from": 2, "text": "inserted\n"},
			}})
		}
	case "command/execute":
		var p struct {
			ID   string
			Args json.RawMessage
		}
		_ = json.Unmarshal(m.Params, &p)
		command(m, p.ID, p.Args)
	default:
		send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "error": map[string]any{"code": -32601, "message": "no " + m.Method}})
	}
}

func command(m msg, id string, args json.RawMessage) {
	var a struct {
		Path, Key, Value, URL string
		Set                   string
		Version               *int
		Count                 int
	}
	_ = json.Unmarshal(args, &a)
	switch strings.TrimPrefix(id, "test.ext.") {
	case "status":
		notify("status/update", map[string]any{"id": "test.ext.count", "text": strings.Repeat("x", 60), "command": "test.ext.status", "severity": "bogus"})
		notify("view/refresh", map[string]any{"view": "test.ext.items"})
		notify("window/showMessage", map[string]any{"type": "warning", "message": "hello " + string(args)})
	case "read":
		notify("log", map[string]string{"level": "result", "message": call("workspace/readFile", map[string]string{"path": a.Path})})
	case "open":
		notify("log", map[string]string{"level": "result", "message": call("window/openFile", map[string]any{"path": a.Path, "line": 3})})
	case "url":
		notify("log", map[string]string{"level": "result", "message": call("window/openUrl", map[string]any{"url": a.URL})})
	case "secret":
		call("secrets/set", map[string]string{"key": a.Key, "value": a.Value})
		notify("log", map[string]string{"level": "result", "message": call("secrets/get", map[string]string{"key": a.Key})})
	case "ask":
		notify("log", map[string]string{"level": "result", "message": call("agent/ask", map[string]any{"prompt": "explain", "attachments": []any{map[string]string{"path": a.Path}}})})
	case "unknown":
		notify("log", map[string]string{"level": "result", "message": call("exec", map[string]string{"cmd": "rm"})})
	case "decorate":
		params := map[string]any{"set": a.Set, "path": a.Path, "decorations": decorations(a.Count)}
		if a.Version != nil {
			params["version"] = *a.Version
		}
		notify("editor/setDecorations", params)
	case "clear":
		params := map[string]any{"set": a.Set}
		if a.Path != "" {
			params["path"] = a.Path
		}
		notify("editor/clearDecorations", params)
	case "crash":
		os.Exit(3)
	case "garbage":
		outMu.Lock()
		os.Stdout.WriteString("debug print on stdout\n")
		outMu.Unlock()
		select {}
	case "hang":
		select {}
	case "child":
		// A grandchild in the same process group, killed with it.
		cmd := exec.Command("sleep", "600")
		_ = cmd.Start()
		notify("log", map[string]string{"level": "result", "message": fmt.Sprint(cmd.Process.Pid)})
	}
	reply(m.ID, nil)
}

// decorations are n gutter markers, or (n = 0) one of each kind with the
// things Flint must clean: control characters, foreign commands, unknown
// kinds, icons and severities, missing lines, messages and texts.
func decorations(n int) []any {
	if n > 0 {
		out := make([]any, n)
		for i := range out {
			out[i] = map[string]any{"kind": "gutter", "line": i + 1}
		}
		return out
	}
	return []any{
		map[string]any{"kind": "diagnostic", "id": "d1", "line": 2, "from": 1, "to": 4, "severity": "warning", "message": "shadowed\x1b[31m\nsecond line",
			"actions": []any{
				map[string]any{"id": "fix", "label": "Fix it"},
				map[string]any{"id": "run", "label": "Run", "command": map[string]any{"id": "test.ext.status"}},
				map[string]any{"id": "evil", "label": "Evil", "command": map[string]any{"id": "git.push"}},
				map[string]any{"id": "", "label": "No id"},
			}},
		map[string]any{"kind": "gutter", "line": 3, "icon": "check", "severity": "ok", "hover": "Covered"},
		map[string]any{"kind": "gutter", "line": 4, "icon": "<svg>", "severity": "bogus"},
		map[string]any{"kind": "inline", "line": 5, "text": strings.Repeat("y", 300), "command": map[string]any{"id": "git.push"}},
		map[string]any{"kind": "highlight", "line": 6, "endLine": 2, "from": -3, "to": 2},
		map[string]any{"kind": "bogus", "line": 1},
		map[string]any{"kind": "diagnostic", "line": 0, "message": "no line"},
		map[string]any{"kind": "diagnostic", "line": 7},
		map[string]any{"kind": "inline", "line": 8},
	}
}
