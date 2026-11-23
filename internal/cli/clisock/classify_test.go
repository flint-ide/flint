package clisock

import (
	"errors"
	"strings"
	"testing"

	"flint/internal/cli"
	"flint/internal/proctree"
)

// fakeTree is a process table: pid → parent, uid, command line (nil args:
// unreadable), session and terminal.
type fakeTree map[int]fakeProc

type fakeProc struct {
	ppid int
	uid  uint32
	args []string
	sid  int
	tty  bool
}

func (t fakeTree) Parent(pid int) (proctree.Proc, error) {
	p, ok := t[pid]
	if !ok {
		return proctree.Proc{}, errors.New("no such process")
	}
	return proctree.Proc{PID: pid, PPID: p.ppid, UID: p.uid, SID: p.sid, TTY: p.tty}, nil
}

func (t fakeTree) Args(pid int) ([]string, error) {
	p, ok := t[pid]
	if !ok || p.args == nil {
		return nil, errors.New("unreadable")
	}
	return p.args, nil
}

const (
	me   = 501
	root = 0
	exe  = "/Applications/flint.app/Contents/MacOS/flint"
)

// user is a process of ours without a terminal; term one in the terminal
// session whose leader is sid.
func user(ppid int, args ...string) fakeProc { return fakeProc{ppid: ppid, uid: me, args: args} }

func term(sid, ppid int, args ...string) fakeProc {
	return fakeProc{ppid: ppid, uid: me, args: args, sid: sid, tty: true}
}

func TestClassify(t *testing.T) {
	facts := Facts{
		Self: 100, UID: me, HostPID: 700,
		Shells: map[int]Shell{400: {Tab: "t1"}, 401: {Tab: "t2", Agent: true}},
		Agents: AgentNames("/usr/local/bin/my-acp", ""),
	}
	// The processes every case shares: Flint under launchd, its two tabs'
	// shells, the background host it started.
	base := fakeTree{
		1:   {ppid: 0, uid: root, args: nil},
		100: user(1, exe),
		400: term(400, 100, "-zsh"),
		401: term(401, 100, "-zsh"),
		700: user(100, exe, "--agent-host", "--ready-fd=3"),
	}
	cases := []struct {
		name     string
		procs    fakeTree
		peer     int
		agent    bool
		what     string // substring; "" for a person
		unk      bool
		detached bool
	}{
		{"Terminal outside Flint", fakeTree{500: term(499, 499, "flint", "run", "approve"), 499: term(499, 498, "-zsh"), 498: user(1, "Terminal")}, 500, false, "", false, false},
		{"ssh: the session's leader under root's sshd", fakeTree{500: term(499, 499, "flint"), 499: term(499, 498, "-zsh"), 498: {ppid: 1, uid: root}}, 500, false, "", false, false},
		{"a script in Flint's terminal tab", fakeTree{500: term(400, 450, exe, "run"), 450: term(400, 400, "sh", "./release.sh")}, 500, false, "", false, false},
		{"claude in a terminal tab", fakeTree{500: term(400, 450, "flint"), 450: term(400, 420, "bash", "-c", "flint run approve x"), 420: term(400, 400, "node", "/opt/homebrew/bin/claude")}, 500, true, "claude", false, false},
		{"a tab whose agent reports working", fakeTree{500: term(401, 410, "flint"), 410: term(401, 401, "renamed-agent")}, 500, true, "an agent CLI in a terminal tab", false, false},
		{"an agent chat", fakeTree{500: user(300, "flint"), 300: user(200, "/bin/sh", "-c", "flint run approve x"), 200: user(100, "/opt/bin/codex", "app-server")}, 500, true, "codex", false, false},
		{"a custom ACP agent by its command", fakeTree{500: user(200, "flint"), 200: user(100, "/usr/local/bin/my-acp", "--acp")}, 500, true, "my-acp", false, false},
		{"a workflow's script step", fakeTree{500: user(310, "flint"), 310: user(100, "make", "test")}, 500, true, "started by Flint", false, false},
		{"a child of Flint that calls itself the host", fakeTree{500: user(710, "flint"), 710: user(100, exe, "--agent-host")}, 500, true, "started by Flint", false, false},
		{"a background host's tab", fakeTree{500: term(600, 600, "flint"), 600: term(600, 700, "-zsh")}, 500, false, "", false, false},
		{"under the host, not in a tab's session", fakeTree{500: user(600, "flint"), 600: user(700, "sh")}, 500, false, "no terminal", false, true},
		{"under the host, a session not led by a tab", fakeTree{500: term(500, 600, "flint"), 600: term(600, 700, "-zsh")}, 500, false, "no longer in the terminal session", false, true},
		{"a host the agent started", fakeTree{500: term(600, 600, "flint"), 600: term(600, 760, "-zsh"), 760: user(200, exe, "--agent-host"), 200: user(100, "claude")}, 500, true, "claude", false, false},
		{"a host that outlived its window", fakeTree{500: term(600, 600, "flint"), 600: term(600, 770, "-zsh"), 770: user(1, exe, "--agent-host")}, 500, false, "", false, false},
		{"claude outside Flint", fakeTree{500: term(499, 420, "flint"), 420: term(499, 499, "claude"), 499: term(499, 1, "-zsh")}, 500, true, "claude", false, false},
		{"detached: reparented to launchd, no terminal", fakeTree{500: user(510, "flint", "run", "approve"), 510: user(1, "sh", "-c", "sleep 60; flint run approve x")}, 500, false, "no terminal", false, true},
		{"detached: reparented, its terminal's shell gone", fakeTree{500: term(499, 510, "flint"), 510: term(499, 1, "sh", "-c", "sleep 60; flint run approve x")}, 500, false, "no longer in the terminal session", false, true},
		{"detached under a user's service manager", fakeTree{500: user(510, "flint"), 510: user(90, "sh"), 90: user(1, "systemd", "--user")}, 500, false, "no terminal", false, true},
		{"another user's ancestor is not read", fakeTree{500: term(500, 450, "flint"), 450: {ppid: 1, uid: root, args: nil}}, 500, false, "", false, false},
		{"an ancestor of ours that cannot be read", fakeTree{500: user(450, "flint"), 450: {ppid: 1, uid: me, args: nil}}, 500, false, "cannot be read", true, false},
		{"the peer is gone", fakeTree{}, 500, false, "no such process", true, false},
		{"no peer pid", fakeTree{}, 0, false, "does not say", true, false},
		{"a loop", fakeTree{500: user(501, "a"), 501: user(500, "b")}, 500, false, "more than", true, false},
		{"Flint itself", fakeTree{}, 100, true, "Flint itself", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tree := fakeTree{}
			for k, v := range base {
				tree[k] = v
			}
			for k, v := range tc.procs {
				tree[k] = v
			}
			got := Classify(tc.peer, tree, facts)
			if got.Agent != tc.agent || got.Unknown != tc.unk || got.Detached != tc.detached ||
				(tc.what == "") != (got.What == "") || !strings.Contains(got.What, tc.what) {
				t.Errorf("Classify = %+v, want agent %v unknown %v detached %v what %q", got, tc.agent, tc.unk, tc.detached, tc.what)
			}
			if tc.peer == 500 && got.PID != 500 {
				t.Errorf("PID %d", got.PID)
			}
		})
	}
}

