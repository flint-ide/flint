package terminal

// What the command-line socket (internal/cli/clisock) asks to tell a command
// typed by a person in a terminal tab from one run by an agent.

// ShellPIDs are the process ids of the shells of the running terminal tabs,
// by pid, with the tab's id. Stream sessions (Docker, extensions) have no
// shell and are not listed.
func (m *Manager) ShellPIDs() map[int]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[int]string, len(m.sessions))
	for id, s := range m.sessions {
		if s.cmd == nil || s.cmd.Process == nil {
			continue
		}
		if s.gone != nil {
			select {
			case <-s.gone: // ended: its pid may belong to another process now
				continue
			default:
			}
		}
		out[s.cmd.Process.Pid] = id
	}
	return out
}

// ProcessArgs is the command line of a process of this user (nil and an
// error when it cannot be read, e.g. the process ended).
func ProcessArgs(pid int) ([]string, error) { return processArgs(pid) }

// ProgramName is the program a command line runs, with a script run by an
// interpreter named after the script ("node /opt/homebrew/bin/claude" is
// claude), as tab titles and session restore name it.
func ProgramName(args []string) string {
	name, _ := programName(args)
	return name
}
