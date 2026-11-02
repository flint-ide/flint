package manifest

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"flint/internal/settings"
)

// ViewKinds are the declarative views Flint draws for panels and tab views.
var ViewKinds = []string{"tree", "list", "table", "form", "markdown", "chart", "cards", "stack"}

// Limits on a manifest's lists and strings.
const (
	maxIDLen       = 64
	maxContributed = 100 // entries per kind
	maxThemes      = 50
	maxArgs        = 64
	maxArgLen      = 4096
	maxTitleLen    = 100
	maxDescLen     = 1000
	maxToolName    = 48
)

var (
	extensionID   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*\.[a-z0-9][a-z0-9-]*$`)
	contributedID = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*$`)
	iconName      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)
	toolName      = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	semver        = regexp.MustCompile(`^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
	hostname      = regexp.MustCompile(`^(?:\*\.)?(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)*[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
)

// problems collects what is wrong with a manifest.
type problems []error

func (p *problems) add(format string, args ...any) { *p = append(*p, fmt.Errorf(format, args...)) }

func (m Manifest) check() error {
	var p problems
	if m.Contract != Contract {
		if m.Contract == 0 {
			p.add("contract: missing; this Flint supports extension contract %d", Contract)
		} else {
			p.add("built for Flint's extension contract %d; this Flint supports %d", m.Contract, Contract)
		}
		return errors.Join(p...) // the rest may mean something else in another contract
	}
	if len(m.ID) > maxIDLen || !extensionID.MatchString(m.ID) {
		p.add("id %q must be publisher.name in lower-case letters, digits and dashes", m.ID)
	}
	checkText(&p, "name", m.Name, maxTitleLen, true)
	if !semver.MatchString(m.Version) {
		p.add("version %q must be a semantic version (1.2.3)", m.Version)
	}
	checkText(&p, "publisher", m.Publisher, maxTitleLen, false)
	checkText(&p, "description", m.Description, maxDescLen, false)
	m.checkRun(&p)
	m.checkPermissions(&p)
	m.checkContributes(&p)
	m.checkActivation(&p)
	return errors.Join(p...)
}

// checkText refuses control characters, invalid UTF-8 and long strings.
func checkText(p *problems, field, s string, max int, required bool) {
	switch {
	case required && strings.TrimSpace(s) == "":
		p.add("%s: missing", field)
	case len(s) > max:
		p.add("%s: longer than %d bytes", field, max)
	case !utf8.ValidString(s) || strings.ContainsFunc(s, unicode.IsControl):
		p.add("%s: contains control characters", field)
	}
}

func (m Manifest) checkRun(p *problems) {
	if m.Run == nil {
		if m.NeedsProgram() {
			p.add("run: missing, but the extension contributes commands, views, status items, file menu entries, agent tools or activation events")
		}
		return
	}
	checkProgram(p, "run", m.Run.Command, m.Run.Args)
	if m.Run.Darwin != nil {
		checkProgram(p, "run.darwin", m.Run.Darwin.Command, m.Run.Darwin.Args)
	}
	if m.Run.Linux != nil {
		checkProgram(p, "run.linux", m.Run.Linux.Command, m.Run.Linux.Args)
	}
}

// checkProgram: the command is a relative path inside the extension's
// directory, written with forward slashes.
func checkProgram(p *problems, field, command string, args []string) {
	clean := path.Clean(command)
	switch {
	case command == "":
		p.add("%s.command: missing", field)
	case strings.ContainsAny(command, "\\\x00") || path.IsAbs(command) || clean != command || clean == "." || clean == ".." || strings.HasPrefix(clean, "../"):
		p.add("%s.command %q must be a clean relative path inside the extension (bin/tool)", field, command)
	}
	if len(args) > maxArgs {
		p.add("%s.args: more than %d arguments", field, maxArgs)
	}
	for _, a := range args {
		if len(a) > maxArgLen || strings.ContainsRune(a, 0) {
			p.add("%s.args: an argument is longer than %d bytes or contains NUL", field, maxArgLen)
			break
		}
	}
}

// Permissions an extension may declare (besides network:<hosts>).
var Permissions = []string{"workspace.read", "secrets", "agent.ask"}

func (m Manifest) checkPermissions(p *problems) {
	seen := map[string]bool{}
	for _, perm := range m.Permissions {
		if seen[perm] {
			p.add("permissions: %q listed twice", perm)
			continue
		}
		seen[perm] = true
		if hosts, ok := strings.CutPrefix(perm, "network:"); ok {
			for _, h := range strings.Split(hosts, ",") {
				if len(h) > 253 || !hostname.MatchString(h) {
					p.add("permissions: %q is not a host name", h)
				}
			}
			continue
		}
		if !slices.Contains(Permissions, perm) {
			p.add("permissions: unknown permission %q", perm)
		}
	}
}

func (m Manifest) checkActivation(p *problems) {
	c := m.Contributes
	commands := ids(c.Commands, func(x Command) string { return x.ID })
	views := append(ids(c.Panels, func(x Panel) string { return x.ID }), ids(c.TabViews, func(x TabView) string { return x.ID })...)
	tools := ids(c.AgentTools, func(x AgentTool) string { return x.Name })
	for _, ev := range m.Activation {
		kind, arg, _ := strings.Cut(ev, ":")
		ok := false
		switch kind {
		case "onStartup":
			ok = arg == "" && ev == "onStartup"
		case "onCommand":
			ok = slices.Contains(commands, arg)
		case "onView":
			ok = slices.Contains(views, arg)
		case "onAgentTool":
			ok = slices.Contains(tools, arg)
		}
		if !ok {
			p.add("activation: %q is not onStartup or onCommand/onView/onAgentTool with a contributed id", ev)
		}
	}
}

func ids[T any](list []T, id func(T) string) []string {
	out := make([]string, 0, len(list))
	for _, x := range list {
		out = append(out, id(x))
	}
	return out
}

func (m Manifest) checkContributes(p *problems) {
	c := m.Contributes
	ownID := func(kind, id string) {
		if len(id) > maxIDLen+40 || !contributedID.MatchString(id) || !strings.HasPrefix(id, m.ID+".") || id == m.ID+"." {
			p.add("%s: id %q must start with %q", kind, id, m.ID+".")
		}
	}
	unique := func(kind string, list []string) {
		if len(list) > maxContributed {
			p.add("%s: more than %d entries", kind, maxContributed)
		}
		seen := map[string]bool{}
		for _, id := range list {
			if seen[id] {
				p.add("%s: %q listed twice", kind, id)
			}
			seen[id] = true
		}
	}
	commands := ids(c.Commands, func(x Command) string { return x.ID })
	unique("commands", commands)
	for _, x := range c.Commands {
		ownID("commands", x.ID)
		checkText(p, "commands."+x.ID+".title", x.Title, maxTitleLen, true)
		checkText(p, "commands."+x.ID+".category", x.Category, 50, false)
		checkText(p, "commands."+x.ID+".keys", x.Keys, 50, false)
	}
	unique("panels", ids(c.Panels, func(x Panel) string { return x.ID }))
	for _, x := range c.Panels {
		ownID("panels", x.ID)
		checkText(p, "panels."+x.ID+".title", x.Title, maxTitleLen, true)
		if x.Icon != "" && !iconName.MatchString(x.Icon) {
			p.add("panels.%s.icon %q is not an icon name", x.ID, x.Icon)
		}
		checkView(p, "panels."+x.ID, x.View)
	}
	unique("statusItems", ids(c.StatusItems, func(x StatusItem) string { return x.ID }))
	for _, x := range c.StatusItems {
		ownID("statusItems", x.ID)
	}
	unique("fileMenu", ids(c.FileMenu, func(x FileMenu) string { return x.ID }))
	for _, x := range c.FileMenu {
		ownID("fileMenu", x.ID)
		checkText(p, "fileMenu."+x.ID+".label", x.Label, maxTitleLen, true)
		if !slices.Contains(commands, x.Command) {
			p.add("fileMenu.%s.command %q is not a contributed command", x.ID, x.Command)
		}
	}
	unique("tabViews", ids(c.TabViews, func(x TabView) string { return x.ID }))
	for _, x := range c.TabViews {
		ownID("tabViews", x.ID)
		checkView(p, "tabViews."+x.ID, x.View)
	}
	checkAgentTools(p, c.AgentTools)
	if c.Settings != nil {
		checkSettings(p, *c.Settings)
	}
	m.checkThemes(p)
}

func checkView(p *problems, field, view string) {
	if view != "" && !slices.Contains(ViewKinds, view) {
		p.add("%s.view %q is not one of %s", field, view, strings.Join(ViewKinds, ", "))
	}
}

func checkAgentTools(p *problems, tools []AgentTool) {
	seen := map[string]bool{}
	if len(tools) > maxContributed {
		p.add("agentTools: more than %d entries", maxContributed)
	}
	for _, t := range tools {
		if len(t.Name) > maxToolName || !toolName.MatchString(t.Name) {
			p.add("agentTools: name %q must be lower-case letters, digits and underscores, at most %d", t.Name, maxToolName)
		}
		if seen[t.Name] {
			p.add("agentTools: %q listed twice", t.Name)
		}
		seen[t.Name] = true
		checkText(p, "agentTools."+t.Name+".title", t.Title, maxTitleLen, false)
		checkText(p, "agentTools."+t.Name+".description", t.Description, maxDescLen, true)
		var schema struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(t.InputSchema, &schema) != nil || schema.Type != "object" {
			p.add("agentTools.%s.inputSchema must be a JSON Schema object with \"type\": \"object\"", t.Name)
		}
	}
}

func (m Manifest) checkThemes(p *problems) {
	themes := m.Contributes.Themes
	if len(themes) > maxThemes {
		p.add("themes: more than %d entries", maxThemes)
	}
	seen := map[string]bool{}
	for _, t := range themes {
		if len(t.ID) > maxIDLen+40 || !contributedID.MatchString(t.ID) || !strings.HasPrefix(t.ID, m.ID+".") {
			p.add("themes: id %q must start with %q", t.ID, m.ID+".")
		}
		if seen[t.ID] {
			p.add("themes: %q listed twice", t.ID)
		}
		seen[t.ID] = true
		checkText(p, "themes."+t.ID+".label", t.Label, settings.MaxThemeNameLen, true)
		if len(t.Colors)+len(t.Terminal) == 0 {
			p.add("themes.%s: no colors", t.ID)
		}
		if err := settings.ValidateCustomTheme(t.CustomTheme()); err != nil {
			p.add("themes.%s: %w", t.ID, err)
		}
	}
}

// CustomTheme is the theme as Settings ▸ Appearance applies it: a custom
// theme whose Source is the theme's id.
func (t Theme) CustomTheme() settings.CustomTheme {
	colors := maps.Clone(t.Colors)
	if colors == nil {
		colors = map[string]string{}
	}
	var terminal map[string]string
	if len(t.Terminal) > 0 {
		terminal = maps.Clone(t.Terminal)
	}
	return settings.CustomTheme{Name: t.Label, Base: t.Base, Colors: colors, Terminal: terminal, Source: t.ID}
}
