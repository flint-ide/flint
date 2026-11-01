//go:build darwin

package desktop

// Reveal selects path in a Finder window.
func Reveal(path string) error { return run("open", "-R", path) }

// PrefersDark is not used on macOS: the window code reads NSApp's appearance.
func PrefersDark() bool { return true }
