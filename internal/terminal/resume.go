package terminal

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Resume says how to bring back the agent CLI a terminal was running, so a
// restarted Flint can continue the conversation in the reopened terminal.
// The zero value means there is nothing to resume.
type Resume struct {
	// Agent is "claude" or "codex", "" when the terminal runs neither.
	Agent string `json:"agent"`
	// Session is the conversation's id when it is known (Claude Code), else ""
	// and the agent's most recent conversation in the folder is continued.
	Session string `json:"session"`
}

// maxSessionFile caps Claude Code's per-process record; it is a few hundred bytes.
const maxSessionFile = 64 << 10

var sessionIDRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// notConversations are subcommands and flags of the agent CLIs that don't run
// an interactive conversation: nothing to come back to.
var notConversations = map[string]map[string]bool{
	"claude": set("-p", "--print", "-v", "--version", "-h", "--help",
		"mcp", "update", "upgrade", "doctor", "config", "install", "plugin", "plugins", "setup-token", "auth", "agents", "migrate-installer"),
	"codex": set("-V", "--version", "-h", "--help",
		"exec", "e", "login", "logout", "mcp", "mcp-server", "app-server", "completion", "apply", "a", "sandbox", "debug", "cloud", "features"),
}

func set(words ...string) map[string]bool {
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}

// Resume reports the agent conversation running in the terminal's foreground,
// the zero value when the shell waits at the prompt or runs something else.
func (m *Manager) Resume(id string) (Resume, error) {
	s, err := m.get(id)
	if err != nil {
		return Resume{}, err
	}
	rc, err := s.pty.SyscallConn() // not Fd(): that would switch the PTY to blocking reads
	if err != nil {
		return Resume{}, err
	}
	pgid, ferr := 0, error(nil)
	if err := rc.Control(func(fd uintptr) { pgid, ferr = foregroundGroup(fd) }); err != nil {
		return Resume{}, err
	}
	if ferr != nil || pgid <= 0 || pgid == s.cmd.Process.Pid {
		return Resume{}, nil
	}
	args, err := processArgs(pgid)
	if err != nil {
		return Resume{}, nil // the command ended meanwhile
	}
	return resumeOf(args, pgid, claudeSessionsDir()), nil
}

// resumeOf decides from a foreground command line (and, for Claude Code, the
// record it keeps of its running process in sessionsDir) what to resume.
func resumeOf(args []string, pid int, sessionsDir string) Resume {
	agent, rest := programName(args)
	skip, known := notConversations[agent]
	if !known {
		return Resume{}
	}
	for _, a := range rest {
		if skip[a] {
			return Resume{}
		}
	}
	r := Resume{Agent: agent}
	if agent == "claude" {
		r.Session = claudeSession(sessionsDir, pid)
	}
	return r
}

// programName is the program a command line runs and its arguments:
// "node /opt/homebrew/bin/claude --resume" is claude (see commandLabel).
func programName(args []string) (string, []string) {
	if len(args) == 0 {
		return "", nil
	}
	name := strings.TrimPrefix(filepath.Base(args[0]), "-")
	rest := args[1:]
	if interpreters[strings.TrimRight(name, "0123456789.")] && len(rest) > 0 && strings.HasPrefix(rest[0], "/") && filepath.Ext(rest[0]) == "" {
		name, rest = filepath.Base(rest[0]), rest[1:]
	}
	return name, rest
}

// claudeSessionsDir is where Claude Code records its running processes
// (<pid>.json with the conversation's sessionId), "" when there is no home.
func claudeSessionsDir() string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "sessions")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "sessions")
}

// claudeSession reads the conversation id of the Claude Code process pid, ""
// when there is no (valid) record: the file is not a documented interface, so
// anything unexpected only means "continue the latest conversation instead".
func claudeSession(dir string, pid int) string {
	if dir == "" || pid <= 0 {
		return ""
	}
	f, err := os.Open(filepath.Join(dir, strconv.Itoa(pid)+".json"))
	if err != nil {
		return ""
	}
	defer f.Close()
	var rec struct {
		PID       int    `json:"pid"`
		SessionID string `json:"sessionId"`
	}
	if err := json.NewDecoder(io.LimitReader(f, maxSessionFile)).Decode(&rec); err != nil {
		return ""
	}
	// A record left by an earlier process that had the same pid names itself.
	if rec.PID != pid || !sessionIDRe.MatchString(rec.SessionID) {
		return ""
	}
	return rec.SessionID
}
