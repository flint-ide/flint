package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Queries run for someone other than the user (an agent's run_sql_query
// tool): only on a connection in read-only mode, one reading statement at a
// time, with a bounded result.

// Bounds of such a query and of its rendered result.
const (
	MaxReadOnlySQL   = 64 << 10 // bytes of the statement
	MaxReadOnlyRows  = 500
	DefaultReadRows  = 100
	maxRenderedCell  = 200 // bytes of one value
	maxRenderedBytes = 48 << 10
)

var databaseName = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.$-]{0,127}$`)

// ReadOnlyConnection finds the saved connection called name (its name, else
// its id) and requires it to be in read-only mode.
func (m *Manager) ReadOnlyConnection(name string) (Connection, error) {
	if name == "" {
		return Connection{}, errors.New("connection is required")
	}
	var found []Connection
	for _, c := range m.store.List() {
		if c.Name == name {
			found = append(found, c)
		}
	}
	if len(found) == 0 {
		if c, ok := m.store.Get(name); ok {
			found = []Connection{c}
		}
	}
	switch {
	case len(found) == 0:
		var names []string
		for _, c := range m.store.List() {
			if c.ReadOnly {
				names = append(names, c.Name)
			}
		}
		list := "none"
		if len(names) > 0 {
			list = strings.Join(names[:min(len(names), 40)], ", ")
		}
		return Connection{}, fmt.Errorf("no saved database connection named %q (read-only connections: %s)", cutText(name, 200), list)
	case len(found) > 1:
		return Connection{}, fmt.Errorf("%d connections are named %q; rename one in Flint's Databases panel", len(found), name)
	}
	return requireReadOnly(found[0])
}

func requireReadOnly(c Connection) (Connection, error) {
	if !c.ReadOnly {
		return Connection{}, fmt.Errorf("the connection %q is not read-only: agents may only query connections in read-only mode. "+
			"The user can turn on \"Read-only\" for it in Flint's Databases panel (Edit Connection)", c.Name)
	}
	return c, nil
}

// CheckReadStatement requires sql to be one statement that only reads
// (SELECT, WITH, SHOW, DESCRIBE, EXPLAIN, VALUES, TABLE). A semicolon
// anywhere but at the end is refused, also inside a string: a second
// statement could switch the session out of read-only mode.
func CheckReadStatement(sql string) error {
	trimmed := strings.TrimRight(strings.TrimSpace(sql), "; \t\r\n")
	switch {
	case trimmed == "":
		return errors.New("sql is required")
	case len(sql) > MaxReadOnlySQL:
		return fmt.Errorf("the statement is longer than %d KiB", MaxReadOnlySQL>>10)
	case !utf8.ValidString(sql) || strings.ContainsRune(sql, 0):
		return errors.New("the statement is not valid text")
	case strings.Contains(trimmed, ";"):
		return errors.New("only one statement per call, without a semicolon inside it")
	case !isReadStatement(trimmed):
		return fmt.Errorf("only reading statements are allowed (SELECT, WITH, SHOW, DESCRIBE, EXPLAIN, VALUES, TABLE), not %s",
			orText(firstKeyword(trimmed), "this one"))
	case changesSessionMode(trimmed):
		return errors.New("statements that call set_config or name the read-only settings are not allowed: they could switch the session out of read-only mode")
	}
	return nil
}

// changesSessionMode reports a reading statement that could still turn off
// read-only mode for the statements after it: SELECT set_config(...) changes
// default_transaction_read_only for the whole pooled session. The name can
// also be spelled with Unicode escapes (U&"\0073et_config"), so those
// identifiers are refused as well.
func changesSessionMode(sql string) bool {
	lower := strings.ToLower(sql)
	for _, word := range []string{"set_config", "transaction_read_only", `u&"`} {
		if strings.Contains(lower, word) {
			return true
		}
	}
	return false
}

// CheckDatabaseName accepts "" (the profile's default) or a plain name.
func CheckDatabaseName(name string) error {
	if name != "" && !databaseName.MatchString(name) {
		return errors.New("database must be a plain name (letters, digits, _ . $ -)")
	}
	return nil
}

// QueryReadOnly runs one reading statement on the read-only connection
// connID, recorded in its query history like a console run. The profile is
// read again here: it may have changed since the caller looked it up.
func (m *Manager) QueryReadOnly(ctx context.Context, connID, database, queryID, sql string, limit int) (*QueryResult, error) {
	conn, ok := m.store.Get(connID)
	if !ok {
		return nil, fmt.Errorf("unknown connection %q", connID)
	}
	if _, err := requireReadOnly(conn); err != nil {
		return nil, err
	}
	if err := CheckDatabaseName(database); err != nil {
		return nil, err
	}
	if err := CheckReadStatement(sql); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = DefaultReadRows
	}
	return m.Query(ctx, connID, database, queryID, sql, min(limit, MaxReadOnlyRows))
}

// RenderResult shows a result as tab-separated text, bounded: a header line,
// one line per row, values cut and with tabs and line breaks escaped.
func RenderResult(res *QueryResult) string {
	if res == nil {
		return "No result."
	}
	var b strings.Builder
	if len(res.Columns) == 0 {
		fmt.Fprintf(&b, "%s (%d rows affected, %d ms)", orText(res.Command, "Done"), res.Affected, res.ElapsedMs)
		return b.String()
	}
	fmt.Fprintf(&b, "%d rows (%d ms)", len(res.Rows), res.ElapsedMs)
	if res.Truncated {
		b.WriteString("; more rows exist, add a LIMIT or a WHERE to see others")
	}
	b.WriteByte('\n')
	names := make([]string, len(res.Columns))
	for i, c := range res.Columns {
		names[i] = renderCell(c.Name)
	}
	b.WriteString(strings.Join(names, "\t"))
	b.WriteByte('\n')
	for i, row := range res.Rows {
		if b.Len() > maxRenderedBytes {
			fmt.Fprintf(&b, "… %d more rows not shown (answer too long)\n", len(res.Rows)-i)
			break
		}
		cells := make([]string, len(row))
		for j, v := range row {
			cells[j] = renderCell(v)
		}
		b.WriteString(strings.Join(cells, "\t"))
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

var cellEscapes = strings.NewReplacer("\\", `\\`, "\t", `\t`, "\n", `\n`, "\r", `\r`)

func renderCell(v any) string {
	var s string
	switch x := v.(type) {
	case nil:
		return "NULL"
	case string:
		s = x
	default:
		if data, err := json.Marshal(x); err == nil {
			s = string(data)
		} else {
			s = fmt.Sprint(x)
		}
	}
	if len(s) > maxRenderedCell {
		s = cutText(s, maxRenderedCell) + "…"
	}
	return cellEscapes.Replace(s)
}

// cutText cuts s at n bytes without splitting a character.
func cutText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func orText(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
