package host

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode/utf8"

	"flint/internal/extensions/manifest"
)

// View models (view/resolve's result), as the frontend draws them. What an
// extension sends is untrusted: it is decoded into these types, cut to the
// limits below and stripped of control characters, and anything that would
// reach another extension's or Flint's ids is dropped. Text is always plain
// text.

// Limits on a view model.
const (
	maxItems       = 5000
	maxLabel       = 2000
	maxItemID      = 256
	maxItemActions = 10
	maxArgs        = 16 << 10
)

// View is a panel's or a tab's content: items of a tree or list, a table, a
// form or a chart (richviews.go), or a message.
type View struct {
	// Kind is "tree", "list", "table", "form", "chart" or "message".
	Kind  string `json:"kind"`
	Items []Item `json:"items"`
	// Table, Form and Chart are the content of a view of that kind.
	Table *Table `json:"table,omitempty"`
	Form  *Form  `json:"form,omitempty"`
	Chart *Chart `json:"chart,omitempty"`
	// Text and Actions are a message's.
	Text    string   `json:"text,omitempty"`
	Actions []Action `json:"actions,omitempty"`
	// Truncated: the extension sent more than maxItems items or rows, or
	// more series or points than a chart holds.
	Truncated bool `json:"truncated,omitempty"`
}

// Item is a row of a tree or list.
type Item struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Tooltip     string `json:"tooltip,omitempty"`
	// Icon is a name from Flint's icon set; unknown names show none.
	Icon string `json:"icon,omitempty"`
	// HasChildren: a tree node the user can expand (view/resolve with parent).
	HasChildren bool `json:"hasChildren,omitempty"`
	// Actions are the item's context menu.
	Actions []Action `json:"actions,omitempty"`
	// Command runs on click: one of the extension's own commands.
	Command *CommandRef `json:"command,omitempty"`
}

// Action is a button or context menu entry; choosing it sends view/action.
type Action struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// CommandRef names a command of the extension and its arguments.
type CommandRef struct {
	ID   string          `json:"id"`
	Args json.RawMessage `json:"args,omitempty"`
}

var iconName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}$`)

// cleanView makes v safe to draw as panel's content.
func cleanView(v View, panel manifest.Panel, m *manifest.Manifest) View {
	out := View{Kind: v.Kind, Items: []Item{}}
	if v.Kind == "message" {
		out.Text = cleanText(v.Text, maxLabel)
		out.Actions = cleanActions(v.Actions)
		return out
	}
	if v.Kind != panel.View {
		return View{Kind: "message", Items: []Item{}, Text: "The extension sent a " + cleanText(v.Kind, 20) + " view for a " + panel.View + " panel."}
	}
	seen := map[string]bool{}
	for _, it := range v.Items {
		if len(out.Items) == maxItems {
			out.Truncated = true
			break
		}
		it.ID = cleanText(it.ID, maxItemID)
		if it.ID == "" || seen[it.ID] {
			continue
		}
		seen[it.ID] = true
		it.Label = cleanText(it.Label, maxLabel)
		it.Description = cleanText(it.Description, maxLabel)
		it.Tooltip = cleanText(it.Tooltip, maxLabel)
		if !iconName.MatchString(it.Icon) {
			it.Icon = ""
		}
		it.HasChildren = it.HasChildren && panel.View == "tree"
		it.Actions = cleanActions(it.Actions)
		it.Command = cleanCommand(it.Command, m)
		out.Items = append(out.Items, it)
	}
	return out
}

func cleanActions(list []Action) []Action {
	var out []Action
	for _, a := range list {
		if len(out) == maxItemActions {
			break
		}
		a.ID, a.Label = cleanText(a.ID, 64), cleanText(a.Label, 60)
		if a.ID != "" && a.Label != "" {
			out = append(out, a)
		}
	}
	return out
}

// cleanCommand keeps a reference to one of the extension's own commands.
func cleanCommand(c *CommandRef, m *manifest.Manifest) *CommandRef {
	if c == nil {
		return nil
	}
	if _, ok := m.Command(c.ID); !ok || len(c.Args) > maxArgs {
		return nil
	}
	return &CommandRef{ID: c.ID, Args: c.Args}
}

// cleanText drops control characters (a tab becomes a space) and invalid
// UTF-8, and cuts the text at limit runes.
func cleanText(s string, limit int) string {
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "�")
	}
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\t' || r == '\n':
			return ' '
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) || (r >= '‪' && r <= '‮') || (r >= '⁦' && r <= '⁩'):
			return -1
		}
		return r
	}, s)
	if utf8.RuneCountInString(s) > limit {
		s = string([]rune(s)[:limit-1]) + "…"
	}
	return s
}
