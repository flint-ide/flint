package manifest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"flint/internal/settings"
)

// Limits on what one extension contributes.
const (
	maxCommands    = 100
	maxPanels      = 10
	maxStatusItems = 10
	maxAgentTools  = 20
	maxToolSchema  = 16 << 10
)

// Permissions Flint grants on request; network: is declarative.
const (
	PermWorkspaceRead = "workspace.read"
	PermSecrets       = "secrets"
	PermAgentAsk      = "agent.ask"
	PermTerminal      = "terminal"
	permNetwork       = "network:"
)

var (
	// idPattern is publisher.name, lower case.
	idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,30}\.[a-z0-9][a-z0-9-]{0,30}$`)
	// localPattern is what follows "<id>." in a contributed id.
	localPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	versionPattern = regexp.MustCompile(`^\d{1,6}\.\d{1,6}\.\d{1,6}([-+][0-9A-Za-z.-]{1,40})?$`)
	iconPattern    = regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}$`)
	toolPattern    = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)
	keysPattern    = regexp.MustCompile(`^[A-Za-z0-9+\-\[\]\\;',./=` + "`" + `]{1,40}$`)
	hostPattern    = regexp.MustCompile(`^(\*\.)?[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?)*(:\d{1,5})?$`)
)

// ValidID reports whether id is a valid extension id (publisher.name).
func ValidID(id string) bool { return idPattern.MatchString(id) }

func (m *Manifest) check() error {
	if !ValidID(m.ID) {
		return fmt.Errorf("id %q: use publisher.name in lower-case letters, digits and dashes", m.ID)
	}
	for _, f := range []struct {
		name, value string
		max         int
		required    bool
	}{
		{"name", m.Name, 60, true},
		{"publisher", m.Publisher, 60, false},
		{"description", m.Description, 500, false},
	} {
		if err := checkText(f.name, f.value, f.max, f.required); err != nil {
			return err
		}
	}
	if !versionPattern.MatchString(m.Version) {
		return fmt.Errorf("version %q: use major.minor.patch", m.Version)
	}
	if err := m.checkRun(); err != nil {
		return err
	}
	if err := m.checkPermissions(); err != nil {
		return err
	}
	if err := m.checkContributions(); err != nil {
		return err
	}
	return m.checkActivation()
}

func (m *Manifest) checkRun() error {
	if !m.HasProgram() {
		if m.NeedsProgram() {
			return errors.New("run.command is missing, but the extension contributes commands, panels, status items, agent tools or activation events")
		}
		return nil // data only: themes and settings
	}
	if err := checkCommandPath(m.Run.Command); err != nil {
		return err
	}
	if err := checkArgs(m.Run.Args); err != nil {
		return err
	}
	if err := checkRunEnv(m.Run.Env); err != nil {
		return err
	}
	for _, o := range []*RunOS{m.Run.Darwin, m.Run.Linux} {
		if o == nil {
			continue
		}
		if err := checkCommandPath(o.Command); err != nil {
			return err
		}
		if err := checkArgs(o.Args); err != nil {
			return err
		}
	}
	return nil
}

// checkCommandPath: a relative, slash-separated path that stays inside the
// extension's folder.
func checkCommandPath(command string) error {
	switch {
	case command == "":
		return errors.New("run.command is missing")
	case len(command) > 1024 || strings.ContainsFunc(command, unicode.IsControl):
		// The command is shown in the install dialog: no newlines to fake text there.
		return fmt.Errorf("run.command %q: too long or contains control characters", truncate(command, 40))
	case strings.HasPrefix(command, "/") || strings.Contains(command, "\\") || strings.ContainsRune(command, 0):
		return fmt.Errorf("run.command %q: use a relative path inside the extension's folder", command)
	case path.Clean(command) != command || command == ".." || strings.HasPrefix(command, "../"):
		return fmt.Errorf("run.command %q: use a clean relative path inside the extension's folder", command)
	}
	return nil
}

func checkArgs(args []string) error {
	if len(args) > 32 {
		return errors.New("run.args: at most 32 arguments")
	}
	for _, a := range args {
		if len(a) > 1024 || strings.ContainsFunc(a, unicode.IsControl) {
			return fmt.Errorf("run.args: argument %q is not allowed", truncate(a, 40))
		}
	}
	return nil
}

func (m *Manifest) checkPermissions() error {
	seen := map[string]bool{}
	for _, p := range m.Permissions {
		if seen[p] {
			return fmt.Errorf("permission %q is listed twice", p)
		}
		seen[p] = true
		switch {
		case p == PermWorkspaceRead || p == PermSecrets || p == PermAgentAsk || p == PermTerminal:
		case strings.HasPrefix(p, permNetwork):
			for _, h := range strings.Split(strings.TrimPrefix(p, permNetwork), ",") {
				if !hostPattern.MatchString(h) {
					return fmt.Errorf("permission %q: %q is not a host name", p, h)
				}
			}
		default:
			return fmt.Errorf("unknown permission %q (known: workspace.read, secrets, agent.ask, network:<host>)", p)
		}
	}
	return nil
}

// NetworkHosts are the hosts the extension says it talks to.
func (m *Manifest) NetworkHosts() []string {
	var out []string
	for _, p := range m.Permissions {
		if rest, ok := strings.CutPrefix(p, permNetwork); ok {
			out = append(out, strings.Split(rest, ",")...)
		}
	}
	return out
}

