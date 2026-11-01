//go:build !darwin && !linux

package workspace

import "errors"

// MoveToTrash is implemented on macOS and Linux; elsewhere (Windows: TODO, the Recycle Bin via SHFileOperation) nothing is deleted.
func MoveToTrash(string) error {
	return errors.New("moving to Trash is not supported on this system yet")
}
