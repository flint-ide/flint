//go:build !darwin

package terminal

import (
	"os"
	"strconv"
)

// processCwd returns the current directory of pid (Linux: /proc/<pid>/cwd).
func processCwd(pid int) (string, error) {
	return os.Readlink("/proc/" + strconv.Itoa(pid) + "/cwd")
}
