//go:build !unix

package agent

import "os/exec"

// killTreeOnCancel keeps the default: only the CLI itself is killed.
func killTreeOnCancel(*exec.Cmd) {}
