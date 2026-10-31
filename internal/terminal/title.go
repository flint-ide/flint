package terminal

import (
	"bytes"
	"encoding/binary"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Info is what a terminal's tab is named after.
type Info struct {
	// Command is the running command ("npm run dev"), "" while the shell waits
	// at the prompt.
	Command string `json:"command"`
	// Cwd is the shell's directory (it follows cd).
	Cwd string `json:"cwd"`
}

// titleCheckDelay: the foreground process is looked up at most this often, and
// only after output — a command starting or ending always prints something
// (the echoed Enter, the next prompt), so an idle terminal costs nothing.
const titleCheckDelay = 300 * time.Millisecond

const (
	maxLabelRunes = 40
	maxLabelArgs  = 3
)

// noteActivity schedules a check of what the terminal runs.
func (m *Manager) noteActivity(id string, s *session) {
	s.titleMu.Lock()
	defer s.titleMu.Unlock()
	if s.titleTimer != nil || s.done {
		return
	}
	s.titleTimer = time.AfterFunc(titleCheckDelay, func() { m.checkTitle(id, s) })
}

func (m *Manager) checkTitle(id string, s *session) {
	s.titleMu.Lock()
	s.titleTimer = nil
	if s.done {
		s.titleMu.Unlock()
		return
	}
	info, ok := s.inspect()
	changed := ok && info != s.info
	if changed {
		s.info = info
	}
	s.titleMu.Unlock()
	if changed {
		m.events.Title(id, info)
	}
}

// stopTitle ends title checks once the shell has exited.
func (s *session) stopTitle() {
	s.titleMu.Lock()
	defer s.titleMu.Unlock()
	s.done = true
	if s.titleTimer != nil {
		s.titleTimer.Stop()
		s.titleTimer = nil
	}
}

// inspect reads the foreground process of the PTY (caller holds titleMu).
func (s *session) inspect() (Info, bool) {
	rc, err := s.pty.SyscallConn() // not Fd(): that would switch the PTY to blocking reads
	if err != nil {
		return Info{}, false
	}
	pgid, ferr := 0, error(nil)
	if err := rc.Control(func(fd uintptr) { pgid, ferr = foregroundGroup(fd) }); err != nil || ferr != nil {
		return Info{}, false
	}
	shell := s.cmd.Process.Pid
	if pgid <= 0 || pgid == shell {
		cwd, err := processCwd(shell)
		if err != nil {
			cwd = s.info.Cwd
		}
		s.fgGroup = 0
		return Info{Cwd: cwd}, true
	}
	if pgid == s.fgGroup { // the same command still runs
		return s.info, true
	}
	args, err := processArgs(pgid)
	if err != nil {
		return Info{}, false
	}
	s.fgGroup = pgid
	return Info{Command: commandLabel(args), Cwd: s.info.Cwd}, true
}

// interpreters run scripts by a shebang: "node /opt/homebrew/bin/claude" is
// named after the script.
var interpreters = map[string]bool{
	"node": true, "python": true, "ruby": true, "perl": true, "bun": true, "deno": true,
	"sh": true, "bash": true, "zsh": true,
}

// secretFlag matches arguments that may carry credentials ("--password=…",
// "Authorization: Bearer …"); the label stops before them.
var secretFlag = regexp.MustCompile(`(?i)(pass|pwd|token|secret|auth|key|credential)`)

// commandLabel names a command line for a tab: the program and its first few
// arguments ("npm run dev", "go test ./..."), paths shortened to their last
// element, and nothing from an argument that looks like a credential onwards.
func commandLabel(args []string) string {
	if len(args) == 0 {
		return ""
	}
	name := strings.TrimPrefix(filepath.Base(args[0]), "-") // "-zsh": a login shell
	rest := args[1:]
	if interpreters[strings.TrimRight(name, "0123456789.")] && len(rest) > 0 && strings.HasPrefix(rest[0], "/") && filepath.Ext(rest[0]) == "" {
		name, rest = filepath.Base(rest[0]), rest[1:]
	}
	words := []string{name}
	for i, a := range rest {
		if i == maxLabelArgs || secretFlag.MatchString(a) {
			words = append(words, "…")
			break
		}
		if strings.HasPrefix(a, "/") && len(a) > 1 {
			a = filepath.Base(a)
		}
		words = append(words, a)
	}
	label := strings.Join(words, " ")
	if utf8.RuneCountInString(label) > maxLabelRunes {
		label = string([]rune(label)[:maxLabelRunes-1]) + "…"
	}
	return label
}

// parseProcArgs reads macOS kern.procargs2: argc (int32), the executable path,
// NUL padding, then argc NUL-terminated arguments (the environment follows).
func parseProcArgs(raw []byte) []string {
	if len(raw) < 4 {
		return nil
	}
	argc := int(binary.LittleEndian.Uint32(raw))
	rest := raw[4:]
	if i := bytes.IndexByte(rest, 0); i >= 0 {
		rest = rest[i:]
	}
	rest = bytes.TrimLeft(rest, "\x00")
	args := make([]string, 0, min(argc, 16))
	for len(args) < argc && len(rest) > 0 {
		i := bytes.IndexByte(rest, 0)
		if i < 0 {
			args = append(args, string(rest))
			break
		}
		args = append(args, string(rest[:i]))
		rest = rest[i+1:]
	}
	return args
}
