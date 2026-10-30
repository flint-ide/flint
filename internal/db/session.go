package db

import (
	"context"
	"fmt"
	"strings"
	"unicode"
)

// DatabaseInfo is one database of a server, as listed in the Databases tree node.
type DatabaseInfo struct {
	Name string `json:"name"`
	// CanConnect is false when the user lacks the CONNECT privilege (or the database
	// refuses connections); such databases are listed but can't be opened.
	CanConnect bool `json:"canConnect"`
	// Default marks the database the connection opens by default (the one listing it).
	Default bool `json:"default"`
}

// session is an open connection pool to one database.
type session interface {
	Query(ctx context.Context, sql string, limit int) (*QueryResult, error)
	// Schema introspects the tables of the session's database.
	Schema(ctx context.Context) ([]Table, error)
	// Databases lists every database on the server.
	Databases(ctx context.Context) ([]DatabaseInfo, error)
	Ping(ctx context.Context) error
	Close()
}

func openSession(ctx context.Context, c Connection, password string) (session, error) {
	switch c.Driver {
	case Postgres:
		return openPostgres(ctx, c, password)
	case ClickHouse:
		return openClickHouse(ctx, c, password)
	}
	return nil, fmt.Errorf("unsupported driver %q", c.Driver)
}

// readKeywords start statements that only read data and return rows.
var readKeywords = map[string]bool{
	"SELECT": true, "WITH": true, "SHOW": true, "DESCRIBE": true, "DESC": true,
	"EXPLAIN": true, "EXISTS": true, "VALUES": true, "TABLE": true,
}

// firstKeyword returns the upper-cased first word of a statement, skipping
// whitespace, comments and opening parentheses.
func firstKeyword(sql string) string {
	s := sql
	for {
		s = strings.TrimLeftFunc(s, func(r rune) bool { return unicode.IsSpace(r) || r == '(' })
		switch {
		case strings.HasPrefix(s, "--"):
			if i := strings.IndexByte(s, '\n'); i >= 0 {
				s = s[i+1:]
				continue
			}
			return ""
		case strings.HasPrefix(s, "/*"):
			if i := strings.Index(s, "*/"); i >= 0 {
				s = s[i+2:]
				continue
			}
			return ""
		}
		break
	}
	end := strings.IndexFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && r != '_' })
	if end < 0 {
		end = len(s)
	}
	return strings.ToUpper(s[:end])
}

// isReadStatement reports whether the statement is expected to only read data.
func isReadStatement(sql string) bool {
	return readKeywords[firstKeyword(sql)]
}

// schemaRow is one column of an introspection query, flattened.
type schemaRow struct {
	schema, table, kind, column, colType string
}

// groupTables folds column rows (ordered by schema, table) into tables.
func groupTables(rows []schemaRow) []Table {
	var tables []Table
	for _, r := range rows {
		n := len(tables)
		if n == 0 || tables[n-1].Schema != r.schema || tables[n-1].Name != r.table {
			tables = append(tables, Table{Schema: r.schema, Name: r.table, Kind: r.kind})
			n++
		}
		tables[n-1].Columns = append(tables[n-1].Columns, Column{Name: r.column, Type: r.colType})
	}
	return tables
}
