package runconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseValid(t *testing.T) {
	configs, err := Parse([]byte(`{
		"version": 1,
		"configurations": [
			{"name": "Server", "type": "go", "package": "./cmd/server", "args": ["-port", "8080"], "env": {"DEBUG": "1"}, "debug": {"adapter": "dlv"}},
			{"id": "lint", "name": "Lint", "type": "command", "command": "go vet ./...", "before": "make gen"},
			{"name": "Server", "type": "make", "target": "build"}
		]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(configs) != 3 {
		t.Fatalf("got %d configs", len(configs))
	}
	if configs[0].ID != "server" || configs[2].ID != "server-2" {
		t.Errorf("ids not derived/unique: %q %q", configs[0].ID, configs[2].ID)
	}
	if configs[1].ID != "lint" {
		t.Errorf("explicit id lost: %q", configs[1].ID)
	}
	if configs[0].Debug == nil || configs[0].Debug.Adapter != "dlv" {
		t.Errorf("debug shape lost: %+v", configs[0].Debug)
	}
	if !CapabilitiesOf(configs[0]).Debug || CapabilitiesOf(configs[1]).Debug {
		t.Error("only go configurations are debuggable")
	}
}

func TestParseRejects(t *testing.T) {
	cases := map[string]string{
		"unknown field":   `{"configurations":[{"name":"a","type":"command","comand":"x"}]}`,
		"unknown type":    `{"configurations":[{"name":"a","type":"rust"}]}`,
		"missing command": `{"configurations":[{"name":"a","type":"command"}]}`,
		"missing name":    `{"configurations":[{"type":"command","command":"ls"}]}`,
		"absolute cwd":    `{"configurations":[{"name":"a","type":"command","command":"ls","cwd":"/etc"}]}`,
		"escaping cwd":    `{"configurations":[{"name":"a","type":"command","command":"ls","cwd":"../x"}]}`,
		"bad env":         `{"configurations":[{"name":"a","type":"command","command":"ls","env":{"A=B":"1"}}]}`,
		"go no package":   `{"configurations":[{"name":"a","type":"go"}]}`,
		"go abs package":  `{"configurations":[{"name":"a","type":"go","package":"/x"}]}`,
		"npm no script":   `{"configurations":[{"name":"a","type":"npm"}]}`,
		"bad make target": `{"configurations":[{"name":"a","type":"make","target":"x; rm"}]}`,
		"duplicate id":    `{"configurations":[{"id":"x","name":"a","type":"make"},{"id":"x","name":"b","type":"make"}]}`,
		"future version":  `{"version":9,"configurations":[]}`,
		"bad debugger":    `{"configurations":[{"name":"a","type":"go","package":".","debug":{"adapter":"gdb"}}]}`,
		"not json":        `{`,
	}
	for name, doc := range cases {
		if _, err := Parse([]byte(doc)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	root := t.TempDir()
	if got, err := Load(root); err != nil || len(got) != 0 {
		t.Fatalf("missing file: %v, %v", got, err)
	}
	in := []Config{
		{ID: DetectedPrefix + "go:.", Name: " App ", Type: TypeGo, Package: ".", Env: map[string]string{}},
		{Name: "Tests", Type: TypeCommand, Command: "go", Args: []string{"test", "./..."}},
	}
	if err := Save(root, in); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(Path(root))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("perm = %v, want 0644 (committed file)", info.Mode().Perm())
	}
	out, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || out[0].ID != "app" || out[0].Name != "App" || out[0].Env != nil || out[1].ID != "tests" {
		t.Errorf("round trip: %+v", out)
	}
	data, _ := os.ReadFile(Path(root))
	if !strings.Contains(string(data), `"version": 1`) {
		t.Errorf("version not written: %s", data)
	}
	if err := Save(root, []Config{{Name: "bad", Type: "nope"}}); err == nil {
		t.Error("invalid configs must not be saved")
	}
}

func TestResolve(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "web"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "web", "pnpm-lock.yaml"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	l, err := Resolve(root, Config{ID: "a", Name: "My App", Type: TypeGo, Package: "./cmd/app", BuildFlags: []string{"-race"}, Args: []string{"serve"}, Before: "make gen", Env: map[string]string{"B": "2", "A": "1"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Steps) != 3 || l.Steps[0].Label != "make gen" || l.Steps[1].Program != "go" {
		t.Fatalf("steps: %+v", l.Steps)
	}
	if got := strings.Join(l.Steps[1].Args, " "); got != "build -race -o "+l.Go.Binary+" ./cmd/app" {
		t.Errorf("build args: %s", got)
	}
	if l.Steps[2].Program != l.Go.Binary || strings.Join(l.Steps[2].Args, " ") != "serve" {
		t.Errorf("run step: %+v", l.Steps[2])
	}
	if strings.Join(l.Env, ",") != "A=1,B=2" || l.Cwd != root {
		t.Errorf("env/cwd: %v %s", l.Env, l.Cwd)
	}

	l, err = Resolve(root, Config{ID: "w", Name: "dev", Type: TypeNpm, Script: "dev", Cwd: "web", Args: []string{"--port", "3000"}})
	if err != nil {
		t.Fatal(err)
	}
	if l.Steps[0].Label != "pnpm run dev --port 3000" {
		t.Errorf("npm label: %s", l.Steps[0].Label)
	}

	l, err = Resolve(root, Config{ID: "s", Name: "sh", Type: TypeCommand, Command: "echo 'hi' | wc -c"})
	if err != nil {
		t.Fatal(err)
	}
	if l.Steps[0].Args[0] != "-c" || l.Steps[0].Args[1] != "echo 'hi' | wc -c" {
		t.Errorf("shell line: %+v", l.Steps[0])
	}

	if _, err := Resolve(root, Config{ID: "x", Name: "x", Type: TypeMake, Cwd: "missing"}); err == nil {
		t.Error("missing cwd should fail")
	}
}

func TestQuote(t *testing.T) {
	for in, want := range map[string]string{"plain": "plain", "": "''", "a b": "'a b'", "it's": `'it'\''s'`, "./x=1": "./x=1"} {
		if got := Quote(in); got != want {
			t.Errorf("Quote(%q) = %s, want %s", in, got, want)
		}
	}
}
