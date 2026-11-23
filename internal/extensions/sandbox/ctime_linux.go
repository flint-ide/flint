package sandbox

import (
	"io/fs"
	"syscall"
)

// changeTime is the inode's change time (nanoseconds): it moves with every
// write, and no process can set it back.
func changeTime(info fs.FileInfo) int64 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return st.Ctim.Nano()
	}
	return info.ModTime().UnixNano()
}
