package clickhouse

import (
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fake serves handler as a ClickHouse HTTP interface and returns a client for it.
func fake(t *testing.T, handler http.HandlerFunc, mod func(*Config)) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	host, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	cfg := Config{Host: host, User: "flint", Password: "s3cret"}
	cfg.Port, _ = strconv.Atoi(port)
	if mod != nil {
		mod(&cfg)
	}
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

func body(r *http.Request) string {
	b, _ := io.ReadAll(r.Body)
	return string(b)
}

func collect(t *testing.T, rows *Rows) [][]any {
	t.Helper()
	var out [][]any
	for rows.Next() {
		out = append(out, rows.Values())
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err = %v", err)
	}
	return out
}

func TestQuerySendsCredentialsInHeadersAndSettingsInURL(t *testing.T) {
	var got *http.Request
	var sql string
	c := fake(t, func(w http.ResponseWriter, r *http.Request) {
		got, sql = r, body(r)
		io.WriteString(w, "[\"a\"]\n[\"UInt8\"]\n[1]\n")
	}, func(c *Config) { c.Database = "shop"; c.ReadOnly = true })

	rows, err := c.Query(context.Background(), Request{SQL: "SELECT 1", QueryID: "q-1", SessionID: "console-1"})
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	collect(t, rows)

	if sql != "SELECT 1" || got.Method != http.MethodPost {
		t.Errorf("method %s body %q", got.Method, sql)
	}
	h := got.Header
	if h.Get("X-ClickHouse-User") != "flint" || h.Get("X-ClickHouse-Key") != "s3cret" || h.Get("X-ClickHouse-Database") != "shop" {
		t.Errorf("headers = %v", h)
	}
	if strings.Contains(got.URL.RawQuery, "s3cret") || strings.Contains(got.URL.RawQuery, "password") || strings.Contains(got.URL.RawQuery, "user=") {
		t.Errorf("credentials leaked into the URL: %s", got.URL.RawQuery)
	}
	q := got.URL.Query()
	want := map[string]string{
		"default_format": "JSONCompactEachRowWithNamesAndTypes", "query_id": "q-1", "session_id": "console-1",
		"output_format_json_quote_64bit_integers": "1", "output_format_json_quote_decimals": "1",
		"readonly": "1", "cancel_http_readonly_queries_on_client_close": "1",
	}
	for k, v := range want {
		if q.Get(k) != v {
			t.Errorf("param %s = %q, want %q", k, q.Get(k), v)
		}
	}
	if q.Get("enable_http_compression") != "" {
		t.Error("compression requested on loopback")
	}
}

func TestQueryOmitsEmptyOptionalHeadersAndBadIDs(t *testing.T) {
	var got *http.Request
	c := fake(t, func(w http.ResponseWriter, r *http.Request) { got = r }, func(c *Config) { c.Password = "" })
	rows, err := c.Query(context.Background(), Request{SQL: "CREATE TABLE t (x UInt8) ENGINE = Memory", QueryID: "bad id&x=1", SessionID: "a b"})
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() || len(rows.Columns()) != 0 {
		t.Error("an empty body is a command with no rows")
	}
	h := got.Header
	if _, ok := h["X-Clickhouse-Key"]; ok {
		t.Error("empty password sent")
	}
	if _, ok := h["X-Clickhouse-Database"]; ok {
		t.Error("empty database sent")
	}
	q := got.URL.Query()
	if q.Get("session_id") != "" || q.Get("x") != "" || q.Get("query_id") == "bad id&x=1" || q.Get("query_id") != rows.QueryID() {
		t.Errorf("ids not sanitised: %v (rows %s)", q, rows.QueryID())
	}
}

func TestRowsConvertTypesExactly(t *testing.T) {
	c := fake(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-ClickHouse-Timezone", "UTC")
		io.WriteString(w, `["u","i","d","n","a","m","t","dt","f","k","s","nan","small","big"]`+"\n")
		io.WriteString(w, `["UInt64","Int128","Decimal(18, 2)","Nullable(String)","Array(UInt64)","Map(String, UInt64)","Tuple(a UInt8, b String)","DateTime64(3, 'Europe/Warsaw')","Float64","Tuple(UInt64, UInt8)","String","Float64","UInt64","Int64"]`+"\n")
		io.WriteString(w, `["18446744073709551615", "-5", "1.5", null, ["1","9007199254740993"], {"k":"3"}, {"a":1,"b":"x"}, "2024-01-02 03:04:05.123", 2.5, ["5",2], "zażółć", "nan", "42", "-9223372036854775808"]`+"\n")
	}, nil)
	rows, err := c.Query(context.Background(), Request{SQL: "SELECT 1"})
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := collect(t, rows)
	if len(got) != 1 {
		t.Fatalf("rows = %v", got)
	}
	want := []any{
		"18446744073709551615", "-5", "1.50", nil,
		[]any{int64(1), "9007199254740993"},
		map[string]any{"k": int64(3)},
		map[string]any{"a": int64(1), "b": "x"},
		"2024-01-02 03:04:05.123 +01:00",
		2.5, []any{int64(5), int64(2)}, "zażółć", "nan", int64(42), "-9223372036854775808",
	}
	if !reflect.DeepEqual(got[0], want) {
		for i := range want {
			if !reflect.DeepEqual(got[0][i], want[i]) {
				t.Errorf("col %s = %#v, want %#v", rows.Columns()[i].Name, got[0][i], want[i])
			}
		}
	}
	if rows.Columns()[1] != (Column{Name: "i", Type: "Int128"}) {
		t.Errorf("columns = %+v", rows.Columns())
	}
}

