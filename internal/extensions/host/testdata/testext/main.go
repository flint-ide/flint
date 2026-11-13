// testext is an extension for the host's tests: each command exercises one
// part of the protocol, including the ways an extension misbehaves.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
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
		var p struct {
			View, Key, Parent string
			Data              json.RawMessage
		}
		_ = json.Unmarshal(m.Params, &p)
		if v := richView(p.View, p.Key, p.Data); v != nil {
			reply(m.ID, v)
			return
		}
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
	case "view/action":
		notify("log", map[string]string{"level": "info", "message": "view/action " + string(m.Params)})
		reply(m.ID, nil)
	case "form/submit":
		var p struct{ Values map[string]any }
		_ = json.Unmarshal(m.Params, &p)
		notify("log", map[string]string{"level": "info", "message": "form/submit " + string(m.Params)})
		if p.Values["name"] == "taken" {
			reply(m.ID, map[string]any{"errors": map[string]string{"name": "is taken\x1b[31m", "bogus": "not a field"}, "error": "Nothing was saved."})
			return
		}
		reply(m.ID, map[string]any{"message": "Saved."})
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
		if a.Key != "" {
			notify("view/refresh", map[string]any{"view": "test.ext.tab", "key": a.Key})
			notify("view/refresh", map[string]any{"view": "test.ext.nope"})
		}
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
	case "fs":
		// The file system as the process itself sees it (the sandbox), not
		// through Flint: Value is "read" or "write".
		var err error
		text := ""
		if a.Value == "write" {
			err = os.WriteFile(a.Path, []byte("written by testext"), 0o600)
		} else {
			var data []byte
			data, err = os.ReadFile(a.Path)
			text = string(data)
		}
		if err != nil {
			text = "error: " + err.Error()
		}
		notify("log", map[string]string{"level": "result", "message": a.Value + " " + a.Path + " -> " + text})
	case "dial":
		text := "connected"
		if c, err := net.DialTimeout("tcp", a.URL, 2*time.Second); err != nil {
			text = "error: " + err.Error()
		} else {
			c.Close()
		}
		notify("log", map[string]string{"level": "result", "message": "dial " + a.URL + " -> " + text})
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

// richView answers view/resolve for the table, form and chart views (nil for
// the tree), each with things Flint must clean.
func richView(view, key string, data json.RawMessage) any {
	switch view {
	case "test.ext.table", "test.ext.tab":
		return map[string]any{"kind": "table",
			"columns": []any{
				map[string]any{"id": "name", "title": "Name\x1b[31m", "type": "text"},
				map[string]any{"id": "size", "type": "number"},
				map[string]any{"id": "when", "title": "When", "type": "date", "align": "center"},
				map[string]any{"id": "state", "title": "State", "type": "badge", "badges": map[string]string{"up": "ok", "down": "error", "odd": "purple"}},
				map[string]any{"id": "name", "title": "again"},
				map[string]any{"id": "html", "title": "HTML", "type": "html", "align": "justify"},
			},
			"rows": []any{
				map[string]any{"id": "r1", "cells": map[string]any{"name": "<b>one</b>", "size": 3, "when": "2026-11-01T09:30:00Z", "state": "up", "html": true, "extra": "dropped"},
					"command": map[string]any{"id": "test.ext.status", "args": map[string]int{"n": 1}}, "actions": []any{map[string]any{"id": "own", "label": "Own"}}},
				map[string]any{"id": "r2", "cells": map[string]any{"name": 7, "size": "big", "when": "yesterday", "state": map[string]int{"a": 1}},
					"command": map[string]any{"id": "git.push"}},
				map[string]any{"id": "r1", "cells": map[string]any{"name": "duplicate"}},
				map[string]any{"cells": map[string]any{"name": "no id"}},
				map[string]any{"id": "key:" + key, "cells": map[string]any{"name": string(data)}},
			},
			"sort":    map[string]any{"column": "size", "descending": true},
			"actions": []any{map[string]any{"id": "open", "label": "Open"}},
		}
	case "test.ext.form":
		return map[string]any{"kind": "form", "id": "account", "description": "Your account.", "submit": "Save",
			"fields": []any{
				map[string]any{"id": "name", "type": "string", "title": "Name", "required": true, "value": "Ada", "maxLength": 10, "futureKeyword": 1},
				map[string]any{"id": "token", "type": "string", "secret": true, "value": "leaked-value", "default": "leaked-default"},
				map[string]any{"id": "count", "type": "integer", "minimum": 1, "maximum": 9, "default": 3, "value": 99},
				map[string]any{"id": "mode", "type": "string", "enum": []string{"fast", "safe"}, "value": "safe"},
				map[string]any{"id": "notes", "type": "string", "multiline": true},
			}}
	case "test.ext.chart":
		return map[string]any{"kind": "chart", "type": "line", "title": "Open issues",
			"x": map[string]any{"type": "date"}, "y": map[string]any{"label": "Issues", "min": 0},
			"series": []any{
				map[string]any{"id": "open", "label": "Open", "points": []any{[]any{"2026-10-01", 14}, []any{"2026-10-08", 11}, []any{"soon", 1}, []any{"2026-10-15", "many"}}},
				map[string]any{"id": "blocked", "points": []any{[]any{"2026-10-01T12:00:00Z", 2}}},
			}}
	case "test.ext.badform":
		return map[string]any{"kind": "form", "fields": []any{map[string]any{"id": "x", "type": "object"}}}
	}
	return nil
}
