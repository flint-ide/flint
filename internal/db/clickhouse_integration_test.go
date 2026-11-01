//go:build integration

package db

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Exercises the HTTP client against a real ClickHouse (FLINT_TEST_CH_HTTP, `make test-db-up`).

func chManager(t *testing.T, c Connection) (*Manager, Connection) {
	t.Helper()
	c.Driver, c.User = ClickHouse, "flint"
	return integrationManager(t, "FLINT_TEST_CH_HTTP", c)
}

func chQuery(t *testing.T, m *Manager, ctx context.Context, conn Connection, queryID, sql string) (*QueryResult, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return m.Query(ctx, conn.ID, "", queryID, sql, 1000)
}

func TestClickHouseExactValues(t *testing.T) {
	m, conn := chManager(t, Connection{})
	res, err := chQuery(t, m, context.Background(), conn, "", `SELECT
		toUInt64(18446744073709551615) AS u64, toInt64(-9223372036854775808) AS i64, toUInt64(42) AS small,
		toInt128('-170141183460469231731687303715884105728') AS i128,
		toDecimal64('12345678901234.5', 2) AS dec, toDecimal128('1.10', 4) AS dec2,
		CAST(NULL AS Nullable(UInt64)) AS nul, toNullable(toUInt64(9007199254740993)) AS notnull,
		[toUInt64(1), 18446744073709551615] AS arr, map('k', toDecimal32('0.5', 1)) AS mp,
		tuple(toUInt64(1), 'x') AS tup, CAST((1, 'y') AS Tuple(a UInt8, b String)) AS named,
		toDateTime64('2024-01-02 03:04:05.123456', 6, 'Europe/Warsaw') AS dt64,
		toDateTime('2024-07-01 12:00:00', 'UTC') AS dt, toDate('2024-02-29') AS d,
		nan AS n, inf AS i, 1.5 AS f, true AS b, 'zażółć 😀' AS s`)
	if err != nil {
		t.Fatal(err)
	}
	row := res.Rows[0]
	want := map[string]any{
		"u64": "18446744073709551615", "i64": "-9223372036854775808", "small": int64(42),
		"i128": "-170141183460469231731687303715884105728",
		"dec":  "12345678901234.50", "dec2": "1.1000",
		"nul": nil, "notnull": "9007199254740993",
		"dt64": "2024-01-02 03:04:05.123456 +01:00", "dt": "2024-07-01 12:00:00 Z", "d": "2024-02-29",
		"n": "nan", "i": "inf", "f": 1.5, "s": "zażółć 😀",
	}
	for i, c := range res.Columns {
		if w, ok := want[c.Name]; ok && row[i] != w {
			t.Errorf("%s (%s) = %#v, want %#v", c.Name, c.Type, row[i], w)
		}
	}
	byName := map[string]any{}
	for i, c := range res.Columns {
		byName[c.Name] = row[i]
	}
	if arr, _ := byName["arr"].([]any); len(arr) != 2 || arr[0] != int64(1) || arr[1] != "18446744073709551615" {
		t.Errorf("arr = %#v", byName["arr"])
	}
	if mp, _ := byName["mp"].(map[string]any); mp["k"] != "0.5" {
		t.Errorf("map = %#v", byName["mp"])
	}
	if tup, _ := byName["tup"].([]any); len(tup) != 2 || tup[0] != int64(1) || tup[1] != "x" {
		t.Errorf("tuple = %#v", byName["tup"])
	}
	if named, _ := byName["named"].(map[string]any); named["a"] != int64(1) || named["b"] != "y" {
		t.Errorf("named tuple = %#v", byName["named"])
	}
}

func TestClickHouseConsoleSessionKeepsState(t *testing.T) {
	m, conn := chManager(t, Connection{})
	a := WithConsole(context.Background(), fmt.Sprintf("it-a-%d", time.Now().UnixNano()))
	b := WithConsole(context.Background(), fmt.Sprintf("it-b-%d", time.Now().UnixNano()))
	if _, err := chQuery(t, m, a, conn, "", "SET max_result_rows = 77"); err != nil {
		t.Fatal(err)
	}
	if _, err := chQuery(t, m, a, conn, "", "CREATE TEMPORARY TABLE flint_tmp (x UInt8)"); err != nil {
		t.Fatal(err)
	}
	if _, err := chQuery(t, m, a, conn, "", "INSERT INTO flint_tmp VALUES (1), (2)"); err != nil {
		t.Fatal(err)
	}
	res, err := chQuery(t, m, a, conn, "", "SELECT count(), (SELECT getSetting('max_result_rows')) FROM flint_tmp")
	if err != nil || res.Rows[0][0] != "2" && res.Rows[0][0] != int64(2) || res.Rows[0][1] != "77" && res.Rows[0][1] != int64(77) {
		t.Fatalf("console a: %+v, %v", res, err)
	}
	if _, err := chQuery(t, m, b, conn, "", "SELECT * FROM flint_tmp"); err == nil || !strings.Contains(err.Error(), "flint_tmp") {
		t.Errorf("another console sees the temporary table: %v", err)
	}
	if _, err := chQuery(t, m, context.Background(), conn, "", "SELECT * FROM flint_tmp"); err == nil {
		t.Error("a statement outside any console sees the temporary table")
	}
}

