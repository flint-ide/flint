package proctree

// Proc is what Parent tells about a process.
type Proc struct {
	PID, PPID int
	// UID is the process's real user id.
	UID uint32
	// SID is its session's id (the pid of the session's leader, a
	// terminal's shell for a command typed there); 0 when unknown.
	SID int
	// TTY: the process has a controlling terminal.
	TTY bool
}
