package remote

import (
	"slices"

	as "flint/internal/agentsession"
	"flint/internal/redact"
)

// secretFileText replaces the content of a diff of a credentials file
// (.env, keys): the phone sees that the file changed, not what is in it.
const secretFileText = "[secret file: not shown remotely]"

// redactEvent returns a copy of e with credentials and the home directory
// taken out of every text a phone would see. e itself is not changed (the
// desktop and the event log share it).
func redactEvent(e as.Event) as.Event {
	out := e
	out.Text = redact.String(e.Text)
	if e.ToolCall != nil {
		tc := redactToolCall(*e.ToolCall)
		out.ToolCall = &tc
	}
	if e.Permission != nil {
		p := *e.Permission
		p.ToolCall = redactToolCall(p.ToolCall)
		p.Options = slices.Clone(p.Options)
		for i := range p.Options {
			p.Options[i].Name = redact.String(p.Options[i].Name)
		}
		out.Permission = &p
	}
	if e.Plan != nil {
		out.Plan = make([]as.PlanEntry, len(e.Plan))
		for i, p := range e.Plan {
			p.Content = redact.String(p.Content)
			out.Plan[i] = p
		}
	}
	return out
}

func redactToolCall(tc as.ToolCall) as.ToolCall {
	out := tc
	out.Title = redact.String(tc.Title)
	out.Input = redact.String(tc.Input)
	out.Output = redact.String(tc.Output)
	if tc.Paths != nil {
		out.Paths = make([]string, len(tc.Paths))
		for i, p := range tc.Paths {
			out.Paths[i] = redact.String(p)
		}
	}
	if tc.Diffs != nil {
		out.Diffs = make([]as.Diff, len(tc.Diffs))
		for i, d := range tc.Diffs {
			if redact.SecretFile(d.Path) {
				d.OldText, d.NewText = "", secretFileText
			} else {
				d.OldText, d.NewText = redact.String(d.OldText), redact.String(d.NewText)
			}
			d.Path = redact.String(d.Path)
			out.Diffs[i] = d
		}
	}
	return out
}

// shrinkEvent drops the bulky parts of an event too big for one message
// (whole-file diffs, long output); the laptop still has them.
func shrinkEvent(e as.Event) as.Event {
	const keep = 16 << 10
	cut := func(s string) string {
		if len(s) <= keep {
			return s
		}
		return s[:keep] + "\n[… cut: see the laptop]"
	}
	out := e
	out.Text = cut(e.Text)
	shrinkCall := func(tc as.ToolCall) as.ToolCall {
		tc.Input, tc.Output = cut(tc.Input), cut(tc.Output)
		diffs := make([]as.Diff, len(tc.Diffs))
		for i, d := range tc.Diffs {
			diffs[i] = as.Diff{Path: d.Path, NewText: "[diff too large: see the laptop]"}
		}
		if tc.Diffs != nil {
			tc.Diffs = diffs
		}
		return tc
	}
	if e.ToolCall != nil {
		tc := shrinkCall(*e.ToolCall)
		out.ToolCall = &tc
	}
	if e.Permission != nil {
		p := *e.Permission
		p.ToolCall = shrinkCall(p.ToolCall)
		out.Permission = &p
	}
	return out
}

func redactChat(c Chat) Chat {
	c.Project = redact.String(c.Project)
	return c
}

func redactTask(t Task) Task {
	t.Title = redact.String(t.Title)
	t.Error = redact.String(t.Error)
	t.Branch = redact.Secrets(t.Branch)
	return t
}
