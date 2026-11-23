//go:build !darwin && !linux

package proctree

import "errors"

// Parent is not available here: callers treat the process as unknown.
func Parent(int) (Proc, error) {
	return Proc{}, errors.New("process parents are not available on this system")
}
