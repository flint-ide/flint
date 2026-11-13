package manifest

import (
	"strings"
	"testing"
)

func sandboxManifest(run string) string {
	return `{"contract": 1, "id": "acme.ext", "name": "Ext", "version": "1.0.0", "run": ` + run + `,
	  "contributes": {"commands": [{"id": "acme.ext.go", "title": "Go"}]}}`
}

func TestSandboxSection(t *testing.T) {
	long := "/" + strings.Repeat("a", maxSandboxPathLen)
	var many []string
	for i := range maxSandboxPaths + 1 {
		many = append(many, `"/data/`+strings.Repeat("x", i+1)+`"`)
	}
	tests := []struct {
		name    string
		sandbox string // the value of run.sandbox
		env     string // run.env, a JSON array
		wantErr string // "" when valid
	}{
		{name: "empty: the strict profile", sandbox: `{}`},
		{name: "strict by name", sandbox: `{"mode": "strict"}`},
		{name: "none", sandbox: `{"mode": "none"}`},
		{name: "everything", sandbox: `{"read": ["~/.kube", "/var/run/docker.sock", "${XDG_CONFIG_HOME}/tool", "${XDG_DATA_HOME}", "${env:KUBECONFIG}", "~/a b/c~", "/opt/x-1.2/lib_y"], "write": ["~/.cache/tool", "${XDG_STATE_HOME}/tool"], "network": true, "localSockets": true, "exec": true}`, env: `["KUBECONFIG"]`},
		{name: "the same path read and written", sandbox: `{"read": ["~/x"], "write": ["~/x"]}`},

		{name: "not an object", sandbox: `true`, wantErr: "run.sandbox must be an object"},
		{name: "null", sandbox: `[]`, wantErr: "run.sandbox must be an object"},
		{name: "unknown field", sandbox: `{"reads": ["~/x"]}`, wantErr: `unknown field "reads"`},
		{name: "unknown mode", sandbox: `{"mode": "off"}`, wantErr: `mode "off" is not "strict" or "none"`},
		{name: "none with grants", sandbox: `{"mode": "none", "network": true}`, wantErr: "remove read, write, network, localSockets and exec"},
		{name: "none with local sockets", sandbox: `{"mode": "none", "localSockets": true}`, wantErr: "remove read, write, network, localSockets and exec"},
		{name: "none with paths", sandbox: `{"mode": "none", "read": ["~/x"]}`, wantErr: "remove read, write, network, localSockets and exec"},
		{name: "wrong type", sandbox: `{"network": "yes"}`, wantErr: "run.sandbox:"},
		{name: "paths not a list", sandbox: `{"read": "~/x"}`, wantErr: "run.sandbox:"},
		{name: "too many", sandbox: `{"read": [` + strings.Join(many, ",") + `]}`, wantErr: "run.sandbox.read: at most 32 paths"},
		{name: "too long", sandbox: `{"write": ["` + long + `"]}`, wantErr: "run.sandbox.write:"},
		{name: "twice", sandbox: `{"read": ["~/x", "~/x"]}`, wantErr: `"~/x" is listed twice`},
		{name: "empty path", sandbox: `{"read": [""]}`, wantErr: "an empty path"},
		{name: "relative", sandbox: `{"read": ["data/x"]}`, wantErr: "start with /, ~/ or a variable"},
		{name: "dot relative", sandbox: `{"read": ["./x"]}`, wantErr: "start with /, ~/ or a variable"},
		{name: "the root", sandbox: `{"read": ["/"]}`, wantErr: "below the root or the home folder"},
		{name: "the home folder", sandbox: `{"read": ["~"]}`, wantErr: "start with /, ~/ or a variable"},
		{name: "the home folder with a slash", sandbox: `{"read": ["~/"]}`, wantErr: "below the root or the home folder"},
		{name: "another user's home", sandbox: `{"read": ["~root/x"]}`, wantErr: "start with /, ~/ or a variable"},
		{name: "dot dot", sandbox: `{"read": ["~/../other"]}`, wantErr: "clean path"},
		{name: "dot dot in the middle", sandbox: `{"read": ["/opt/tool/../../etc/shadow"]}`, wantErr: "clean path"},
		{name: "dot dot at the end", sandbox: `{"write": ["~/x/.."]}`, wantErr: "clean path"},
		{name: "dot", sandbox: `{"read": ["/opt/./tool"]}`, wantErr: "clean path"},
		{name: "doubled slash", sandbox: `{"read": ["/opt//tool"]}`, wantErr: "clean path"},
		{name: "trailing slash", sandbox: `{"read": ["~/x/"]}`, wantErr: "clean path"},
		{name: "wildcard", sandbox: `{"read": ["~/.config/*"]}`, wantErr: "wildcards"},
		{name: "backslash", sandbox: `{"read": ["~\\x"]}`, wantErr: "wildcards and backslashes"},
		{name: "newline", sandbox: `{"read": ["~/x\ny"]}`, wantErr: "control characters"},
		{name: "unknown variable", sandbox: `{"read": ["${HOME}/x"]}`, wantErr: "unknown variable"},
		{name: "plain dollar variable", sandbox: `{"read": ["$HOME/x"]}`, wantErr: "start with /, ~/ or a variable"},
		{name: "variable in the middle", sandbox: `{"read": ["/opt/${XDG_CONFIG_HOME}"]}`, wantErr: "only expanded at the start"},
		{name: "variable glued to a name", sandbox: `{"read": ["${XDG_CONFIG_HOME}tool"]}`, wantErr: "write ${XDG_CONFIG_HOME}/…"},
		{name: "variable with dot dot", sandbox: `{"read": ["${XDG_CONFIG_HOME}/../x"]}`, wantErr: "clean path"},
		{name: "unclosed variable", sandbox: `{"read": ["${XDG_CONFIG_HOME/x"]}`, wantErr: "unknown variable"},
		{name: "env variable not in run.env", sandbox: `{"read": ["${env:KUBECONFIG}"]}`, wantErr: "KUBECONFIG is not listed in run.env"},
		{name: "env variable with a suffix", sandbox: `{"read": ["${env:KUBECONFIG}/x"]}`, env: `["KUBECONFIG"]`, wantErr: "unknown variable"},
		{name: "env variable in lower case", sandbox: `{"read": ["${env:path}"]}`, wantErr: "unknown variable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := `{"command": "bin/ext", "sandbox": ` + tt.sandbox
			if tt.env != "" {
				run += `, "env": ` + tt.env
			}
			m, err := Parse([]byte(sandboxManifest(run + "}")))
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("refused: %v", err)
			case tt.wantErr != "" && err == nil:
				t.Fatalf("accepted, want an error with %q", tt.wantErr)
			case tt.wantErr != "" && !strings.Contains(err.Error(), tt.wantErr):
				t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
			case err == nil && m.Run.Sandbox == nil:
				t.Fatal("the section is dropped")
			}
		})
	}
}

