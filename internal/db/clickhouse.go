package db

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"flint/internal/clickhouse"
)

// chSession is a ClickHouse connection over the HTTP interface (internal/clickhouse).
// There is no pool: each statement is one request, and idle connections are reused.
type chSession struct {
	client *clickhouse.Client
}

func openClickHouse(_ context.Context, c Connection, password string) (*chSession, error) {
	client, err := clickhouse.New(clickhouse.Config{
		Host:     c.Host,
		Port:     c.Port,
		TLS:      c.TLS,
		Insecure: c.TLSInsecure,
		User:     c.User,
		Password: password,
		Database: c.DefaultDatabase(), // "" = the user's default_database
		ReadOnly: c.ReadOnly,
	})
	if err != nil {
		return nil, fmt.Errorf("clickhouse config: %w", err)
	}
	return &chSession{client: client}, nil
}

type ctxKey int

const (
	queryIDKey ctxKey = iota
	consoleKey
)

// withQueryID names the server-side run, so Cancel's KILL QUERY can find it.
func withQueryID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, queryIDKey, id)
}

// WithConsole marks statements as belonging to one SQL console. ClickHouse keeps SET and
// temporary tables per session, so a console's statements share a session id.
func WithConsole(ctx context.Context, consoleID string) context.Context {
	return context.WithValue(ctx, consoleKey, consoleID)
}

func ctxString(ctx context.Context, key ctxKey) string {
	s, _ := ctx.Value(key).(string)
	return s
}

func (s *chSession) Ping(ctx context.Context) error { return s.client.Ping(ctx) }

func (s *chSession) Close() { s.client.Close() }

// Query runs one statement in the console's session (if the context names one).
func (s *chSession) Query(ctx context.Context, sql string, limit int) (*QueryResult, error) {
	var session string
	if console := ctxString(ctx, consoleKey); console != "" {
		session = "flint-" + console
	}
	return s.run(ctx, sql, limit, session)
}

// run streams the statement's rows into a QueryResult of at most limit rows. Cancelling
// ctx (the user's Cancel, the row limit, a timeout) closes the request and kills the
// statement on the server.
func (s *chSession) run(ctx context.Context, sql string, limit int, session string) (*QueryResult, error) {
	start := time.Now()
	queryID := ctxString(ctx, queryIDKey)
	if queryID == "" {
		queryID = clickhouse.NewQueryID()
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	// Registered before the request goes out: Cancel may arrive while the server is still
	// computing and hasn't sent a byte. stop is called on every path that isn't a cancel.
	stop := context.AfterFunc(ctx, func() { _ = s.client.Kill(queryID) })

	rows, err := s.client.Query(ctx, clickhouse.Request{SQL: sql, QueryID: queryID, SessionID: session})
	if err != nil {
		stop()
		return nil, err
	}
	defer rows.Close()

	cols := rows.Columns()
	res := &QueryResult{Rows: [][]any{}, Columns: make([]Column, len(cols))}
	for i, c := range cols {
		res.Columns[i] = Column{Name: c.Name, Type: c.Type}
	}
	for rows.Next() {
		if len(res.Rows) >= limit {
			res.Truncated = true
			cancel() // stops the transfer and kills the rest of the statement
			break
		}
		res.Rows = append(res.Rows, rows.Values())
	}
	if err := rows.Err(); err != nil && !res.Truncated {
		stop()
		return nil, err
	}
	stop()
	if len(cols) == 0 {
		res.Command = "OK"
		res.Affected = rows.Written()
	} else {
		res.Command = fmt.Sprintf("SELECT %d", len(res.Rows))
	}
	res.ElapsedMs = time.Since(start).Milliseconds()
	return res, nil
}

// introspect runs a metadata query outside any console session and hands each row to scan.
func (s *chSession) introspect(ctx context.Context, sql string, scan func(row []any)) error {
	res, err := s.run(ctx, sql, math.MaxInt, "")
	if err != nil {
		return err
	}
	for _, row := range res.Rows {
		scan(row)
	}
	return nil
}

func cell(row []any, i int) string {
	if i >= len(row) {
		return ""
	}
	if s, ok := row[i].(string); ok {
		return s
	}
	return fmt.Sprint(row[i])
}

// truthy reads a boolean cell: ClickHouse comparisons are UInt8, Bool columns are JSON booleans.
func truthy(row []any, i int) bool {
	if i >= len(row) {
		return false
	}
	switch v := row[i].(type) {
	case bool:
		return v
	case int64:
		return v != 0
	}
	return false
}

// Explain returns EXPLAIN PLAN (or, with pipeline, EXPLAIN PIPELINE) of sql as indented
// lines. Neither executes the query.
func (s *chSession) Explain(ctx context.Context, sql string, pipeline bool) (*ExplainResult, error) {
	target, err := explainTarget(sql)
	if err != nil {
		return nil, err
	}
	stmt := clickhouseExplainSQL(target, pipeline)
	start := time.Now()
	var lines []string
	if err := s.introspect(ctx, stmt, func(row []any) { lines = append(lines, cell(row, 0)) }); err != nil {
		return nil, err
	}
	return &ExplainResult{
		Driver:    ClickHouse,
		Analyze:   pipeline,
		Statement: stmt,
		Raw:       strings.Join(lines, "\n"),
		Lines:     ParseTextPlan(lines),
		ElapsedMs: time.Since(start).Milliseconds(),
	}, nil
}

// chSchemaSQL introspects the session's database only; other databases are listed by
// Databases and opened as sessions of their own, like Postgres databases.
const chSchemaSQL = `
SELECT c.database, c.table, t.engine, c.name, c.type
FROM system.columns AS c
INNER JOIN system.tables AS t ON t.database = c.database AND t.name = c.table
WHERE c.database = currentDatabase()
ORDER BY c.table, c.position`

func (s *chSession) Schema(ctx context.Context) ([]Table, error) {
	var collected []schemaRow
	err := s.introspect(ctx, chSchemaSQL, func(row []any) {
		collected = append(collected, schemaRow{
			schema: cell(row, 0), table: cell(row, 1), kind: clickhouseKind(cell(row, 2)),
			column: cell(row, 3), colType: cell(row, 4),
		})
	})
	if err != nil {
		return nil, fmt.Errorf("clickhouse schema: %w", err)
	}
	return groupTables(collected), nil
}

// chDatabasesSQL is SHOW DATABASES; system.databases already hides what the user can't see.
const chDatabasesSQL = `SELECT name, name = currentDatabase() FROM system.databases ORDER BY name`

func (s *chSession) Databases(ctx context.Context) ([]DatabaseInfo, error) {
	dbs := []DatabaseInfo{}
	err := s.introspect(ctx, chDatabasesSQL, func(row []any) {
		dbs = append(dbs, DatabaseInfo{Name: cell(row, 0), CanConnect: true, Default: truthy(row, 1)})
	})
	if err != nil {
		return nil, fmt.Errorf("clickhouse databases: %w", err)
	}
	return dbs, nil
}

func clickhouseKind(engine string) string {
	switch {
	case engine == "MaterializedView":
		return "materialized view"
	case strings.HasSuffix(engine, "View"):
		return "view"
	case engine == "Dictionary":
		return "dictionary"
	}
	return "table"
}
