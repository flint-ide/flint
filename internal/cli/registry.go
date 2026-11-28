package cli

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

var (
	wordRe = regexp.MustCompile(`^[a-z][a-z-]{0,23}$`)
	flagRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,23}$`)
)

// reserved flags every command has.
var reserved = []string{"json", "help", "folder"}

// Registry is the commands of a running Flint.
type Registry struct {
	cmds []Command
	// waiting holds a slot per command that waits (MaxWaiting).
	waiting chan struct{}
}

// NewRegistry checks the commands and keeps them in order: a path once, a
// known group, a Kind, flags that are well-formed and not reserved, optional
// arguments last, a Run.
func NewRegistry(cmds []Command) (*Registry, error) {
	seen := map[string]bool{}
	for _, c := range cmds {
		if err := check(c); err != nil {
			return nil, fmt.Errorf("cli: %q: %w", c.Path, err)
		}
		if seen[c.Path] {
			return nil, fmt.Errorf("cli: %q declared twice", c.Path)
		}
		seen[c.Path] = true
	}
	return &Registry{cmds: slices.Clone(cmds), waiting: make(chan struct{}, MaxWaiting)}, nil
}

func check(c Command) error {
	w := c.Words()
	if len(w) != 2 || strings.Join(w, " ") != c.Path {
		return fmt.Errorf("a path is two words separated by one space")
	}
	for _, x := range w {
		if !wordRe.MatchString(x) {
			return fmt.Errorf("invalid word %q", x)
		}
	}
	if !slices.Contains(Groups, w[0]) {
		return fmt.Errorf("unknown group %q (cli.Groups)", w[0])
	}
	if c.Kind != Reads && c.Kind != Acts {
		return fmt.Errorf("Kind must be Reads or Acts")
	}
	if c.Agents != AgentRefused && (c.Kind != Acts || c.Human || c.Agents != AgentAsks) {
		return fmt.Errorf("only a command that acts and is no human decision may let agents ask")
	}
	if c.AgentCheck != nil && c.Agents != AgentAsks {
		return fmt.Errorf("an AgentCheck needs Agents: AgentAsks")
	}
	if c.Run == nil || c.Summary == "" {
		return fmt.Errorf("Run and Summary are required")
	}
	if c.Human != (c.Ask != nil) || c.Human && c.Kind != Acts {
		return fmt.Errorf("a human decision acts and has an Ask, and only it has one")
	}
	names := map[string]bool{}
	for _, f := range c.Flags {
		if !flagRe.MatchString(f.Name) || slices.Contains(reserved, f.Name) || strings.HasSuffix(f.Name, "-file") || names[f.Name] {
			return fmt.Errorf("invalid or repeated flag %q", f.Name)
		}
		if f.File && f.Value == "" {
			return fmt.Errorf("the switch --%s cannot be read from a file", f.Name)
		}
		names[f.Name] = true
	}
	optional := false
	for _, a := range c.Args {
		if a.Optional {
			optional = true
		} else if optional {
			return fmt.Errorf("a required argument after an optional one")
		}
	}
	return nil
}

// Commands are the registered commands in order.
func (r *Registry) Commands() []Command { return slices.Clone(r.cmds) }

// find is the command named by the first two words of args.
func (r *Registry) find(group, name string) (Command, bool) {
	i := slices.IndexFunc(r.cmds, func(c Command) bool { return c.Path == group+" "+name })
	if i < 0 {
		return Command{}, false
	}
	return r.cmds[i], true
}

// inGroup are the group's commands in order.
func (r *Registry) inGroup(group string) []Command {
	var out []Command
	for _, c := range r.cmds {
		if c.Group() == group {
			out = append(out, c)
		}
	}
	return out
}

// Help is `flint help`: every command with its summary, by group.
func (r *Registry) Help() string {
	var b strings.Builder
	b.WriteString("usage: flint <command> [arguments] [--json]\n")
	b.WriteString("       flint [folder]   open a folder in Flint\n")
	for _, g := range Groups {
		cmds := r.inGroup(g)
		if len(cmds) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n%s:\n", groupTitles[g])
		writeSummaries(&b, cmds)
	}
	b.WriteString(actsFooter)
	b.WriteString("flint <group> --help and flint <command> --help say more.\n")
	b.WriteString("Exit status: 0 ok, 1 failed, 2 wrong usage, 3 the run needs you, 4 timed out (run wait).\n")
	return b.String()
}

// GroupHelp is `flint <group> --help`.
func (r *Registry) GroupHelp(group string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s:\n", groupTitles[group])
	cmds := r.inGroup(group)
	if len(cmds) == 0 {
		b.WriteString("  (switched off in Settings ▸ Extensions ▸ Built-in)\n")
		return b.String()
	}
	writeSummaries(&b, cmds)
	b.WriteString(actsFooter)
	return b.String()
}

func writeSummaries(b *strings.Builder, cmds []Command) {
	rows := make([][]string, len(cmds))
	for i, c := range cmds {
		mark := " "
		if c.Kind == Acts {
			mark = "*"
		}
		rows[i] = []string{"  " + c.Path + mark, c.Summary}
	}
	b.WriteString(Table(nil, rows))
}

// Usage is the command's usage line.
func Usage(c Command) string {
	parts := []string{"flint", c.Path}
	for _, a := range c.Args {
		if a.Optional {
			parts = append(parts, "[<"+a.Name+">]")
		} else {
			parts = append(parts, "<"+a.Name+">")
		}
	}
	for _, f := range c.Flags {
		s := "--" + f.Name
		if f.Value != "" {
			s += " " + f.Value
		}
		if f.File {
			s = "(" + s + " | --" + f.Name + "-file <path|->)"
		}
		if !f.Required {
			s = "[" + s + "]"
		}
		parts = append(parts, s)
	}
	if c.Folder {
		parts = append(parts, "[--folder <path>]")
	}
	parts = append(parts, "[--json]")
	return "usage: " + strings.Join(parts, " ")
}

// CommandHelp is `flint <command> --help`.
func CommandHelp(c Command) string {
	var b strings.Builder
	b.WriteString(Usage(c) + "\n\n" + c.Summary + "\n")
	if c.Help != "" {
		b.WriteString("\n" + strings.TrimRight(c.Help, "\n") + "\n")
	}
	var rows [][]string
	for _, f := range c.Flags {
		name := "--" + f.Name
		if f.Value != "" {
			name += " " + f.Value
		}
		rows = append(rows, []string{"  " + name, f.Usage})
		if f.File {
			rows = append(rows, []string{"  --" + f.Name + "-file <path|->", "read --" + f.Name + " from a file, - for standard input"})
		}
	}
	if c.Folder {
		rows = append(rows, []string{"  --folder <path>", "the workspace folder (default: the current directory)"})
	}
	rows = append(rows, []string{"  --json", "print JSON instead of text"})
	b.WriteString("\n" + Table(nil, rows))
	if c.Human {
		b.WriteString("\nOnly you decide this: Flint asks you first in a dialog in its window (Cancel is the default),\n" +
			"and the command waits for your answer. Declined, it changes nothing and exits 1 (\"" + NotApproved + "\").\n" +
			"Agents cannot run it, nor can a process that is not in a terminal.\n")
	} else if c.Kind == Acts && c.Agents == AgentAsks {
		b.WriteString("\nThis command changes something. An agent you run outside Flint can run it only after you allow\n" +
			"that one call in Flint's own dialog (Cancel is the default); agents Flint started cannot run it,\n" +
			"nor can a process that is not in a terminal.\n")
	} else if c.Kind == Acts {
		b.WriteString("\nThis command changes something: agents cannot run it, nor can a process that is not in a terminal.\n")
	}
	return b.String()
}

// actsFooter explains the mark of a command that acts in the lists.
const actsFooter = "\n* changes something: an agent outside Flint runs it only after you allow it in Flint (some never).\n"
