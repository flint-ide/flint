package lsp

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCheckArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string // error substring; "" means valid
	}{
		{"none", nil, ""},
		{"flags", []string{"-remote=auto", "--log-level", "4"}, ""},
		{"empty", []string{""}, "empty"},
		{"newline", []string{"a\nb"}, "newline"},
		{"too long", []string{strings.Repeat("x", MaxArgLen+1)}, "longer"},
		{"too many", make([]string, MaxArgs+1), "too many"},
		{"tsserver path", []string{"--tsserver-path=/repo/node_modules/typescript/lib"}, "own TypeScript"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckArgs(tt.args)
			if tt.want == "" {
				if err != nil {
					t.Errorf("CheckArgs() error = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("CheckArgs() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestCommandFor(t *testing.T) {
	bins := map[string]string{
		"gopls":                   "/bin/gopls",
		"pyright-langserver":      "/bin/pyright-langserver",
		"/opt/pyright-langserver": "/opt/pyright-langserver",
		"/opt/my-go-server":       "/opt/my-go-server",
	}
	trusted := true
	cfg := map[string]ServerConfig{}
	m := NewManager()
	m.lookPath = func(name string) (string, error) {
		if p, ok := bins[name]; ok {
			return p, nil
		}
		return "", os.ErrNotExist
	}
	m.Trusted = func(string) bool { return trusted }
	m.Config = func(language string) ServerConfig { return cfg[language] }
	goKey, pyKey := key{root: "/w", language: "go"}, key{root: "/w", language: "python"}

	tests := []struct {
		name    string
		trusted bool
		key     key
		cfg     ServerConfig
		want    []string
		wantErr error  // errors.Is target
		errText string // or a substring
	}{
		{"default", true, goKey, ServerConfig{}, []string{"/bin/gopls"}, nil, ""},
		{"extra args", true, goKey, ServerConfig{Args: []string{"-remote=auto"}}, []string{"/bin/gopls", "-remote=auto"}, nil, ""},
		{"extra args dropped when untrusted", false, goKey, ServerConfig{Args: []string{"-remote=auto"}}, []string{"/bin/gopls"}, nil, ""},
		{"disabled", true, goKey, ServerConfig{Disabled: true}, nil, ErrDisabled, ""},
		{"known server by path keeps its args", true, pyKey, ServerConfig{Command: "/opt/pyright-langserver"}, []string{"/opt/pyright-langserver", "--stdio"}, nil, ""},
		{"gated language stays restricted", false, pyKey, ServerConfig{Command: "/opt/pyright-langserver"}, nil, ErrRestricted, ""},
		{"custom server trusted", true, goKey, ServerConfig{Command: "/opt/my-go-server", Args: []string{"serve"}}, []string{"/opt/my-go-server", "serve"}, nil, ""},
		{"custom server untrusted", false, goKey, ServerConfig{Command: "/opt/my-go-server"}, nil, ErrRestricted, ""},
		{"relative command", true, goKey, ServerConfig{Command: "bin/gopls"}, nil, nil, "set in Settings"},
		{"missing command", true, goKey, ServerConfig{Command: "/opt/none"}, nil, nil, "set in Settings"},
		{"invalid args", true, goKey, ServerConfig{Args: []string{"a\nb"}}, nil, nil, "arguments"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trusted = tt.trusted
			cfg[tt.key.language] = tt.cfg
			got, err := m.commandFor(tt.key)
			switch {
			case tt.wantErr != nil:
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("commandFor() error = %v, want %v", err, tt.wantErr)
				}
			case tt.errText != "":
				if err == nil || !strings.Contains(err.Error(), tt.errText) {
					t.Errorf("commandFor() error = %v, want containing %q", err, tt.errText)
				}
			case err != nil || !reflect.DeepEqual(got, tt.want):
				t.Errorf("commandFor() = %q, %v, want %q", got, err, tt.want)
			}
		})
	}
}

// A changed setting restarts the language's server for its open documents;
// disabling it stops the server.
func TestReconfigureRestartsServer(t *testing.T) {
	dir := t.TempDir()
	starts := filepath.Join(dir, "starts")
	server := filepath.Join(dir, "gopls")
	if err := os.WriteFile(server, []byte("#!/bin/sh\necho x >> "+starts+"\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	disabled := false
	m := NewManager()
	m.lookPath = func(name string) (string, error) {
		if name == "gopls" {
			return server, nil
		}
		return "", os.ErrNotExist
	}
	m.Config = func(string) ServerConfig { return ServerConfig{Disabled: disabled} }
	defer m.Shutdown()
	count := func() int {
		data, _ := os.ReadFile(starts)
		return strings.Count(string(data), "x")
	}

	root := t.TempDir()
	m.Open(root, filepath.Join(root, "main.go"), "package main")
	waitFor(t, func() bool { return count() == 1 && hasState(m, "initialize") })
	m.Reconfigure("go")
	waitFor(t, func() bool { return count() == 2 })
	m.Reconfigure("python") // other languages are left alone
	disabled = true
	m.Reconfigure("go")
	waitFor(t, func() bool { return hasState(m, "disabled") })
	if n := count(); n != 2 {
		t.Errorf("server started %d times, want 2", n)
	}
	for _, s := range m.Statuses() {
		if s.Language == "go" && s.State != StateUnavailable {
			t.Errorf("disabled server state = %q, want %q", s.State, StateUnavailable)
		}
	}
}

// A link to typescript-language-server under another name is still pinned to
// its own TypeScript, so the project's never loads.
func TestRenamedTypeScriptServerIsStillPinned(t *testing.T) {
	prefix := t.TempDir()
	cli := filepath.Join(prefix, "node_modules", "typescript-language-server", "lib", "cli.mjs")
	if err := os.MkdirAll(filepath.Dir(cli), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cli, []byte("//"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(prefix, "tsls")
	if err := os.Symlink(cli, link); err != nil {
		t.Fatal(err)
	}
	if _, err := initializationOptions(link); err == nil {
		t.Fatal("renamed typescript-language-server without its own TypeScript allowed")
	}
}

func TestLanguages(t *testing.T) {
	var python LanguageInfo
	for _, l := range Languages() {
		if l.Language == "python" {
			python = l
		}
	}
	if !python.NeedsTrust || len(python.Servers) == 0 || python.Servers[0] != "basedpyright-langserver" {
		t.Errorf("python = %+v", python)
	}
	if !KnownLanguage("go") || KnownLanguage("cobol") {
		t.Error("KnownLanguage misclassifies languages")
	}
}
