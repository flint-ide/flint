package db

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type pgSession struct {
	pool *pgxpool.Pool
}

func postgresDSN(c Connection, password string) string {
	sslmode := "prefer"
	switch {
	case c.TLS && c.TLSInsecure:
		sslmode = "require"
	case c.TLS:
		sslmode = "verify-full"
	}
	q := url.Values{}
	q.Set("sslmode", sslmode)
	q.Set("application_name", "flint")
	q.Set("connect_timeout", "10")
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(c.User, password),
		Host:     net.JoinHostPort(c.Host, strconv.Itoa(c.Port)),
		Path:     "/" + c.DefaultDatabase(),
		RawQuery: q.Encode(),
	}
	return u.String()
}

func openPostgres(ctx context.Context, c Connection, password string) (*pgSession, error) {
	cfg, err := pgxpool.ParseConfig(postgresDSN(c, password))
	if err != nil {
		return nil, fmt.Errorf("postgres config: %w", err)
	}
	cfg.MaxConns = 4
	cfg.MaxConnIdleTime = 5 * time.Minute
	// Simple protocol: no prepared-statement cache pollution from ad-hoc scratch queries.
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	if c.ReadOnly {
		cfg.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("postgres connect: %w", err)
	}
	return &pgSession{pool: pool}, nil
}

func (s *pgSession) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

func (s *pgSession) Close() { s.pool.Close() }

func (s *pgSession) Query(ctx context.Context, sql string, limit int) (*QueryResult, error) {
	// A child context lets us abort the server-side query once we have enough rows,
	// instead of draining a huge result set on Close.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	start := time.Now()
	rows, err := s.pool.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	res := &QueryResult{Rows: [][]any{}, Columns: []Column{}}
	typeMap := rows.Conn().TypeMap()
	fields := rows.FieldDescriptions()
	oids, attnums := make([]uint32, len(fields)), make([]uint16, len(fields))
	for i, fd := range fields {
		oids[i], attnums[i] = fd.TableOID, fd.TableAttributeNumber
	}
	if len(fields) > 0 {
		res.TableOID, res.Attnums, res.EditBlocked = resultSource(oids, attnums)
	}
	for _, fd := range fields {
		typeName := "oid:" + strconv.FormatUint(uint64(fd.DataTypeOID), 10)
		if t, ok := typeMap.TypeForOID(fd.DataTypeOID); ok {
			typeName = t.Name
		}
		res.Columns = append(res.Columns, Column{Name: fd.Name, Type: typeName})
	}

	for rows.Next() {
		if len(res.Rows) >= limit {
			res.Truncated = true
			cancel()
			break
		}
		vals, err := rows.Values()
		if err != nil {
			return nil, err
		}
		row := make([]any, len(vals))
		for i, v := range vals {
			row[i] = normalize(v)
		}
		res.Rows = append(res.Rows, row)
	}
	rows.Close()
	if err := rows.Err(); err != nil && !res.Truncated {
		return nil, err
	}

	tag := rows.CommandTag()
	res.Command = tag.String()
	if !tag.Select() {
		res.Affected = tag.RowsAffected()
	}
	res.ElapsedMs = time.Since(start).Milliseconds()
	return res, nil
}

const pgSchemaSQL = `
SELECT n.nspname, c.relname,
       CASE c.relkind WHEN 'v' THEN 'view' WHEN 'm' THEN 'materialized view'
                      WHEN 'f' THEN 'foreign table' ELSE 'table' END,
       a.attname, format_type(a.atttypid, a.atttypmod)
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped
WHERE c.relkind IN ('r', 'v', 'm', 'f', 'p')
  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
  AND n.nspname NOT LIKE 'pg_toast%'
  AND n.nspname NOT LIKE 'pg_temp%'
ORDER BY n.nspname, c.relname, a.attnum`

func (s *pgSession) Schema(ctx context.Context) ([]Table, error) {
	rows, err := s.pool.Query(ctx, pgSchemaSQL)
	if err != nil {
		return nil, fmt.Errorf("postgres schema: %w", err)
	}
	collected, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (schemaRow, error) {
		var sr schemaRow
		err := r.Scan(&sr.schema, &sr.table, &sr.kind, &sr.column, &sr.colType)
		return sr, err
	})
	if err != nil {
		return nil, fmt.Errorf("postgres schema: %w", err)
	}
	return groupTables(collected), nil
}

// pgDatabasesSQL lists the cluster's databases like pgAdmin does: templates hidden, and
// databases the user may not CONNECT to (or that refuse connections) marked as such.
const pgDatabasesSQL = `
SELECT datname, datallowconn AND has_database_privilege(oid, 'CONNECT'), datname = current_database()
FROM pg_database
WHERE NOT datistemplate
ORDER BY datname`

func (s *pgSession) Databases(ctx context.Context) ([]DatabaseInfo, error) {
	rows, err := s.pool.Query(ctx, pgDatabasesSQL)
	if err != nil {
		return nil, fmt.Errorf("postgres databases: %w", err)
	}
	dbs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (DatabaseInfo, error) {
		var d DatabaseInfo
		err := r.Scan(&d.Name, &d.CanConnect, &d.Default)
		return d, err
	})
	if err != nil {
		return nil, fmt.Errorf("postgres databases: %w", err)
	}
	return dbs, nil
}
