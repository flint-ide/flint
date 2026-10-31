package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestParseVersion(t *testing.T) {
	tests := []struct {
		name, out, version, line string
	}{
		{"git", "git version 2.39.3 (Apple Git-145)\n", "2.39.3", "git version 2.39.3 (Apple Git-145)"},
		{"kubectl", "Client Version: v1.31.0\nKustomize Version: v5.4.2\n", "1.31.0", "Client Version: v1.31.0"},
		{"aws", "aws-cli/2.15.0 Python/3.11.6 Darwin/23.1.0 exe/x86_64\n", "2.15.0", "aws-cli/2.15.0 Python/3.11.6 Darwin/23.1.0 exe/x86_64"},
		{"docker", "Docker version 27.3.1, build ce12230", "27.3.1", "Docker version 27.3.1, build ce12230"},
		{"dlv banner first", "Delve Debugger\nVersion: 1.23.1\nBuild: $Id$\n", "1.23.1", "Version: 1.23.1"},
		{"gopls", "golang.org/x/tools/gopls v0.16.2\n", "0.16.2", "golang.org/x/tools/gopls v0.16.2"},
		{"bare", "2.0.14 (Claude Code)\n", "2.0.14", "2.0.14 (Claude Code)"},
		{"node", "v22.1.0\n", "22.1.0", "v22.1.0"},
		{"prerelease", "codex-cli 0.46.0-alpha.3", "0.46.0-alpha.3", "codex-cli 0.46.0-alpha.3"},
		{"no number", "\n  error: Unknown binary 'rust-analyzer' in official toolchain 'stable-aarch64-apple-darwin'\n", "", "error: Unknown binary 'rust-analyzer' in official toolchain 'stable-aarch64-apple-darwin'"},
		{"empty", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, line := parseVersion(tt.out)
			if v != tt.version || line != tt.line {
				t.Fatalf("parseVersion = %q, %q; want %q, %q", v, line, tt.version, tt.line)
			}
		})
	}
}

func TestCheck(t *testing.T) {
	list := []Tool{
		{ID: "git", Required: true, Docs: "guides/git-and-github.md", candidates: one("git", "--version")},
		{ID: "gh", Docs: "guides/git-and-github.md#pull-requests", candidates: one("gh", "--version")},
		{ID: "ts", candidates: []candidate{{"vtsls", []string{"--version"}}, {"typescript-language-server", []string{"--version"}}}},
		{ID: "py", candidates: []candidate{{"basedpyright-langserver", nil}, {"pyright-langserver", nil}}},
		{ID: "rust-analyzer", candidates: one("rust-analyzer", "--version")},
		{ID: "hang", candidates: one("hang", "--version")},
	}
	installed := map[string]string{
		"git": "git version 2.43.0", "typescript-language-server": "4.3.3",
		"pyright-langserver": "", "rust-analyzer": "error: Unknown binary 'rust-analyzer'", "hang": "",
	}
	lookPath := func(bin string) (string, error) {
		if _, ok := installed[bin]; ok {
			return "/opt/bin/" + bin, nil
		}
		return "", errors.New("not found")
	}
	var ran []string
	run := func(_ context.Context, path string, args []string) (string, error) {
		ran = append(ran, path)
		switch filepath.Base(path) {
		case "rust-analyzer":
			return installed["rust-analyzer"], errors.New("exit status 1")
		case "hang":
			return "", context.DeadlineExceeded
		}
		return installed[filepath.Base(path)], nil
	}
	got := check(context.Background(), list, lookPath, serial(run))

	want := []struct{ state, binary, version string }{
		{StateOK, "git", "2.43.0"},
		{StateMissing, "", ""},
		{StateOK, "typescript-language-server", "4.3.3"}, // the second alternative satisfies the group
		{StateOK, "pyright-langserver", ""},              // found, never run: no version flag
		{StateError, "rust-analyzer", ""},
		{StateError, "hang", ""},
	}
	for i, w := range want {
		s := got[i]
		if s.State != w.state || s.Binary != w.binary || s.Version != w.version {
			t.Errorf("%s: got %s/%q/%q, want %s/%q/%q", s.ID, s.State, s.Binary, s.Version, w.state, w.binary, w.version)
		}
	}
	if got[1].DocsURL != DocsBase+"guides/git-and-github.html#pull-requests" {
		t.Errorf("docs URL %q", got[1].DocsURL)
	}
	if !slices.Equal(got[2].Binaries, []string{"vtsls", "typescript-language-server"}) {
		t.Errorf("binaries %v", got[2].Binaries)
	}
	if !strings.Contains(got[4].Detail, "Unknown binary") || got[5].Detail != context.DeadlineExceeded.Error() {
		t.Errorf("details %q, %q", got[4].Detail, got[5].Detail)
	}
	if slices.Contains(ran, "/opt/bin/pyright-langserver") {
		t.Error("ran a language server that has no version flag")
	}
}