func TestSandboxState(t *testing.T) {
	for run, want := range map[string]SandboxState{
		`{"command": "bin/ext"}`:                               SandboxUndeclared,
		`{"command": "bin/ext", "sandbox": {}}`:                SandboxDeclared,
		`{"command": "bin/ext", "sandbox": {"network": true}}`: SandboxDeclared,
		`{"command": "bin/ext", "sandbox": {"mode": "none"}}`:  SandboxDeclaredNone,
	} {
		m, err := Parse([]byte(sandboxManifest(run)))
		if err != nil {
			t.Fatalf("%s: %v", run, err)
		}
		if got := m.SandboxState(); got != want {
			t.Errorf("%s: state %d, want %d", run, got, want)
		}
		if m.Run.Sandbox.Unconfined() != (want == SandboxDeclaredNone) {
			t.Errorf("%s: Unconfined = %v", run, m.Run.Sandbox.Unconfined())
		}
	}
	// A manifest written before the field existed parses as before, and one
	// with only data has nothing to confine.
	m, err := Parse([]byte(`{"contract": 1, "id": "acme.theme", "name": "T", "version": "1.0.0", "contributes": {"themes": [{"id": "acme.theme.dark", "label": "D", "base": "dark", "colors": {"--bg": "#000000"}}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if m.SandboxState() != SandboxNoProgram || m.Run.Sandbox != nil {
		t.Errorf("data only: state %d", m.SandboxState())
	}
}
