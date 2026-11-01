//go:build !darwin && !linux

package desktop

import "errors"

// Reveal is not implemented here yet (Windows: `explorer /select,<path>`).
func Reveal(string) error {
	return errors.New("showing files in the file manager is not supported on this system yet")
}

// PrefersDark keeps Flint's default, the dark theme.
func PrefersDark() bool { return true }