// serial makes the fake safe to call from check's goroutines.
func serial(run runFunc) runFunc {
	ch := make(chan struct{}, 1)
	return func(ctx context.Context, path string, args []string) (string, error) {
		ch <- struct{}{}
		defer func() { <-ch }()
		return run(ctx, path, args)
	}
}

func TestReportLine(t *testing.T) {
	tests := []struct {
		s    Status
		want string
	}{
		{Status{ID: "gh", State: StateMissing}, "gh: not installed"},
		{Status{ID: "git", Binary: "git", State: StateOK, Detail: "git version 2.43.0"}, "git: git version 2.43.0"},
		{Status{ID: "python-ls", Binary: "basedpyright-langserver", State: StateOK}, "python-ls (basedpyright-langserver): installed (version unknown)"},
		{Status{ID: "rust-analyzer", Binary: "rust-analyzer", State: StateError, Detail: "error: x"}, "rust-analyzer: version check failed: error: x"},
	}
	for _, tt := range tests {
		if got := ReportLine(tt.s); got != tt.want {
			t.Errorf("ReportLine = %q, want %q", got, tt.want)
		}
	}
}

func TestRunVersionOutsideTheProject(t *testing.T) {
	// A real process: the version command must not start in the current
	// directory (which may be an untrusted project) and must not read stdin.
	project := t.TempDir()
	t.Chdir(project)
	out, err := runVersion(context.Background(), "/bin/pwd", nil)
	if err != nil {
		t.Fatal(err)
	}
	if dir := strings.TrimSpace(out); dir == project || dir != neutralDir() {
		t.Fatalf("ran in %q, want %q", dir, neutralDir())
	}
}

func TestCappedBuffer(t *testing.T) {
	var b cappedBuffer
	chunk := []byte(strings.Repeat("x", maxOutput/2+1))
	for range 3 {
		if n, err := b.Write(chunk); n != len(chunk) || err != nil {
			t.Fatalf("Write = %d, %v", n, err)
		}
	}
	if b.Len() != maxOutput {
		t.Fatalf("kept %d bytes, want %d", b.Len(), maxOutput)
	}
}

func TestTable(t *testing.T) {
	seen := map[string]bool{}
	required := 0
	for _, tool := range All() {
		if tool.ID == "" || tool.Name == "" || tool.Feature == "" || tool.Install == "" || len(tool.candidates) == 0 {
			t.Errorf("%q: incomplete entry", tool.ID)
		}
		if seen[tool.ID] {
			t.Errorf("%q: duplicate id", tool.ID)
		}
		seen[tool.ID] = true
		if tool.Required {
			required++
		}
		switch tool.Group {
		case GroupCore, GroupAgents, GroupLanguages, GroupInfra:
		default:
			t.Errorf("%q: unknown group %q", tool.ID, tool.Group)
		}
		// Docs point at a real page and heading of the user site.
		page, anchor, _ := strings.Cut(tool.Docs, "#")
		md, err := os.ReadFile(filepath.Join("..", "..", "site", page))
		if err != nil {
			t.Errorf("%q: docs page %q: %v", tool.ID, tool.Docs, err)
		} else if anchor != "" && !slices.Contains(headingIDs(string(md)), anchor) {
			t.Errorf("%q: no heading #%s in %s", tool.ID, anchor, page)
		}
	}
	if required != 1 || !All()[0].Required {
		t.Errorf("only git is required, got %d required", required)
	}
	// Language servers come from the LSP specs, not a second list.
	for _, tool := range All() {
		if tool.ID == "typescript-ls" && !slices.Equal(tool.Binaries(), []string{"vtsls", "typescript-language-server"}) {
			t.Errorf("typescript-ls binaries %v", tool.Binaries())
		}
	}
}

// headingIDs are the anchors the docs site gives "## " and "### " headings
// (goldmark's automatic IDs: lower case, spaces to dashes, punctuation dropped).
func headingIDs(md string) []string {
	var ids []string
	for l := range strings.Lines(md) {
		if !strings.HasPrefix(l, "## ") && !strings.HasPrefix(l, "### ") {
			continue
		}
		var b strings.Builder
		for _, r := range strings.ToLower(strings.TrimSpace(strings.TrimLeft(l, "#"))) {
			switch {
			case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
				b.WriteRune(r)
			case r == ' ':
				b.WriteRune('-')
			}
		}
		ids = append(ids, b.String())
	}
	return ids
}
