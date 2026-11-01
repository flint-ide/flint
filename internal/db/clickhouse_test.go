package db

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// chFake is a ClickHouse HTTP endpoint that records every request.
type chFake struct {
	mu       sync.Mutex
	requests []chRequest
	handle   func(w http.ResponseWriter, r *http.Request, sql string)
}

type chRequest struct {
	SQL    string
	Params map[string]string
	Header http.Header
}

func (f *chFake) all() []chRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]chRequest(nil), f.requests...)
}

// kills returns the query ids of the KILL QUERY requests received so far.
func (f *chFake) kills() []string {
	var ids []string
	for _, r := range f.all() {
		if strings.HasPrefix(r.SQL, "KILL QUERY") {
			ids = append(ids, r.Params["param_qid"])
		}
	}
	return ids
}

func newCH(t *testing.T, handle func(w http.ResponseWriter, r *http.Request, sql string)) (*chSession, *chFake) {
	t.Helper()
	f := &chFake{handle: handle}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		params := map[string]string{}
		for k, v := range r.URL.Query() {
			params[k] = v[0]
		}
		f.mu.Lock()
		f.requests = append(f.requests, chRequest{SQL: string(b), Params: params, Header: r.Header.Clone()})
		f.mu.Unlock()
		f.handle(w, r, string(b))
	}))
	t.Cleanup(srv.Close)
	host, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	p, _ := strconv.Atoi(port)
	s, err := openClickHouse(context.Background(), Connection{Driver: ClickHouse, Host: host, Port: p, User: "u"}, "pw")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s, f
}

const oneRowHeader = "[\"n\",\"s\"]\n[\"UInt64\",\"Nullable(String)\"]\n"

func TestChQueryReturnsColumnsAndRows(t *testing.T) {
	s, _ := newCH(t, func(w http.ResponseWriter, r *http.Request, sql string) {
		io.WriteString(w, oneRowHeader+"[\"18446744073709551615\", null]\n[\"7\", \"x\"]\n")
	})
	res, err := s.Query(context.Background(), "SELECT 1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Columns) != 2 || res.Columns[0] != (Column{Name: "n", Type: "UInt64"}) || res.Columns[1].Type != "Nullable(String)" {
		t.Errorf("columns = %+v", res.Columns)
	}
	if len(res.Rows) != 2 || res.Rows[0][0] != "18446744073709551615" || res.Rows[0][1] != nil || res.Rows[1][0] != int64(7) {
		t.Errorf("rows = %#v", res.Rows)
	}
	if res.Command != "SELECT 2" || res.Truncated {
		t.Errorf("command %q truncated %v", res.Command, res.Truncated)
	}
}

func TestChStatementWithoutRows(t *testing.T) {
	s, _ := newCH(t, func(w http.ResponseWriter, r *http.Request, sql string) {
		w.Header().Set("X-ClickHouse-Summary", `{"written_rows":"5"}`)
	})
	res, err := s.Query(context.Background(), "INSERT INTO t SELECT 1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if res.Command != "OK" || res.Affected != 5 || len(res.Columns) != 0 || res.Rows == nil {
		t.Errorf("result = %+v", res)
	}
}

func TestChServerError(t *testing.T) {
	s, _ := newCH(t, func(w http.ResponseWriter, r *http.Request, sql string) {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, "Code: 60. DB::Exception: Table default.nope does not exist. (UNKNOWN_TABLE) (version 24.8.14.39 (official build))")
	})
	_, err := s.Query(context.Background(), "SELECT * FROM nope", 10)
	if err == nil || err.Error() != "code: 60, message: Table default.nope does not exist." {
		t.Errorf("err = %v", err)
	}
}

func TestChSessionOnlyForConsoles(t *testing.T) {
	s, f := newCH(t, func(w http.ResponseWriter, r *http.Request, sql string) {
		io.WriteString(w, oneRowHeader)
	})
	if _, err := s.Query(WithConsole(context.Background(), "abc-123"), "SET max_threads = 1", 10); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Query(context.Background(), "SELECT 1", 10); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Schema(WithConsole(context.Background(), "abc-123")); err != nil {
		t.Fatal(err)
	}
	reqs := f.all()
	if reqs[0].Params["session_id"] != "flint-abc-123" || reqs[0].Params["session_timeout"] == "" {
		t.Errorf("console statement params = %v", reqs[0].Params)
	}
	if reqs[1].Params["session_id"] != "" {
		t.Errorf("statement outside a console got a session: %v", reqs[1].Params)
	}
	if reqs[2].Params["session_id"] != "" {
		t.Errorf("schema query joined the console session (would lock it): %v", reqs[2].Params)
	}
}

