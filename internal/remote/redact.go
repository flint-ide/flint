package remote

import (
	"slices"
	"unicode/utf8"

	as "flint/internal/agentsession"
	"flint/internal/redact"
)

// maxFieldBytes caps each text a phone gets. Texts are cut before they are
// redacted: redacting is linear but not cheap (about 0.1 s for 512 KB), and a
// stream busy redacting can't end when it is cut (Stop, Revoke).
const maxFieldBytes = 128 << 10

// cutMargin is redacted beyond the cut, so a secret straddling it is found
// whole and its start doesn't survive the cut.
const cutMargin = 4 << 10

const cutNote = "\n[… cut: see the laptop]"

// redactText redacts s, cut to maxFieldBytes first.
func redactText(s string) string {
	if len(s) <= maxFieldBytes {
		return redact.String(s)
	}
	s = redact.String(prefix(s, maxFieldBytes+cutMargin))
	return prefix(s, maxFieldBytes) + cutNote
}

// prefix is at most n bytes of s, ending on a rune boundary.
func prefix(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// redactEvent returns a copy of e with credentials and the home directory
// taken out of every text a phone would see; secret withholds the input and
// output of its tool call (secretcalls.go). e itself is not changed (the
// desktop and the event log share it).
func redactEvent(e as.Event, secret bool) as.Event {
	out := e
	out.Text = redactText(e.Text)
	if e.ToolCall != nil {
		tc := redactToolCall(*e.ToolCall, secret)
		out.ToolCall = &tc
	}
	if e.Permission != nil {
		p := *e.Permission
		p.ToolCall = redactToolCall(p.ToolCall, secret)
		p.Options = slices.Clone(p.Options)
		for i := range p.Options {
			p.Options[i].Name = redact.String(p.Options[i].Name)
		}
		out.Permission = &p
	}
	if e.Plan != nil {
		out.Plan = make([]as.PlanEntry, len(e.Plan))
		for i, p := range e.Plan {
			p.Content = redactText(p.Content)
			out.Plan[i] = p
		}
	}
	return out
}

func redactToolCall(tc as.ToolCall, secret bool) as.ToolCall {
	out := tc
	out.Title = redact.String(tc.Title)
	out.Input = redactText(tc.Input)
	out.Output = redactText(tc.Output)
	if secret {
		if tc.Input != "" {
			out.Input = secretFileText
		}
		if tc.Output != "" {
			out.Output = secretFileText
		}
	}
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
				d.OldText, d.NewText = redactText(d.OldText), redactText(d.NewText)
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
