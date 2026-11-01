//go:build server

package main

import "os"

// FLINT_IMPORT_HOME points Import from VS Code at another home folder in the
// development-only server build, so the import can be tried and screenshotted
// against fixtures (internal/vscodeimport/testdata/home) instead of your own
// editor's settings.
func init() {
	if home := os.Getenv("FLINT_IMPORT_HOME"); home != "" {
		importHome = func() (string, error) { return home, nil }
	}
}
