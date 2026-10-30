//go:build !darwin

package workspace

import "errors"

// MoveToTrash is only implemented on macOS; elsewhere nothing is deleted.
func MoveToTrash(string) error { return errors.New("moving to Trash is only supported on macOS") }
