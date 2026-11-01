package host

import (
	"strings"
	"sync"
	"time"
)

// maxLog bounds an extension's log in memory; the oldest lines go first.
const maxLog = 256 << 10

// ring is an extension's log: the last maxLog bytes of lines.
type ring struct {
	mu    sync.Mutex
	lines []string
	size  int
}

func newRing() *ring { return &ring{} }

// add appends a line, "15:04:05 [source] text".
func (r *ring) add(source, text string) {
	line := time.Now().Format("15:04:05") + " [" + source + "] " + strings.TrimRight(text, "\r\n")
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, line)
	r.size += len(line) + 1
	drop := 0
	for r.size > maxLog && drop < len(r.lines)-1 {
		r.size -= len(r.lines[drop]) + 1
		drop++
	}
	r.lines = r.lines[drop:] // append reallocates the live part as it grows
}

func (r *ring) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.lines) == 0 {
		return ""
	}
	return strings.Join(r.lines, "\n") + "\n"
}
