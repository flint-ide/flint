package claude

import (
	"strings"
	"sync"
)

// stderrCapacity bounds how much CLI stderr is retained.
const stderrCapacity = 32 << 10

// ringBuffer is a concurrency-safe io.Writer that keeps only the last
// capacity bytes written to it.
type ringBuffer struct {
	mu    sync.Mutex
	buf   []byte
	pos   int   // next write position
	total int64 // bytes ever written
}

func newRingBuffer(capacity int) *ringBuffer {
	return &ringBuffer{buf: make([]byte, capacity)}
}

// Write implements io.Writer; it never fails.
func (r *ringBuffer) Write(p []byte) (int, error) {
	n := len(p)
	size := len(r.buf)
	if n == 0 || size == 0 {
		return n, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.total += int64(n)
	if n >= size {
		copy(r.buf, p[n-size:])
		r.pos = 0
		return n, nil
	}
	written := copy(r.buf[r.pos:], p)
	if written < n {
		copy(r.buf, p[written:])
	}
	r.pos = (r.pos + n) % size
	return n, nil
}

// Bytes returns a copy of the retained bytes, oldest first.
func (r *ringBuffer) Bytes() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.total < int64(len(r.buf)) {
		return append([]byte(nil), r.buf[:r.pos]...)
	}
	out := make([]byte, 0, len(r.buf))
	out = append(out, r.buf[r.pos:]...)
	return append(out, r.buf[:r.pos]...)
}

// Tail returns at most the last n retained bytes as valid UTF-8 text.
func (r *ringBuffer) Tail(n int) string {
	b := r.Bytes()
	if len(b) > n {
		b = b[len(b)-n:]
	}
	return strings.ToValidUTF8(string(b), "")
}
