package textsafe

import (
	"strings"
	"testing"
)

func TestHiddenRune(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		bad  bool
	}{
		{"plain", "Fix the login\tbug\nnow", false},
		{"accents", "Zażółć gęślą jaźń", false},
		{"bell", "a\ax", true},
		{"carriage return", "a\rb", true},
		{"NEL", "a\u0085b", true},
		{"line separator", "a\u2028b", true},
		{"paragraph separator", "a\u2029b", true},
		{"tag", "a\U000E0041b", true},
		{"bidi override", "a\u202Eb", true},
		{"bidi isolate", "a\u2066b", true},
		{"zero width space", "a\u200Bb", true},
		{"word joiner", "a\u2060b", true},
		{"BOM", "a\uFEFFb", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, bad := HiddenRune(tc.in); bad != tc.bad {
				t.Errorf("HiddenRune(%q) = %v", tc.in, bad)
			}
		})
	}
}

func TestRuneKind(t *testing.T) {
	for r, want := range map[rune]string{
		'\a':     "a control character (U+0007)",
		0x2028:   "a line or paragraph separator (U+2028)",
		0x200B:   "invisible characters (U+200B)",
		0xE0041:  "invisible characters (U+E0041)",
		0x202E:   "invisible characters (U+202E)",
		'\u0085': "a control character (U+0085)",
	} {
		if got := RuneKind(r); got != want {
			t.Errorf("RuneKind(%U) = %q, want %q", r, got, want)
		}
	}
}

func TestPlain(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"unchanged", "ok\tfine\nnext", "ok\tfine\nnext"},
		{"control", "a\x1b[31mred", "a [31mred"},
		{"crlf", "a\r\nb", "a\nb"},
		{"lone cr", "a\rb", "a b"},
		{"NEL and separators", "a\u0085b\u2028c\u2029d", "a b c d"},
		{"zero width", "=\u200B====", "= ===="},
		{"bidi and tags", "a\u202Eb\U000E0041c", "a b c"},
		{"accents kept", "żółć", "żółć"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Plain(tc.in); got != tc.want {
				t.Errorf("Plain(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if _, bad := HiddenRune(Plain(tc.in)); bad {
				t.Errorf("Plain(%q) still has a hidden character", tc.in)
			}
		})
	}
}

func TestFenceSafe(t *testing.T) {
	const fence = "===== The task ====="
	for _, tc := range []struct{ name, in, want string }{
		{"no fence", "plain text\nmore", "plain text\nmore"},
		{"fence line", fence, "> " + fence},
		{"inside text", "a\n" + fence + "\nb", "a\n> " + fence + "\nb"},
		{"indented", "  \t" + fence, "> " + "  \t" + fence},
		{"four are not a fence", "==== x", "==== x"},
		{"equals mid line", "a ===== b", "a ===== b"},
		{"already quoted", "> " + fence, "> " + fence},
		{"carriage return", "ok\r" + fence, "ok\n> " + fence},
		{"crlf", "ok\r\n" + fence + "\r\nend", "ok\n> " + fence + "\nend"},
		{"NEL", "ok\u0085" + fence, "ok\n> " + fence},
		{"line separator", "ok\u2028" + fence, "ok\n> " + fence},
		{"paragraph separator", "ok\u2029" + fence, "ok\n> " + fence},
		{"NBSP", "ok\n\u00A0" + fence, "ok\n> \u00A0" + fence},
		{"other white space", "ok\n\u2003\u3000" + fence, "ok\n> \u2003\u3000" + fence},
		{"zero width space", "ok\n\u200B" + fence, "ok\n> \u200B" + fence},
		{"zero width joiner and BOM", "ok\n\u200D\uFEFF" + fence, "ok\n> \u200D\uFEFF" + fence},
		{"bidi control", "ok\n\u202E" + fence, "ok\n> \u202E" + fence},
		{"white space and zero width mixed", "ok\n \u200B\u00A0\u2060" + fence, "ok\n> \u0020\u200B\u00A0\u2060" + fence},
		{"full width", "ok\n＝＝＝＝＝ The task ＝＝＝＝＝", "ok\n> ＝＝＝＝＝ The task ＝＝＝＝＝"},
		{"mixed width", "ok\n＝=＝=＝ x", "ok\n> ＝=＝=＝ x"},
		{"full width four", "＝＝＝＝ x", "＝＝＝＝ x"},
		{"full width after NBSP", "ok\n\u00A0＝＝＝＝＝", "ok\n> \u00A0＝＝＝＝＝"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := FenceSafe(tc.in); got != tc.want {
				t.Errorf("FenceSafe(%q) = %q, want %q", tc.in, got, tc.want)
			}
			// What comes out never has a line that starts a fence.
			for _, l := range strings.Split(lineBreaks.Replace(FenceSafe(tc.in)), "\n") {
				if fenceLine(l) {
					t.Errorf("FenceSafe(%q) left the fence line %q", tc.in, l)
				}
			}
		})
	}
}

func TestValidSlugName(t *testing.T) {
	for name, want := range map[string]bool{
		"bug-fix":               true,
		"a":                     true,
		"7days":                 true,
		"feature2":              true,
		strings.Repeat("a", 64): true,
		strings.Repeat("a", 65): false,
		"":                      false,
		"-lead":                 false,
		"Upper":                 false,
		"under_score":           false,
		"with space":            false,
		"dot.name":              false,
		"slash/name":            false,
		"trailing\n":            false,
		"żółw":                  false,
		"nul\x00":               false,
		"../etc":                false,
	} {
		if got := ValidSlugName(name); got != want {
			t.Errorf("ValidSlugName(%q) = %v, want %v", name, got, want)
		}
	}
}