func TestClickHouseCancelKillsServerSide(t *testing.T) {
	m, conn := chManager(t, Connection{})
	const id = "flint-it-cancel"
	done := make(chan error, 1)
	go func() {
		_, err := chQuery(t, m, context.Background(), conn, id, "SELECT sleepEachRow(1) FROM numbers(60) SETTINGS max_block_size = 1")
		done <- err
	}()
	running := func() int64 {
		res, err := chQuery(t, m, context.Background(), conn, "", fmt.Sprintf("SELECT count() FROM system.processes WHERE query_id = '%s'", id))
		if err != nil {
			t.Fatal(err)
		}
		n, _ := res.Rows[0][0].(int64)
		return n
	}
	waitFor(t, func() bool { return running() == 1 })
	m.Cancel(id)
	select {
	case err := <-done:
		if err == nil || err.Error() != "query cancelled" {
			t.Errorf("err = %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Query did not return after Cancel")
	}
	waitFor(t, func() bool { return running() == 0 })

	// The console can run its next statement right away (the killed one may still hold the session).
	a := WithConsole(context.Background(), fmt.Sprintf("it-c-%d", time.Now().UnixNano()))
	go func() {
		_, err := chQuery(t, m, a, conn, id+"-2", "SELECT sleepEachRow(1) FROM numbers(60) SETTINGS max_block_size = 1")
		done <- err
	}()
	time.Sleep(500 * time.Millisecond)
	m.Cancel(id + "-2")
	<-done
	if res, err := chQuery(t, m, a, conn, "", "SELECT 1"); err != nil || len(res.Rows) != 1 {
		t.Errorf("statement after cancel in the same console: %v", err)
	}
}

func TestClickHouseLimitStopsTheServerQuery(t *testing.T) {
	m, conn := chManager(t, Connection{})
	res, err := m.Query(context.Background(), conn.ID, "", "flint-it-limit", "SELECT number, sleepEachRow(0.01) FROM system.numbers SETTINGS max_block_size = 10", 50)
	if err != nil || len(res.Rows) != 50 || !res.Truncated {
		t.Fatalf("rows %v err %v", res, err)
	}
	waitFor(t, func() bool {
		r, err := chQuery(t, m, context.Background(), conn, "", "SELECT count() FROM system.processes WHERE query_id = 'flint-it-limit'")
		return err == nil && r.Rows[0][0] == int64(0)
	})
}

func TestClickHouseErrors(t *testing.T) {
	m, conn := chManager(t, Connection{})
	if _, err := chQuery(t, m, context.Background(), conn, "", "SELEC 1"); err == nil || !strings.HasPrefix(err.Error(), "code: 62, message: Syntax error") {
		t.Errorf("syntax error = %v", err)
	}
	if _, err := chQuery(t, m, context.Background(), conn, "", "SELECT * FROM flint_missing_table"); err == nil || !strings.HasPrefix(err.Error(), "code: 60,") {
		t.Errorf("unknown table = %v", err)
	}

	// An error after rows were already streamed.
	_, err := m.Query(context.Background(), conn.ID, "", "", "SELECT throwIf(number = 50000, 'boom') FROM numbers(200000) SETTINGS max_block_size = 1000", 100000)
	if err == nil || !strings.HasPrefix(err.Error(), "code: 395,") || !strings.Contains(err.Error(), "boom") {
		t.Errorf("mid-stream error = %v", err)
	}

	// The connection survives it.
	if res, err := chQuery(t, m, context.Background(), conn, "", "SELECT 1"); err != nil || len(res.Rows) != 1 {
		t.Errorf("after errors: %v", err)
	}

	wrong := conn
	if err := m.Test(context.Background(), wrong, ptr("wrong-password")); err == nil || !strings.HasPrefix(err.Error(), "code: 516,") {
		t.Errorf("bad password = %v", err)
	}
}

func TestClickHouseReadOnlyIsEnforcedByTheServer(t *testing.T) {
	m, conn := chManager(t, Connection{ReadOnly: true})
	if _, err := chQuery(t, m, context.Background(), conn, "", "SELECT 1"); err != nil {
		t.Fatal(err)
	}
	// Flint's own guard refuses writes before they are sent; the server setting covers what
	// the guard can't see, such as a write hidden in a SELECT.
	if _, err := chQuery(t, m, context.Background(), conn, "", "CREATE TABLE flint_ro (x UInt8) ENGINE = Memory"); err != ErrReadOnly {
		t.Errorf("guard: %v", err)
	}
	_, err := chQuery(t, m, context.Background(), conn, "", "SELECT * FROM url('http://127.0.0.1:1/x', 'CSV', 'a UInt8') SETTINGS readonly = 0")
	if err == nil {
		t.Error("a statement changed the readonly setting")
	}
	if _, err := chQuery(t, m, context.Background(), conn, "", "SELECT sleep(0)"); err != nil {
		t.Errorf("read-only select: %v", err)
	}
}
