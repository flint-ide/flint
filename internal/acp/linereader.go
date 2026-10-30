package acp

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
)

// maxMessageBytes caps a single inbound JSON-RPC line. Messages of several
// megabytes (file contents, large tool output) are normal; this only guards
// against a runaway agent exhausting memory.
const maxMessageBytes = 64 << 20

// errMessageTooLarge is returned when a line exceeds the configured cap.
var errMessageTooLarge = errors.New("acp: inbound message exceeds size limit")

// lineReader splits a byte stream into newline-delimited messages of
// arbitrary (bounded) length.
type lineReader struct {
	r   *bufio.Reader
	max int
}

func newLineReader(r io.Reader, max int) *lineReader {
	return &lineReader{r: bufio.NewReaderSize(r, 64<<10), max: max}
}

// next returns the next line without its trailing "\n" or "\r\n". The
// returned slice is owned by the caller. A final unterminated line is
// returned before io.EOF.
func (l *lineReader) next() ([]byte, error) {
	var line []byte
	for {
		chunk, err := l.r.ReadSlice('\n')
		if len(line)+len(chunk) > l.max+2 { // +2 tolerates the "\r\n" terminator
			return nil, fmt.Errorf("%w (%d bytes)", errMessageTooLarge, l.max)
		}
		line = append(line, chunk...)
		switch {
		case err == nil:
			return trimEOL(line), nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF) && len(line) > 0:
			return trimEOL(line), nil
		default:
			return nil, err
		}
	}
}

func trimEOL(b []byte) []byte {
	b = bytes.TrimSuffix(b, []byte("\n"))
	return bytes.TrimSuffix(b, []byte("\r"))
}