func TestDateTimeUsesServerZoneWithoutOwn(t *testing.T) {
	c := fake(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-ClickHouse-Timezone", "Europe/Warsaw")
		io.WriteString(w, "[\"a\",\"b\",\"c\"]\n[\"DateTime\",\"DateTime('UTC')\",\"Date\"]\n[\"2024-07-01 12:00:00\",\"2024-07-01 12:00:00\",\"2024-07-01\"]\n")
	}, nil)
	rows, _ := c.Query(context.Background(), Request{SQL: "SELECT 1"})
	defer rows.Close()
	got := collect(t, rows)[0]
	if got[0] != "2024-07-01 12:00:00 +02:00" || got[1] != "2024-07-01 12:00:00 Z" || got[2] != "2024-07-01" {
		t.Errorf("times = %v", got)
	}
}

func TestErrorBeforeAnyRow(t *testing.T) {
	c := fake(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-ClickHouse-Exception-Code", "62")
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, "[]\n[]\n[\"Code: 62. DB::Exception: Syntax error: failed at position 1 ('SELEC'): SELEC 1. (SYNTAX_ERROR) (version 24.8.14.39 (official build))\"]\n")
	}, nil)
	_, err := c.Query(context.Background(), Request{SQL: "SELEC 1"})
	var ce *Error
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v", err)
	}
	if ce.Code != 62 || ce.Name != "SYNTAX_ERROR" || !strings.HasPrefix(ce.Message, "Syntax error: failed at position 1") || strings.Contains(ce.Message, "version") {
		t.Errorf("error = %+v", ce)
	}
	if !strings.HasPrefix(err.Error(), "code: 62, message: Syntax error") {
		t.Errorf("text = %q", err)
	}
}

func TestPlainTextErrorBody(t *testing.T) {
	c := fake(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, "Code: 516. DB::Exception: flint: Authentication failed: password is incorrect, or there is no user with such name. (AUTHENTICATION_FAILED) (version 24.8.14.39 (official build))\n")
	}, nil)
	_, err := c.Query(context.Background(), Request{SQL: "SELECT 1"})
	var ce *Error
	if !errors.As(err, &ce) || ce.Code != 516 || ce.Name != "AUTHENTICATION_FAILED" {
		t.Fatalf("err = %#v", err)
	}
}

func TestNonClickHouseErrorBody(t *testing.T) {
	c := fake(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream down", http.StatusBadGateway)
	}, nil)
	_, err := c.Query(context.Background(), Request{SQL: "SELECT 1"})
	if err == nil || !strings.Contains(err.Error(), "HTTP 502") || !strings.Contains(err.Error(), "upstream down") {
		t.Errorf("err = %v", err)
	}
}

