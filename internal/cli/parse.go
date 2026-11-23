package cli

import (
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"
)

// Parse reads the words after the command's path (args) and the texts the
// client read from files, against c's declaration. cwd is the client's
// working directory; Call.Folder is --folder resolved against it.
func Parse(c Command, args []string, files map[string]string, cwd string) (*Call, error) {
	call := &Call{flags: map[string]string{}}
	var positional []string
	flagsDone := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if flagsDone || a == "-" || !strings.HasPrefix(a, "-") {
			positional = append(positional, a)
			continue
		}
		if a == "--" {
			flagsDone = true
			continue
		}
		if !strings.HasPrefix(a, "--") {
			return nil, Usagef("unknown option %s (options start with --)", short(a))
		}
		name, value, hasValue := strings.Cut(a[2:], "=")
		f, ok := flagOf(c, name)
		if !ok {
			return nil, Usagef("unknown option --%s", short(name))
		}
		if _, dup := call.flags[name]; dup {
			return nil, Usagef("--%s is given twice", name)
		}
		switch {
		case f.Value == "" && hasValue:
			return nil, Usagef("--%s takes no value", name)
		case f.Value == "":
			value = ""
		case !hasValue:
			if i+1 >= len(args) {
				return nil, Usagef("--%s needs a value %s", name, f.Value)
			}
			i++
			value = args[i]
		}
		call.flags[name] = value
	}
	for name, text := range files {
		f, ok := flagOf(c, name)
		if !ok || !f.File {
			return nil, Usagef("unknown option --%s-file", short(name))
		}
		if _, dup := call.flags[name]; dup {
			return nil, Usagef("give --%s or --%s-file, not both", name, name)
		}
		call.flags[name] = text
	}
	if err := checkValues(c, call.flags); err != nil {
		return nil, err
	}
	if err := checkArgs(c, positional); err != nil {
		return nil, err
	}
	call.Args = positional
	call.JSON = call.Has("json")
	call.Folder = resolveFolder(call.flags["folder"], cwd)
	call.workDir = resolveFolder("", cwd)
	return call, nil
}

// flagOf finds a declared flag or one every command has.
func flagOf(c Command, name string) (Flag, bool) {
	switch name {
	case "json", "help":
		return Flag{Name: name}, true
	case "folder":
		if c.Folder {
			return Flag{Name: name, Value: "<path>", Max: MaxPath}, true
		}
		return Flag{}, false
	}
	i := slices.IndexFunc(c.Flags, func(f Flag) bool { return f.Name == name })
	if i < 0 {
		return Flag{}, false
	}
	return c.Flags[i], true
}

func checkValues(c Command, flags map[string]string) error {
	for name, v := range flags {
		f, _ := flagOf(c, name)
		if n := limit(f.Max); len(v) > n {
			return Usagef("--%s is longer than %d bytes", name, n)
		}
		if !utf8.ValidString(v) {
			return Usagef("--%s is not UTF-8 text", name)
		}
	}
	for _, f := range c.Flags {
		if _, ok := flags[f.Name]; f.Required && !ok {
			if f.File {
				return Usagef("--%s or --%s-file is required", f.Name, f.Name)
			}
			return Usagef("--%s is required", f.Name)
		}
	}
	return nil
}

func checkArgs(c Command, positional []string) error {
	required := 0
	for _, a := range c.Args {
		if !a.Optional {
			required++
		}
	}
	switch {
	case len(positional) < required:
		return Usagef("<%s> is missing", c.Args[len(positional)].Name)
	case len(positional) > len(c.Args):
		return Usagef("unexpected argument %q", short(positional[len(c.Args)]))
	}
	for i, v := range positional {
		if n := limit(c.Args[i].Max); len(v) > n || !utf8.ValidString(v) {
			return Usagef("<%s> is longer than %d bytes or not text", c.Args[i].Name, n)
		}
	}
	return nil
}

func limit(n int) int {
	if n <= 0 {
		return DefaultMaxValue
	}
	return min(n, MaxText)
}

// resolveFolder makes --folder absolute against the client's directory.
func resolveFolder(folder, cwd string) string {
	switch {
	case folder == "":
		folder = cwd
	case !filepath.IsAbs(folder) && cwd != "":
		folder = filepath.Join(cwd, folder)
	}
	if folder == "" || !filepath.IsAbs(folder) {
		return ""
	}
	return filepath.Clean(folder)
}

// Path resolves a path argument (a folder to open, a parent for a clone)
// against the client's directory, like --folder.
func (c *Call) Path(p string) string {
	if p == "" {
		return ""
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return resolveFolder(p, c.workDir)
}

// short keeps an echoed word readable.
func short(s string) string { return Cut(Clean(s), 40) }
