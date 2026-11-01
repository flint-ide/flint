package remote

import (
	"strings"
	"testing"
	"unicode/utf8"

	as "flint/internal/agentsession"
)

// A huge event is cut before it is redacted: redacting 512 KB takes long
// enough (about 0.1 s, seconds under -race) to hold up Stop and Revoke.
func TestLargeFieldsAreCutBeforeRedaction(t *testing.T) {
	big := strings.Repeat("é", maxFieldBytes) // two bytes per rune: twice the cap
	e := redactEvent(as.Event{Text: big, ToolCall: &as.ToolCall{Output: big}}, false)
	for name, s := range map[string]string{"text": e.Text, "output": e.ToolCall.Output} {
		if len(s) > maxFieldBytes+len(cutNote) {
			t.Errorf("%s is %d bytes, want at most %d", name, len(s), maxFieldBytes+len(cutNote))
		}
		if !utf8.ValidString(s) || !strings.HasSuffix(s, cutNote) {
			t.Errorf("%s was not cut on a rune boundary with the note", name)
		}
	}
}

// A secret that straddles the cut is redacted, not cut in half and left
// partly visible.
func TestASecretAtTheCutIsStillRedacted(t *testing.T) {
	const key = "ghp_" + "0123456789abcdefghij0123456789abcdef"
	text := strings.Repeat("a", maxFieldBytes-10) + " " + key + " tail"
	got := redactEvent(as.Event{Text: text}, false).Text
	if strings.Contains(got, "ghp_0123") {
		t.Fatalf("the start of the token leaked: …%s", got[len(got)-80:])
	}
}

func TestSmallFieldsAreNotCut(t *testing.T) {
	if got := redactEvent(as.Event{Text: "hello"}, false).Text; got != "hello" {
		t.Errorf("Text = %q", got)
	}
}