func TestExceptionMidStream(t *testing.T) {
	c := fake(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "[\"n\"]\n[\"UInt8\"]\n[1]\n[2]\n")
		io.WriteString(w, "[\"Code: 395. DB::Exception: Value passed to 'throwIf' function is non-zero. (FUNCTION_THROW_IF_VALUE_IS_NON_ZERO) (version 24.8.14.39 (official build))\"]\n")
	}, nil)
	rows, err := c.Query(context.Background(), Request{SQL: "SELECT 1"})
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		n++
	}
	var ce *Error
	if n != 2 || !errors.As(rows.Err(), &ce) || ce.Code != 395 || ce.Name != "FUNCTION_THROW_IF_VALUE_IS_NON_ZERO" {
		t.Errorf("rows %d err %v", n, rows.Err())
	}
}

func TestStringColumnThatQuotesAnExceptionIsNotMistakenIfNoVersion(t *testing.T) {
	c := fake(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "[\"s\"]\n[\"String\"]\n[\"Code: 1. DB::Exception: just data\"]\n")
	}, nil)
	rows, _ := c.Query(context.Background(), Request{SQL: "SELECT 1"})
	defer rows.Close()
	if got := collect(t, rows); len(got) != 1 {
		t.Errorf("rows = %v", got)
	}
}

func TestExceptionAppendedAsPlainText(t *testing.T) {
	c := fake(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "[\"n\"]\n[\"UInt8\"]\n[1]\nCode: 241. DB::Exception: Memory limit exceeded. (MEMORY_LIMIT_EXCEEDED) (version 24.8.14.39 (official build))\n")
	}, nil)
	rows, _ := c.Query(context.Background(), Request{SQL: "SELECT 1"})
	defer rows.Close()
	for rows.Next() {
	}
	var ce *Error
	if !errors.As(rows.Err(), &ce) || ce.Code != 241 || ce.Name != "MEMORY_LIMIT_EXCEEDED" {
		t.Errorf("err = %#v", rows.Err())
	}
}

func TestOwnFormatClauseIsExplained(t *testing.T) {
	c := fake(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "{\"meta\": [], \"data\": []}\n")
	}, nil)
	_, err := c.Query(context.Background(), Request{SQL: "SELECT 1 FORMAT JSON"})
	if err == nil || !strings.Contains(err.Error(), "FORMAT clause") {
		t.Errorf("err = %v", err)
	}
}

func TestSessionLockedIsRetried(t *testing.T) {
	var calls atomic.Int32
	c := fake(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusLocked)
			io.WriteString(w, "Code: 373. DB::Exception: Session is locked by a concurrent client. (SESSION_IS_LOCKED) (version 24.8.14.39 (official build))")
			return
		}
		io.WriteString(w, "[\"a\"]\n[\"UInt8\"]\n[1]\n")
	}, nil)
	rows, err := c.Query(context.Background(), Request{SQL: "SELECT 1", SessionID: "s"})
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if len(collect(t, rows)) != 1 || calls.Load() != 3 {
		t.Errorf("calls = %d", calls.Load())
	}
}

func TestSessionLockedWithoutSessionIsNotRetried(t *testing.T) {
	var calls atomic.Int32
	c := fake(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusLocked)
		io.WriteString(w, "Code: 373. DB::Exception: locked. (SESSION_IS_LOCKED) (version 1)")
	}, nil)
	if _, err := c.Query(context.Background(), Request{SQL: "SELECT 1"}); err == nil || calls.Load() != 1 {
		t.Errorf("err %v calls %d", err, calls.Load())
	}
}

func TestKillSendsParameterisedStatement(t *testing.T) {
	var got *http.Request
	var sql string
	c := fake(t, func(w http.ResponseWriter, r *http.Request) { got, sql = r, body(r) }, nil)
	if err := c.Kill("q-1"); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(sql, "KILL QUERY WHERE query_id = {qid:String}") || got.URL.Query().Get("param_qid") != "q-1" {
		t.Errorf("kill: %q %v", sql, got.URL.Query())
	}
	if got.Header.Get("X-ClickHouse-Key") != "s3cret" {
		t.Error("kill sent without credentials")
	}
	if err := c.Kill("x' OR 1=1 --"); err == nil {
		t.Error("an id that is not a plain identifier was sent")
	}
}

