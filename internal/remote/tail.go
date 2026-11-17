package remote

import (
	"bytes"
	"strings"
	"sync"

	"flint/internal/redact"
)

// StderrLimit is the most of ssh's standard error kept and shown.
const StderrLimit = 8 << 10

// tail keeps the last StderrLimit bytes of what ssh wrote to its standard
// error, already passed through redact.Secrets: text is redacted a line at a
// time as it arrives, so cutting the front of the buffer never leaves half a
// secret behind for a later pass to miss. ssh's diagnostics and the remote
// shell's messages can name keys and hosts, and the UI shows them.
type tail struct {
	mu      sync.Mutex
	done    string // redacted complete lines
	partial []byte // a line not finished yet, at most StderrLimit bytes
}

// Write implements io.Writer; it never fails.
func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.partial = append(t.partial, p...)
	for {
		i := bytes.IndexByte(t.partial, '\n')
		if i < 0 {
			break
		}
		t.done += redact.Secrets(string(t.partial[:i+1]))
		t.partial = t.partial[i+1:]
	}
	if len(t.partial) > StderrLimit { // one enormous line
		t.done += redact.Secrets(string(t.partial))
		t.partial = nil
	}
	t.done = clipTail(t.done, StderrLimit)
	return len(p), nil
}

// String is the redacted tail, at most StderrLimit bytes of valid UTF-8.
func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.TrimSpace(clipTail(t.done+redact.Secrets(string(t.partial)), StderrLimit))
}

// clipTail keeps the last n bytes of s, without a broken character at the cut.
func clipTail(s string, n int) string {
	if len(s) > n {
		s = s[len(s)-n:]
	}
	return strings.ToValidUTF8(s, "")
}
