package remote

import (
	"encoding/json"
	"slices"
	"strings"

	as "flint/internal/agentsession"
	"flint/internal/redact"
)

// A tool call that reads or writes a credentials file (redact.SecretFile:
// .env, keys, kubeconfig) has its input and output withheld from the phone:
// pattern redaction alone misses values such as DB_PASS=hunter2. Agents send
// a call's paths with its start and its output in later updates, so each
// stream remembers which calls touched such a file (secretCalls).

// secretFileText replaces what such a call read or wrote.
const secretFileText = "[secret file: not shown remotely]"

// maxInputScan bounds how much of a tool input is parsed for paths.
const maxInputScan = 256 << 10

// secretCall reports whether a tool call touches a credentials file: one of
// its paths or diffs, a path in its JSON input (file_path, path,
// notebook_path, … at any depth) or a word of its command line is one.
func secretCall(tc as.ToolCall) bool {
	for _, p := range tc.Paths {
		if redact.SecretFile(p) {
			return true
		}
	}
	for _, d := range tc.Diffs {
		if redact.SecretFile(d.Path) {
			return true
		}
	}
	return slices.ContainsFunc(inputPaths(tc.Input), redact.SecretFile)
}

// inputPaths lists what in a tool input could name a file: string values of
// keys that mention a path or file, and the words of commands (JSON
// "command"/"cmd" values, or the input itself when it isn't JSON).
func inputPaths(input string) []string {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil
	}
	if len(input) > maxInputScan {
		input = input[:maxInputScan]
	}
	var v any
	if (input[0] == '{' || input[0] == '[') && json.Unmarshal([]byte(input), &v) == nil {
		var out []string
		collectPaths(v, false, &out)
		return out
	}
	return commandWords(input)
}

func collectPaths(v any, pathKey bool, out *[]string) {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			lk := strings.ToLower(k)
			if lk == "command" || lk == "cmd" {
				if s, ok := val.(string); ok {
					*out = append(*out, commandWords(s)...)
				} else {
					collectPaths(val, true, out) // ["cat", ".env"]
				}
				continue
			}
			collectPaths(val, strings.Contains(lk, "path") || strings.Contains(lk, "file"), out)
		}
	case []any:
		for _, item := range x {
			collectPaths(item, pathKey, out)
		}
	case string:
		if pathKey {
			*out = append(*out, x)
		}
	}
}

// commandWords splits a command line into words, also at '=' (as in
// --env-file=.env) and shell punctuation, without quotes.
func commandWords(cmd string) []string {
	return strings.FieldsFunc(cmd, func(r rune) bool { return strings.ContainsRune(" \t\r\n=\"'`;|&<>()", r) })
}

// maxTrackedCalls bounds the tool calls one stream remembers; past it the
// memory is cleared, and later updates of forgotten calls are withheld.
const maxTrackedCalls = 20000

// secretCalls is what one stream knows about the tool calls it has seen.
type secretCalls struct {
	seen   map[string]bool // session + call id → secret
	maxCap int
}

func newSecretCalls() *secretCalls {
	return &secretCalls{seen: map[string]bool{}, maxCap: maxTrackedCalls}
}

// observe records the tool calls of e and reports whether e's input and
// output must be withheld: the call touches a credentials file now or did
// when it started, or it is an update with output of a call the stream
// never saw start (so it can't know what was read).
func (sc *secretCalls) observe(e as.Event) bool {
	var calls []as.ToolCall
	if e.ToolCall != nil {
		calls = append(calls, *e.ToolCall)
	}
	if e.Permission != nil {
		calls = append(calls, e.Permission.ToolCall)
	}
	secret := false
	for _, tc := range calls {
		key := e.SessionID + "\x00" + tc.ID
		wasSecret, seen := sc.seen[key]
		isSecret := wasSecret || secretCall(tc)
		if !seen && e.Kind == as.KindToolCallUpdate && (tc.Output != "" || tc.Input != "") {
			isSecret = true // started before what this stream has seen
		}
		if len(sc.seen) >= sc.maxCap {
			clear(sc.seen)
		}
		sc.seen[key] = isSecret
		secret = secret || isSecret
	}
	return secret
}