func TestClosingEarlyStopsReading(t *testing.T) {
	released := make(chan struct{})
	c := fake(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "[\"n\"]\n[\"UInt32\"]\n")
		for i := 0; i < 1_000_000; i++ {
			if _, err := io.WriteString(w, "[1]\n"); err != nil {
				break
			}
			if f, ok := w.(http.Flusher); ok && i%1000 == 0 {
				f.Flush()
			}
			if r.Context().Err() != nil {
				break
			}
		}
		close(released)
	}, nil)
	rows, err := c.Query(context.Background(), Request{SQL: "SELECT 1"})
	if err != nil {
		t.Fatal(err)
	}
	rows.Next()
	rows.Close()
	select {
	case <-released:
	case <-time.After(5 * time.Second):
		t.Fatal("server kept writing after the client closed the body")
	}
}

func TestContextCancelClosesRequest(t *testing.T) {
	c := fake(t, func(w http.ResponseWriter, r *http.Request) { body(r); <-r.Context().Done() }, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := c.Query(ctx, Request{SQL: "SELECT sleep(3)"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v", err)
	}
	if err != nil && strings.Contains(err.Error(), "?") {
		t.Errorf("error leaks the request URL: %v", err)
	}
}

func TestGzipResponse(t *testing.T) {
	var asked string
	c := fake(t, func(w http.ResponseWriter, r *http.Request) {
		asked = r.Header.Get("Accept-Encoding")
		w.Header().Set("Content-Encoding", "gzip")
		zw := gzip.NewWriter(w)
		io.WriteString(zw, "[\"a\"]\n[\"UInt8\"]\n[7]\n")
		zw.Close()
	}, nil)
	c.compress = true
	rows, err := c.Query(context.Background(), Request{SQL: "SELECT 7"})
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := collect(t, rows)
	if len(got) != 1 || got[0][0] != int64(7) || !strings.Contains(asked, "gzip") {
		t.Errorf("rows %v accept-encoding %q", got, asked)
	}
}

func TestSummaryWrittenRows(t *testing.T) {
	c := fake(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-ClickHouse-Summary", `{"read_rows":"0","written_rows":"3"}`)
	}, nil)
	rows, err := c.Query(context.Background(), Request{SQL: "INSERT INTO t SELECT 1"})
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Written() != 3 {
		t.Errorf("written = %d", rows.Written())
	}
}

func TestTLSUsesHTTPS(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "[\"a\"]\n[\"UInt8\"]\n[1]\n")
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	mk := func(insecure bool) *Client {
		c, err := New(Config{Host: "127.0.0.1", Port: port, TLS: true, Insecure: insecure, User: "u"})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	if err := mk(false).Ping(context.Background()); err == nil {
		t.Error("an untrusted certificate was accepted without the insecure option")
	}
	if err := mk(true).Ping(context.Background()); err != nil {
		t.Errorf("insecure ping: %v", err)
	}
}

func TestNewValidates(t *testing.T) {
	if _, err := New(Config{Port: 8123}); err == nil {
		t.Error("missing host accepted")
	}
	if _, err := New(Config{Host: "h", Port: 0}); err == nil {
		t.Error("missing port accepted")
	}
}

func TestParseType(t *testing.T) {
	tt := parseType("Tuple(a Array(Nullable(UInt64)), b Map(String, Decimal(10, 2)))")
	if tt.name != "Tuple" || len(tt.args) != 2 {
		t.Fatalf("tuple = %+v", tt)
	}
	n, et := tupleElement(tt.args[1])
	if n != "b" || et.name != "Map" || et.sub(1).name != "Decimal" || decimalScale(et.sub(1)) != 2 {
		t.Errorf("element %q %+v", n, et)
	}
	if n, et := tupleElement("DateTime64(3, 'UTC')"); n != "" || zoneArg(et) != "UTC" {
		t.Errorf("unnamed element %q %+v", n, et)
	}
	if decimalScale(parseType("Decimal64(4)")) != 4 {
		t.Error("DecimalN(S) scale")
	}
}
