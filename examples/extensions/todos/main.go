// Command todos is an example Flint extension, "TODO Marks": it marks the
// lines with a TODO in the files open in editors (a marker in the gutter and
// an underline with a hover) and offers a quick fix that removes the marker.
// It shows the editor decoration part of the extension contract: a
// decoration set in the manifest, the editor/opened, editor/changed and
// editor/closed notifications, editor/setDecorations and editor/action. It
// uses only the standard library.
//
// Build it next to its manifest and install the folder from Flint
// (Install Extension from Folder…):
//
//	go build -o todos .
package main

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"regexp"
	"strings"
	"sync"
	"unicode/utf16"
)

const setID = "example.todos.marks"

// marker is a TODO with the colon and space that may follow it.
var marker = regexp.MustCompile(`\bTODO\b:? ?`)

// document is an open file as Flint last described it.
type document struct {
	version int
	lines   []string
}

type ext struct {
	c *conn

	mu   sync.Mutex
	docs map[string]document
}

func main() {
	log.SetFlags(0)
	log.SetPrefix("todos: ") // log goes to stderr, Flint's log of the extension
	e := &ext{c: newConn(os.Stdout), docs: map[string]document{}}
	if err := e.c.serve(os.Stdin, e.handle, e.notified); err != nil {
		log.Fatal(err)
	}
}

// handle answers Flint's requests.
func (e *ext) handle(method string, params json.RawMessage) (any, error) {
	switch method {
	case "initialize":
		return map[string]any{"contract": 1, "capabilities": map[string]any{}}, nil
	case "shutdown":
		return nil, nil
	case "editor/action":
		var p struct {
			Path    string `json:"path"`
			Version int    `json:"version"`
			Action  string `json:"action"`
			Line    int    `json:"line"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		return e.fix(p.Path, p.Version, p.Action, p.Line)
	}
	return nil, &rpcError{Code: -32601, Message: "method not found: " + method}
}

// notified handles Flint's notifications, in order.
func (e *ext) notified(method string, params json.RawMessage) {
	var p struct {
		Path    string  `json:"path"`
		Version int     `json:"version"`
		Text    *string `json:"text"`
	}
	if json.Unmarshal(params, &p) != nil {
		return
	}
	switch method {
	case "editor/opened", "editor/changed":
		if p.Text != nil {
			e.update(p.Path, p.Version, *p.Text)
			return
		}
		// A document too large to come with its text: read what is saved. In
		// a goroutine, because the answer arrives on the loop that called us.
		go func() {
			var res struct {
				Text string `json:"text"`
			}
			if err := e.c.call("workspace/readFile", map[string]string{"path": p.Path}, &res); err != nil {
				log.Printf("%s: %v", p.Path, err)
				return
			}
			e.update(p.Path, p.Version, res.Text)
		}()
	case "editor/closed":
		e.mu.Lock()
		delete(e.docs, p.Path)
		e.mu.Unlock()
	}
}

// update remembers the document's text and publishes its marks.
func (e *ext) update(path string, version int, text string) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	e.mu.Lock()
	if old, ok := e.docs[path]; ok && old.version > version {
		e.mu.Unlock()
		return // a newer change overtook this one
	}
	e.docs[path] = document{version: version, lines: lines}
	e.mu.Unlock()

	decorations := []any{}
	for i, line := range lines {
		loc := marker.FindStringIndex(line)
		if loc == nil {
			continue
		}
		note := strings.TrimSpace(line[loc[1]:])
		if note == "" {
			note = "TODO"
		}
		decorations = append(decorations, map[string]any{
			"kind": "diagnostic", "line": i + 1, "from": column(line, loc[0]), "to": column(line, loc[0]+len("TODO")),
			"severity": "info", "message": "To do: " + note,
			"actions": []any{map[string]string{"id": "remove", "label": "Remove TODO marker"}},
		})
		if len(decorations) == 5000 { // Flint's limit per set and file
			break
		}
	}
	e.c.notify("editor/setDecorations", map[string]any{"set": setID, "path": path, "version": version, "decorations": decorations})
}

// fix answers a quick fix with the edit that removes the line's marker. The
// edit is for the version Flint asked about: if that isn't the text this
// extension last saw, there is nothing safe to answer.
func (e *ext) fix(path string, version int, action string, line int) (any, error) {
	e.mu.Lock()
	doc, ok := e.docs[path]
	e.mu.Unlock()
	switch {
	case action != "remove":
		return nil, errors.New("unknown action " + action)
	case !ok || doc.version != version || line < 1 || line > len(doc.lines):
		return map[string]any{"edits": []any{}}, nil
	}
	text := doc.lines[line-1]
	loc := marker.FindStringIndex(text)
	if loc == nil {
		return map[string]any{"edits": []any{}}, nil
	}
	return map[string]any{"edits": []any{
		map[string]any{"line": line, "from": column(text, loc[0]), "to": column(text, loc[1]), "text": ""},
	}}, nil
}

// column is the byte offset in line as Flint counts columns: UTF-16 code units.
func column(line string, offset int) int {
	return len(utf16.Encode([]rune(line[:offset])))
}
