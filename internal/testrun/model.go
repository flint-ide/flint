// Package testrun plans test runs (go test, node --test and package.json test
// scripts), parses their machine-readable output (`go test -json`, TAP) into a
// tree of results and finds tests in source files for the editor gutter.
package testrun

import (
	"os"
	"path/filepath"
	"strings"
)

// Status of a test or package.
type Status string

const (
	Running   Status = "running"
	Passed    Status = "passed"
	Failed    Status = "failed"
	Skipped   Status = "skipped"
	Cancelled Status = "cancelled"
)

// Kinds of result nodes.
const (
	KindPackage = "package"
	KindSuite   = "suite"
	KindTest    = "test"
)

// maxOutput caps the output kept per test; the console has all of it.
const maxOutput = 64 << 10

// Node is one package, suite or test of a run. Updates for the same ID replace
// the previous state.
type Node struct {
	ID     string `json:"id"`
	Parent string `json:"parent,omitempty"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	// Test is the full test name a rerun selects: "TestA/sub" (Go), the names
	// from the outermost suite joined by spaces (node:test).
	Test      string  `json:"test,omitempty"`
	Status    Status  `json:"status"`
	ElapsedMs float64 `json:"elapsedMs,omitempty"`
	// Output of a failed or skipped test (capped).
	Output string `json:"output,omitempty"`
	// Dir is the package directory; relative paths in Output resolve against it.
	Dir      string    `json:"dir,omitempty"`
	Location *Location `json:"location,omitempty"`
}

// Location points at a source line (1-based).
type Location struct {
	Path   string `json:"path"`
	Line   int    `json:"line"`
	Column int    `json:"column,omitempty"`
}

// Parser turns a test process's stdout into console text and result updates.
type Parser interface {
	// Line consumes one stdout line (newline included) and returns what the
	// console shows (nil: the line as is) plus changed nodes.
	Line(line []byte) (shown []byte, updates []Node)
	// Finish settles nodes that never reported a result once the process ended.
	Finish(exitCode int, stopped bool) []Node
}

// unfinished is the status of a node still running when the process ended.
func unfinished(exitCode int, stopped bool) Status {
	if stopped || exitCode == 0 {
		return Cancelled
	}
	return Failed
}

// capOutput keeps the head of long output.
func capOutput(s string) string {
	if len(s) <= maxOutput {
		return s
	}
	return s[:maxOutput] + "\n… (truncated)"
}

// existingPath returns the first candidate that is an existing file.
func existingPath(candidates ...string) string {
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if info, err := os.Stat(c); err == nil && !info.IsDir() {
			return c
		}
	}
	return ""
}

// findUp returns the closest directory at or above dir that contains name.
func findUp(dir, name string) string {
	for d := filepath.Clean(dir); ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, name)); err == nil {
			return d
		}
		if parent := filepath.Dir(d); parent == d {
			return ""
		}
	}
}

func within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, "../")
}
