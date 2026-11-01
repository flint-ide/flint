package clickhouse

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// Column is a result column: its name and ClickHouse type, e.g. Nullable(DateTime64(3)).
type Column struct {
	Name string
	Type string
}

// Rows streams the rows of one response. Use it like database/sql: Next, Values, then
// Err and Close.
type Rows struct {
	body    io.ReadCloser
	tail    *tailBuffer
	dec     *json.Decoder
	cols    []Column
	types   []chType
	conv    converter
	row     []any
	err     error
	done    bool
	queryID string
	written int64
}

func newRows(resp *http.Response, queryID string) (*Rows, error) {
	tail := &tailBuffer{}
	r := &Rows{body: resp.Body, tail: tail, queryID: queryID}
	r.dec = json.NewDecoder(io.TeeReader(resp.Body, tail))
	r.dec.UseNumber()
	if zone := resp.Header.Get("X-ClickHouse-Timezone"); zone != "" {
		if loc, err := time.LoadLocation(zone); err == nil {
			r.conv.serverZone = loc
		}
	}
	r.written = writtenRows(resp.Header.Get("X-ClickHouse-Summary"))

	// The first two lines are the names and the types. An empty body is a statement that
	// returns nothing (INSERT, CREATE, …).
	var names, types []string
	if err := r.dec.Decode(&names); err != nil {
		if errors.Is(err, io.EOF) {
			r.done = true
			return r, nil
		}
		return nil, r.streamError(err)
	}
	if err := r.dec.Decode(&types); err != nil {
		return nil, r.streamError(err)
	}
	if len(names) != len(types) {
		return nil, fmt.Errorf("clickhouse: malformed header: %d names, %d types", len(names), len(types))
	}
	r.cols = make([]Column, len(names))
	r.types = make([]chType, len(names))
	for i := range names {
		r.cols[i] = Column{Name: names[i], Type: types[i]}
		r.types[i] = parseType(types[i])
	}
	return r, nil
}

// Columns returns the result columns (none for statements that return no rows).
func (r *Rows) Columns() []Column { return r.cols }

// QueryID is the server-side id of the run, for Kill.
func (r *Rows) QueryID() string { return r.queryID }

// Written is the number of rows the statement wrote, from X-ClickHouse-Summary.
func (r *Rows) Written() int64 { return r.written }

// Next reads the next row; false at the end or on error (see Err).
func (r *Rows) Next() bool {
	if r.done || r.err != nil {
		return false
	}
	var raw []any
	if err := r.dec.Decode(&raw); err != nil {
		if errors.Is(err, io.EOF) {
			r.done = true
			return false
		}
		r.err = r.streamError(err)
		return false
	}
	if len(raw) == 1 {
		// The server reports a failure that happens after the first rows as one more row
		// holding the exception text.
		if s, ok := raw[0].(string); ok && looksLikeException(s) {
			r.err = parseError(s, "")
			return false
		}
	}
	row := make([]any, len(raw))
	for i, v := range raw {
		var t chType
		if i < len(r.types) {
			t = r.types[i]
		}
		row[i] = r.conv.convert(v, t)
	}
	r.row = row
	return true
}

// Values returns the current row, converted for display. The slice is the caller's.
func (r *Rows) Values() []any { return r.row }

// Err is the error that ended the stream, if any: a ClickHouse *Error for failures the
// server reported, even mid-stream.
func (r *Rows) Err() error { return r.err }

// Close releases the connection. Closing before the end abandons the rest of the rows.
func (r *Rows) Close() error { return r.body.Close() }

// streamError explains why decoding stopped: an exception the server wrote into the body
// (when it couldn't use the output format), else the transport or syntax error.
func (r *Rows) streamError(err error) error {
	var syn *json.SyntaxError
	var typ *json.UnmarshalTypeError
	malformed := errors.As(err, &syn) || errors.As(err, &typ)
	if malformed || errors.Is(err, io.ErrUnexpectedEOF) {
		if malformed { // the decoder stopped at the first bad byte; the exception text follows it
			_, _ = io.Copy(r.tail, io.LimitReader(r.body, tailSize))
		}
		if ce := parseError(r.tail.String(), ""); ce != nil {
			return ce
		}
	}
	if malformed && r.cols == nil {
		return errors.New("clickhouse: unexpected response; a statement with its own FORMAT clause can't be shown, remove the clause")
	}
	return fmt.Errorf("clickhouse: reading rows: %w", cleanTransportError(err))
}

// writtenRows reads written_rows from the X-ClickHouse-Summary JSON header.
func writtenRows(summary string) int64 {
	if summary == "" {
		return 0
	}
	var s struct {
		Written string `json:"written_rows"`
	}
	if json.Unmarshal([]byte(summary), &s) != nil {
		return 0
	}
	n, _ := strconv.ParseInt(s.Written, 10, 64)
	return n
}

// tailBuffer keeps the last bytes read from the body, so an exception the server appends
// to a broken stream can be recovered.
type tailBuffer struct{ buf []byte }

const tailSize = 8 << 10

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if len(t.buf) > tailSize {
		t.buf = append(t.buf[:0], t.buf[len(t.buf)-tailSize:]...)
	}
	return len(p), nil
}

func (t *tailBuffer) String() string { return string(bytes.ToValidUTF8(t.buf, nil)) }
