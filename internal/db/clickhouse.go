package db

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

type chSession struct {
	conn driver.Conn
}

func openClickHouse(_ context.Context, c Connection, password string) (*chSession, error) {
	opts := &clickhouse.Options{
		Protocol: clickhouseProtocol(c.Protocol),
		Addr:     []string{net.JoinHostPort(c.Host, strconv.Itoa(c.Port))},
		Auth: clickhouse.Auth{
			Database: c.DefaultDatabase(), // "" = the user's default_database
			Username: c.User,
			Password: password,
		},
		DialTimeout:     10 * time.Second,
		MaxOpenConns:    4,
		MaxIdleConns:    2,
		ConnMaxLifetime: 10 * time.Minute,
		ClientInfo: clickhouse.ClientInfo{
			Products: []struct{ Name, Version string }{{Name: "flint", Version: "0.1"}},
		},
	}
	if c.Protocol == ProtocolHTTP {
		// Over HTTP a cancelled context only closes the request; without this setting the
		// server keeps running the query after Cancel or the row limit.
		opts.Settings = clickhouse.Settings{"cancel_http_readonly_queries_on_client_close": 1}
	}
	if c.TLS {
		opts.TLS = &tls.Config{ServerName: c.Host, MinVersion: tls.VersionTLS12}
	}
	conn, err := clickhouse.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("clickhouse config: %w", err)
	}
	return &chSession{conn: conn}, nil
}

func clickhouseProtocol(p string) clickhouse.Protocol {
	if p == ProtocolHTTP {
		return clickhouse.HTTP
	}
	return clickhouse.Native
}

func (s *chSession) Ping(ctx context.Context) error { return s.conn.Ping(ctx) }

func (s *chSession) Close() { _ = s.conn.Close() }

func (s *chSession) Query(ctx context.Context, sql string, limit int) (*QueryResult, error) {
	start := time.Now()
	if !isReadStatement(sql) {
		if err := s.conn.Exec(ctx, sql); err != nil {
			return nil, err
		}
		return &QueryResult{Rows: [][]any{}, Columns: []Column{}, Command: "OK", ElapsedMs: time.Since(start).Milliseconds()}, nil
	}

	// Cancelling aborts the server-side query once the row limit is reached.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	rows, err := s.conn.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	colTypes := rows.ColumnTypes()
	res := &QueryResult{Rows: [][]any{}, Columns: make([]Column, len(colTypes))}
	for i, ct := range colTypes {
		res.Columns[i] = Column{Name: ct.Name(), Type: ct.DatabaseTypeName()}
	}

	for rows.Next() {
		if len(res.Rows) >= limit {
			res.Truncated = true
			cancel()
			break
		}
		dest := make([]any, len(colTypes))
		for i, ct := range colTypes {
			dest[i] = reflect.New(ct.ScanType()).Interface()
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		row := make([]any, len(dest))
		for i, d := range dest {
			row[i] = normalize(d) // normalize dereferences the scan pointers
		}
		res.Rows = append(res.Rows, row)
	}
	if err := rows.Err(); err != nil && !res.Truncated {
		return nil, err
	}
	res.Command = fmt.Sprintf("SELECT %d", len(res.Rows))
	res.ElapsedMs = time.Since(start).Milliseconds()
	return res, nil
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
	rows, err := s.conn.Query(ctx, chSchemaSQL)
	if err != nil {
		return nil, fmt.Errorf("clickhouse schema: %w", err)
	}
	defer rows.Close()

	var collected []schemaRow
	for rows.Next() {
		var r schemaRow
		var engine string
		if err := rows.Scan(&r.schema, &r.table, &engine, &r.column, &r.colType); err != nil {
			return nil, fmt.Errorf("clickhouse schema: %w", err)
		}
		r.kind = clickhouseKind(engine)
		collected = append(collected, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("clickhouse schema: %w", err)
	}
	return groupTables(collected), nil
}

// chDatabasesSQL is SHOW DATABASES; system.databases already hides what the user can't see.
const chDatabasesSQL = `SELECT name, name = currentDatabase() FROM system.databases ORDER BY name`

func (s *chSession) Databases(ctx context.Context) ([]DatabaseInfo, error) {
	rows, err := s.conn.Query(ctx, chDatabasesSQL)
	if err != nil {
		return nil, fmt.Errorf("clickhouse databases: %w", err)
	}
	defer rows.Close()

	dbs := []DatabaseInfo{}
	for rows.Next() {
		var name string
		var isDefault bool
		if err := rows.Scan(&name, &isDefault); err != nil {
			return nil, fmt.Errorf("clickhouse databases: %w", err)
		}
		dbs = append(dbs, DatabaseInfo{Name: name, CanConnect: true, Default: isDefault})
	}
	if err := rows.Err(); err != nil {
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
