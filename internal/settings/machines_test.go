package settings

import (
	"encoding/json"
	"strings"
	"testing"
)

func okMachine() Machine {
	return Machine{ID: "dev-box", Name: "Dev box", Target: "me@dev.example.com", Port: 2222, Command: "flint"}
}

func TestCheckMachineAccepts(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Machine)
	}{
		{"typical", func(*Machine) {}},
		{"no name", func(m *Machine) { m.Name = "" }},
		{"no port", func(m *Machine) { m.Port = 0 }},
		{"max port", func(m *Machine) { m.Port = 65535 }},
		{"bare host", func(m *Machine) { m.Target = "devbox" }},
		{"ssh alias with dash and dot", func(m *Machine) { m.Target = "dev-box.internal" }},
		{"underscore first", func(m *Machine) { m.Target = "_svc@host" }},
		{"ip address", func(m *Machine) { m.Target = "10.0.0.5" }},
		{"path command", func(m *Machine) { m.Command = "~/bin/flint" }},
		{"absolute command", func(m *Machine) { m.Command = "/opt/flint/bin/flint-host" }},
		{"one-char id", func(m *Machine) { m.ID = "a" }},
		{"digit id", func(m *Machine) { m.ID = "9" }},
		{"32-char id", func(m *Machine) { m.ID = strings.Repeat("a", 32) }},
		{"unicode name", func(m *Machine) { m.Name = "Büro ☕" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := okMachine()
			tt.edit(&m)
			if err := CheckMachine(m); err != nil {
				t.Errorf("CheckMachine(%+v) = %v", m, err)
			}
		})
	}
}

