// Package runconfig loads, validates and saves a project's run configurations
// (`.flint/run.json`, meant to be committed like `.idea/runConfigurations` or
// `.vscode/launch.json`) and detects runnable targets nobody has saved yet:
// Go main packages, Makefile targets and package.json scripts.
package runconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"flint/internal/config"
)

// Location of the configurations inside a project.
const (
	Dir      = ".flint"
	FileName = "run.json"
)

// Version is the file format written by this package.
const Version = 1

// maxFileSize guards against loading something that is clearly not a config file.
const maxFileSize = 1 << 20

// Type selects how a configuration is turned into processes.
type Type string

const (
	// TypeCommand runs a program with arguments, or a shell line when Args is empty.
	TypeCommand Type = "command"
	// TypeGo builds a Go package (or file) and runs the binary.
	TypeGo Type = "go"
	// TypeNpm runs a package.json script with the project's package manager.
	TypeNpm Type = "npm"
	// TypeMake runs a Makefile target.
	TypeMake Type = "make"
)

// Config is one run configuration. Fields not used by its Type stay empty.
type Config struct {
	// ID is stable across renames; derived from Name when missing in the file.
	ID   string `json:"id"`
	Name string `json:"name"`
	Type Type   `json:"type"`
	// Cwd is relative to the project root ("" = the root).
	Cwd string            `json:"cwd,omitempty"`
	Env map[string]string `json:"env,omitempty"`
	// Before is a shell line run first (e.g. a build); the run stops when it fails.
	Before string `json:"before,omitempty"`

	// Command (TypeCommand): the program, or a whole shell line when Args is empty.
	Command string `json:"command,omitempty"`
	// Args are the program's arguments (TypeCommand, TypeGo, TypeNpm).
	Args []string `json:"args,omitempty"`

	// Package (TypeGo): "./cmd/app", "." or a single file such as "main.go".
	Package    string   `json:"package,omitempty"`
	BuildFlags []string `json:"buildFlags,omitempty"`

	// Script (TypeNpm): the package.json script to run.
	Script string `json:"script,omitempty"`
	// Target (TypeMake): the Makefile target ("" = the default one).
	Target string `json:"target,omitempty"`

	// Debug tunes debugging (Go via `dlv dap`, see DebugAPI); optional.
	Debug *Debug `json:"debug,omitempty"`
}

// Debug describes how a debugger would launch the configuration.
type Debug struct {
	// Adapter names the DAP adapter, e.g. "dlv" (`dlv dap`).
	Adapter     string `json:"adapter,omitempty"`
	StopOnEntry bool   `json:"stopOnEntry,omitempty"`
	// Args are extra adapter arguments.
	Args []string `json:"args,omitempty"`
}

// File is the on-disk document.
type File struct {
	Version        int      `json:"version"`
	Configurations []Config `json:"configurations"`
}

// Capabilities tell the UI what a configuration supports.
type Capabilities struct {
	Run bool `json:"run"`
	// Debug: a DAP adapter exists for the type (Delve for Go).
	Debug bool `json:"debug"`
}

// CapabilitiesOf reports what can be done with c.
func CapabilitiesOf(c Config) Capabilities {
	return Capabilities{Run: true, Debug: c.Type == TypeGo}
}

// Path returns the run configuration file of a project.
func Path(root string) string { return filepath.Join(root, Dir, FileName) }

// Load reads the project's configurations. A missing file yields none.
func Load(root string) ([]Config, error) {
	path := Path(root)
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return []Config{}, nil
	}
	if err != nil {
		return nil, err
	}
	if info.Size() > maxFileSize {
		return nil, fmt.Errorf("%s is larger than %d bytes", path, maxFileSize)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

// Parse decodes and validates a run.json document. Unknown fields are errors so
// typos in a hand-edited file don't silently do nothing.
func Parse(data []byte) ([]Config, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var f File
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("%s: %w", FileName, err)
	}
	if f.Version > Version {
		return nil, fmt.Errorf("%s: version %d is newer than this Flint understands (%d)", FileName, f.Version, Version)
	}
	configs := normalizeAll(f.Configurations)
	if err := ValidateAll(configs); err != nil {
		return nil, fmt.Errorf("%s: %w", FileName, err)
	}
	return configs, nil
}

// Save validates configs and writes them to the project's run.json.
func Save(root string, configs []Config) error {
	normalized := normalizeAll(configs)
	if err := ValidateAll(normalized); err != nil {
		return err
	}
	data, err := json.MarshalIndent(File{Version: Version, Configurations: normalized}, "", "  ")
	if err != nil {
		return err
	}
	// A committed project file: world-readable like the rest of the work tree.
	if err := os.MkdirAll(filepath.Join(root, Dir), 0o755); err != nil {
		return err
	}
	return config.WriteFileAtomic(Path(root), append(data, '\n'), 0o644)
}

