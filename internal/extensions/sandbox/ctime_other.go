//go:build !darwin && !linux

package sandbox

import "io/fs"

// changeTime is the modification time here: this system's change time is
// not read, so a large file changed and set back to its old time and size
// goes unnoticed.
func changeTime(info fs.FileInfo) int64 { return info.ModTime().UnixNano() }