func TestAgentNames(t *testing.T) {
	n := AgentNames("/opt/x/goose-cli", "flint", "/", "")
	for _, want := range []string{"claude", "codex", "gemini", "goose-cli"} {
		if !n[want] {
			t.Errorf("%s is not an agent", want)
		}
	}
	for _, not := range []string{"flint", "/", "", "zsh", "make"} {
		if n[not] {
			t.Errorf("%q is an agent", not)
		}
	}
}

// A custom (ACP) agent Flint started is named after the agent, not its
// program: "node /x/fake-cli.js" is the agent fake-cli when the settings
// say so. Another program of the same name keeps the program's name.
func TestClassifyNamesACustomAgent(t *testing.T) {
	custom := []CustomAgent{
		{ID: "fake-cli", Command: "node", Args: []string{"/x/fake-cli.js"}},
		{ID: "shebang", Command: "/opt/acp/shebang-agent", Args: []string{"--acp"}},
	}
	facts := Facts{Self: 100, UID: me, Custom: custom, Agents: AgentNames("node", "/opt/acp/shebang-agent")}
	base := fakeTree{1: {ppid: 0, uid: root}, 100: user(1, exe)}
	for _, tc := range []struct {
		name      string
		agentArgs []string
		agentName string // "" when only the program is known
		refusal   string
	}{
		{"node and its script", []string{"/usr/local/bin/node", "/x/fake-cli.js", "--verbose"}, "fake-cli", "this command was run by the agent fake-cli."},
		{"a script run by its interpreter", []string{"node", "/opt/acp/shebang-agent", "--acp"}, "shebang", "this command was run by the agent shebang."},
		{"another node program", []string{"node", "/y/other.js"}, "", "this command was run by an agent (node)."},
		{"the agent without its arguments", []string{"/opt/acp/shebang-agent"}, "", "this command was run by an agent (shebang-agent)."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			procs := fakeTree{500: user(450, "flint", "run", "stop", "r1"), 450: user(300, "bash", "-c", "flint run stop r1"), 300: user(100, tc.agentArgs...)}
			for pid, p := range base {
				procs[pid] = p
			}
			c := Classify(500, procs, facts)
			if !c.Agent || c.AgentName != tc.agentName {
				t.Fatalf("caller %+v, want the agent %q", c, tc.agentName)
			}
			if got := cli.RefuseText(c); !strings.Contains(got, tc.refusal) {
				t.Errorf("refusal %q lacks %q", got, tc.refusal)
			}
		})
	}
}