func TestChTruncationKillsTheStatement(t *testing.T) {
	s, f := newCH(t, func(w http.ResponseWriter, r *http.Request, sql string) {
		if strings.HasPrefix(sql, "KILL") {
			return
		}
		io.WriteString(w, oneRowHeader)
		for i := 0; i < 100000; i++ {
			if _, err := fmt.Fprintf(w, "[\"%d\", \"row\"]\n", i); err != nil {
				return
			}
			if i%100 == 0 {
				w.(http.Flusher).Flush()
			}
		}
	})
	ctx := withQueryID(context.Background(), "q-trunc")
	res, err := s.Query(ctx, "SELECT number FROM numbers(100000)", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 5 || !res.Truncated {
		t.Fatalf("rows %d truncated %v", len(res.Rows), res.Truncated)
	}
	waitFor(t, func() bool { return len(f.kills()) == 1 && f.kills()[0] == "q-trunc" })
}

func TestChCancelKillsBeforeAnyRowArrived(t *testing.T) {
	started := make(chan struct{})
	s, f := newCH(t, func(w http.ResponseWriter, r *http.Request, sql string) {
		if strings.HasPrefix(sql, "KILL") {
			return
		}
		close(started)
		<-r.Context().Done() // sleepEachRow: nothing is sent until the client gives up
	})
	ctx, cancel := context.WithCancel(withQueryID(context.Background(), "q-slow"))
	done := make(chan error, 1)
	go func() {
		_, err := s.Query(ctx, "SELECT sleepEachRow(1) FROM numbers(30)", 10)
		done <- err
	}()
	<-started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Query did not return after cancel")
	}
	waitFor(t, func() bool { return len(f.kills()) == 1 && f.kills()[0] == "q-slow" })
	for _, r := range f.all() {
		if strings.HasPrefix(r.SQL, "KILL") && r.Header.Get("X-ClickHouse-Key") != "pw" {
			t.Error("kill sent without the password header")
		}
	}
}

func TestChFinishedQueryIsNotKilled(t *testing.T) {
	s, f := newCH(t, func(w http.ResponseWriter, r *http.Request, sql string) {
		io.WriteString(w, oneRowHeader+"[\"1\", \"a\"]\n")
	})
	if _, err := s.Query(context.Background(), "SELECT 1", 10); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if k := f.kills(); len(k) != 0 {
		t.Errorf("a completed query was killed: %v", k)
	}
}

func TestChSchemaAndDatabases(t *testing.T) {
	s, _ := newCH(t, func(w http.ResponseWriter, r *http.Request, sql string) {
		if strings.Contains(sql, "system.columns") {
			io.WriteString(w, "[\"a\",\"b\",\"c\",\"d\",\"e\"]\n[\"String\",\"String\",\"String\",\"String\",\"String\"]\n"+
				"[\"default\",\"t\",\"MergeTree\",\"id\",\"UInt64\"]\n[\"default\",\"t\",\"MergeTree\",\"v\",\"Array(String)\"]\n"+
				"[\"default\",\"mv\",\"MaterializedView\",\"x\",\"UInt8\"]\n")
			return
		}
		io.WriteString(w, "[\"name\",\"equals(name, currentDatabase())\"]\n[\"String\",\"UInt8\"]\n[\"default\",1]\n[\"system\",0]\n")
	})
	tables, err := s.Schema(context.Background())
	if err != nil || len(tables) != 2 || tables[0].Name != "t" || len(tables[0].Columns) != 2 || tables[1].Kind != "materialized view" {
		t.Fatalf("schema = %+v, %v", tables, err)
	}
	dbs, err := s.Databases(context.Background())
	if err != nil || len(dbs) != 2 || !dbs[0].Default || dbs[1].Default || !dbs[1].CanConnect {
		t.Fatalf("databases = %+v, %v", dbs, err)
	}
}

func TestChExplain(t *testing.T) {
	s, f := newCH(t, func(w http.ResponseWriter, r *http.Request, sql string) {
		io.WriteString(w, "[\"explain\"]\n[\"String\"]\n[\"Expression ((Project names))\"]\n[\"  ReadFromStorage (SystemNumbers)\"]\n")
	})
	res, err := s.Explain(context.Background(), "SELECT 1;", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Lines) != 2 || res.Lines[1].Depth != 1 || !strings.HasPrefix(f.all()[0].SQL, "EXPLAIN PLAN") {
		t.Errorf("explain = %+v, sql %q", res, f.all()[0].SQL)
	}
}

func TestChOpenUsesProfile(t *testing.T) {
	s, err := openClickHouse(context.Background(), Connection{Driver: ClickHouse, Host: "h", Port: 8443, TLS: true, User: "u", Database: "logs", ReadOnly: true}, "pw")
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	if _, err := openClickHouse(context.Background(), Connection{Driver: ClickHouse, Host: "", Port: 8123}, ""); err == nil {
		t.Error("an empty host was accepted")
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}
