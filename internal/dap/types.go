package dap

import "encoding/json"

// The subset of DAP types Flint uses. Lines and columns are 1-based (the client
// says so in initialize).

// LaunchConfig describes what to debug. For Delve, Mode is "debug" (build and
// run Program, a package or file), "test" (go test) or "exec" (a prebuilt binary).
type LaunchConfig struct {
	Mode        string            `json:"mode"`
	Program     string            `json:"program"`
	Args        []string          `json:"args,omitempty"`
	Cwd         string            `json:"cwd,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	BuildFlags  string            `json:"buildFlags,omitempty"`
	StopOnEntry bool              `json:"stopOnEntry,omitempty"`
	OutputMode  string            `json:"outputMode,omitempty"` // Delve: "remote" sends output events
	Output      string            `json:"output,omitempty"`     // Delve: where the built binary goes
}

// SourceBreakpoint is a requested breakpoint; Condition and HitCondition are
// expressions in the debuggee's language, LogMessage turns it into a logpoint.
type SourceBreakpoint struct {
	Line         int    `json:"line"`
	Condition    string `json:"condition,omitempty"`
	HitCondition string `json:"hitCondition,omitempty"`
	LogMessage   string `json:"logMessage,omitempty"`
}

// Breakpoint is the adapter's answer for one requested breakpoint: it may move
// it to the nearest line with code, or refuse it.
type Breakpoint struct {
	ID       int    `json:"id,omitempty"`
	Verified bool   `json:"verified"`
	Line     int    `json:"line,omitempty"`
	Message  string `json:"message,omitempty"`
}

type Thread struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type Source struct {
	Name string `json:"name,omitempty"`
	Path string `json:"path,omitempty"`
}

type StackFrame struct {
	ID     int     `json:"id"`
	Name   string  `json:"name"`
	Source *Source `json:"source,omitempty"`
	Line   int     `json:"line"`
	Column int     `json:"column"`
}

type Scope struct {
	Name               string `json:"name"`
	VariablesReference int    `json:"variablesReference"`
	Expensive          bool   `json:"expensive,omitempty"`
}

// Variable is one value; a non-zero VariablesReference means it has children.
type Variable struct {
	Name               string `json:"name"`
	Value              string `json:"value"`
	Type               string `json:"type,omitempty"`
	EvaluateName       string `json:"evaluateName,omitempty"`
	VariablesReference int    `json:"variablesReference"`
}

type EvaluateResult struct {
	Result             string `json:"result"`
	Type               string `json:"type,omitempty"`
	VariablesReference int    `json:"variablesReference"`
}

// Event is an adapter event passed through to the UI, e.g. "stopped",
// "continued", "output", "terminated", "exited", "breakpoint".
type Event struct {
	Name string          `json:"name"`
	Body json.RawMessage `json:"body,omitempty"`
}

// StoppedEvent is the body of a "stopped" event.
type StoppedEvent struct {
	Reason            string `json:"reason"`
	ThreadID          int    `json:"threadId"`
	AllThreadsStopped bool   `json:"allThreadsStopped"`
	Text              string `json:"text,omitempty"`
}

// OutputEvent is the body of an "output" event.
type OutputEvent struct {
	Category string `json:"category"`
	Output   string `json:"output"`
}
