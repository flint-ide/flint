//go:build darwin

package terminal

/*
#include <libproc.h>
#include <string.h>
#include <sys/proc_info.h>

// flintProcCwd copies the working directory of pid into buf.
static int flintProcCwd(int pid, char *buf, int len) {
	struct proc_vnodepathinfo vpi;
	if (proc_pidinfo(pid, PROC_PIDVNODEPATHINFO, 0, &vpi, sizeof(vpi)) != sizeof(vpi)) return -1;
	strlcpy(buf, vpi.pvi_cdir.vip_path, len);
	return 0;
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// processCwd returns the current directory of pid (macOS: proc_pidinfo).
func processCwd(pid int) (string, error) {
	buf := make([]byte, C.MAXPATHLEN)
	if C.flintProcCwd(C.int(pid), (*C.char)(unsafe.Pointer(&buf[0])), C.int(len(buf))) != 0 {
		return "", fmt.Errorf("cwd of pid %d unavailable", pid)
	}
	return C.GoString((*C.char)(unsafe.Pointer(&buf[0]))), nil
}
