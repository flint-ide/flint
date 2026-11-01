//go:build !unix

// Package proctree runs a command in its own process group so the whole tree
// it starts can be signalled at once. Without process groups only the process
// itself is signalled.
package proctree

import (
	"os"
	"os/exec"
)

// Isolate keeps the default.
func Isolate(*exec.Cmd) {}

// Kill kills p.
func Kill(p *os.Process) error {
	if p == nil {
		return nil
	}
	return p.Kill()
}

// Terminate kills p (no SIGTERM without process groups).
func Terminate(p *os.Process) error { return Kill(p) }
