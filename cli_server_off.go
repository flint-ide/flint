//go:build server

package main

// The server build has no command-line socket: its bindings already answer
// anyone on localhost without authentication (single_instance_server.go).
func (a *App) startCLI() {}

func (a *App) closeCLI() {}