func TestCheckMachineRefuses(t *testing.T) {
	long := func(n int) string { return strings.Repeat("a", n) }
	tests := []struct {
		name string
		edit func(*Machine)
		want string // part of the error
	}{
		{"empty id", func(m *Machine) { m.ID = "" }, "id"},
		{"upper-case id", func(m *Machine) { m.ID = "Dev" }, "id"},
		{"leading dash id", func(m *Machine) { m.ID = "-dev" }, "id"},
		{"id with colon", func(m *Machine) { m.ID = "a:b" }, "id"},
		{"id with space", func(m *Machine) { m.ID = "a b" }, "id"},
		{"33-char id", func(m *Machine) { m.ID = long(33) }, "id"},
		{"id with newline", func(m *Machine) { m.ID = "dev\n" }, "id"},

		{"empty target", func(m *Machine) { m.Target = "" }, "target"},
		{"leading dash target", func(m *Machine) { m.Target = "-oProxyCommand=x" }, "target"},
		{"leading dash user", func(m *Machine) { m.Target = "-x@host" }, "target"},
		{"option as target", func(m *Machine) { m.Target = "-F/etc/passwd" }, "target"},
		{"space in target", func(m *Machine) { m.Target = "host -v" }, "target"},
		{"semicolon", func(m *Machine) { m.Target = "host;id" }, "target"},
		{"backtick", func(m *Machine) { m.Target = "host`id`" }, "target"},
		{"command substitution", func(m *Machine) { m.Target = "$(id)@host" }, "target"},
		{"dollar", func(m *Machine) { m.Target = "host$IFS" }, "target"},
		{"newline", func(m *Machine) { m.Target = "host\nid" }, "target"},
		{"trailing newline", func(m *Machine) { m.Target = "host\n" }, "target"},
		{"tab", func(m *Machine) { m.Target = "host\tx" }, "target"},
		{"pipe", func(m *Machine) { m.Target = "host|id" }, "target"},
		{"ampersand", func(m *Machine) { m.Target = "host&id" }, "target"},
		{"quote", func(m *Machine) { m.Target = "ho'st" }, "target"},
		{"slash", func(m *Machine) { m.Target = "host/x" }, "target"},
		{"empty user", func(m *Machine) { m.Target = "@host" }, "target"},
		{"empty host", func(m *Machine) { m.Target = "user@" }, "target"},
		{"two at signs", func(m *Machine) { m.Target = "a@b@c" }, "target"},
		{"host starts with dash", func(m *Machine) { m.Target = "user@-host" }, "target"},
		{"host starts with dot", func(m *Machine) { m.Target = "user@.host" }, "target"},
		{"cyrillic look-alike", func(m *Machine) { m.Target = "dеv.example.com" }, "target"}, // the e is U+0435
		{"fullwidth dash", func(m *Machine) { m.Target = "－oFoo" }, "target"},
		{"unicode host", func(m *Machine) { m.Target = "user@bücher.example" }, "target"},
		{"64-char user", func(m *Machine) { m.Target = long(64) + "@host" }, "target"},
		{"254-char host", func(m *Machine) { m.Target = "u@" + long(254) }, "target"},
		{"64-char bare target", func(m *Machine) { m.Target = long(64) }, "target"},
		{"ipv6 brackets", func(m *Machine) { m.Target = "[::1]" }, "target"},
		{"nul", func(m *Machine) { m.Target = "host\x00" }, "target"},

		{"negative port", func(m *Machine) { m.Port = -1 }, "port"},
		{"port 65536", func(m *Machine) { m.Port = 65536 }, "port"},

		{"empty command", func(m *Machine) { m.Command = "" }, "command"},
		{"leading dash command", func(m *Machine) { m.Command = "-flint" }, "command"},
		{"command option", func(m *Machine) { m.Command = "--version" }, "command"},
		{"command with args", func(m *Machine) { m.Command = "flint --x" }, "command"},
		{"command with semicolon", func(m *Machine) { m.Command = "flint;id" }, "command"},
		{"command with backtick", func(m *Machine) { m.Command = "`id`" }, "command"},
		{"command substitution", func(m *Machine) { m.Command = "$(id)" }, "command"},
		{"command with newline", func(m *Machine) { m.Command = "flint\nid" }, "command"},
		{"command with quote", func(m *Machine) { m.Command = "fl'int" }, "command"},
		{"command with glob", func(m *Machine) { m.Command = "fl*" }, "command"},
		{"command with unicode", func(m *Machine) { m.Command = "flіnt" }, "command"}, // the і is U+0456
		{"257-char command", func(m *Machine) { m.Command = long(257) }, "command"},

		{"name with newline", func(m *Machine) { m.Name = "a\nb" }, "name"},
		{"name with escape", func(m *Machine) { m.Name = "a\x1b[31m" }, "name"},
		{"name with NUL", func(m *Machine) { m.Name = "a\x00" }, "name"},
		{"name with DEL", func(m *Machine) { m.Name = "a\x7f" }, "name"},
		{"padded name", func(m *Machine) { m.Name = " a" }, "name"},
		{"65-rune name", func(m *Machine) { m.Name = strings.Repeat("é", 65) }, "name"},
		{"invalid UTF-8 name", func(m *Machine) { m.Name = "a\xff" }, "name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := okMachine()
			tt.edit(&m)
			err := CheckMachine(m)
			if err == nil {
				t.Fatalf("CheckMachine(%+v) accepted it", m)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not mention %q", err, tt.want)
			}
		})
	}
}

func TestCheckMachinesList(t *testing.T) {
	a, b := okMachine(), okMachine()
	b.ID = "other"
	if err := CheckMachines([]Machine{a, b}); err != nil {
		t.Errorf("two machines: %v", err)
	}
	if err := CheckMachines([]Machine{a, a}); err == nil || !strings.Contains(err.Error(), "twice") {
		t.Errorf("duplicate id: %v", err)
	}
	var many []Machine
	for i := range MaxMachines + 1 {
		m := okMachine()
		m.ID = "m" + strings.Repeat("x", i%5) + string(rune('a'+i%26)) + string(rune('a'+i/26))
		many = append(many, m)
	}
	if err := CheckMachines(many[:MaxMachines]); err != nil {
		t.Errorf("%d machines: %v", MaxMachines, err)
	}
	if err := CheckMachines(many); err == nil {
		t.Errorf("%d machines accepted", MaxMachines+1)
	}
	s := Default()
	s.Machines = []Machine{a, a}
	if Validate(s) == nil {
		t.Error("Validate accepted a repeated machine id")
	}
}