// normalizeAll trims fields and fills in missing ids, keeping them unique.
func normalizeAll(configs []Config) []Config {
	out := make([]Config, 0, len(configs))
	used := map[string]bool{}
	for _, c := range configs {
		n := normalize(c)
		if IsDetected(n.ID) {
			n.ID = "" // a detected configuration saved for the first time gets its own id
		}
		if n.ID == "" {
			n.ID = uniqueID(Slug(n.Name), used)
		}
		used[n.ID] = true
		out = append(out, n)
	}
	return out
}

func normalize(c Config) Config {
	n := c
	n.ID = strings.TrimSpace(c.ID)
	n.Name = strings.TrimSpace(c.Name)
	n.Type = Type(strings.TrimSpace(string(c.Type)))
	n.Cwd = strings.TrimSpace(c.Cwd)
	n.Before = strings.TrimSpace(c.Before)
	n.Command = strings.TrimSpace(c.Command)
	n.Package = strings.TrimSpace(c.Package)
	n.Script = strings.TrimSpace(c.Script)
	n.Target = strings.TrimSpace(c.Target)
	if len(c.Env) == 0 {
		n.Env = nil
	}
	if len(c.Args) == 0 {
		n.Args = nil
	}
	if len(c.BuildFlags) == 0 {
		n.BuildFlags = nil
	}
	return n
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// Slug turns a name into an id: lower case, dashes, never empty.
func Slug(name string) string {
	s := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if s == "" {
		return "config"
	}
	return s
}

func uniqueID(base string, used map[string]bool) string {
	id := base
	for i := 2; used[id]; i++ {
		id = fmt.Sprintf("%s-%d", base, i)
	}
	return id
}

var (
	envKey   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	makeName = regexp.MustCompile(`^[A-Za-z0-9_./-]+$`)
)

// ValidateAll checks every configuration and that ids are unique.
func ValidateAll(configs []Config) error {
	seen := map[string]bool{}
	var errs []error
	for i, c := range configs {
		if err := Validate(c); err != nil {
			errs = append(errs, fmt.Errorf("configuration %d (%q): %w", i+1, c.Name, err))
			continue
		}
		if seen[c.ID] {
			errs = append(errs, fmt.Errorf("configuration %d (%q): duplicate id %q", i+1, c.Name, c.ID))
		}
		seen[c.ID] = true
	}
	return errors.Join(errs...)
}

// Validate checks one configuration.
func Validate(c Config) error {
	if c.ID == "" {
		return errors.New("id is required")
	}
	if c.Name == "" {
		return errors.New("name is required")
	}
	if err := validateCwd(c.Cwd); err != nil {
		return err
	}
	for k := range c.Env {
		if !envKey.MatchString(k) {
			return fmt.Errorf("invalid environment variable name %q", k)
		}
	}
	if c.Debug != nil && c.Debug.Adapter != "" && c.Debug.Adapter != "dlv" {
		return fmt.Errorf("unknown debug adapter %q (supported: dlv)", c.Debug.Adapter)
	}
	switch c.Type {
	case TypeCommand:
		if c.Command == "" {
			return errors.New("command is required")
		}
	case TypeGo:
		if c.Package == "" {
			return errors.New("package is required (e.g. ./cmd/app or main.go)")
		}
		if filepath.IsAbs(c.Package) || escapes(c.Package) {
			return fmt.Errorf("package %q must be relative to the working directory", c.Package)
		}
	case TypeNpm:
		if c.Script == "" {
			return errors.New("script is required")
		}
	case TypeMake:
		if c.Target != "" && !makeName.MatchString(c.Target) {
			return fmt.Errorf("invalid make target %q", c.Target)
		}
	case "":
		return errors.New("type is required")
	default:
		return fmt.Errorf("unknown type %q (command, go, npm, make)", c.Type)
	}
	return nil
}

// validateCwd keeps the working directory inside the project, so a committed
// file behaves the same in every checkout.
func validateCwd(cwd string) error {
	if cwd == "" {
		return nil
	}
	if filepath.IsAbs(cwd) {
		return fmt.Errorf("cwd %q must be relative to the project root", cwd)
	}
	if escapes(cwd) {
		return fmt.Errorf("cwd %q points outside the project", cwd)
	}
	return nil
}

func escapes(rel string) bool {
	clean := filepath.Clean(rel)
	return clean == ".." || strings.HasPrefix(clean, "../")
}
