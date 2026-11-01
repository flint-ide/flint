package runconfig

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"flint/internal/shellenv"
)

// Command is one process of a launch.
type Command struct {
	// Label is the command line as shown above its output.
	Label   string   `json:"label"`
	Program string   `json:"program"`
	Args    []string `json:"args"`
}

// Launch is a resolved configuration: the processes to run in order (a failing
// one stops the rest), where and with what environment.
type Launch struct {
	Steps []Command `json:"steps"`
	// Cwd is absolute.
	Cwd string `json:"cwd"`
	// Env holds KEY=VALUE pairs added to the inherited environment.
	Env []string `json:"env"`
	// Go is set for TypeGo, so a debugger can build and launch the target itself
	// (dlv dap takes the package, build flags and program arguments).
	Go *GoTarget `json:"go,omitempty"`
}

// GoTarget is what a Go configuration builds and runs.
type GoTarget struct {
	Package    string   `json:"package"`
	BuildFlags []string `json:"buildFlags"`
	Args       []string `json:"args"`
	// Binary is where the build step writes the executable.
	Binary string `json:"binary"`
}

// Resolve turns c into the processes to start for the project at root.
func Resolve(root string, c Config) (Launch, error) {
	c = normalize(c)
	if err := Validate(c); err != nil {
		return Launch{}, err
	}
	cwd := filepath.Join(root, c.Cwd)
	if info, err := os.Stat(cwd); err != nil || !info.IsDir() {
		return Launch{}, fmt.Errorf("working directory %s does not exist", cwd)
	}
	launch := Launch{Cwd: cwd, Env: envList(c.Env)}
	if c.Before != "" {
		launch.Steps = append(launch.Steps, Shell(c.Before))
	}
	switch c.Type {
	case TypeCommand:
		if len(c.Args) == 0 {
			launch.Steps = append(launch.Steps, Shell(c.Command))
		} else {
			launch.Steps = append(launch.Steps, NewCommand(c.Command, c.Args...))
		}
	case TypeGo:
		target := GoTarget{
			Package:    c.Package,
			BuildFlags: slices.Clone(c.BuildFlags),
			Args:       slices.Clone(c.Args),
			Binary:     goBinary(root, c),
		}
		build := append(append([]string{"build"}, target.BuildFlags...), "-o", target.Binary, target.Package)
		launch.Steps = append(launch.Steps, NewCommand("go", build...), NewCommand(target.Binary, target.Args...))
		launch.Go = &target
	case TypeNpm:
		launch.Steps = append(launch.Steps, scriptCommand(PackageManager(cwd), c.Script, c.Args))
	case TypeMake:
		var args []string
		if c.Target != "" {
			args = []string{c.Target}
		}
		launch.Steps = append(launch.Steps, NewCommand("make", args...))
	}
	return launch, nil
}

// NewCommand builds a command whose label is its quoted command line.
func NewCommand(program string, args ...string) Command {
	parts := make([]string, 0, len(args)+1)
	for _, a := range append([]string{program}, args...) {
		parts = append(parts, Quote(a))
	}
	return Command{Label: strings.Join(parts, " "), Program: program, Args: args}
}

// Shell runs line with the user's shell (PATH comes from the login shell, see
// internal/shellenv), so pipes, globs and && work as in a terminal.
func Shell(line string) Command {
	return Command{Label: line, Program: shellenv.UserShell(), Args: []string{"-c", line}}
}

// Quote quotes s for a POSIX shell when it needs it.
func Quote(s string) string {
	if s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_./=:,+@%", r))
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// PackageManager picks npm, pnpm, yarn or bun from the lock file in dir.
func PackageManager(dir string) string {
	for _, pm := range []struct{ lock, name string }{
		{"pnpm-lock.yaml", "pnpm"},
		{"yarn.lock", "yarn"},
		{"bun.lockb", "bun"},
		{"bun.lock", "bun"},
	} {
		if _, err := os.Stat(filepath.Join(dir, pm.lock)); err == nil {
			return pm.name
		}
	}
	return "npm"
}

func scriptCommand(pm, script string, args []string) Command {
	cmd := []string{"run", script}
	if len(args) > 0 {
		if pm == "npm" {
			cmd = append(cmd, "--") // npm would take the arguments as its own flags
		}
		cmd = append(cmd, args...)
	}
	return NewCommand(pm, cmd...)
}

func envList(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+env[k])
	}
	return out
}

// goBinary is a stable per-project, per-configuration path, overwritten on each
// run (like GoLand's ___go_build files) so temp space doesn't grow.
func goBinary(root string, c Config) string {
	sum := sha256.Sum256([]byte(root + "\x00" + c.ID))
	return filepath.Join(os.TempDir(), "flint-run", hex.EncodeToString(sum[:6]), Slug(c.Name))
}