func TestNormalizeMachinesDropsWithReasons(t *testing.T) {
	good := okMachine()
	noCmd := Machine{ID: "nocmd", Target: "h"}
	badTarget := okMachine()
	badTarget.ID, badTarget.Target = "bad", "-oProxyCommand=x"
	dup := okMachine()
	dup.Target = "elsewhere"

	got, issues := NormalizeMachines([]Machine{good, noCmd, badTarget, dup})
	if len(got) != 2 || got[0] != good || got[1].Command != DefaultMachineCmd || got[1].ID != "nocmd" {
		t.Fatalf("kept = %+v", got)
	}
	if len(issues) != 2 {
		t.Fatalf("issues = %+v", issues)
	}
	if issues[0].Index != 2 || issues[0].ID != "bad" || !strings.Contains(issues[0].Reason, "target") {
		t.Errorf("issue 0 = %+v", issues[0])
	}
	if issues[1].Index != 3 || !strings.Contains(issues[1].Reason, "twice") {
		t.Errorf("issue 1 = %+v", issues[1])
	}
}

func TestNormalizeMachinesLimit(t *testing.T) {
	var list []Machine
	for i := range MaxMachines + 3 {
		list = append(list, Machine{ID: "m" + string(rune('a'+i%26)) + string(rune('a'+i/26)), Target: "h"})
	}
	got, issues := NormalizeMachines(list)
	if len(got) != MaxMachines || len(issues) != 3 {
		t.Errorf("kept %d, dropped %d", len(got), len(issues))
	}
}

func TestMachinesFromFile(t *testing.T) {
	data := `{"machines":[
		{"id":"ok","target":"me@host","port":22},
		{"id":"evil","target":"host;rm -rf ~","command":"flint"},
		{"id":"flag","target":"h","command":"--agent-host"},
		{"id":"typed","target":"h","port":"22"}
	]}`
	var s Settings
	_ = json.Unmarshal([]byte(data), &s) // the wrong-typed port loses the whole decode of that entry; Normalize still runs
	n := Normalize(s)
	for _, m := range n.Machines {
		if err := CheckMachine(m); err != nil {
			t.Errorf("Normalize kept an invalid machine %+v: %v", m, err)
		}
	}
	if len(n.Machines) == 0 || n.Machines[0].ID != "ok" || n.Machines[0].Command != "flint" {
		t.Fatalf("machines = %+v", n.Machines)
	}
	var reasons []string
	for _, is := range n.MachineIssues() {
		reasons = append(reasons, is.ID+": "+is.Reason)
	}
	if len(reasons) < 2 {
		t.Errorf("issues = %v", reasons)
	}
	if Validate(n) != nil {
		t.Errorf("normalized settings do not validate: %v", Validate(n))
	}
}

func TestMachinesDefaultAndLookup(t *testing.T) {
	d := Default()
	if d.Machines == nil || len(d.Machines) != 0 {
		t.Errorf("default machines = %#v", d.Machines)
	}
	b, _ := json.Marshal(d)
	if !strings.Contains(string(b), `"machines":[]`) {
		t.Errorf("a default file lacks machines: []")
	}
	d.Machines = []Machine{okMachine()}
	if m, ok := d.Machine("dev-box"); !ok || m.Target != "me@dev.example.com" {
		t.Errorf("Machine = %+v, %v", m, ok)
	}
	if _, ok := d.Machine("nope"); ok {
		t.Error("found a machine that is not there")
	}
}