func (m *Manifest) checkContributions() error {
	c := m.Contributes
	if err := checkCount("commands", len(c.Commands), maxCommands); err != nil {
		return err
	}
	if err := checkCount("panels", len(c.Panels), maxPanels); err != nil {
		return err
	}
	if err := checkCount("statusItems", len(c.StatusItems), maxStatusItems); err != nil {
		return err
	}
	if err := checkCount("agentTools", len(c.AgentTools), maxAgentTools); err != nil {
		return err
	}
	ids := map[string]bool{}
	own := func(kind, id string) error {
		local, ok := strings.CutPrefix(id, m.ID+".")
		if !ok || !localPattern.MatchString(local) {
			return fmt.Errorf("%s id %q must start with %q, then letters, digits, dots, dashes or underscores", kind, id, m.ID+".")
		}
		if ids[kind+":"+id] {
			return fmt.Errorf("%s id %q is used twice", kind, id)
		}
		ids[kind+":"+id] = true
		return nil
	}
	for _, cmd := range c.Commands {
		if err := own("command", cmd.ID); err != nil {
			return err
		}
		if err := checkText("command title", cmd.Title, 100, true); err != nil {
			return err
		}
		if err := checkText("command category", cmd.Category, 40, false); err != nil {
			return err
		}
		if cmd.Keys != "" && !keysPattern.MatchString(cmd.Keys) {
			return fmt.Errorf("command %s: keys %q is not a shortcut like Mod+Alt+J", cmd.ID, cmd.Keys)
		}
	}
	for _, p := range c.Panels {
		if err := own("panel", p.ID); err != nil {
			return err
		}
		if err := checkText("panel title", p.Title, 40, true); err != nil {
			return err
		}
		if !iconPattern.MatchString(p.Icon) {
			return fmt.Errorf("panel %s: icon %q is not a name from Flint's icon set", p.ID, p.Icon)
		}
		switch p.View {
		case "tree", "list":
		case "webview":
			if err := checkWebview("panel", p.ID, p.Webview); err != nil {
				return err
			}
		default:
			return fmt.Errorf(`panel %s: view %q is not supported (this Flint draws "tree", "list" and "webview")`, p.ID, p.View)
		}
	}
	if err := m.checkTabViews(own); err != nil {
		return err
	}
	for _, s := range c.StatusItems {
		if err := own("status item", s.ID); err != nil {
			return err
		}
	}
	tools := map[string]bool{}
	for _, t := range c.AgentTools {
		if !toolPattern.MatchString(t.Name) {
			return fmt.Errorf("agent tool %q: use lower-case letters, digits and underscores, at most 40", t.Name)
		}
		if tools[t.Name] {
			return fmt.Errorf("agent tool %q is listed twice", t.Name)
		}
		tools[t.Name] = true
		if err := checkText("agent tool title", t.Title, 60, false); err != nil {
			return err
		}
		if err := checkText("agent tool description", t.Description, 1000, true); err != nil {
			return err
		}
		if err := checkToolSchema(t); err != nil {
			return err
		}
	}
	if err := m.checkLanguages(); err != nil {
		return err
	}
	return m.checkThemes(own)
}

// maxThemes bounds the themes of one extension.
const maxThemes = 50

// checkThemes applies the custom theme rules to each theme: Flint's color
// names and hex values only, so nothing else can reach the page's style.
func (m *Manifest) checkThemes(own func(kind, id string) error) error {
	if err := checkCount("themes", len(m.Contributes.Themes), maxThemes); err != nil {
		return err
	}
	for _, t := range m.Contributes.Themes {
		if err := own("theme", t.ID); err != nil {
			return err
		}
		if err := checkText("theme label", t.Label, settings.MaxThemeNameLen, true); err != nil {
			return err
		}
		if len(t.Colors)+len(t.Terminal) == 0 {
			return fmt.Errorf("theme %s has no colors", t.ID)
		}
		if err := settings.ValidateCustomTheme(t.CustomTheme()); err != nil {
			return fmt.Errorf("theme %s: %w", t.ID, err)
		}
	}
	return nil
}

func checkToolSchema(t AgentTool) error {
	if len(t.InputSchema) == 0 || bytes.Equal(t.InputSchema, []byte("null")) {
		return nil
	}
	if len(t.InputSchema) > maxToolSchema {
		return fmt.Errorf("agent tool %s: inputSchema is larger than %d KiB", t.Name, maxToolSchema>>10)
	}
	var s struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(t.InputSchema, &s); err != nil || s.Type != "object" {
		return fmt.Errorf(`agent tool %s: inputSchema must be a JSON Schema with "type": "object"`, t.Name)
	}
	return nil
}

func (m *Manifest) checkActivation() error {
	tools := map[string]bool{}
	for _, t := range m.Contributes.AgentTools {
		tools[t.Name] = true
	}
	for _, ev := range m.Activation {
		kind, arg, _ := strings.Cut(ev, ":")
		var ok bool
		switch kind {
		case "onStartup":
			ok = arg == ""
		case "onCommand":
			_, ok = m.Command(arg)
		case "onView":
			_, tab := m.TabView(arg)
			ok = m.HasPanel(arg) || tab
		case "onAgentTool":
			ok = tools[arg]
		}
		if !ok {
			return fmt.Errorf("activation event %q: use onStartup, or onCommand / onView / onAgentTool with one of the extension's own ids", ev)
		}
	}
	return nil
}

func checkCount(kind string, n, limit int) error {
	if n > limit {
		return fmt.Errorf("contributes.%s: at most %d", kind, limit)
	}
	return nil
}

// checkText: valid UTF-8 without control characters, at most limit runes.
func checkText(name, v string, limit int, required bool) error {
	switch {
	case required && strings.TrimSpace(v) == "":
		return fmt.Errorf("%s is missing", name)
	case !utf8.ValidString(v) || strings.ContainsFunc(v, func(r rune) bool { return r < 0x20 || r == 0x7f }):
		return fmt.Errorf("%s contains control characters", name)
	case utf8.RuneCountInString(v) > limit:
		return fmt.Errorf("%s is longer than %d characters", name, limit)
	}
	return nil
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}
