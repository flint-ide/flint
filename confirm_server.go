//go:build server

package main

// askNativeAnswered says yes: the server build has no native dialogs, and it
// is a development-only build whose bindings (terminals included) answer
// anyone on localhost without authentication (see single_instance_server.go),
// so a confirmation would protect nothing there.
func (a *App) askNativeAnswered(string, string, string) (bool, bool) { return true, true }

// chooseNative answers nothing (as if dismissed): the server build has no
// native dialogs.
func (a *App) chooseNative(string, string, []string) string { return "" }
