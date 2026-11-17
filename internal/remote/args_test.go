package remote

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"flint/internal/settings"
)

var fixedOptions = []string{
	"ssh", "-T", "-e", "none",
	"-o", "BatchMode=yes", "-o", "ConnectTimeout=10",
	"-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=3",
}

func machine(edit func(*settings.Machine)) settings.Machine {
	m := settings.Machine{ID: "dev", Target: "me@dev.example.com", Command: "flint"}
	if edit != nil {
		edit(&m)
	}
	return m
}

func TestHostArgvGolden(t *testing.T) {
	tests := []struct {
		name string
		m    settings.Machine
		want []string
	}{
		{"no port", machine(nil), append(slices.Clone(fixedOptions), "--", "me@dev.example.com", "flint", "--agent-host", "--stdio")},
		{"with port", machine(func(m *settings.Machine) { m.Port = 2222 }),
			append(slices.Clone(fixedOptions), "-p", "2222", "--", "me@dev.example.com", "flint", "--agent-host", "--stdio")},
		{"path command, alias target", machine(func(m *settings.Machine) { m.Target, m.Command = "devbox", "~/bin/flint-host" }),
			append(slices.Clone(fixedOptions), "--", "devbox", "~/bin/flint-host", "--agent-host", "--stdio")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := HostArgv(tt.m)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("argv\n got  %q\n want %q", got, tt.want)
			}
		})
	}
}

func TestCheckArgvGolden(t *testing.T) {
	m := machine(func(m *settings.Machine) { m.Port = 2200; m.Command = "/opt/flint/bin/flint" })
	got, err := CheckArgv(m)
	if err != nil {
		t.Fatal(err)
	}
	want := append(slices.Clone(fixedOptions), "-p", "2200", "--", "me@dev.example.com", "/opt/flint/bin/flint", "--version")
	if !slices.Equal(got, want) {
		t.Errorf("argv\n got  %q\n want %q", got, want)
	}
}

// Whatever the machine, every user-derived element is after "--", exactly one
// "--" exists, and no user-derived element can be read as an option.
func TestUserValuesAreAfterDashDash(t *testing.T) {
	for _, m := range []settings.Machine{
		machine(nil),
		machine(func(m *settings.Machine) { m.Port = 65535 }),
		machine(func(m *settings.Machine) { m.Target = "_x" }),
		machine(func(m *settings.Machine) { m.Command = "./flint" }),
	} {
		for name, build := range map[string]func(settings.Machine) ([]string, error){"host": HostArgv, "check": CheckArgv} {
			got, err := build(m)
			if err != nil {
				t.Fatal(err)
			}
			if n := count(got, "--"); n != 1 {
				t.Fatalf("%s %q: %d times --", name, got, n)
			}
			dd := slices.Index(got, "--")
			for i, a := range got {
				user := a == m.Target || a == m.Command
				if i < dd && user && a != "ssh" {
					t.Errorf("%s: user value %q at %d, before --", name, a, i)
				}
				if user && strings.HasPrefix(a, "-") {
					t.Errorf("%s: user value %q looks like an option", name, a)
				}
			}
			if got[dd+1] != m.Target || got[dd+2] != m.Command {
				t.Errorf("%s: after -- = %q", name, got[dd+1:])
			}
			if m.Port != 0 {
				if p := slices.Index(got, "-p"); p < 0 || p > dd || got[p+1] != itoa(m.Port) {
					t.Errorf("%s: -p %d misplaced in %q", name, m.Port, got)
				}
			}
		}
	}
}

func count(s []string, v string) int {
	n := 0
	for _, x := range s {
		if x == v {
			n++
		}
	}
	return n
}

func itoa(n int) string { return strconv.Itoa(n) }

// The builder re-validates: nothing hostile gets an argument vector, even
// though the registry would have dropped it already.
func TestArgvRefusesUnvalidatedMachines(t *testing.T) {
	bad := map[string]func(*settings.Machine){
		"option as target":     func(m *settings.Machine) { m.Target = "-oProxyCommand=touch /tmp/x" },
		"dash user":            func(m *settings.Machine) { m.Target = "-l@h" },
		"space in target":      func(m *settings.Machine) { m.Target = "h -v" },
		"semicolon target":     func(m *settings.Machine) { m.Target = "h;id" },
		"backtick target":      func(m *settings.Machine) { m.Target = "`id`" },
		"substitution":         func(m *settings.Machine) { m.Target = "$(id)" },
		"newline target":       func(m *settings.Machine) { m.Target = "h\nid" },
		"empty user":           func(m *settings.Machine) { m.Target = "@h" },
		"empty host":           func(m *settings.Machine) { m.Target = "u@" },
		"look-alike":           func(m *settings.Machine) { m.Target = "hоst" }, // Cyrillic o
		"long target":          func(m *settings.Machine) { m.Target = strings.Repeat("a", 64) },
		"negative port":        func(m *settings.Machine) { m.Port = -22 },
		"huge port":            func(m *settings.Machine) { m.Port = 70000 },
		"command option":       func(m *settings.Machine) { m.Command = "-oFoo" },
		"command with args":    func(m *settings.Machine) { m.Command = "flint --x" },
		"command substitution": func(m *settings.Machine) { m.Command = "$(id)" },
		"command semicolon":    func(m *settings.Machine) { m.Command = "flint;id" },
		"command newline":      func(m *settings.Machine) { m.Command = "flint\nid" },
		"empty command":        func(m *settings.Machine) { m.Command = "" },
		"long command":         func(m *settings.Machine) { m.Command = strings.Repeat("a", 257) },
		"bad id":               func(m *settings.Machine) { m.ID = "A b" },
		"control in name":      func(m *settings.Machine) { m.Name = "a\nb" },
		"zero value":           func(m *settings.Machine) { *m = settings.Machine{} },
	}
	for name, edit := range bad {
		t.Run(name, func(t *testing.T) {
			m := machine(edit)
			if got, err := HostArgv(m); err == nil || got != nil {
				t.Errorf("HostArgv(%+v) = %q, %v", m, got, err)
			}
			if got, err := CheckArgv(m); err == nil || got != nil {
				t.Errorf("CheckArgv(%+v) = %q, %v", m, got, err)
			}
		})
	}
}

func TestNewCommandResolvesSSHAtCallTime(t *testing.T) {
	argv, _ := HostArgv(machine(nil))

	t.Setenv("PATH", t.TempDir())
	if _, err := newCommand(argv); err == nil || !strings.Contains(err.Error(), "ssh was not found") {
		t.Fatalf("with no ssh on PATH: %v", err)
	}

	dir := fakeBin(t, "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", dir) // installed after the package was loaded
	cmd, err := newCommand(argv)
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Path != dir+"/ssh" || !slices.Equal(cmd.Args[1:], argv[1:]) {
		t.Errorf("cmd = %q %q", cmd.Path, cmd.Args)
	}
	if _, err := newCommand([]string{"sh", "-c", "id"}); err == nil {
		t.Error("newCommand accepted a vector that is not ssh's")
	}
}
