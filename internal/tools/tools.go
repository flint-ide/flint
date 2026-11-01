// Package tools is the list of external programs Flint drives (git, rg,
// agents, cloud CLIs, language servers) and a check of which are installed.
// GitHub needs no program: Flint talks to its API (internal/github).
// One table feeds both the first-run tool check and the bug report, so the
// two never disagree about what Flint needs.
package tools

import (
	"path/filepath"
	"runtime"

	"flint/internal/lsp"
)

// install picks the install command for this system: Homebrew on macOS, the
// Debian/Ubuntu package (or the vendor's npm/snap package) on Linux.
func install(mac, linux string) string {
	return installFor(runtime.GOOS, mac, linux)
}

func installFor(goos, mac, linux string) string {
	if goos == "linux" {
		return linux
	}
	return mac
}

// DocsBase is the user documentation site; Tool.Docs is a page relative to it.
const DocsBase = "https://flint-ide.github.io/flint/"

// Groups, in the order the tool check shows them.
const (
	GroupCore      = "core"
	GroupAgents    = "agents"
	GroupLanguages = "languages"
	GroupInfra     = "infra"
)

// Tool is one external program, or a set of alternatives where any one will do
// (vtsls or typescript-language-server).
type Tool struct {
	ID       string
	Name     string
	Group    string
	Feature  string // what it enables, in the UI's words
	Required bool   // Flint is of little use without it
	Install  string // a command to paste into Terminal
	Docs     string // site page (relative to DocsBase, optionally with #anchor)

	// candidates are tried in order; the first on PATH is reported.
	candidates []candidate
}

// candidate is a binary and the arguments that make it print its version.
// No arguments means the binary isn't run at all: some (language servers)
// have no version flag and would start serving instead.
type candidate struct {
	bin  string
	args []string
}

// lspVersionArgs: how each language server binary prints its version. The
// Pyright servers have no such flag (being found is enough).
var lspVersionArgs = map[string][]string{
	"gopls":                      {"version"},
	"rust-analyzer":              {"--version"},
	"vtsls":                      {"--version"},
	"typescript-language-server": {"--version"},
	"pylsp":                      {"--version"},
}

func one(bin string, args ...string) []candidate { return []candidate{{bin, args}} }

// languageServer reuses the LSP manager's binaries, so the check reports the
// server the editor would actually start.
func languageServer(language string) []candidate {
	var out []candidate
	for _, bin := range lsp.ServerBinaries(language) {
		out = append(out, candidate{bin, lspVersionArgs[bin]})
	}
	return out
}

const (
	langDocs  = "guides/editor-and-navigation.md#language-servers"
	agentDocs = "guides/ai-agents.md#supported-agents"
)

// All is every tool, in display order within its group.
func All() []Tool {
	return []Tool{
		{
			ID: "git", Name: "Git", Group: GroupCore, Required: true,
			Feature: "Commit panel, diffs, blame, history and branches",
			Install: install("xcode-select --install", "sudo apt install git"), Docs: "guides/git-and-github.md",
			candidates: one("git", "--version"),
		},
		{
			ID: "rg", Name: "ripgrep", Group: GroupCore,
			Feature: "Fast project search, Go to File and the TODO list",
			Install: install("brew install ripgrep", "sudo apt install ripgrep"), Docs: "how-to/search-and-replace.md#make-search-faster",
			candidates: one("rg", "--version"),
		},
		{
			ID: "claude", Name: "Claude Code", Group: GroupAgents,
			Feature: "Agent chat, commit messages and merge conflict help",
			Install: install("brew install --cask claude-code", "npm install -g @anthropic-ai/claude-code"), Docs: agentDocs,
			candidates: one("claude", "--version"),
		},
		{
			ID: "codex", Name: "Codex", Group: GroupAgents,
			Feature: "Agent chat, commit messages and merge conflict help",
			Install: install("brew install --cask codex", "npm install -g @openai/codex"), Docs: agentDocs,
			candidates: one("codex", "--version"),
		},
		{
			ID: "gopls", Name: "Go language server", Group: GroupLanguages,
			Feature: "Go completion, navigation and diagnostics",
			Install: "go install golang.org/x/tools/gopls@latest", Docs: langDocs,
			candidates: languageServer("go"),
		},
		{
			ID: "typescript-ls", Name: "TypeScript language server", Group: GroupLanguages,
			Feature: "TypeScript and JavaScript completion, navigation and diagnostics",
			Install: "npm install -g @vtsls/language-server", Docs: langDocs,
			candidates: languageServer("typescript"),
		},
		{
			ID: "python-ls", Name: "Python language server", Group: GroupLanguages,
			Feature: "Python completion, navigation and diagnostics",
			Install: "pipx install basedpyright", Docs: langDocs,
			candidates: languageServer("python"),
		},
		{
			ID: "rust-analyzer", Name: "Rust language server", Group: GroupLanguages,
			Feature: "Rust completion, navigation and diagnostics",
			Install: "rustup component add rust-analyzer", Docs: langDocs,
			candidates: languageServer("rust"),
		},
		{
			ID: "dlv", Name: "Delve", Group: GroupLanguages,
			Feature: "Go debugger",
			Install: "go install github.com/go-delve/delve/cmd/dlv@latest", Docs: "guides/run-debug-test.md#debugger-go",
			candidates: one("dlv", "version"),
		},
		{
			// Only needed to install and run the npm-based language servers.
			ID: "node", Name: "Node.js", Group: GroupLanguages,
			Feature: "Runs the npm-installed TypeScript and Pyright language servers",
			Install: install("brew install node", "sudo apt install nodejs npm"), Docs: langDocs,
			candidates: one("node", "--version"),
		},
		{
			ID: "docker", Name: "Docker", Group: GroupInfra,
			Feature: "Compose up, down and pull (the Docker panel itself talks to the Engine API)",
			Install: install("brew install --cask docker-desktop", "sudo apt install docker.io"), Docs: "guides/docker.md",
			candidates: one("docker", "--version"),
		},
		{
			ID: "kubectl", Name: "kubectl", Group: GroupInfra,
			Feature: "Kubernetes panel",
			Install: install("brew install kubectl", "sudo snap install kubectl --classic"), Docs: "guides/kubernetes.md",
			candidates: one("kubectl", "version", "--client=true"),
		},
		{
			ID: "aws", Name: "AWS CLI", Group: GroupInfra,
			Feature: "AWS profiles panel, login and credential refresh",
			Install: install("brew install awscli", "sudo snap install aws-cli --classic"), Docs: "guides/aws.md",
			candidates: one("aws", "--version"),
		},
	}
}

// Binaries lists the binaries a tool looks for, in order.
func (t Tool) Binaries() []string {
	out := make([]string, len(t.candidates))
	for i, c := range t.candidates {
		out[i] = c.bin
	}
	return out
}

// withBinary puts a configured binary first. It is asked for its version only
// when it has a known tool's name; another program may not take the flag.
func (t Tool) withBinary(bin string) Tool {
	c := candidate{bin: bin}
	for _, known := range t.candidates {
		if known.bin == filepath.Base(bin) {
			c.args = known.args
		}
	}
	t.candidates = append([]candidate{c}, t.candidates...)
	return t
}
